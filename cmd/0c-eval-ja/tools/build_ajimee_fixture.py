#!/usr/bin/env python3
"""Build the AJIMEE-Bench JWTD_v2/v1 evaluation corpus.

The source is the 200-item ``evaluation_items.json`` at the commit pinned by
T02.  The data is CC-BY-SA-3.0; the license text is copied next to the
fixture as LICENSE-ajimee.txt.  AJIMEE input is katakana, so the kana is
mapped to hiragana for the 0c reading interface.  Expected outputs stay
verbatim, including the accepted alternatives.

This builder only reads the AJIMEE file passed on the command line.  It never
opens Anthy corpus.2, corpus.3, or corpus.5.

Usage:
    python3 build_ajimee_fixture.py \
        --items /path/to/JWTD_v2/v1/evaluation_items.json \
        --out ../../../testdata/ja/ajimee_200.json
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

COMMIT = "401666cd56d1a570c2021798b64b6da4396bfd45"


def to_hiragana(s: str) -> str:
    out = []
    for ch in s:
        code = ord(ch)
        if 0x30A1 <= code <= 0x30F6:
            out.append(chr(code - 0x60))
        else:
            out.append(ch)
    return "".join(out)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--items", required=True, type=Path)
    ap.add_argument("--out", required=True, type=Path)
    args = ap.parse_args()

    raw = args.items.read_bytes()
    data = json.loads(raw)
    if not isinstance(data, list) or not data:
        raise SystemExit("AJIMEE evaluation_items.json is empty")

    items = []
    for i, src in enumerate(data, 1):
        expected = [x for x in src.get("expected_output", []) if x]
        if not expected:
            raise SystemExit(f"item {src.get('index')} has no expected output")
        # Keep the first expected output as the primary answer and the rest as
        # accepted alternatives.  Duplicates are noise for top-5 recall.
        accept = []
        for x in expected[1:]:
            if x != expected[0] and x not in accept:
                accept.append(x)
        items.append({
            "id": f"ajimee-{i:03d}",
            "source_index": str(src.get("index", i)),
            "reading": to_hiragana(src["input"]),
            "surface": expected[0],
            "accept": accept,
            "tags": ["ajimee"],
        })

    payload = {
        "name": "ajimee-200",
        "license": (
            "CC-BY-SA-3.0. Evaluation data from AJIMEE-Bench JWTD_v2/v1, "
            "azooKey Project; derived from 日本語Wikipedia入力誤りデータセット v2 "
            "and Wikipedia contributors. See LICENSE-ajimee.txt."
        ),
        "source": f"https://github.com/azooKey/AJIMEE-Bench commit {COMMIT} JWTD_v2/v1/evaluation_items.json",
        "notes": (
            "All 200 items. Input katakana was mapped to hiragana for the 0c "
            "reading interface; expected outputs are verbatim. This dataset "
            "has no bunsetsu annotation, so only top-1 and top-5 are scored."
        ),
        "source_sha256": hashlib.sha256(raw).hexdigest(),
        "count": len(items),
        "items": items,
    }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(payload, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(f"{len(items)} items -> {args.out}")


if __name__ == "__main__":
    main()
