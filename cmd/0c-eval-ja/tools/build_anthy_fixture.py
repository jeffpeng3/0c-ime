#!/usr/bin/env python3
"""Build the frozen Anthy corpus.1 evaluation fixture.

Only ``corpus.1.txt`` is read.  The Anthy tarball also contains corpus.0,
corpus.2, corpus.3, corpus.4 and corpus.5, but only corpus.1 declares
"public domain" in its header.  corpus.4 is a list of known bad conversions,
not gold data.  The other files must never enter a fixture or test data, so
this builder fails closed on any other file name.

The sampling parameters match T03 (200 items, seed 20261117), and the output
is checked against the T03 fixture when ``--t03-fixture`` is supplied.  Unlike
the T03 fixture, this one keeps the segment boundaries from corpus.1 so the
same items can score bunsetsu boundaries.

Usage:
    python3 build_anthy_fixture.py \
        --corpus /path/to/anthy-9100h/calctrans/corpus.1.txt \
        --t03-fixture ../../../scripts/spike/ja-zenz/fixtures/anthy_sample200.json \
        --out ../../../testdata/ja/anthy_corpus1.json
"""

from __future__ import annotations

import argparse
import hashlib
import json
import random
from pathlib import Path

ALLOWED = "corpus.1.txt"
SAMPLE_SIZE = 200
SEED = 20261117
LICENSE_MARKER = "public domain"


def split_segments(block: str) -> list[str]:
    block = block.strip().strip("|")
    if not block:
        return []
    return block.split("|")


def parse_line(line: str) -> dict | None:
    line = line.strip()
    if not line or line.startswith("#"):
        return None
    halves = line.split("| |", 1)
    if len(halves) != 2:
        return None
    reading_segments = split_segments(halves[0])
    surface_segments = split_segments(halves[1])
    reading = "".join(reading_segments)
    surface = "".join(surface_segments)
    if not reading or not surface:
        return None
    # One corpus line has mismatched segment counts.  Keep the item for top-1
    # and top-5 scoring but leave segments empty so boundary scoring skips it.
    if len(reading_segments) != len(surface_segments):
        reading_segments, surface_segments = [], []
    return {
        "reading": reading,
        "surface": surface,
        "reading_segments": reading_segments,
        "segments": surface_segments,
    }


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpus", required=True, type=Path)
    ap.add_argument("--out", required=True, type=Path)
    ap.add_argument("--t03-fixture", type=Path)
    args = ap.parse_args()

    if args.corpus.name != ALLOWED:
        raise SystemExit(f"refusing corpus file that is not {ALLOWED}: {args.corpus}")

    raw = args.corpus.read_bytes()
    text = raw.decode("euc_jp")
    header = text.splitlines()[:5]
    if not any(LICENSE_MARKER in line.lower() for line in header):
        raise SystemExit(f"{args.corpus} does not declare '{LICENSE_MARKER}'")

    rows = []
    for line in text.splitlines():
        parsed = parse_line(line)
        if parsed is not None:
            rows.append(parsed)
    if len(rows) < SAMPLE_SIZE:
        raise SystemExit(f"need {SAMPLE_SIZE} rows, found {len(rows)}")

    rng = random.Random(SEED)
    rng.shuffle(rows)
    sample = sorted(rows[:SAMPLE_SIZE], key=lambda r: r["reading"])

    if args.t03_fixture:
        t03 = json.loads(args.t03_fixture.read_text(encoding="utf-8"))
        allowed = t03.get("corpus_files", {})
        if list(allowed) != [ALLOWED]:
            raise SystemExit(f"T03 fixture is not corpus.1-only: {list(allowed)}")
        expected = {(x["reading"], x["surface"]) for x in t03["items"]}
        got = {(x["reading"], x["surface"]) for x in sample}
        if got != expected:
            raise SystemExit("sample does not match the T03 corpus.1 fixture")

    for i, item in enumerate(sample, 1):
        item["id"] = f"anthy-{i:03d}"

    payload = {
        "name": "anthy-corpus1",
        "license": "Public Domain. The corpus.1.txt header says 'copying: public domain'.",
        "source": "Anthy 9100h calctrans/corpus.1.txt",
        "notes": (
            "Sample of 200 lines with seed 20261117, matching the T03 fixture. "
            "corpus.2/3/5 have no license statement and corpus.4 is not gold data; "
            "none of them were read. One line has mismatched segment counts and is "
            "scored for top-1/top-5 only, leaving 199 boundary cases."
        ),
        "seed": SEED,
        "corpus_files": {
            ALLOWED: hashlib.sha256(raw).hexdigest(),
        },
        "count": len(sample),
        "items": sample,
    }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(
        json.dumps(payload, ensure_ascii=False, indent=1) + "\n", encoding="utf-8"
    )
    scored = sum(1 for x in sample if x["segments"])
    print(f"{len(sample)} items ({scored} boundary cases) -> {args.out}")


if __name__ == "__main__":
    main()
