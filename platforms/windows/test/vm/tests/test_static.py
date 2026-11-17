from __future__ import annotations

import re
import unittest
from pathlib import Path

VM = Path(__file__).resolve().parents[1]
TEST = VM.parent
REPO = TEST.parents[2]


class StaticContractTest(unittest.TestCase):
    def test_required_files_exist(self):
        for name in (
            "p2vm.py",
            "Invoke-Interactive.ps1",
            "Remote-P2Acceptance.ps1",
            "Capture-Desktop.ps1",
        ):
            self.assertTrue((VM / name).is_file(), name)
        self.assertTrue((TEST / "p2-mixed-test.ps1").is_file())

    def test_acceptance_test_knows_the_p2_suite(self):
        text = (TEST / "acceptance-test.ps1").read_text(encoding="utf-8-sig")
        self.assertIn("'p2-mixed'", text)
        # The suite name maps to p2-mixed-test.ps1 through the shared
        # "<script>-test.ps1" convention.
        self.assertIn("'p2-mixed'='p2-mixed'", text)
        self.assertTrue((TEST / "p2-mixed-test.ps1").is_file())

    def test_trace_step_names_match_the_python_checks(self):
        script = (TEST / "p2-mixed-test.ps1").read_text(encoding="utf-8-sig")
        for step in (
            "english.direct",
            "zhuyin.commit",
            "segment.compose",
            "segment.cursor0.candidates",
            "segment.cursor2.candidates",
        ):
            self.assertIn(step, script)

    def test_remote_script_has_the_safety_hooks(self):
        text = (VM / "Remote-P2Acceptance.ps1").read_text(encoding="utf-8-sig")
        for marker in (
            "Get-Preflight",
            "acceptance-test.ps1",
            "Capture-Desktop.ps1",
            "PreflightOnly",
            "desktop-failure.png",
            "close the daily 0c engine",
        ):
            self.assertIn(marker, text)
        self.assertNotIn("Remove-Item -LiteralPath $env:LOCALAPPDATA", text)
        self.assertNotIn("Stop-Process -Name 0c-server", text)

    def test_harness_never_fixes_or_patches_the_product(self):
        # The harness tree is under platforms/windows/test; product sources are
        # read-only inputs to the run.  Guard against a future script writing to
        # the engine/shell trees.
        product = re.compile(
            r"(?:Set-Content|Out-File|Copy-Item|Remove-Item).*"
            r"(?:platforms[\\/]+windows[\\/]+(?:tsf|ui|installer)|core[\\/]|cmd[\\/])",
            re.IGNORECASE,
        )
        for path in VM.rglob("*.ps1"):
            text = path.read_text(encoding="utf-8-sig", errors="replace")
            self.assertIsNone(product.search(text), f"harness writes product files: {path}")

    def test_no_baked_in_host_user_or_key(self):
        pattern = re.compile(
            r"\b\d{1,3}(?:\.\d{1,3}){3}\b|C:\\\\Users\\\\|BEGIN (?:OPENSSH|RSA|EC) PRIVATE KEY"
        )
        for path in VM.rglob("*"):
            if "tests" in path.parts:
                continue  # test fixtures use RFC 5737 addresses on purpose
            if path.is_file() and path.suffix in {".py", ".ps1", ".md", ".json"}:
                text = path.read_text(encoding="utf-8-sig", errors="replace")
                for match in pattern.finditer(text):
                    value = match.group(0)
                    # RFC 5737 documentation addresses are safe in examples.
                    if value.startswith(("192.0.2.", "198.51.100.", "203.0.113.")):
                        continue
                    self.fail(f"credential-like value {value!r} in {path}")

    def test_powershell_files_are_bom_or_ascii(self):
        # Windows PowerShell 5.1 reads a UTF-8 file without BOM as ANSI; Chinese
        # literals in p2-mixed-test.ps1 would be corrupted without the BOM.
        for path in list(VM.glob("*.ps1")) + [TEST / "p2-mixed-test.ps1"]:
            data = path.read_bytes()
            if any(byte > 0x7F for byte in data):
                self.assertTrue(data.startswith(b"\xef\xbb\xbf"), f"missing UTF-8 BOM: {path}")

    def test_harness_lives_under_the_owned_test_tree(self):
        self.assertTrue(str(VM).startswith(str(TEST)))
        self.assertTrue((REPO / "platforms" / "windows" / "test").is_dir())


if __name__ == "__main__":
    unittest.main()
