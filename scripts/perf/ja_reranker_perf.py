#!/usr/bin/env python3
"""P2-63: latency, memory and soak harness for the Japanese reranker.

Measures the trained ``reranker-public-core.int8.onnx`` on ONNX Runtime CPU
with the same session options the product uses (sequential execution, inter-op
1, optional ``session.intra_op.allow_spinning=0``).  The model is never
committed; pass ``--model`` and ``--manifest`` explicitly.

The input fixture is synthetic because the 8000-group homophone benchmark is
not present on the measurement machine.  It has the product's reranker shape:

    <bos> left(120) <eos> candidate <eos> right(120)   -> seq 244

Token ids come from a fixed PCG64 seed, so every run sees the same 200 groups
of 5 candidates.  Shorter shapes truncate left/right context symmetrically for
the context-length mitigation.

Usage (venv outside the repository, see README next to this file):

    python ja_reranker_perf.py fixture --fixture fixtures/p2-63-groups.npz
    python ja_reranker_perf.py orchestrate --model ... --manifest ... \
        --fixture fixtures/p2-63-groups.npz --out-dir results
    python ja_reranker_perf.py soak --model ... --minutes 30 --period-ms 200 \
        --json results/soak.json --samples results/soak.jsonl
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import statistics
import subprocess
import sys
import threading
import time
from pathlib import Path

os.environ.setdefault("ORT_DISABLE_TELEMETRY", "1")
os.environ.setdefault("ORT_RUNNING_UNIT_TESTS", "1")

FIXTURE_SCHEMA = "0c.p2-63.fixture/1"
RESULT_SCHEMA = "0c.p2-63.perf/1"
BOS_ID = 2
EOS_ID = 3
DEFAULT_GROUPS = 200
DEFAULT_PER_GROUP = 5
DEFAULT_SEQ = 244
DEFAULT_SEED = 20261118
# left + bos + eos + candidate + eos + right = 2 * context + 4
CONTEXT = (DEFAULT_SEQ - 4) // 2


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def context_for_seq(seq: int) -> int:
    if seq < 8 or seq % 2:
        raise ValueError("seq must be even and >= 8")
    return (seq - 4) // 2


def make_fixture(seed: int, groups: int, per_group: int, seq: int):
    """Return (ids, mask) with a fixed seed for the given sequence length."""
    import numpy as np

    context = context_for_seq(seq)
    rng = np.random.default_rng(seed)
    ids = np.empty((groups, per_group, seq), dtype=np.int64)
    for g in range(groups):
        for c in range(per_group):
            left = rng.integers(1000, 8000, context, dtype=np.int64)
            candidate = rng.integers(8000, 24000, dtype=np.int64)
            right = rng.integers(1000, 8000, context, dtype=np.int64)
            row = ids[g, c]
            row[0] = BOS_ID
            row[1 : 1 + context] = left
            row[1 + context] = EOS_ID
            row[2 + context] = candidate
            row[3 + context] = EOS_ID
            row[4 + context :] = right
    mask = np.ones_like(ids)
    return ids, mask


def load_fixture(path: Path, seq: int, seed: int):
    import numpy as np

    if path.is_file():
        with np.load(path) as data:
            ids = data["ids"]
            mask = data["mask"]
        if seq != ids.shape[2]:
            raise ValueError(f"fixture seq {ids.shape[2]} != requested {seq}")
        return ids, mask
    return make_fixture(seed, DEFAULT_GROUPS, DEFAULT_PER_GROUP, seq)


def machine_info() -> dict:
    info = {
        "platform": platform.platform(),
        "machine": platform.machine(),
        "processor": platform.processor(),
        "python": platform.python_version(),
        "cpu_count": os.cpu_count(),
        "affinity": len(os.sched_getaffinity(0)) if hasattr(os, "sched_getaffinity") else None,
    }
    try:
        for line in Path("/proc/cpuinfo").read_text().splitlines():
            if line.startswith("model name"):
                info["cpu_model"] = line.split(":", 1)[1].strip()
                break
    except OSError:
        pass
    try:
        info["load_average"] = list(os.getloadavg())
        info["mem_total_kb"] = next(
            int(line.split()[1])
            for line in Path("/proc/meminfo").read_text().splitlines()
            if line.startswith("MemTotal:")
        )
    except OSError:
        pass
    return info


def rss_kb() -> int:
    try:
        for line in Path("/proc/self/status").read_text().splitlines():
            if line.startswith("VmRSS:"):
                return int(line.split()[1])
    except OSError:
        pass
    return 0


def hwm_kb() -> int:
    try:
        for line in Path("/proc/self/status").read_text().splitlines():
            if line.startswith("VmHWM:"):
                return int(line.split()[1])
    except OSError:
        pass
    return 0


class RssSampler:
    """Sample VmRSS from a daemon thread while inference is running."""

    def __init__(self, interval_s: float = 0.025):
        self.interval_s = interval_s
        self.peak = rss_kb()
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, daemon=True)

    def _run(self):
        while not self._stop.wait(self.interval_s):
            self.peak = max(self.peak, rss_kb())

    def __enter__(self):
        self._thread.start()
        return self

    def __exit__(self, *exc):
        self._stop.set()
        self._thread.join(timeout=2)


def session_options(ort, intra: int, inter: int, spinning: bool, sequential: bool):
    opts = ort.SessionOptions()
    if intra > 0:
        opts.intra_op_num_threads = intra
    opts.inter_op_num_threads = inter
    if sequential:
        opts.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    if not spinning:
        opts.add_session_config_entry("session.intra_op.allow_spinning", "0")
    return opts


def open_session(ort, model: Path, opts):
    import numpy as np  # noqa: F401  (documented dependency)

    providers = ["CPUExecutionProvider"]
    return ort.InferenceSession(str(model), sess_options=opts, providers=providers)


def output_names(session):
    return [o.name for o in session.get_outputs()]


def run_one(session, names, ids, mask):
    return session.run(names, {"input_ids": ids, "attention_mask": mask})[0]


def timed(fn):
    started = time.perf_counter()
    value = fn()
    return (time.perf_counter() - started) * 1000.0, value


def common_header() -> dict:
    return {
        "schema": RESULT_SCHEMA,
        "machine": machine_info(),
        "python": sys.version.split()[0],
    }


# --------------------------------------------------------------------------
# fixture


def cmd_fixture(args) -> int:
    import numpy as np

    ids, mask = make_fixture(args.seed, args.groups, args.per_group, args.seq)
    out = Path(args.fixture)
    out.parent.mkdir(parents=True, exist_ok=True)
    np.savez_compressed(out, ids=ids, mask=mask)
    meta = {
        "schema": FIXTURE_SCHEMA,
        "file": out.name,
        "sha256": sha256(out),
        "seed": args.seed,
        "groups": args.groups,
        "per_group": args.per_group,
        "seq": args.seq,
        "context": context_for_seq(args.seq),
        "layout": "<bos> left(context) <eos> candidate <eos> right(context)",
        "dtype": "int64",
        "id_ranges": {
            "left_right": [1000, 8000],
            "candidate": [8000, 24000],
        },
        "source": "synthetic; homophone-v1 benchmark was not present on the measurement machine",
        "generator": "scripts/perf/ja_reranker_perf.py make_fixture",
    }
    out.with_suffix(".json").write_text(json.dumps(meta, indent=2, ensure_ascii=False) + "\n")
    print(json.dumps(meta, indent=2, ensure_ascii=False))
    return 0


# --------------------------------------------------------------------------
# cold start


def cmd_cold_once(args) -> int:
    script_started = time.perf_counter()
    import numpy as np
    import onnxruntime as ort

    import_ms = (time.perf_counter() - script_started) * 1000.0
    rss_after_imports = rss_kb()
    model = Path(args.model)
    load_started = time.perf_counter()
    opts = session_options(ort, args.threads, 1, False, True)
    session = open_session(ort, model, opts)
    load_ms = (time.perf_counter() - load_started) * 1000.0
    rss_after_load = rss_kb()

    ids, mask = load_fixture(Path(args.fixture), args.seq, args.seed)
    names = output_names(session)
    one_ids = np.ascontiguousarray(ids[0, 0:1])
    one_mask = np.ascontiguousarray(mask[0, 0:1])
    first_ms, scores = timed(lambda: run_one(session, names, one_ids, one_mask))
    ready_ms = (time.perf_counter() - script_started) * 1000.0
    print(json.dumps({
        "import_ms": round(import_ms, 3),
        "session_load_ms": round(load_ms, 3),
        "first_inference_ms": round(first_ms, 3),
        "ready_ms": round(ready_ms, 3),
        "total_child_ms": round((time.perf_counter() - script_started) * 1000.0, 3),
        "scores": [round(float(v), 6) for v in np.asarray(scores).ravel()],
        "onnxruntime": ort.__version__,
        "numpy": np.__version__,
        "providers": session.get_providers(),
        "intra_op": args.threads,
        "rss_after_imports_mb": round(rss_after_imports / 1024, 1),
        "rss_after_load_mb": round(rss_after_load / 1024, 1),
    }))
    return 0


def cmd_cold(args) -> int:
    """Spawn fresh interpreters and time them from the parent."""
    runs = []
    for i in range(args.repeats):
        started = time.perf_counter()
        proc = subprocess.run(
            [sys.executable, str(Path(__file__).resolve()), "cold-once",
             "--model", args.model, "--fixture", args.fixture,
             "--threads", str(args.threads), "--seq", str(args.seq),
             "--seed", str(args.seed)],
            capture_output=True, text=True, check=True,
            env=dict(os.environ, ORT_DISABLE_TELEMETRY="1", ORT_RUNNING_UNIT_TESTS="1"),
        )
        wall_ms = (time.perf_counter() - started) * 1000.0
        child = json.loads(proc.stdout.strip().splitlines()[-1])
        child["process_wall_ms"] = round(wall_ms, 3)
        child["run"] = i
        runs.append(child)

    def median(field):
        return round(statistics.median(r[field] for r in runs), 3)

    result = common_header()
    result.update({
        "mode": "cold",
        "model": model_record(args.model),
        "options": {"intra_op": args.threads, "inter_op": 1, "spinning": False},
        "repeats": args.repeats,
        "runs": runs,
        "summary": {
            "import_ms": median("import_ms"),
            "session_load_ms": median("session_load_ms"),
            "first_inference_ms": median("first_inference_ms"),
            "ready_ms": median("ready_ms"),
            "process_wall_ms": median("process_wall_ms"),
            "rss_after_imports_mb": median("rss_after_imports_mb"),
            "rss_after_load_mb": median("rss_after_load_mb"),
        },
        "cache_note": "model file was already in the OS page cache; cache drop needs root and was not attempted",
    })
    write_json(args.json, result)
    print(json.dumps(result["summary"]))
    return 0


# --------------------------------------------------------------------------
# bench


THREAD_CONFIGS = [
    ("t1", 1, False),
    ("t2", 2, False),
    ("t4", 4, False),
    ("t6", 6, False),
    ("t8", 8, False),
    ("default", 0, False),
    ("t8-spin", 8, True),
]


def percentile(values, p: float) -> float:
    import numpy as np

    return round(float(np.percentile(np.asarray(values, dtype=np.float64), p)), 3)


def describe(values) -> dict:
    import numpy as np

    arr = np.asarray(values, dtype=np.float64)
    return {
        "samples": int(arr.size),
        "min_ms": round(float(arr.min()), 3),
        "median_ms": round(float(np.median(arr)), 3),
        "mean_ms": round(float(arr.mean()), 3),
        "p95_ms": percentile(arr, 95),
        "p99_ms": percentile(arr, 99),
        "max_ms": round(float(arr.max()), 3),
    }


def pass_median(values) -> float:
    return round(statistics.median(values), 3)


def pick_pass(passes: list[list[float]]) -> tuple[int, list[float]]:
    """Pick the pass whose median is closest to the median of pass medians."""
    medians = [statistics.median(p) for p in passes]
    target = statistics.median(medians)
    index = min(range(len(passes)), key=lambda i: abs(medians[i] - target))
    return index, passes[index]


def group_sums(candidate_ms: list[float], per_group: int) -> list[float]:
    return [sum(candidate_ms[i : i + per_group]) for i in range(0, len(candidate_ms), per_group)]


def cmd_bench(args) -> int:
    import numpy as np
    import onnxruntime as ort

    model = Path(args.model)
    if not model.is_file():
        raise SystemExit(f"model not found: {model}")
    expected = args.model_sha256
    actual = sha256(model)
    if expected and actual != expected:
        raise SystemExit(f"model sha256 {actual} != {expected}")

    ids, mask = load_fixture(Path(args.fixture), args.seq, args.seed)
    groups, per_group, seq = ids.shape
    candidates = groups * per_group
    flat_ids = ids.reshape(-1, seq)
    flat_mask = mask.reshape(-1, seq)
    all_ids = [np.ascontiguousarray(flat_ids[i : i + 1]) for i in range(candidates)]
    all_mask = [np.ascontiguousarray(flat_mask[i : i + 1]) for i in range(candidates)]
    group_ids = [np.ascontiguousarray(ids[g]) for g in range(groups)]
    group_mask = [np.ascontiguousarray(mask[g]) for g in range(groups)]
    chunk8 = candidates // 8
    batch8_ids = []
    batch8_mask = []
    if not args.individual_only:
        if candidates % 8:
            raise SystemExit("fixture must hold a multiple of 8 candidates for batch 8")
        batch8_ids = [np.ascontiguousarray(flat_ids[g * 8 : (g + 1) * 8]) for g in range(chunk8)]
        batch8_mask = [np.ascontiguousarray(flat_mask[g * 8 : (g + 1) * 8]) for g in range(chunk8)]

    load_started = time.perf_counter()
    opts = session_options(ort, args.threads, args.inter, not args.allow_spinning, True)
    session = open_session(ort, model, opts)
    load_ms = (time.perf_counter() - load_started) * 1000.0
    names = output_names(session)
    rss_after_load = rss_kb()

    # Warmup: shape-specific kernels, ORT thread pool, memory arenas.
    for _ in range(args.warmup):
        run_one(session, names, all_ids[0], all_mask[0])
    for _ in range(max(1, args.warmup // 2)):
        run_one(session, names, group_ids[0], group_mask[0])
    if batch8_ids:
        for _ in range(max(1, args.warmup // 4)):
            run_one(session, names, batch8_ids[0], batch8_mask[0])

    def batch_stats(batch_ids, batch_mask):
        passes = []
        for _ in range(args.passes):
            samples = []
            for i in range(len(batch_ids)):
                elapsed, _ = timed(lambda i=i: run_one(session, names, batch_ids[i], batch_mask[i]))
                samples.append(elapsed)
            passes.append(samples)
        sel, chosen = pick_pass(passes)
        return sel, chosen, describe(chosen), [pass_median(p) for p in passes]

    passes_candidate: list[list[float]] = []
    passes_group: list[list[float]] = []
    with RssSampler() as sampler:
        for _ in range(args.passes):
            samples = []
            for i in range(candidates):
                elapsed, _ = timed(lambda i=i: run_one(session, names, all_ids[i], all_mask[i]))
                samples.append(elapsed)
            passes_candidate.append(samples)
            passes_group.append(group_sums(samples, per_group))

        selected, candidate_ms = pick_pass(passes_candidate)
        _, group_ms = pick_pass(passes_group)
        if args.passes > 1:
            # Keep the group view aligned with the selected candidate pass.
            group_ms = group_sums(candidate_ms, per_group)

        batch = {}
        sweep = {}
        if not args.individual_only:
            b5_sel, b5_ms, b5, b5_medians = batch_stats(group_ids, group_mask)
            b8_sel, b8_ms, b8, b8_medians = batch_stats(batch8_ids, batch8_mask)
            batch = {
                "5": {
                    "groups": len(b5_ms),
                    "selected_pass": b5_sel,
                    "pass_medians_ms": b5_medians,
                    "per_group": b5,
                    "per_candidate_equivalent_ms": round(b5["median_ms"] / 5, 3),
                },
                "8": {
                    "groups": len(b8_ms),
                    "selected_pass": b8_sel,
                    "pass_medians_ms": b8_medians,
                    "per_group": b8,
                    "per_candidate_equivalent_ms": round(b8["median_ms"] / 8, 3),
                },
            }
            for n in (1, 2, 3, 4, 5, 8):
                if n == 5:
                    chosen, stats, medians, sel = b5_ms, b5, b5_medians, b5_sel
                elif n == 8:
                    chosen, stats, medians, sel = b8_ms, b8, b8_medians, b8_sel
                else:
                    b_ids = [np.ascontiguousarray(ids[g, :n]) for g in range(groups)]
                    b_mask = [np.ascontiguousarray(mask[g, :n]) for g in range(groups)]
                    sel, chosen, stats, medians = batch_stats(b_ids, b_mask)
                sweep[str(n)] = {
                    "batch": n,
                    "groups": len(chosen),
                    "selected_pass": sel,
                    "pass_medians_ms": medians,
                    "per_group": stats,
                    "per_candidate_equivalent_ms": round(stats["median_ms"] / n, 3),
                }
            # speedup uses batch 1 as the reference, not batch 5.
            for n in sweep:
                sweep[n]["speedup_vs_batch1"] = round(
                    sweep["1"]["per_group"]["median_ms"] / sweep[n]["per_group"]["median_ms"], 3
                )

        peak_sampled = sampler.peak
    rss_after = rss_kb()
    result = common_header()
    result.update({
        "mode": "bench",
        "model": model_record(model),
        "ort": {
            "version": ort.__version__,
            "numpy": np.__version__,
            "providers": session.get_providers(),
        },
        "options": {
            "intra_op": args.threads,
            "inter_op": args.inter,
            "spinning": args.allow_spinning,
            "execution_mode": "sequential",
            "config_name": args.config_name,
        },
        "fixture": {
            "path": args.fixture,
            "sha256": sha256(Path(args.fixture)) if Path(args.fixture).is_file() else None,
            "groups": groups,
            "per_group": per_group,
            "seq": seq,
            "candidates": candidates,
        },
        "warmup_runs": args.warmup,
        "passes": args.passes,
        "session_load_ms": round(load_ms, 3),
        "individual": {
            "per_candidate": describe(candidate_ms),
            "per_group": describe(group_ms),
            "selected_pass": selected,
            "pass_medians_ms": [pass_median(p) for p in passes_candidate],
        },
        "individual_only": args.individual_only,
        "batch": batch,
        "batch_sweep": sweep,
        "rss": {
            "after_load_mb": round(rss_after_load / 1024, 1),
            "peak_sampled_mb": round(peak_sampled / 1024, 1),
            "hwm_mb": round(hwm_kb() / 1024, 1),
            "after_scoring_mb": round(rss_after / 1024, 1),
            "delta_peak_mb": round((peak_sampled - rss_after_load) / 1024, 1),
        },
        "duration_s": None,
    })
    write_json(args.json, result)
    print(json.dumps({
        "config": args.config_name,
        "seq": seq,
        "individual_group_median_ms": result["individual"]["per_group"]["median_ms"],
        "batch5_group_median_ms": (result["batch"].get("5") or {}).get("per_group", {}).get("median_ms"),
        "batch8_group_median_ms": (result["batch"].get("8") or {}).get("per_group", {}).get("median_ms"),
    }))
    return 0


# --------------------------------------------------------------------------
# soak


def linear_slope(xs, ys) -> float:
    """Least-squares slope of ys per unit xs."""
    n = len(xs)
    if n < 2:
        return 0.0
    mx = sum(xs) / n
    my = sum(ys) / n
    num = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    den = sum((x - mx) ** 2 for x in xs)
    return num / den if den else 0.0


def cmd_soak(args) -> int:
    import numpy as np
    import onnxruntime as ort

    model = Path(args.model)
    ids, mask = load_fixture(Path(args.fixture), args.seq, args.seed)
    groups, per_group, _ = ids.shape
    opts = session_options(ort, args.threads, 1, not args.allow_spinning, True)
    session = open_session(ort, model, opts)
    names = output_names(session)
    group_ids = [np.ascontiguousarray(ids[g]) for g in range(groups)]
    group_mask = [np.ascontiguousarray(mask[g]) for g in range(groups)]

    for _ in range(args.warmup):
        run_one(session, names, group_ids[0], group_mask[0])

    rss_load = rss_kb()
    deadline = time.perf_counter() + args.minutes * 60.0
    period = args.period_ms / 1000.0
    samples = []
    dropped = 0
    started = time.perf_counter()
    next_tick = started
    index = 0
    sample_path = Path(args.samples) if args.samples else None
    if sample_path:
        sample_path.parent.mkdir(parents=True, exist_ok=True)
        fh = sample_path.open("w")
    else:
        fh = None
    try:
        while time.perf_counter() < deadline:
            now = time.perf_counter()
            if now < next_tick:
                time.sleep(next_tick - now)
            else:
                dropped += 1
            g = index % groups
            group_ids_g = group_ids[g]
            group_mask_g = group_mask[g]
            candidate_ms = []
            group_started = time.perf_counter()
            for c in range(per_group):
                elapsed, _ = timed(lambda c=c: run_one(
                    session, names, group_ids_g[c : c + 1], group_mask_g[c : c + 1]))
                candidate_ms.append(round(elapsed, 3))
            elapsed = (time.perf_counter() - group_started) * 1000.0
            memory = rss_kb()
            record = {
                "i": index,
                "t_s": round(time.perf_counter() - started, 3),
                "group_ms": round(elapsed, 3),
                "candidate_ms": candidate_ms,
                "rss_mb": round(memory / 1024, 1),
            }
            samples.append(record)
            if fh:
                fh.write(json.dumps(record) + "\n")
            index += 1
            next_tick += period
            # If we fell behind by more than one period, resynchronise instead
            # of burning through a backlog of ticks.
            if time.perf_counter() - next_tick > period:
                next_tick = time.perf_counter() + period
                dropped += 1
    finally:
        if fh:
            fh.close()

    latencies = [s["group_ms"] for s in samples]
    candidate_latencies = [ms for s in samples for ms in s["candidate_ms"]]
    memories = [s["rss_mb"] for s in samples]
    n = len(samples)
    window = max(1, n // 10)
    times_h = [s["t_s"] / 3600.0 for s in samples]
    result = common_header()
    result.update({
        "mode": "soak",
        "model": model_record(model),
        "ort": {"version": ort.__version__, "providers": session.get_providers()},
        "options": {"intra_op": args.threads, "inter_op": 1, "spinning": args.allow_spinning},
        "fixture": {"path": args.fixture, "groups": groups, "per_group": per_group, "seq": args.seq},
        "duration_s": round(time.perf_counter() - started, 1),
        "target_period_ms": args.period_ms,
        "groups_scored": n,
        "dropped_ticks": dropped,
        "latency": {
            **describe(latencies),
            "per_candidate": describe(candidate_latencies),
            "first_10pct_median_ms": round(statistics.median(latencies[:window]), 3),
            "last_10pct_median_ms": round(statistics.median(latencies[-window:]), 3),
            "drift_ms": round(statistics.median(latencies[-window:]) - statistics.median(latencies[:window]), 3),
            "slope_ms_per_hour": round(linear_slope(times_h, latencies), 3),
        },
        "rss": {
            "after_load_mb": round(rss_load / 1024, 1),
            "first_10pct_median_mb": round(statistics.median(memories[:window]), 1),
            "last_10pct_median_mb": round(statistics.median(memories[-window:]), 1),
            "growth_mb": round(statistics.median(memories[-window:]) - statistics.median(memories[:window]), 1),
            "max_mb": round(max(memories), 1),
            "hwm_mb": round(hwm_kb() / 1024, 1),
            "slope_mb_per_hour": round(linear_slope(times_h, memories), 3),
        },
        "samples_path": str(sample_path) if sample_path else None,
    })
    write_json(args.json, result)
    print(json.dumps({
        "groups": n,
        "latency_median_ms": result["latency"]["median_ms"],
        "latency_p95_ms": result["latency"]["p95_ms"],
        "rss_growth_mb": result["rss"]["growth_mb"],
    }))
    return 0


# --------------------------------------------------------------------------


def model_record(path) -> dict:
    p = Path(path)
    record = {"path": str(p), "bytes": p.stat().st_size}
    if p.is_file():
        record["sha256"] = sha256(p)
    return record


def write_json(path, result):
    text = json.dumps(result, indent=2, ensure_ascii=False) + "\n"
    if path:
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        Path(path).write_text(text)
    else:
        print(text)


# --------------------------------------------------------------------------
# orchestrate


def run_child(argv: list[str]) -> dict:
    proc = subprocess.run(
        [sys.executable, str(Path(__file__).resolve())] + argv,
        capture_output=True, text=True, check=True,
        env=dict(os.environ, ORT_DISABLE_TELEMETRY="1", ORT_RUNNING_UNIT_TESTS="1"),
    )
    if proc.stderr.strip():
        print(proc.stderr.strip(), file=sys.stderr)
    # Every child mode used here writes its result with --json; cold-once is
    # only spawned by the cold parent, never through run_child.
    if "--json" in argv:
        return json.loads(Path(argv[argv.index("--json") + 1]).read_text())
    return json.loads(proc.stdout.strip().splitlines()[-1])


def cmd_orchestrate(args) -> int:
    out = Path(args.out_dir)
    out.mkdir(parents=True, exist_ok=True)
    fixture = Path(args.fixture)
    if not fixture.is_file():
        cmd_fixture(argparse.Namespace(
            fixture=str(fixture), seed=args.seed,
            groups=DEFAULT_GROUPS, per_group=DEFAULT_PER_GROUP, seq=DEFAULT_SEQ,
        ))
    summary = common_header()
    summary["model"] = model_record(args.model)
    summary["fixture_meta"] = json.loads(fixture.with_suffix(".json").read_text()) if fixture.with_suffix(".json").is_file() else None
    summary["cold"] = None
    summary["bench"] = {}
    summary["shape"] = {}
    summary["soak"] = None

    if not args.skip_cold:
        summary["cold"] = run_child([
            "cold", "--model", args.model, "--fixture", str(fixture),
            "--threads", str(args.cold_threads), "--seq", str(DEFAULT_SEQ),
            "--seed", str(args.seed), "--repeats", str(args.cold_repeats),
            "--json", str(out / "cold.json"),
        ])

    if not args.skip_bench:
        for name, intra, spin in THREAD_CONFIGS:
            final = name == args.final_config
            argv = [
                "bench", "--model", args.model, "--fixture", str(fixture),
                "--threads", str(intra), "--allow-spinning" if spin else "--no-allow-spinning",
                "--config-name", name, "--passes",
                str(args.passes if final else args.thread_passes),
                "--warmup", str(args.warmup), "--seq", str(DEFAULT_SEQ),
                "--seed", str(args.seed), "--json", str(out / f"bench-{name}.json"),
            ]
            if not final:
                argv.append("--individual-only")
            summary["bench"][name] = run_child(argv)
        summary["final_config"] = args.final_config

    if not args.skip_shape:
        best = args.shape_threads
        for seq in (244, 164, 124):
            result = run_child([
                "bench", "--model", args.model, "--fixture", "",
                "--threads", str(best), "--no-allow-spinning",
                "--config-name", f"seq{seq}", "--passes", str(args.shape_passes),
                "--warmup", str(args.warmup), "--seq", str(seq), "--individual-only",
                "--seed", str(args.seed), "--json", str(out / f"shape-{seq}.json"),
            ])
            summary["shape"][str(seq)] = result

    if args.soak_minutes > 0:
        summary["soak"] = run_child([
            "soak", "--model", args.model, "--fixture", str(fixture),
            "--threads", str(args.soak_threads), "--no-allow-spinning",
            "--minutes", str(args.soak_minutes), "--period-ms", str(args.soak_period_ms),
            "--seq", str(DEFAULT_SEQ), "--seed", str(args.seed),
            "--json", str(out / "soak.json"), "--samples", str(out / "soak.jsonl"),
        ])

    (out / "summary.json").write_text(json.dumps(summary, indent=2, ensure_ascii=False) + "\n")
    print(json.dumps({
        "summary": str(out / "summary.json"),
        "cold_ready_ms": (summary["cold"] or {}).get("summary", {}).get("ready_ms"),
        "bench_configs": list(summary["bench"]),
        "shape_configs": list(summary["shape"]),
        "soak_groups": (summary["soak"] or {}).get("groups_scored"),
    }, indent=2))
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="cmd", required=True)

    def add_common(p, model_required=True):
        p.add_argument("--model", required=model_required)
        p.add_argument("--fixture", default="scripts/perf/fixtures/p2-63-groups.npz")
        p.add_argument("--seq", type=int, default=DEFAULT_SEQ)
        p.add_argument("--seed", type=int, default=DEFAULT_SEED)

    p = sub.add_parser("fixture")
    p.add_argument("--fixture", default="scripts/perf/fixtures/p2-63-groups.npz")
    p.add_argument("--groups", type=int, default=DEFAULT_GROUPS)
    p.add_argument("--per-group", type=int, default=DEFAULT_PER_GROUP)
    p.add_argument("--seq", type=int, default=DEFAULT_SEQ)
    p.add_argument("--seed", type=int, default=DEFAULT_SEED)
    p.set_defaults(func=cmd_fixture)

    p = sub.add_parser("cold-once")
    add_common(p)
    p.add_argument("--threads", type=int, default=8)
    p.set_defaults(func=cmd_cold_once)

    p = sub.add_parser("cold")
    add_common(p)
    p.add_argument("--threads", type=int, default=8)
    p.add_argument("--repeats", type=int, default=5)
    p.add_argument("--json")
    p.set_defaults(func=cmd_cold)

    p = sub.add_parser("bench")
    add_common(p)
    p.add_argument("--threads", type=int, default=8)
    p.add_argument("--inter", type=int, default=1)
    p.add_argument("--allow-spinning", dest="allow_spinning", action="store_true")
    p.add_argument("--no-allow-spinning", dest="allow_spinning", action="store_false")
    p.set_defaults(allow_spinning=False)
    p.add_argument("--warmup", type=int, default=30)
    p.add_argument("--passes", type=int, default=3)
    p.add_argument("--individual-only", action="store_true")
    p.add_argument("--model-sha256", default=None)
    p.add_argument("--config-name", default="custom")
    p.add_argument("--json")
    p.set_defaults(func=cmd_bench)

    p = sub.add_parser("soak")
    add_common(p)
    p.add_argument("--threads", type=int, default=8)
    p.add_argument("--allow-spinning", dest="allow_spinning", action="store_true")
    p.add_argument("--no-allow-spinning", dest="allow_spinning", action="store_false")
    p.set_defaults(allow_spinning=False)
    p.add_argument("--minutes", type=float, default=30)
    p.add_argument("--period-ms", type=float, default=200)
    p.add_argument("--warmup", type=int, default=30)
    p.add_argument("--json")
    p.add_argument("--samples")
    p.set_defaults(func=cmd_soak)

    p = sub.add_parser("orchestrate")
    p.add_argument("--model", required=True)
    p.add_argument("--fixture", default="scripts/perf/fixtures/p2-63-groups.npz")
    p.add_argument("--out-dir", default="scripts/perf/results")
    p.add_argument("--seed", type=int, default=DEFAULT_SEED)
    p.add_argument("--passes", type=int, default=3)
    p.add_argument("--thread-passes", type=int, default=2)
    p.add_argument("--final-config", default="t8")
    p.add_argument("--shape-passes", type=int, default=2)
    p.add_argument("--warmup", type=int, default=30)
    p.add_argument("--cold-threads", type=int, default=8)
    p.add_argument("--cold-repeats", type=int, default=5)
    p.add_argument("--shape-threads", type=int, default=8)
    p.add_argument("--soak-threads", type=int, default=4)
    p.add_argument("--soak-minutes", type=float, default=0)
    p.add_argument("--soak-period-ms", type=float, default=200)
    p.add_argument("--skip-cold", action="store_true")
    p.add_argument("--skip-bench", action="store_true")
    p.add_argument("--skip-shape", action="store_true")
    p.set_defaults(func=cmd_orchestrate)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
