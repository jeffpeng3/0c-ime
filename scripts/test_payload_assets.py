"""P2-50：固定輸入 lock、下載器與安裝包完整性檢查的單元測試（不連網）。"""

import hashlib
import io
import json
from pathlib import Path
import struct
import sys
import tempfile
import unittest
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_windows_payload
import fetch_pinned_payload


def sha(data):
    return hashlib.sha256(data).hexdigest()


def pe(machine=0x8664):
    header = bytearray(0x40)
    header[:2] = b"MZ"
    struct.pack_into("<I", header, 0x3C, 0x40)
    return bytes(header) + b"PE\0\0" + struct.pack("<H", machine) + b"\0" * 32


class FetchPinnedPayloadTests(unittest.TestCase):
    def test_downloads_verifies_and_seeds_the_tokenizer_cache(self):
        root = Path(tempfile.mkdtemp(prefix="payload-assets-"))
        source = root / "source"
        source.mkdir()
        files = {}
        for name, body in {"pack": b"pack-bytes", "manifest.json": b"{}", "public_core_ja.model": b"tokenizer"}.items():
            (source / name).write_bytes(body)
            files[name] = (body, "file://" + str(source / name))
        lock = {
            "schema": "0c.payload-assets/1",
            "assets": {
                "ja_pack": {"name": "pack", "url": files["pack"][1], "bytes": len(files["pack"][0]), "sha256": sha(files["pack"][0])},
                "ja_manifest": {"name": "ja-pack-manifest.json", "download_name": "manifest.json",
                                "url": files["manifest.json"][1], "bytes": 2, "sha256": sha(b"{}")},
                "ja_tokenizer": {"name": "public_core_ja.model", "url": files["public_core_ja.model"][1],
                                 "bytes": 9, "sha256": sha(b"tokenizer")},
            },
        }
        lock_path = root / "payload.lock.json"
        lock_path.write_text(json.dumps(lock), encoding="utf-8")
        pairing = root / "pairing.json"
        pairing.write_text(json.dumps({"model_package": {"artifacts": [{
            "artifact_id": "reranker",
            "hf": {"repository": "owner/ja-reranker", "revision": "abc",
                   "assets": {"tokenizer.model": {"name": "public_core_ja.model"}}},
        }]}}), encoding="utf-8")

        loaded = fetch_pinned_payload.load_lock(lock_path)
        out = root / "out"
        downloaded = fetch_pinned_payload.download_assets(loaded, out)
        self.assertEqual((out / "manifest.json").read_bytes(), b"{}")
        self.assertEqual(Path(downloaded["ja_pack"]).read_bytes(), b"pack-bytes")
        seeded = fetch_pinned_payload.seed_tokenizer(downloaded["ja_tokenizer"], root / "cache", pairing)
        self.assertEqual(Path(seeded[0]).read_bytes(), b"tokenizer")
        self.assertEqual(Path(seeded[0]), root / "cache/owner/ja-reranker/abc/public_core_ja.model")

    def test_rejects_a_tampered_cached_file(self):
        root = Path(tempfile.mkdtemp(prefix="payload-bad-"))
        cached = root / "pack"
        cached.write_bytes(b"tampered")
        spec = {"bytes": 4, "sha256": sha(b"good")}
        with self.assertRaises(ValueError):
            fetch_pinned_payload.check(cached, spec, "pack")

    def test_official_lock_pins_the_published_assets(self):
        lock = fetch_pinned_payload.load_lock()
        self.assertEqual(lock["chinese"]["tag"], "v0.2.12")
        self.assertEqual(lock["release"]["tag"], "p2-assets-2026.11.0")
        self.assertEqual(lock["assets"]["ja_pack"]["sha256"],
                         "8e57a18deb64d0fdc8147b53b5e234ecda412fa49feb0e8096addc4349582840")
        self.assertEqual(lock["assets"]["ja_tokenizer"]["sha256"],
                         "b25260c9c3d90a754c719e62eea6b8125f08448d48dd4d1755c18edbd00e2078")
        targets = fetch_pinned_payload.tokenizer_targets()
        self.assertEqual({(repo, revision) for repo, revision, _ in targets},
                         {("jeffpeng3/0c-ja-reranker", "7450e10fe8b673560044c5db08ad3006a06d7a6e"),
                          ("jeffpeng3/0c-ja-punct", "ff9166abc620b6716ac7de73cfa67335e6b985d2")})


class PayloadCheckTests(unittest.TestCase):
    def test_expected_payload_names_cover_the_shipped_layout(self):
        for name in ("0c-server.exe", "0c-ui.exe", "0c-settings.exe", "x86/0c_tsf.dll",
                     "0c-update.exe", "0c-updatew.exe", "0c-mcp.exe",
                     "data/ja/lexicon/words.tsv", "models/active.json"):
            self.assertIn(name, check_windows_payload.EXPECTED)

    def test_missing_file_and_python_or_extra_x86_are_rejected(self):
        files = {name: zipfile.ZipInfo(name) for name in check_windows_payload.EXPECTED}
        release = {"os": "windows", "arch": "amd64", "native_runtime": {"platform": "windows-amd64", "onnxruntime": "1.30.0"},
                   "model_package": {"version": "2026.11.0"}}
        check_windows_payload.check_entries(files, release)
        with self.assertRaises(ValueError):
            check_windows_payload.check_entries({k: v for k, v in files.items() if k != "0c-settings.exe"}, release)
        bad = dict(files)
        bad["neural/run.py"] = zipfile.ZipInfo("neural/run.py")
        with self.assertRaises(ValueError):
            check_windows_payload.check_entries(bad, release)
        bad = dict(files)
        bad["x86/0c-server.exe"] = zipfile.ZipInfo("x86/0c-server.exe")
        with self.assertRaises(ValueError):
            check_windows_payload.check_entries(bad, release)
        bad = dict(files)
        bad["data/lexicon.tsv"] = zipfile.ZipInfo("data/lexicon.tsv")
        with self.assertRaises(ValueError):
            check_windows_payload.check_entries(bad, release)

    def test_pe_machine_distinguishes_x64_and_x86(self):
        self.assertEqual(check_windows_payload.pe_machine(pe(0x8664)), 0x8664)
        self.assertEqual(check_windows_payload.pe_machine(pe(0x14C)), 0x14C)
        with self.assertRaises(ValueError):
            check_windows_payload.pe_machine(b"not a PE")

    def test_archive_check_rejects_unlisted_or_tampered_files(self):
        root = Path(tempfile.mkdtemp(prefix="payload-zip-"))
        archive = root / "bundle.zip"
        with zipfile.ZipFile(archive, "w") as package:
            package.writestr("bundle/0c-server.exe", pe())
            package.writestr("bundle/release.json", json.dumps({
                "os": "windows", "arch": "amd64", "version": "0.2.13-preview.1",
                "native_runtime": {"platform": "windows-amd64", "onnxruntime": "1.30.0"},
                "model_package": {"version": "2026.11.0"},
                "files": {"0c-server.exe": sha(pe())},
            }))
        _, files, release = check_windows_payload.peek_archive(archive)
        self.assertEqual(sorted(files), ["0c-server.exe", "release.json"])
        self.assertEqual(release["version"], "0.2.13-preview.1")

        with zipfile.ZipFile(archive, "a") as package:
            package.writestr("bundle/extra.txt", "extra")
        with self.assertRaises(ValueError):
            check_windows_payload.peek_archive(archive)


if __name__ == "__main__":
    unittest.main()
