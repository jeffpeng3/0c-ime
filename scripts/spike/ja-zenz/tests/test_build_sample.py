#!/usr/bin/env python3
"""Unit tests for the licensed-corpus fixture builder.

Run from scripts/spike/ja-zenz:
    python3 -m unittest discover -s tests -v
"""

import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))

import build_sample


def write_corpus(path: Path, header: str, rows: list[str]) -> None:
    text = "\n".join([header, *rows]) + "\n"
    path.write_bytes(text.encode("euc_jp"))


class CorpusLicenseTest(unittest.TestCase):
    def test_only_corpus1_is_allowed(self):
        self.assertEqual(build_sample.CORPUS_FILES, ("corpus.1.txt",))
        self.assertEqual(build_sample.ALLOWED_CORPUS_FILES, ("corpus.1.txt",))

    def test_unlicensed_file_is_not_read(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_corpus(root / "corpus.1.txt", "# copying: public domain",
                         ["|かな| |仮名|"])
            write_corpus(root / "corpus.2.txt", "# no license",
                         ["|ひみつ| |秘密|"])
            rows, digests = build_sample.load_corpus_rows(root)
            self.assertEqual(rows, [{"reading": "かな", "surface": "仮名"}])
            self.assertEqual(list(digests), ["corpus.1.txt"])

    def test_missing_license_marker_fails_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_corpus(root / "corpus.1.txt", "# generated", ["|かな| |仮名|"])
            with self.assertRaises(ValueError):
                build_sample.load_corpus_rows(root)

    def test_payload_records_license(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_corpus(root / "corpus.1.txt", "# copying: public domain",
                         ["|かな| |仮名|", "|かき| |柿|", "|くるま| |車|"])
            payload = build_sample.build_payload(root, size=2)
            self.assertEqual(payload["count"], 2)
            self.assertIn("corpus.1.txt", payload["corpus_files"])
            self.assertIn("public domain", payload["license"])

    def test_not_enough_rows_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_corpus(root / "corpus.1.txt", "# copying: public domain",
                         ["|かな| |仮名|"])
            with self.assertRaises(ValueError):
                build_sample.build_payload(root, size=2)

    def test_parse_line(self):
        self.assertEqual(build_sample.parse_line("|きょう|は| |今日|は|"),
                         ("きょうは", "今日は"))
        self.assertIsNone(build_sample.parse_line("# comment"))
        self.assertIsNone(build_sample.parse_line(""))


if __name__ == "__main__":
    unittest.main()
