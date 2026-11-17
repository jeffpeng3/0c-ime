from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from p2vm.config import RunConfig  # noqa: E402
from p2vm.model import Check  # noqa: E402
from p2vm.report import (  # noqa: E402
    bundle_info,
    collect_suite_results,
    write_summary,
)


class ReportTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.key = self.root / "key"
        self.key.write_text("secret", encoding="utf-8")
        self.config = RunConfig(
            host="192.0.2.10", user="tester", key=self.key,
            result_root=self.root / "results",
        )

    def tearDown(self):
        self.temp.cleanup()

    def test_summary_and_bug_report_are_written(self):
        result = self.root / "run"
        log = result / "logs" / "01-ssh.log"
        log.parent.mkdir(parents=True)
        log.write_text("$ ssh -i /secret/key\nconnection refused\n", encoding="utf-8")
        checks = [
            Check("vm.reachable", "infra", "PASS", "ok"),
            Check("candidate.long_word_one", "vm", "FAIL", "long word duplicated", {"step": "segment.cursor1.candidates"}),
        ]
        summary = write_summary(
            result,
            config=self.config,
            revision="a" * 40,
            created="2026-11-18T00:00:00+08:00",
            bundle={"version": "0.3.0", "sha256": "b" * 64},
            checks=checks,
            environment={"computer": "VM", "os": "Windows 11", "build": 22631, "apps": {"edge": "1"}},
            suites=[],
            live_reason="",
            run_command=f"python3 p2vm.py run --key {self.key}",
            validation_dir=None,
            artifacts={"screenshots": []},
        )
        self.assertEqual(summary["live_run"]["status"], "FAIL")
        self.assertTrue((result / "summary.json").is_file())
        self.assertTrue((result / "summary.md").is_file())
        bug = (result / "bug-report.md").read_text(encoding="utf-8")
        self.assertIn("candidate.long_word_one", bug)
        self.assertIn("segment.cursor1.candidates", bug)
        self.assertIn("does not change product code", bug)
        self.assertNotIn(str(self.key), bug)
        self.assertIn("<ssh-key>", bug)

    def test_not_run_is_reported_without_bug_report(self):
        result = self.root / "run"
        summary = write_summary(
            result,
            config=self.config,
            revision="a" * 40,
            created="2026-11-18T00:00:00+08:00",
            bundle=None,
            checks=[Check("vm.reachable", "infra", "NOT RUN", "no route")],
            environment=None,
            suites=[],
            live_reason="no reachable Windows VM",
            run_command="python3 p2vm.py run",
            validation_dir=None,
        )
        self.assertEqual(summary["live_run"]["status"], "NOT RUN")
        self.assertFalse((result / "bug-report.md").exists())
        self.assertIn("no reachable Windows VM", (result / "summary.md").read_text(encoding="utf-8"))

    def test_collect_suite_results_walks_nested_directories(self):
        validation = self.root / "validation"
        suite = validation / "acceptance-1" / "notepad"
        suite.mkdir(parents=True)
        (suite / "notepad-result.json").write_text(json.dumps({
            "passed": True, "checks": ["a", "b"], "error": ""
        }), encoding="utf-8")
        results = collect_suite_results(validation)
        self.assertEqual(len(results), 1)
        self.assertEqual(results[0]["suite"], "notepad")
        self.assertEqual(results[0]["checks"], ["a", "b"])

    def test_bundle_info_reads_release_json(self):
        import zipfile

        bundle = self.root / "bundle.zip"
        with zipfile.ZipFile(bundle, "w") as archive:
            archive.writestr("release.json", json.dumps({
                "version": "0.3.0", "channel": "internal-preview", "os": "windows", "arch": "amd64"
            }))
        info = bundle_info(bundle)
        self.assertEqual(info["version"], "0.3.0")
        self.assertEqual(info["os"], "windows")
        self.assertEqual(len(info["sha256"]), 64)


if __name__ == "__main__":
    unittest.main()
