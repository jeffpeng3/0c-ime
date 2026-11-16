#!/usr/bin/env python3
"""Build the fixed 200-line evaluation sample from the Anthy corpus.

Only `calctrans/corpus.1.txt` is used. Its header marks it as public domain.
`corpus.2.txt`, `corpus.3.txt` and `corpus.5.txt` carry no license statement,
so they must not be read into a redistributable fixture. `corpus.4.txt` is not
gold data either: it records known bad conversions.

The corpus file is EUC-JP and uses one `|reading|...| |surface|...|` pair per
line. The script decodes it, drops comments, and samples 200 lines with a fixed
seed so that every run measures the same sentences.

Usage:
    python3 build_sample.py --corpus-dir /path/to/anthy-9100h/calctrans \
        --out fixtures/anthy_sample200.json

The corpus tarball used for the committed fixture is recorded in pin.json.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import random
from pathlib import Path

ALLOWED_CORPUS_FILES = ("corpus.1.txt",)
CORPUS_FILES = ALLOWED_CORPUS_FILES
SAMPLE_SIZE = 200
SEED = 20261117
LICENSE_MARKER = "public domain"


def parse_line(line: str) -> tuple[str, str] | None:
    line = line.strip()
    if not line or line.startswith("#"):
        return None
    # "|reading|reading| |surface|surface|"
    halves = line.split("| |", 1)
    if len(halves) != 2:
        return None
    reading = halves[0].lstrip("|").replace("|", "")
    surface = halves[1].rstrip("|").replace("|", "")
    if not reading or not surface:
        return None
    return reading, surface


def load_corpus_rows(corpus_dir: Path) -> tuple[list[dict[str, str]], dict[str, str]]:
    """Read the allowed corpus files and return rows plus per-file SHA-256."""
    rows: list[dict[str, str]] = []
    digests: dict[str, str] = {}
    for name in CORPUS_FILES:
        if name not in ALLOWED_CORPUS_FILES:
            raise ValueError(f"corpus file is not cleared for redistribution: {name}")
        path = corpus_dir / name
        raw = path.read_bytes()
        header = raw.decode("euc_jp").splitlines()[:5]
        if not any(LICENSE_MARKER in line.lower() for line in header):
            raise ValueError(f"{path} does not declare '{LICENSE_MARKER}'")
        digests[name] = hashlib.sha256(raw).hexdigest()
        text = raw.decode("euc_jp")
        for line in text.splitlines():
            parsed = parse_line(line)
            if parsed is not None:
                rows.append({"reading": parsed[0], "surface": parsed[1]})
    if not rows:
        raise ValueError(f"no corpus rows in {corpus_dir}")
    return rows, digests


def build_payload(corpus_dir: Path, size: int = SAMPLE_SIZE, seed: int = SEED) -> dict:
    rows, digests = load_corpus_rows(corpus_dir)
    if len(rows) < size:
        raise ValueError(f"need {size} rows, found {len(rows)}")
    rng = random.Random(seed)
    rng.shuffle(rows)
    sample = sorted(rows[:size], key=lambda r: r["reading"])
    return {
        "source": "Anthy corpus.1.txt (calctrans), public domain",
        "license": "public domain, declared in the corpus.1.txt header",
        "excluded": "corpus.2.txt, corpus.3.txt and corpus.5.txt have no license statement; corpus.4.txt is not gold data",
        "seed": seed,
        "corpus_files": digests,
        "count": len(sample),
        "items": sample,
    }


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpus-dir", required=True, type=Path)
    ap.add_argument("--out", required=True, type=Path)
    ap.add_argument("--size", type=int, default=SAMPLE_SIZE)
    args = ap.parse_args()

    payload = build_payload(args.corpus_dir, args.size)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(payload, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(f"{payload['count']} items -> {args.out}")


if __name__ == "__main__":
    main()
