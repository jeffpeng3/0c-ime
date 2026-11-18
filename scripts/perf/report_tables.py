#!/usr/bin/env python3
"""Render the P2-63 markdown tables from ``results/summary.json``."""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def ms(value, digits=1):
    return f"{value:.{digits}f}"


def table(headers, rows):
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join("---" for _ in headers) + "|"]
    lines += ["| " + " | ".join(str(c) for c in row) + " |" for row in rows]
    return "\n".join(lines)


def thread_table(summary):
    rows = []
    for name, result in summary["bench"].items():
        ind = result["individual"]
        rows.append([
            name,
            result["options"]["intra_op"] or "default",
            "yes" if result["options"]["spinning"] else "no",
            ms(ind["per_candidate"]["median_ms"]),
            ms(ind["per_candidate"]["p95_ms"]),
            ms(ind["per_group"]["median_ms"]),
            ms(ind["per_group"]["p95_ms"]),
        ])
    return table(
        ["config", "intra_op", "spinning", "candidate median", "candidate p95",
         "group median", "group p95"],
        rows,
    )


def headline_table(result):
    ind = result["individual"]
    batch = result["batch"]
    one = ind["per_candidate"]["median_ms"]
    rows = [
        ["batch 1, per candidate", ind["per_candidate"]["samples"], 1,
         ms(ind["per_candidate"]["median_ms"]), ms(ind["per_candidate"]["p95_ms"]),
         ms(ind["per_candidate"]["mean_ms"]), "-", "1.00"],
        ["batch 1, group of 5 (5 calls)", ind["per_group"]["samples"], 5,
         ms(ind["per_group"]["median_ms"]), ms(ind["per_group"]["p95_ms"]),
         ms(ind["per_group"]["mean_ms"]), ms(ind["per_candidate"]["median_ms"]), "1.00"],
    ]
    for n in ("5", "8"):
        b = batch[n]
        per = b["per_candidate_equivalent_ms"]
        rows.append([
            f"batch {n} (one call)", b["groups"], n,
            ms(b["per_group"]["median_ms"]), ms(b["per_group"]["p95_ms"]),
            ms(b["per_group"]["mean_ms"]), ms(per),
            f"{per / one:.2f}",
        ])
    return table(
        ["mode", "samples", "candidates/call", "median ms", "p95 ms",
         "mean ms", "ms/candidate", "per-candidate cost vs batch 1"],
        rows,
    )


def sweep_table(result):
    rows = []
    for n in ("1", "2", "3", "4", "5", "8"):
        b = result["batch_sweep"].get(n)
        if not b:
            continue
        rows.append([
            n, b["groups"], ms(b["per_group"]["median_ms"]),
            ms(b["per_group"]["p95_ms"]), ms(b["per_candidate_equivalent_ms"]),
            f'{b["speedup_vs_batch1"]:.2f}',
        ])
    return table(
        ["batch", "groups", "median ms", "p95 ms", "ms/candidate", "speedup vs batch 1"],
        rows,
    )


def shape_table(summary):
    rows = []
    base = summary["shape"].get("244")
    base_group = base["individual"]["per_group"]["median_ms"] if base else None
    for seq, result in summary["shape"].items():
        group = result["individual"]["per_group"]["median_ms"]
        rows.append([
            seq,
            (int(seq) - 4) // 2,
            ms(result["individual"]["per_candidate"]["median_ms"]),
            ms(group),
            ms(result["individual"]["per_group"]["p95_ms"]),
            f"{group / base_group:.2f}" if base_group else "-",
        ])
    return table(
        ["seq", "context each side", "candidate median", "group median",
         "group p95", "relative to seq 244"],
        rows,
    )


def cold_table(summary):
    cold = summary.get("cold")
    if not cold:
        return "(no cold result)"
    s = cold["summary"]
    rows = [[k.replace("_ms", ""), ms(s[k])] for k in
            ("import_ms", "session_load_ms", "first_inference_ms", "ready_ms", "process_wall_ms")]
    return table(["stage", "median ms"], rows)


def soak_table(summary):
    soak = summary.get("soak")
    if not soak:
        return "(no soak result)"
    lat = soak["latency"]
    rss = soak["rss"]
    rows = [
        ["groups scored", soak["groups_scored"]],
        ["target period / duration", f'{soak["target_period_ms"]} ms / {soak["duration_s"] / 60:.1f} min'],
        ["group median / p95 / p99", f'{ms(lat["median_ms"])} / {ms(lat["p95_ms"])} / {ms(lat["p99_ms"])} ms'],
        ["group max", f'{ms(lat["max_ms"])} ms'],
        ["candidate median / p95", f'{ms(lat["per_candidate"]["median_ms"])} / {ms(lat["per_candidate"]["p95_ms"])} ms'],
        ["first 10% / last 10% group median", f'{ms(lat["first_10pct_median_ms"])} / {ms(lat["last_10pct_median_ms"])} ms'],
        ["drift / slope", f'{ms(lat["drift_ms"])} ms / {ms(lat["slope_ms_per_hour"])} ms per hour'],
        ["RSS after warmup", f'{rss["after_load_mb"]:.1f} MiB'],
        ["RSS first 10% / last 10%", f'{rss["first_10pct_median_mb"]:.1f} / {rss["last_10pct_median_mb"]:.1f} MiB'],
        ["RSS growth / max / HWM", f'{rss["growth_mb"]:.1f} / {rss["max_mb"]:.1f} / {rss["hwm_mb"]:.1f} MiB'],
        ["RSS slope", f'{ms(rss["slope_mb_per_hour"], 2)} MiB per hour'],
        ["dropped ticks", soak["dropped_ticks"]],
    ]
    return table(["metric", "value"], rows)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--summary", default="scripts/perf/results/summary.json")
    args = parser.parse_args()
    summary = json.loads(Path(args.summary).read_text())
    final = summary.get("final_config", "t4")
    result = summary["bench"][final]
    print("## Cold start\n")
    print(cold_table(summary))
    print("\n## Thread comparison (individual candidates)\n")
    print(thread_table(summary))
    print(f"\n## Headline latency ({final}, seq 244)\n")
    print(headline_table(result))
    print(f"\n## Batch sweep ({final}, seq 244)\n")
    print(sweep_table(result))
    print("\n## Context length\n")
    print(shape_table(summary))
    print("\n## Soak\n")
    print(soak_table(summary))


if __name__ == "__main__":
    main()
