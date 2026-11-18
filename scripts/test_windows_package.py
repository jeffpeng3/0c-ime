"""P2-43: Windows packaging, native runtime lock and the Python-free product boundary."""

import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import package_windows
import windows_runtime


def sha(data):
    return hashlib.sha256(data).hexdigest()


class RuntimeLockTests(unittest.TestCase):
    def test_lock_pins_exactly_the_shipped_runtime(self):
        lock = windows_runtime.load_lock()
        entry = windows_runtime.platform_entry(lock)
        self.assertEqual(sorted(entry["files"]), sorted(windows_runtime.DLLS))
        self.assertEqual(entry["onnxruntime"]["version"], "1.30.0")
        self.assertEqual(entry["msvc_runtime"]["version"], "14.44.35112")
        self.assertEqual(len(windows_runtime.DLLS), 5)
        for name in ("onnxruntime.dll", "vcruntime140.dll", "vcruntime140_1.dll",
                     "msvcp140.dll", "msvcp140_1.dll"):
            self.assertIn(name, entry["files"])
        for name, file in entry["files"].items():
            self.assertEqual(len(file["sha256"]), 64, name)
            self.assertGreater(file["bytes"], 0, name)
            self.assertEqual(len(file["wheel_sha256"]), 64) if "wheel_sha256" in file else None

    def test_runtime_description_has_fixed_versions_and_hashes(self):
        entry = windows_runtime.platform_entry(windows_runtime.load_lock())
        described = windows_runtime.describe(entry)
        self.assertEqual(described["onnxruntime"], "1.30.0")
        self.assertEqual(described["msvc_runtime"], "14.44.35112")
        self.assertEqual(set(described["files"]), set(windows_runtime.DLLS))
        for name, file in described["files"].items():
            self.assertEqual(file["sha256"], entry["files"][name]["sha256"])


class RuntimeTreeTests(unittest.TestCase):
    def fixture(self):
        root = Path(tempfile.mkdtemp(prefix="runtime-lock-"))
        files = {}
        for name in windows_runtime.DLLS:
            data = (name + "-fixture").encode()
            (root / name).write_bytes(data)
            files[name] = {"wheel_path": "x/" + name, "bytes": len(data), "sha256": sha(data)}
        lock = {
            "schema": "0c.native-runtime/1",
            "platforms": {"windows-amd64": {
                "onnxruntime": {"version": "1.30.0", "wheel_sha256": "0" * 64,
                                 "files": {n: f for n, f in files.items() if n.startswith("onnxruntime")}},
                "msvc_runtime": {"version": "14.44.35112", "wheel_sha256": "0" * 64,
                                  "files": {n: f for n, f in files.items() if not n.startswith("onnxruntime")}},
            }},
        }
        return root, lock

    def test_accepts_the_pinned_tree(self):
        root, lock = self.fixture()
        entry = windows_runtime.verify_tree(root, lock)
        self.assertEqual(set(entry["files"]), set(windows_runtime.DLLS))

    def test_rejects_missing_tampered_and_extra_dlls(self):
        root, lock = self.fixture()
        (root / "msvcp140_1.dll").unlink()
        with self.assertRaises(ValueError):
            windows_runtime.verify_tree(root, lock)

        root, lock = self.fixture()
        (root / "onnxruntime.dll").write_bytes(b"tampered")
        with self.assertRaises(ValueError):
            windows_runtime.verify_tree(root, lock)

        root, lock = self.fixture()
        (root / "numpy.pyd").write_bytes(b"not a dll, but a wheel payload")
        with self.assertRaises(ValueError):
            windows_runtime.verify_tree(root, lock)

    def test_extract_takes_only_the_five_dlls(self):
        # Two tiny wheels with the same layout as the pinned wheels.
        import zipfile
        tmp = Path(tempfile.mkdtemp(prefix="runtime-wheels-"))
        wheels = {}
        files = {}
        for component, names in (("onnxruntime", ["onnxruntime.dll"]),
                                 ("msvc_runtime", ["vcruntime140.dll", "vcruntime140_1.dll",
                                                   "msvcp140.dll", "msvcp140_1.dll"])):
            wheel = tmp / f"{component}.whl"
            with zipfile.ZipFile(wheel, "w") as archive:
                for name in names:
                    path = f"{component}/data/{name}"
                    archive.writestr(path, (name + "-fixture").encode())
            wheels[component] = wheel
        lock = {"schema": "0c.native-runtime/1", "platforms": {"windows-amd64": {}}}
        for component, names in (("onnxruntime", ["onnxruntime.dll"]),
                                 ("msvc_runtime", ["vcruntime140.dll", "vcruntime140_1.dll",
                                                   "msvcp140.dll", "msvcp140_1.dll"])):
            part = {"version": "test", "wheel_sha256": windows_runtime.digest(wheels[component]),
                    "url": "file://" + str(wheels[component]), "files": {}}
            for name in names:
                part["files"][name] = {"wheel_path": f"{component}/data/{name}",
                                       "bytes": len((name + "-fixture").encode()),
                                       "sha256": sha((name + "-fixture").encode())}
            lock["platforms"]["windows-amd64"][component] = part
        out = tmp / "out"
        windows_runtime.extract(lock, out=out, cache=tmp, wheels=wheels)
        self.assertEqual(sorted(p.name for p in out.iterdir()), sorted(windows_runtime.DLLS))
        self.assertFalse(any(p.suffix in (".py", ".whl") for p in out.iterdir()))


class PythonFreeTests(unittest.TestCase):
    def test_clean_bundle(self):
        root = Path(tempfile.mkdtemp(prefix="bundle-clean-"))
        (root / "data").mkdir()
        (root / "data" / "manifest.json").write_text("{}")
        (root / "onnxruntime.dll").write_bytes(b"MZ")
        (root / "0c-server.exe").write_bytes(b"MZ")
        (root / "THIRD_PARTY_NOTICES.txt").write_text("onnxruntime")
        self.assertEqual(windows_runtime.python_free(root), [])
        windows_runtime.check_python_free(root)
        package_windows.python_free(root)

    def test_rejects_cpython_numpy_and_wheels(self):
        cases = {
            "npython": ("python.exe", b"MZ"),
            "worker": ("neural/sequence_runtime.py", b"print()"),
            "bytecode": ("neural/__pycache__/x.pyc", b"pyc"),
            "extension": ("numpy/core/_multiarray_umath.pyd", b"MZ"),
            "wheel": ("onnxruntime-1.30.0-cp314-cp314-win_amd64.whl", b"PK"),
            "site": ("neural/python/Lib/site-packages/onnxruntime/capi/onnxruntime.dll", b"MZ"),
        }
        for label, (name, data) in cases.items():
            with self.subTest(label):
                root = Path(tempfile.mkdtemp(prefix="bundle-bad-"))
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
                with self.assertRaises(ValueError):
                    windows_runtime.check_python_free(root)
                with self.assertRaises(ValueError):
                    package_windows.python_free(root)

    def test_runtime_plan_requires_native_and_rejects_python(self):
        self.assertEqual(package_windows.runtime_plan(Path("runtime"), False, None), "native")
        self.assertEqual(package_windows.runtime_plan(None, True, None), "statistical")
        with self.assertRaises(ValueError):
            package_windows.runtime_plan(None, False, None)
        with self.assertRaises(ValueError):
            package_windows.runtime_plan(Path("runtime"), True, None)
        with self.assertRaises(ValueError):
            package_windows.runtime_plan(None, True, Path("neural"))

    def test_bundle_check_inspects_pe_and_only_the_pinned_dlls(self):
        root = Path(tempfile.mkdtemp(prefix="runtime-check-"))
        for name in windows_runtime.DLLS:
            (root / name).write_bytes(b"MZ")
        entry = {"files": {name: {"bytes": 2, "sha256": "0" * 64} for name in windows_runtime.DLLS}}
        with patch.object(package_windows.windows_runtime, "verify_tree", return_value=entry) as verify, \
             patch.object(package_windows, "check_pe") as check_pe:
            self.assertIs(package_windows.check_runtime(root), entry)
        verify.assert_called_once_with(root)
        self.assertEqual(check_pe.call_count, len(windows_runtime.DLLS))


class InstallLayoutTests(unittest.TestCase):
    """The installer puts these files at the version-directory root.

    0c-update verifies release.json per file, so the runtime DLLs must be
    siblings of 0c-server.exe or the native loader cannot find them.
    """

    def test_runtime_files_are_root_siblings(self):
        self.assertEqual(set(package_windows.RUNTIME_DLLS), set(windows_runtime.DLLS))
        self.assertIn("onnxruntime.dll", package_windows.RUNTIME_DLLS)
        # The version directory layout is flat; data lives below data/ and the
        # runtime DLLs must not be nested in a package directory.
        for name in package_windows.RUNTIME_DLLS:
            self.assertNotIn("/", name)

    def test_installer_ships_one_mixed_profile(self):
        root = Path(__file__).resolve().parents[1]
        guids = (root / "platforms/windows/tsf/src/profile.rs").read_text(encoding="utf-8")
        self.assertIn("pub const PROFILES: [Profile; 1] = [PROFILE_MIXED];", guids)
        setup = (root / "platforms/windows/installer/setup.nsi").read_text(encoding="utf-8")
        self.assertIn("單一 Mixed", setup)
        self.assertNotIn("「靈犀注音」與「靈犀日本語」已加入", setup)


class ModelSetTests(unittest.TestCase):
    def test_public_core_and_cc_by_sa_are_valid_and_separate(self):
        section = package_windows.load_model_sets()
        variants = {name: value["variant"] for name, value in section["sets"].items()}
        self.assertEqual(variants["ja-public-core"], "public-core")
        self.assertEqual(variants["ja-cc-by-sa"], "cc-by-sa")
        for name in variants:
            chosen = package_windows.check_model_set(name, "int8", True, preview=True)
            self.assertNotIn(name, ("", None))
            self.assertEqual({a["variant"] for a in chosen["artifacts"]}, {chosen["variant"]})

    def test_official_lock_has_hf_sources_and_no_placeholders(self):
        section = package_windows.load_model_sets()
        self.assertEqual(section["default_set"], "ja-cc-by-sa")
        repos = set()
        ids = []
        for name, chosen in section["sets"].items():
            self.assertFalse(chosen["placeholder"], name)
            if name == section["default_set"]:
                self.assertTrue(chosen["production_eligible"], name)
            else:
                # MIT-only 替代組的 reranker repo 尚未發布，不能當 release 候選。
                self.assertFalse(chosen["production_eligible"], name)
            for artifact in package_windows.model_set_artifacts(section, chosen):
                if artifact["artifact_id"] not in ids:
                    ids.append(artifact["artifact_id"])
                self.assertFalse(artifact["placeholder"], artifact["artifact_id"])
                hf = artifact["hf"]
                repos.add(hf["repository"])
                self.assertEqual(hf["license"], "MIT" if artifact["variant"] == "public-core" else "CC-BY-SA-4.0")
                self.assertIn("int8", hf["files"])
                self.assertIn("fp32", hf["files"])
        self.assertEqual(sorted(ids), ["punctuation-public-core", "reranker-extra", "reranker-public-core"])
        self.assertEqual(repos, {"jeffpeng3/0c-ja-reranker", "jeffpeng3/0c-ja-punct",
                                 "jeffpeng3/0c-ja-reranker-extra"})
        extra = section["sets"]["ja-cc-by-sa"]
        self.assertEqual(len(extra["artifacts"]), 1)
        self.assertEqual(extra["artifacts"][0]["artifact_id"], "reranker-extra")
        self.assertEqual(extra["shared_punctuation"], "punctuation-public-core")
        self.assertNotIn("punctuation-extra", json.dumps(section))
        # 預設組與共用 punctuation 的 revision／逐檔雜湊都要 pinned。
        self.assertEqual(extra["artifacts"][0]["hf"]["revision"],
                         "7450e10fe8b673560044c5db08ad3006a06d7a6e")
        self.assertEqual(extra["artifacts"][0]["hf"]["files"]["int8"]["sha256"],
                         "edf1afeacb89fa5da3d10462b5d9afd85392a91056634dd9ac60d41e37faeeb3")
        punct = package_windows.model_artifact(section, "punctuation-public-core")
        self.assertEqual(punct["hf"]["revision"], "ff9166abc620b6716ac7de73cfa67335e6b985d2")
        self.assertEqual(punct["hf"]["files"]["int8"]["sha256"],
                         "2860267deb3da1816b3f53decf786c5f5f478a1b9d327c8c80f2428c643f051b")

    def test_default_set_selection(self):
        section = package_windows.load_model_sets()
        for set_id in ("", "default"):
            chosen = package_windows.check_model_set(set_id, "int8", False)
            self.assertEqual(chosen["variant"], "cc-by-sa")
            artifacts = [a["artifact_id"] for a in package_windows.model_set_artifacts(section, chosen)]
            self.assertEqual(artifacts, ["reranker-extra", "punctuation-public-core"])
        manifest = package_windows.model_set_manifest("default", chosen, "int8")
        self.assertEqual(manifest["set_id"], "ja-cc-by-sa")

    def test_extra_set_carries_cc_by_sa_notice_and_keeps_punctuation_mit(self):
        section = package_windows.load_model_sets()
        chosen = package_windows.check_model_set("ja-cc-by-sa", "int8", False, preview=True)
        manifest = package_windows.model_set_manifest("ja-cc-by-sa", chosen, "int8")
        self.assertEqual(manifest["artifacts"], ["reranker-extra", "punctuation-public-core"])
        licenses = {item["artifact_id"]: item["id"] for item in manifest["licenses"]}
        self.assertEqual(licenses["reranker-extra"], "CC-BY-SA-4.0")
        self.assertEqual(licenses["punctuation-public-core"], "MIT")
        stage = Path(tempfile.mkdtemp(prefix="model-notices-"))
        package_windows.write_model_set_notices(stage, "ja-cc-by-sa", chosen)
        notices = (stage / "MODEL-SET-NOTICES.txt").read_text(encoding="utf-8")
        self.assertIn("reranker-extra", notices)
        self.assertIn("CC-BY-SA-4.0", notices)
        self.assertIn("punctuation-public-core", notices)
        self.assertIn("MIT", notices)
        self.assertIn("Attribution-ShareAlike", (stage / "MODEL-SET-CC-BY-SA-4.0.txt").read_text(encoding="utf-8"))

    def test_release_pins_default_and_rejects_unpublished_alternative(self):
        # 預設組（extra reranker + public punctuation）已 pin，release 檢查通過；
        # key 留空或 "default" 都選 default_set。
        chosen = package_windows.check_model_set("ja-cc-by-sa", "int8", False)
        self.assertEqual(chosen["variant"], "cc-by-sa")
        with self.assertRaisesRegex(ValueError, "尚未 pin|revision|production_eligible"):
            package_windows.check_model_set("ja-public-core", "int8", False)

    def test_placeholder_cannot_enter_a_release_bundle(self):
        fixture = Path(__file__).resolve().parents[1] / "testdata/models/placeholder/pairing.lock.json"
        with self.assertRaisesRegex(ValueError, "placeholder"):
            package_windows.check_model_set("ja-public-core", "int8", False, path=fixture)
        # 明確允許且是 preview 時才可出貨測試用 placeholder。
        chosen = package_windows.check_model_set("ja-public-core", "int8", True, path=fixture, preview=True)
        self.assertTrue(chosen["placeholder"])

    def test_unknown_set_and_bad_format_rejected(self):
        with self.assertRaisesRegex(ValueError, "未知的模型組"):
            package_windows.check_model_set("ja-english", "int8", True, preview=True)
        with self.assertRaisesRegex(ValueError, "沒有"):
            package_windows.check_model_set("ja-public-core", "bf16", True, preview=True)


class ModelPairTests(unittest.TestCase):
    def test_copy_model_pair_keeps_onnx_and_never_workers(self):
        source = Path(tempfile.mkdtemp(prefix="pair-src-"))
        (source / "select").mkdir()
        (source / "select" / "model.onnx").write_bytes(b"onnx")
        (source / "select" / "manifest.json").write_text("{}")
        (source / "pairing.lock.json").write_text("{}")
        stage = Path(tempfile.mkdtemp(prefix="pair-stage-"))
        package_windows.copy_model_pair(source, stage)
        self.assertTrue((stage / "neural/select/model.onnx").is_file())
        self.assertTrue((stage / "neural/pairing.lock.json").is_file())
        (source / "select" / "sequence_runtime.py").write_text("print()")
        with self.assertRaisesRegex(ValueError, "Python"):
            package_windows.copy_model_pair(source, stage)


if __name__ == "__main__":
    unittest.main()
