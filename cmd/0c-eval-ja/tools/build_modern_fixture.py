#!/usr/bin/env python3
"""Build the 500-sentence modern Japanese evaluation set.

These sentences are original, templated test data written for 0c.  They are
not copied from Anthy, Wikipedia, or any other corpus.  The templates are
deliberately simple so that readings and bunsetsu boundaries are mechanical
and reproducible.  They exercise modern vocabulary, particles, tense, and
sentence length; they are not a natural-corpus sample.

Usage:
    python3 build_modern_fixture.py --out ../../../testdata/ja/modern_500.json
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

PERSONS = [
    ("私", "わたし"), ("あなた", "あなた"), ("彼", "かれ"), ("彼女", "かのじょ"),
    ("先生", "せんせい"), ("学生", "がくせい"), ("友達", "ともだち"), ("家族", "かぞく"),
    ("子供", "こども"), ("母", "はは"),
]
PLACES = [
    ("学校", "がっこう"), ("会社", "かいしゃ"), ("駅", "えき"), ("東京", "とうきょう"),
    ("大阪", "おおさか"), ("店", "みせ"), ("家", "いえ"), ("公園", "こうえん"),
    ("図書館", "としょかん"), ("病院", "びょういん"),
]
OBJECTS = [
    ("本", "ほん", "読む", "よむ"),
    ("写真", "しゃしん", "見る", "みる"),
    ("音楽", "おんがく", "聞く", "きく"),
    ("映画", "えいが", "見る", "みる"),
    ("手紙", "てがみ", "書く", "かく"),
    ("ご飯", "ごはん", "食べる", "たべる"),
    ("お茶", "おちゃ", "飲む", "のむ"),
    ("花", "はな", "買う", "かう"),
    ("車", "くるま", "洗う", "あらう"),
    ("傘", "かさ", "持つ", "もつ"),
]
TRANSPORTS = [
    ("自転車", "じてんしゃ"), ("電車", "でんしゃ"), ("徒歩", "とほ"),
    ("新幹線", "しんかんせん"), ("飛行機", "ひこうき"), ("自転車", "じてんしゃ"),
    ("車", "くるま"), ("徒歩", "とほ"), ("バス", "ばす"), ("タクシー", "たくしー"),
]
TIMES = [
    ("今日", "きょう"), ("明日", "あした"), ("朝", "あさ"), ("昼", "ひる"),
    ("夜", "よる"), ("週末", "しゅうまつ"), ("月曜日", "げつようび"),
    ("金曜日", "きんようび"), ("来週", "らいしゅう"), ("毎日", "まいにち"),
]
WEATHERS = [
    ("天気", "てんき", "いい", "いい"), ("天気", "てんき", "悪い", "わるい"),
    ("雨", "あめ", "強い", "つよい"), ("雨", "あめ", "弱い", "よわい"),
    ("雪", "ゆき", "多い", "おおい"), ("雪", "ゆき", "少ない", "すくない"),
    ("風", "かぜ", "冷たい", "つめたい"), ("風", "かぜ", "強い", "つよい"),
    ("気温", "きおん", "高い", "たかい"), ("気温", "きおん", "低い", "ひくい"),
]


def add(items: list[dict], reading: str, surface: str, segments: list[str],
        reading_segments: list[str], pattern: str) -> None:
    items.append({
        "reading": reading,
        "surface": surface,
        "reading_segments": reading_segments,
        "segments": segments,
        "tags": [pattern],
    })


def build() -> list[dict]:
    items: list[dict] = []

    # A: person + object + matching verb.
    for i, (person, pr) in enumerate(PERSONS):
        for j, (obj, ojr, verb, vr) in enumerate(OBJECTS):
            add(
                items,
                pr + "は" + ojr + "を" + vr + "。",
                person + "は" + obj + "を" + verb + "。",
                [person + "は", obj + "を", verb + "。"],
                [pr + "は", ojr + "を", vr + "。"],
                "person-object-verb",
            )

    # B: person + place + transport.
    for person, pr in PERSONS:
        for k, (place, plr) in enumerate(PLACES):
            transport, tr = TRANSPORTS[k]
            add(
                items,
                pr + "は" + plr + "に" + tr + "で" + "いく。",
                person + "は" + place + "に" + transport + "で行く。",
                [person + "は", place + "に", transport + "で", "行く。"],
                [pr + "は", plr + "に", tr + "で", "いく。"],
                "person-place-transport",
            )

    # C: person + time + place.
    for person, pr in PERSONS:
        for time, tr in TIMES:
            add(
                items,
                pr + "は" + tr + "に" + "がっこうへいく。",
                person + "は" + time + "に学校へ行く。",
                [person + "は", time + "に", "学校へ", "行く。"],
                [pr + "は", tr + "に", "がっこうへ", "いく。"],
                "person-time-place",
            )

    # D: object topic + place.
    for obj, ojr, _, _ in OBJECTS:
        for place, plr in PLACES:
            add(
                items,
                ojr + "について" + plr + "ではなす。",
                obj + "について" + place + "で話す。",
                [obj + "について", place + "で", "話す。"],
                [ojr + "について", plr + "で", "はなす。"],
                "object-place-topic",
            )

    # E: time + weather + adjective.
    for time, tr in TIMES:
        for weather, wr, adj, ar in WEATHERS:
            add(
                items,
                tr + "は" + wr + "が" + ar + "。",
                time + "は" + weather + "が" + adj + "。",
                [time + "は", weather + "が", adj + "。"],
                [tr + "は", wr + "が", ar + "。"],
                "time-weather-adjective",
            )

    if len(items) != 500:
        raise SystemExit(f"expected 500 sentences, generated {len(items)}")
    seen = set()
    for item in items:
        if item["surface"] in seen:
            raise SystemExit(f"duplicate sentence: {item['surface']}")
        seen.add(item["surface"])
        if "".join(item["segments"]) != item["surface"]:
            raise SystemExit(f"segment mismatch: {item['surface']}")
        if "".join(item["reading_segments"]) != item["reading"]:
            raise SystemExit(f"reading segment mismatch: {item['reading']}")
    return items


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, type=Path)
    args = ap.parse_args()

    items = build()
    for i, item in enumerate(items, 1):
        item["id"] = f"modern-{i:03d}"
    payload = {
        "name": "modern-500",
        "license": "CC0-1.0. Original test data authored for 0c.",
        "source": "Original templated modern Japanese sentences, not derived from any corpus.",
        "notes": (
            "Five templates, one hundred combinations each. Readings and bunsetsu "
            "boundaries are generated from the same slots, so they are mechanical "
            "reference values rather than expert annotation."
        ),
        "count": len(items),
        "items": items,
    }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(payload, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(f"{len(items)} items -> {args.out}")


if __name__ == "__main__":
    main()
