from __future__ import annotations

import sys
import tempfile
import unittest
from argparse import Namespace
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from p2vm.config import (  # noqa: E402
    APP_SUITES,
    ConfigError,
    RunConfig,
    from_args,
)
from p2vm.report import app_checks  # noqa: E402


class ConfigTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.key = self.root / "id_ed25519"
        self.key.write_text("not a real key", encoding="utf-8")
        self.bundle = self.root / "0c-preview.zip"
        self.bundle.write_bytes(b"PK\x05\x06" + b"\0" * 18)

    def tearDown(self):
        self.temp.cleanup()

    def config(self, **overrides):
        values = dict(
            host="192.0.2.10",
            user="tester",
            key=self.key,
            bundle=self.bundle,
            result_root=self.root / "results",
        )
        values.update(overrides)
        return RunConfig(**values)

    def test_valid_config(self):
        cfg = self.config()
        cfg.validate()
        self.assertEqual(cfg.target, "tester@192.0.2.10")
        self.assertTrue(cfg.remote_test_dir.endswith(r"\repo\platforms\windows\test"))
        self.assertTrue(cfg.scp_remote(r"C:\0c-p2vm\logs").endswith(":C:/0c-p2vm/logs"))

    def test_missing_key_is_rejected(self):
        cfg = self.config(key=self.root / "missing")
        with self.assertRaises(ConfigError):
            cfg.validate()

    def test_missing_bundle_is_rejected_for_full_run(self):
        cfg = self.config(bundle=None)
        with self.assertRaises(ConfigError):
            cfg.validate()

    def test_preflight_does_not_require_bundle(self):
        cfg = self.config(bundle=None)
        cfg.validate(require_bundle=False)

    def test_bundle_must_be_zip(self):
        other = self.root / "bundle.tar"
        other.write_bytes(b"x")
        cfg = self.config(bundle=other)
        with self.assertRaises(ConfigError):
            cfg.validate()

    def test_remote_root_with_space_is_rejected(self):
        cfg = self.config(remote_root=r"C:\0c p2vm")
        with self.assertRaises(ConfigError):
            cfg.validate()

    def test_remote_root_must_be_absolute_c_path(self):
        cfg = self.config(remote_root=r"D:\0c-p2vm")
        with self.assertRaises(ConfigError):
            cfg.validate()

    def test_unknown_suite_is_rejected(self):
        cfg = self.config(suites=["p2-mixed", "not-a-suite"])
        with self.assertRaises(ConfigError):
            cfg.validate()

    def test_duplicate_suites_are_collapsed(self):
        cfg = self.config(suites=["wpf", "wpf", "p2-mixed"])
        self.assertEqual(cfg.suites, ["wpf", "p2-mixed"])

    def test_default_suites_cover_the_phase2_targets(self):
        for suite in ("p2-mixed", "wpf", "notepad", "terminal", "browser", "webview2", "search"):
            self.assertIn(suite, APP_SUITES if suite != "p2-mixed" else ["p2-mixed"])
        self.assertIn("wpf32", APP_SUITES)
        self.assertIn("native32", APP_SUITES)

    def test_environment_variables_are_supported(self):
        args = Namespace(
            host=None, user=None, key=None, bundle=None, port=22, result_root=None,
            remote_root=r"C:\0c-p2vm", suite=None, timeout_minutes=150,
            preflight_only=False, keep_remote=False, skip_local_golden=False,
            dry_run=False, ssh=None, scp=None, ssh_option=None,
        )
        import os

        old = dict(os.environ)
        try:
            os.environ.update({
                "P2VM_HOST": "203.0.113.5",
                "P2VM_USER": "vmuser",
                "P2VM_KEY": str(self.key),
                "P2VM_BUNDLE": str(self.bundle),
            })
            cfg = from_args(args)
            cfg.validate()
            self.assertEqual(cfg.host, "203.0.113.5")
            self.assertEqual(cfg.user, "vmuser")
        finally:
            os.environ.clear()
            os.environ.update(old)


class AppChecksTest(unittest.TestCase):
    def test_search_is_skipped_on_windows_10(self):
        suites = [{"suite": "search", "passed": False, "skipped": "", "error": "missing sandbox", "checks": []}]
        checks = app_checks(suites, ["search"], 19045)
        self.assertEqual(checks[0].status, "SKIP")

    def test_missing_suite_is_not_run(self):
        checks = app_checks([], ["notepad"], 22631)
        self.assertEqual(checks[0].status, "NOT RUN")

    def test_search_on_windows_10_is_skip_even_without_a_result(self):
        checks = app_checks([], ["search"], 19045)
        self.assertEqual(checks[0].status, "SKIP")

    def test_webview2_skip_is_reported_as_skip(self):
        suites = [{"suite": "webview2", "passed": True, "skipped": "no runtime", "error": "", "checks": []}]
        checks = app_checks(suites, ["webview2"], 22631)
        self.assertEqual(checks[0].status, "SKIP")


if __name__ == "__main__":
    unittest.main()
