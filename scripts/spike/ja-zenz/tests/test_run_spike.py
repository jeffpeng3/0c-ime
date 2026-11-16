#!/usr/bin/env python3
"""Unit tests for the pure helpers in run_spike.py.

Run from scripts/spike/ja-zenz:
    python3 -m unittest discover -s tests -v
"""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import run_spike


class PercentileTest(unittest.TestCase):
    def test_empty(self):
        self.assertEqual(run_spike.percentile([], 95), 0.0)

    def test_single(self):
        self.assertEqual(run_spike.percentile([7.5], 99), 7.5)

    def test_interpolates(self):
        values = [1.0, 2.0, 3.0, 4.0, 5.0]
        self.assertAlmostEqual(run_spike.percentile(values, 50), 3.0)
        self.assertAlmostEqual(run_spike.percentile(values, 95), 4.8)
        self.assertAlmostEqual(run_spike.percentile(values, 100), 5.0)

    def test_summary_keys(self):
        summary = run_spike.summarize([1.0, 2.0, 3.0])
        self.assertEqual(summary["n"], 3)
        for key in ("p50_ms", "p95_ms", "p99_ms", "mean_ms", "max_ms"):
            self.assertIn(key, summary)


class TextTest(unittest.TestCase):
    def test_normalize_full_width(self):
        self.assertEqual(run_spike.normalize("ＡＢＣ １２３"), "ABC123")

    def test_normalize_keeps_kana(self):
        self.assertEqual(run_spike.normalize("きょう は"), "きょうは")

    def test_lcs_identical(self):
        self.assertEqual(run_spike.lcs_len("今日は晴れ", "今日は晴れ"), 5)

    def test_lcs_partial(self):
        self.assertEqual(run_spike.lcs_len("今日は晴れ", "今日は雨"), 3)
        self.assertEqual(run_spike.lcs_len("今日は晴れ", "今日は晴"), 4)

    def test_lcs_empty(self):
        self.assertEqual(run_spike.lcs_len("", "abc"), 0)

    def test_prompt_wraps_reading(self):
        self.assertEqual(run_spike.prompt_for("かな"), "\uee00かな\uee01")

    def test_npredict_bounds(self):
        self.assertEqual(run_spike.npredict_for(""), 16)
        self.assertEqual(run_spike.npredict_for("あ" * 32), 68)
        self.assertEqual(run_spike.npredict_for("あ" * 128), 96)


if __name__ == "__main__":
    unittest.main()
