#!/usr/bin/env python3
"""T03 spike: measure zenz-v3.1-small through llama.cpp.

The script talks to llama-server over HTTP. Prompts are tokenised once with
llama-tokenize and sent as token ID arrays so that no chat template or BOS token
is added (zenz was trained without BOS).

It records:
  * model load wall time and resident memory
  * per-keystroke conversion latency, cold and with prefix cache
  * 32-kana conversion latency
  * top-1 agreement with the fixed 200-line Anthy corpus sample
  * the Anthy baseline for the same sample, when an anthy_top1 binary is given

Usage:
    python3 run_spike.py --isolated --backend cpu-6t --threads 6 \
        --llama-bin /path/to/llama.cpp/build-cpu/bin \
        --model /path/to/ggml-model-Q5_K_M.gguf \
        --anthy-bin /path/to/anthy_top1 \
        --out results/cpu-6t.json
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import signal
import socket
import statistics
import subprocess
import sys
import tempfile
import time
import unicodedata
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent
PROMPT_IN = "\uee00"
PROMPT_OUT = "\uee01"


def prompt_for(reading: str) -> str:
    return PROMPT_IN + reading + PROMPT_OUT


def npredict_for(reading: str) -> int:
    """Cap generation so a looping model cannot dominate a latency sample."""
    return max(16, min(96, len(reading) * 2 + 4))


def percentile(values: list[float], p: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    if len(ordered) == 1:
        return ordered[0]
    pos = (len(ordered) - 1) * p / 100.0
    lo = int(pos)
    hi = min(lo + 1, len(ordered) - 1)
    frac = pos - lo
    return ordered[lo] * (1 - frac) + ordered[hi] * frac


def summarize(samples: list[float]) -> dict[str, float | int]:
    return {
        "n": len(samples),
        "p50_ms": round(percentile(samples, 50), 2),
        "p95_ms": round(percentile(samples, 95), 2),
        "p99_ms": round(percentile(samples, 99), 2),
        "mean_ms": round(statistics.fmean(samples), 2) if samples else 0.0,
        "max_ms": round(max(samples), 2) if samples else 0.0,
    }


def normalize(text: str) -> str:
    text = unicodedata.normalize("NFKC", text)
    return re.sub(r"\s+", "", text)


def lcs_len(a: str, b: str) -> int:
    if not a or not b:
        return 0
    prev = [0] * (len(b) + 1)
    for ca in a:
        cur = [0]
        for j, cb in enumerate(b, 1):
            cur.append(prev[j - 1] + 1 if ca == cb else max(prev[j], cur[j - 1]))
        prev = cur
    return prev[-1]


def rss_kb(pid: int) -> int:
    try:
        for line in Path(f"/proc/{pid}/status").read_text().splitlines():
            if line.startswith("VmRSS:"):
                return int(line.split()[1])
    except OSError:
        pass
    return 0


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def read_loadavg() -> dict[str, float | str]:
    fields = Path("/proc/loadavg").read_text().split()
    return {
        "load1": float(fields[0]),
        "load5": float(fields[1]),
        "load15": float(fields[2]),
        "runnable": fields[3],
    }


class ZenzServer:
    def __init__(self, bin_dir: Path, model: Path, threads: int, ngl: int, ctx: int):
        self.bin_dir = bin_dir
        self.model = model
        self.threads = threads
        self.ngl = ngl
        self.ctx = ctx
        self.port = free_port()
        self.proc: subprocess.Popen | None = None

    def __enter__(self) -> "ZenzServer":
        log_dir = Path(tempfile.gettempdir()) / "zenz-spike"
        log_dir.mkdir(parents=True, exist_ok=True)
        log = open(log_dir / f"llama-server-{self.port}.log", "wb")
        self.log_path = log.name
        cmd = [
            str(self.bin_dir / "llama-server"),
            "-m", str(self.model),
            "-c", str(self.ctx),
            "-t", str(self.threads),
            "--host", "127.0.0.1",
            "--port", str(self.port),
            "-np", "1",
            "--no-webui",
            "--no-warmup",
        ]
        if self.ngl:
            cmd += ["-ngl", str(self.ngl)]
        started = time.perf_counter()
        self.proc = subprocess.Popen(cmd, stdout=log, stderr=subprocess.STDOUT)
        while time.perf_counter() - started < 120:
            if self.proc.poll() is not None:
                raise RuntimeError(f"llama-server exited: {Path(log.name).read_text(errors='replace')[-2000:]}")
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{self.port}/health", timeout=0.5) as resp:
                    if resp.status == 200:
                        self.load_ms = (time.perf_counter() - started) * 1000
                        self.rss_after_load_kb = rss_kb(self.proc.pid)
                        return self
            except (urllib.error.URLError, TimeoutError):
                time.sleep(0.02)
        raise RuntimeError("llama-server did not become healthy in 120 s")

    def __exit__(self, *exc) -> None:
        if self.proc and self.proc.poll() is None:
            self.proc.send_signal(signal.SIGTERM)
            try:
                self.proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.proc.kill()

    def tokenize(self, text: str) -> list[int]:
        out = subprocess.run(
            [str(self.bin_dir / "llama-tokenize"), "-m", str(self.model),
             "--stdin", "--ids", "--no-bos"],
            input=text.encode(), capture_output=True, check=True,
        ).stdout.decode()
        return json.loads(out)

    def complete(self, prompt_ids: list[int], n_predict: int = 64, cache_prompt: bool = False) -> tuple[str, float, dict]:
        body = json.dumps({
            "prompt": prompt_ids,
            "n_predict": n_predict,
            "temperature": 0.0,
            "top_k": 1,
            "top_p": 1.0,
            "min_p": 0.0,
            "repeat_penalty": 1.0,
            "cache_prompt": cache_prompt,
            "stream": False,
        }).encode()
        started = time.perf_counter()
        req = urllib.request.Request(
            f"http://127.0.0.1:{self.port}/completion",
            data=body, headers={"Content-Type": "application/json"}, method="POST",
        )
        with urllib.request.urlopen(req, timeout=300) as resp:
            payload = json.loads(resp.read())
        elapsed = (time.perf_counter() - started) * 1000
        return payload.get("content", ""), elapsed, payload.get("timings", {})


def run_quality(server: ZenzServer, items: list[dict], anthy_bin: Path | None,
                measure_latency: bool = True) -> dict:
    exact = 0
    norm = 0
    lcs_total = 0
    ref_total = 0
    preds: list[dict] = []
    latencies: list[float] = []
    for item in items:
        reading = item["reading"]
        ids = server.tokenize(prompt_for(reading))
        pred, elapsed, _ = server.complete(ids, n_predict=npredict_for(reading), cache_prompt=False)
        pred = pred.strip()
        if measure_latency:
            latencies.append(elapsed)
        reference = item["surface"]
        is_exact = pred == reference
        is_norm = normalize(pred) == normalize(reference)
        exact += is_exact
        norm += is_norm
        lcs_total += lcs_len(normalize(reference), normalize(pred))
        ref_total += len(normalize(reference))
        preds.append({
            "reading": reading,
            "reference": reference,
            "zenz": pred,
            "exact": is_exact,
        })

    out = {
        "n": len(items),
        "zenz": {
            "top1_exact": exact,
            "top1_exact_rate": round(exact / len(items), 4),
            "top1_normalized": norm,
            "top1_normalized_rate": round(norm / len(items), 4),
            "lcs_recall": round(lcs_total / ref_total, 4) if ref_total else 0.0,
        },
        "items": preds,
    }
    if measure_latency:
        out["zenz"]["latency"] = summarize(latencies)

    if anthy_bin is not None:
        readings = "".join(item["reading"] + "\n" for item in items)
        proc = subprocess.run([str(anthy_bin)], input=readings.encode(),
                              capture_output=True, check=True)
        lines = proc.stdout.decode().splitlines()
        a_exact = a_norm = a_lcs = a_ref = 0
        for item, pred in zip(items, lines):
            pred = pred.strip()
            reference = item["surface"]
            a_exact += pred == reference
            a_norm += normalize(pred) == normalize(reference)
            a_lcs += lcs_len(normalize(reference), normalize(pred))
            a_ref += len(normalize(reference))
        out["anthy"] = {
            "top1_exact": a_exact,
            "top1_exact_rate": round(a_exact / len(items), 4),
            "top1_normalized": a_norm,
            "top1_normalized_rate": round(a_norm / len(items), 4),
            "lcs_recall": round(a_lcs / a_ref, 4) if a_ref else 0.0,
            "note": "libanthy 9100h, top candidate of every segment, fresh context per line, no commit",
        }
    return out


def run_latency(server: ZenzServer, items: list[dict]) -> dict:
    # Per-keystroke: re-convert every prefix of ten sentences, like live conversion.
    sentences = [item["reading"] for item in items[:10]]
    cold: list[float] = []
    cached: list[float] = []
    for reading in sentences:
        ids = server.tokenize(prompt_for(reading))
        # cold: each prefix is a separate request with cache disabled
        for length in range(1, len(reading) + 1):
            prefix_ids = server.tokenize(prompt_for(reading[:length]))
            _, ms, _ = server.complete(prefix_ids, n_predict=npredict_for(reading[:length]), cache_prompt=False)
            cold.append(ms)
        # cached: sequential typing reuses the previous prefix KV
        for length in range(1, len(reading) + 1):
            prefix_ids = server.tokenize(prompt_for(reading[:length]))
            _, ms, _ = server.complete(prefix_ids, n_predict=npredict_for(reading[:length]), cache_prompt=True)
            cached.append(ms)

    # 32-kana conversions, repeated.
    full: list[float] = []
    long_readings = [r[:32] for r in sentences if len(r) >= 32][:10]
    if not long_readings:
        base = max(sentences, key=len)
        long_readings = [(base * 2)[:32]]
    reps = max(10, 200 // len(long_readings))
    for _ in range(reps):
        for reading in long_readings:
            ids = server.tokenize(prompt_for(reading))
            _, ms, _ = server.complete(ids, n_predict=npredict_for(reading), cache_prompt=False)
            full.append(ms)

    return {
        "per_key_cold": summarize(cold),
        "per_key_cached": summarize(cached),
        "full_32_kana": summarize(full),
        "sentences": sentences,
        "note": "per-key = one greedy conversion of each prefix, 1..len; 32-kana = first 32 chars of long sample lines",
    }


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--backend", required=True, help="label, e.g. cpu-12t or vulkan-rtx4060")
    ap.add_argument("--llama-bin", required=True, type=Path)
    ap.add_argument("--model", required=True, type=Path)
    ap.add_argument("--threads", type=int, default=12)
    ap.add_argument("--ngl", type=int, default=0)
    ap.add_argument("--ctx", type=int, default=512)
    ap.add_argument("--anthy-bin", type=Path)
    ap.add_argument("--fixture", type=Path, default=ROOT / "fixtures/anthy_sample200.json")
    ap.add_argument("--out", required=True, type=Path)
    ap.add_argument("--skip-quality", action="store_true")
    ap.add_argument("--skip-latency", action="store_true",
                    help="quality only; omit timing and memory fields")
    ap.add_argument("--isolated", action="store_true",
                    help="refuse to measure unless the host is quiet")
    ap.add_argument("--max-load-per-core", type=float, default=0.35,
                    help="per-core load1 ceiling for a quiet measurement (default 0.35)")
    args = ap.parse_args()

    if args.skip_quality and args.skip_latency:
        sys.exit("--skip-quality and --skip-latency cannot be used together")

    fixture = json.loads(args.fixture.read_text(encoding="utf-8"))
    items = fixture["items"]

    ncpu = os.cpu_count() or 1
    load_start = read_loadavg()
    ceiling = args.max_load_per_core * ncpu
    if args.isolated and load_start["load1"] > ceiling:
        sys.exit(
            f"refusing isolated run: load1={load_start['load1']:.2f} on {ncpu} CPUs "
            f"(ceiling {ceiling:.2f}); wait for a quiet window"
        )

    with ZenzServer(args.llama_bin, args.model, args.threads, args.ngl, args.ctx) as server:
        # One warm-up request so that first-request allocation is not in the samples.
        server.complete(server.tokenize(prompt_for("きょうはいいてんきですね")), n_predict=16)
        time.sleep(0.2)
        rss_after_warmup_kb = rss_kb(server.proc.pid)

        result: dict = {
            "spike": "T03",
            "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
            "backend": {
                "name": args.backend,
                "threads": args.threads,
                "ngl": args.ngl,
                "ctx": args.ctx,
                "llama_cpp_commit": os.environ.get("LLAMA_CPP_COMMIT", ""),
            },
            "model": {
                "file": args.model.name,
                "size_bytes": args.model.stat().st_size,
                "sha256": hashlib.sha256(args.model.read_bytes()).hexdigest(),
            },
            "host": {
                "platform": platform.platform(),
                "machine": platform.machine(),
                "python": platform.python_version(),
                "cpu_count": ncpu,
                "loadavg_start": load_start,
                "isolated": args.isolated,
                "max_load_per_core": args.max_load_per_core,
            },
        }
        if not args.skip_latency:
            result["load"] = {
                "wall_ms": round(server.load_ms, 1),
                "rss_after_load_kb": server.rss_after_load_kb,
                "rss_after_warmup_kb": rss_after_warmup_kb,
            }
            result["latency"] = run_latency(server, items)
            result["rss_after_latency_kb"] = rss_kb(server.proc.pid)
        if not args.skip_quality:
            result["quality"] = run_quality(server, items, args.anthy_bin,
                                            measure_latency=not args.skip_latency)
        if not args.skip_latency:
            result["rss_after_bench_kb"] = rss_kb(server.proc.pid)

        load_end = read_loadavg()
        result["host"]["loadavg_end"] = load_end
        quiet = max(load_start["load1"], load_end["load1"]) <= ceiling
        result["quality_status"] = "valid_deterministic"
        if args.skip_latency:
            result["latency_status"] = "not_measured"
            result["note"] = "quality-only run; no timing or memory samples were taken"
        else:
            result["latency_status"] = "final" if quiet else "contended_preliminary_not_final"
            if not quiet:
                result["note"] = (
                    "latency samples are an upper bound: load1 exceeded the quiet ceiling of "
                    f"{ceiling:.2f} on {ncpu} CPUs during the run; rerun with --isolated in a quiet window"
                )

    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(result, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(json.dumps({k: v for k, v in result.items() if k != "quality"}, ensure_ascii=False, indent=1))
    if "quality" in result:
        print(json.dumps({k: v for k, v in result["quality"].items() if k != "items"}, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
