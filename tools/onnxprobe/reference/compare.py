#!/usr/bin/env python3
"""Compare the Go probe output with the Python oracle.

Reference-only harness for the P2-30S spike. Reports:
  - exact token id match count and every mismatch (first 200 strings)
  - max absolute score difference over the same inputs
"""
import argparse
import json


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--go', required=True)
    ap.add_argument('--oracle', required=True)
    args = ap.parse_args()

    with open(args.go, encoding='utf-8') as f:
        g = json.load(f)
    with open(args.oracle, encoding='utf-8') as f:
        o = json.load(f)

    assert g['texts'] == o['texts'], (g['texts'], o['texts'])
    gt, ot = g['tokens'], o['tokens']
    mismatches = []
    exact = 0
    for i, (a, b) in enumerate(zip(gt, ot)):
        if a == b:
            exact += 1
        else:
            mismatches.append({'index': i, 'go': a, 'python': b})

    gs, os_ = g['scores'], o['scores']
    diffs = [abs(a - b) for a, b in zip(gs, os_)]
    worst = max(range(len(diffs)), key=lambda i: diffs[i]) if diffs else None

    report = {
        'texts': len(gt),
        'exact_matches': exact,
        'mismatches': len(mismatches),
        'mismatch_details': mismatches,
        'score_pairs': len(diffs),
        'max_abs_diff': max(diffs) if diffs else None,
        'mean_abs_diff': sum(diffs) / len(diffs) if diffs else None,
        'worst_index': worst,
        'worst_go': gs[worst] if worst is not None else None,
        'worst_python': os_[worst] if worst is not None else None,
        'all_finite': g.get('all_finite'),
    }
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if exact == len(gt) else 1


if __name__ == '__main__':
    raise SystemExit(main())
