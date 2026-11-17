from __future__ import annotations

import io
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from p2vm.cli import _new_result_dir, plan  # noqa: E402
from p2vm.config import RunConfig  # noqa: E402


class ResultDirectoryTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.key = self.root / "key"
        self.key.write_text("demo", encoding="utf-8")
        self.config = RunConfig(
            host="192.0.2.10", user="tester", key=self.key,
            result_root=self.root / "results",
        )

    def tearDown(self):
        self.temp.cleanup()

    def test_reruns_never_overwrite_an_existing_result_directory(self):
        first = _new_result_dir(self.config, "a" * 64, "acceptance")
        second = _new_result_dir(self.config, "a" * 64, "acceptance")
        self.assertNotEqual(first, second)
        self.assertTrue(first.is_dir())
        self.assertTrue(second.is_dir())

    def test_plan_does_not_contact_anything(self):
        out = io.StringIO()
        with redirect_stdout(out):
            plan(self.config)
        text = out.getvalue()
        self.assertIn("tester@192.0.2.10", text)
        self.assertIn("C:\\0c-p2vm", text)
        self.assertIn("No host, user or key is stored", text)


if __name__ == "__main__":
    unittest.main()
