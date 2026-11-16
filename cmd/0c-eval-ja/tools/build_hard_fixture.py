#!/usr/bin/env python3
"""Build the homophone and bunsetsu-boundary evaluation set.

The cases are original and target ambiguities that a kana-kanji converter has
to resolve from context: 橋/箸, 雨/飴, 髪/紙, 花/鼻, 雲/蜘蛛, and common
particle or auxiliary boundaries.  They are not derived from Anthy corpus.2,
corpus.3, or corpus.5.

Usage:
    python3 build_hard_fixture.py --out ../../../testdata/ja/hard_cases.json
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

CASES = [
    ("はしをわたる", "橋を渡る", ["橋を", "渡る"], ["はしを", "わたる"], ["homophone"]),
    ("はしでごはんをたべる", "箸でご飯を食べる", ["箸で", "ご飯を", "食べる"],
     ["はしで", "ごはんを", "たべる"], ["homophone"]),
    ("あめがふる", "雨が降る", ["雨が", "降る"], ["あめが", "ふる"], ["homophone"]),
    ("あめをたべる", "飴を食べる", ["飴を", "食べる"], ["あめを", "たべる"], ["homophone"]),
    ("かみをきる", "髪を切る", ["髪を", "切る"], ["かみを", "きる"], ["homophone"]),
    ("かみをすてる", "紙を捨てる", ["紙を", "捨てる"], ["かみを", "すてる"], ["homophone"]),
    ("はなをかぐ", "花を嗅ぐ", ["花を", "嗅ぐ"], ["はなを", "かぐ"], ["homophone"]),
    ("はなをかむ", "鼻をかむ", ["鼻を", "かむ"], ["はなを", "かむ"], ["homophone"]),
    ("くもがうかぶ", "雲が浮かぶ", ["雲が", "浮かぶ"], ["くもが", "うかぶ"], ["homophone"]),
    ("くもがはう", "蜘蛛が這う", ["蜘蛛が", "這う"], ["くもが", "はう"], ["homophone"]),
    ("かきをたべる", "柿を食べる", ["柿を", "食べる"], ["かきを", "たべる"], ["homophone"]),
    ("しゃしんをとる", "写真を撮る", ["写真を", "撮る"], ["しゃしんを", "とる"], ["homophone"]),
    ("いえにかえる", "家に帰る", ["家に", "帰る"], ["いえに", "かえる"], ["homophone"]),
    ("はなしをきく", "話を聞く", ["話を", "聞く"], ["はなしを", "きく"], ["homophone"]),
    ("えいがをみる", "映画を見る", ["映画を", "見る"], ["えいがを", "みる"], ["verb"]),
    ("ともだちにあう", "友達に会う", ["友達に", "会う"], ["ともだちに", "あう"], ["verb"]),
    ("えきまではしる", "駅まで走る", ["駅まで", "走る"], ["えきまで", "はしる"], ["particle"]),
    ("てがみをかく", "手紙を書く", ["手紙を", "書く"], ["てがみを", "かく"], ["homophone"]),
    ("えをかく", "絵を描く", ["絵を", "描く"], ["えを", "かく"], ["homophone"]),
    ("くるまをなおす", "車を直す", ["車を", "直す"], ["くるまを", "なおす"], ["homophone"]),
    ("かさをかす", "傘を貸す", ["傘を", "貸す"], ["かさを", "かす"], ["homophone"]),
    ("ほんをかりる", "本を借りる", ["本を", "借りる"], ["ほんを", "かりる"], ["homophone"]),
    ("きょうはいいてんきですね", "今日はいい天気ですね",
     ["今日は", "いい", "天気ですね"], ["きょうは", "いい", "てんきですね"], ["boundary"]),
    ("わたしはがくせいです", "私は学生です", ["私は", "学生です"],
     ["わたしは", "がくせいです"], ["boundary"]),
    ("とうきょうにいく", "東京に行く", ["東京に", "行く"], ["とうきょうに", "いく"], ["boundary"]),
    ("にほんごをべんきょうする", "日本語を勉強する", ["日本語を", "勉強する"],
     ["にほんごを", "べんきょうする"], ["boundary"]),
    ("あしたはあめがふるらしい", "明日は雨が降るらしい",
     ["明日は", "雨が", "降るらしい"], ["あしたは", "あめが", "ふるらしい"], ["boundary"]),
    ("でんしゃがおくれているのでちこくします", "電車が遅れているので遅刻します",
     ["電車が", "遅れているので", "遅刻します"],
     ["でんしゃが", "おくれているので", "ちこくします"], ["boundary"]),
    ("このほんをよんだことがありますか", "この本を読んだことがありますか",
     ["この", "本を", "読んだことが", "ありますか"],
     ["この", "ほんを", "よんだことが", "ありますか"], ["boundary"]),
    ("ゆっくりはなしてください", "ゆっくり話してください",
     ["ゆっくり", "話して", "ください"], ["ゆっくり", "はなして", "ください"], ["boundary"]),
    ("もういちどおしえてください", "もう一度教えてください",
     ["もう", "一度", "教えてください"], ["もう", "いちど", "おしえてください"], ["boundary"]),
    ("しごとがおわったらでんわします", "仕事が終わったら電話します",
     ["仕事が", "終わったら", "電話します"], ["しごとが", "おわったら", "でんわします"], ["boundary"]),
    ("れんしゅうがたりないのでもういちどやります", "練習が足りないのでもう一度やります",
     ["練習が", "足りないので", "もう一度", "やります"],
     ["れんしゅうが", "たりないので", "もういちど", "やります"], ["boundary"]),
    ("でんわをかけなおしてください", "電話をかけ直してください",
     ["電話を", "かけ直して", "ください"], ["でんわを", "かけなおして", "ください"], ["boundary"]),
    ("つぎのことをかんがえる", "次のことを考える", ["次の", "ことを", "考える"],
     ["つぎの", "ことを", "かんがえる"], ["boundary"]),
]


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, type=Path)
    args = ap.parse_args()

    items = []
    for i, (reading, surface, segments, reading_segments, tags) in enumerate(CASES, 1):
        if "".join(segments) != surface:
            raise SystemExit(f"segment mismatch: {surface}")
        if "".join(reading_segments) != reading:
            raise SystemExit(f"reading segment mismatch: {reading}")
        items.append({
            "id": f"hard-{i:03d}",
            "reading": reading,
            "surface": surface,
            "reading_segments": reading_segments,
            "segments": segments,
            "tags": tags,
        })
    payload = {
        "name": "hard-cases",
        "license": "CC0-1.0. Original test data authored for 0c.",
        "source": "Original homophone and bunsetsu-boundary cases, not derived from any corpus.",
        "count": len(items),
        "items": items,
    }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(payload, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(f"{len(items)} items -> {args.out}")


if __name__ == "__main__":
    main()
