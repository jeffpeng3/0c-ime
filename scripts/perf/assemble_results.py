#!/usr/bin/env python3
"""Assemble ``summary.json`` from per-run result files.

``orchestrate`` writes the same shape in one process.  This helper is for
targeted reruns (a fresh thread config, an extra soak) where rerunning every
measurement would waste half an hour of CPU time.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

THREADS = ["t1", "t2", "t4", "t6", "t8", "default", "t8-spin"]
SHAPES = ["244", "164", "124"]


def load(path: Path):
    return json.loads(path.read_text()) if path.is_file() else None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--dir", default="scripts/perf/results")
    parser.add_argument("--fixture-meta", default="scripts/perf/fixtures/p2-63-groups.json")
    args = parser.parse_args()
    root = Path(args.dir)

    cold = load(root / "cold.json")
    bench = {name: load(root / f"bench-{name}.json") for name in THREADS}
    bench = {k: v for k, v in bench.items() if v}
    shape = {seq: load(root / f"shape-{seq}.json") for seq in SHAPES}
    shape = {k: v for k, v in shape.items() if v}
    soak = load(root / "soak.json")
    meta = load(Path(args.fixture_meta))
    model = (cold or next(iter(bench.values())) or {}).get("model")

    summary = {
        "schema": "0c.p2-63.perf/1",
        "machine": (cold or next(iter(bench.values())))["machine"],
        "python": (cold or next(iter(bench.values())))["python"],
        "model": model,
        "fixture_meta": meta,
        "final_config": "t8",
        "cold": cold,
        "bench": bench,
        "shape": shape,
        "soak": soak,
    }
    target = root / "summary.json"
    target.write_text(json.dumps(summary, indent=2, ensure_ascii=False) + "\n")
    print(target)


if __name__ == "__main__":
    main()
