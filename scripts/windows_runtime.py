#!/usr/bin/env python3
"""Pinned native ONNX Runtime for the Python-free Windows product.

models/runtime.lock.json fixes the onnxruntime and MSVC runtime wheel versions,
their SHA-256, and the size and SHA-256 of every DLL that ships next to
0c-server.exe. This module verifies a runtime directory, extracts the DLLs from
already-downloaded wheels, and reports the installed layout.

The product never bundles CPython, numpy, the onnxruntime Python package or a
wheel; only the PE files listed in the lock leave the build cache. ``python_free``
is the guard the packaging script runs over the finished bundle.
"""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
import shutil
import tempfile
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / "models" / "runtime.lock.json"

# Exactly the runtime the native backend loads next to the executable. The
# native backend uses the CPU execution provider only.
DLLS = ("onnxruntime.dll", "vcruntime140.dll", "vcruntime140_1.dll",
        "msvcp140.dll", "msvcp140_1.dll")

# What must never appear in a shipped Windows bundle. CPython, numpy, the
# onnxruntime Python package, workers and wheels are all build-host only.
PYTHON_SUFFIXES = (".py", ".pyc", ".pyo", ".pyd", ".pyw", ".whl", ".pth", ".dist-info")
PYTHON_DIRS = ("site-packages", "dist-packages", "python", "python3", "python314", "__pycache__",
               "numpy", "onnxruntime", "neural/python")
PYTHON_FILES = ("python.exe", "pythonw.exe", "python3.exe", "python314.dll", "python313.dll",
                "python312.dll", "python311.dll", "python310.dll")

# Byte strings that must not occur in any shipped PE file. They catch a binary
# that still carries the removed interpreter or worker path even when no Python
# files are copied into the bundle.
RUNTIME_MARKERS = (
    b"sequence_runtime.py", b"punct_runtime.py", b"onnx_bundle_probe.py",
    b"-neural-python", b"-system-python", b"-neural-worker", b"-punct-worker",
    b"neural_python", b"python.exe", b"pythonw.exe", b"runtime/python/",
    b"python/bin/python", b"site-packages", b"dist-packages", b"numpy/core",
    b"onnxruntime.capi",
)


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def load_lock(path: Path = LOCK) -> dict:
    with open(path, encoding="utf-8") as stream:
        lock = json.load(stream)
    if lock.get("schema") != "0c.native-runtime/1":
        raise ValueError(f"原生執行階段 lock schema 不符：{path}")
    return lock


def platform_entry(lock: dict, platform: str = "windows-amd64") -> dict:
    try:
        entry = lock["platforms"][platform]
    except KeyError:
        raise ValueError(f"原生執行階段 lock 沒有 {platform}") from None
    files = {}
    for component in ("onnxruntime", "msvc_runtime"):
        part = entry.get(component)
        if not part or not part.get("version") or not part.get("wheel_sha256"):
            raise ValueError(f"{platform} 的 {component} 沒有固定版本與 wheel 雜湊")
        for name, file in part["files"].items():
            if not (len(file.get("sha256", "")) == 64 and file.get("bytes", 0) > 0):
                raise ValueError(f"{platform} 的 {component}/{name} 沒有固定大小與 SHA-256")
            files[name] = {**file, "component": component, "version": part["version"]}
    missing = [name for name in DLLS if name not in files]
    if missing:
        raise ValueError(f"{platform} 的 lock 缺少原生 DLL：{', '.join(missing)}")
    return {**entry, "files": files}


def expected(lock: dict, platform: str = "windows-amd64") -> dict:
    return platform_entry(lock, platform)["files"]


def verify_tree(directory: Path, lock: dict = None, platform: str = "windows-amd64") -> dict:
    """Check every pinned DLL exists with the recorded size and SHA-256.

    Returns the platform entry. Unknown extra DLLs are rejected so an accidental
    numpy or onnxruntime Python package cannot ride along; other files (README,
    notices) are ignored.
    """
    lock = lock or load_lock()
    entry = platform_entry(lock, platform)
    files = entry["files"]
    present = {p.name: p for p in Path(directory).iterdir() if p.is_file()}
    for name, want in files.items():
        path = present.get(name)
        if path is None:
            raise ValueError(f"缺少原生執行階段 DLL：{name}（{directory}）")
        if path.stat().st_size != want["bytes"]:
            raise ValueError(f"{name} 大小不符：{path.stat().st_size}，lock 記 {want['bytes']}")
        if digest(path) != want["sha256"]:
            raise ValueError(f"{name} SHA-256 不符：{path}")
    extra = sorted({p.suffix.lower() for p in present.values() if p.name not in files and p.suffix.lower() == ".dll"})
    if extra:
        raise ValueError(f"原生執行階段目錄有 lock 未固定的 DLL：{', '.join(extra)}")
    if python_free(directory):
        raise ValueError("原生執行階段目錄含 Python 檔案：" + ", ".join(python_free(directory)))
    return entry


def fetch(url: str, sha256: str, cache: Path) -> Path:
    cache.mkdir(parents=True, exist_ok=True)
    target = cache / url.rsplit("/", 1)[-1].replace("%2B", "+")
    if not target.exists():
        with tempfile.NamedTemporaryFile(dir=cache, delete=False) as out, urllib.request.urlopen(url) as reply:
            shutil.copyfileobj(reply, out)
        Path(out.name).replace(target)
    if digest(target) != sha256:
        raise ValueError(f"wheel 雜湊不符：{target.name}")
    return target


def extract(lock: dict = None, platform: str = "windows-amd64", out: Path = None,
            cache: Path = None, wheels: dict = None) -> Path:
    """Copy only the pinned DLLs out of the two wheels into ``out``.

    ``wheels`` maps ``onnxruntime``/``msvc_runtime`` to a local wheel path;
    otherwise the wheel is taken from ``cache`` or downloaded from the pinned
    URL. The output directory must not already exist.
    """
    import tempfile

    lock = lock or load_lock()
    entry = platform_entry(lock, platform)
    out = Path(out)
    if out.exists():
        raise ValueError(f"輸出已存在：{out}")
    cache = Path(cache) if cache else ROOT / "build" / "runtime-cache"
    wheels = wheels or {}
    stage = Path(tempfile.mkdtemp(prefix=".0c-runtime-", dir=out.parent))
    try:
        for component in ("onnxruntime", "msvc_runtime"):
            part = entry[component]
            wheel = wheels.get(component)
            wheel = Path(wheel) if wheel else fetch(part["url"], part["wheel_sha256"], cache)
            if digest(wheel) != part["wheel_sha256"]:
                raise ValueError(f"{component} wheel 雜湊不符：{wheel}")
            with zipfile.ZipFile(wheel) as archive:
                for name, file in part["files"].items():
                    try:
                        data = archive.read(file["wheel_path"])
                    except KeyError:
                        raise ValueError(f"{component} wheel 缺少 {file['wheel_path']}") from None
                    if len(data) != file["bytes"] or hashlib.sha256(data).hexdigest() != file["sha256"]:
                        raise ValueError(f"{component} wheel 內的 {name} 與 lock 不符")
                    (stage / name).write_bytes(data)
        verify_tree(stage, lock, platform)
        out.parent.mkdir(parents=True, exist_ok=True)
        stage.replace(out)
    except BaseException:
        shutil.rmtree(stage, ignore_errors=True)
        raise
    return out


def python_free(root: Path) -> list:
    """Return every path that would make the bundle depend on CPython or a wheel."""
    root = Path(root)
    found = []
    for path in sorted(root.rglob("*")):
        relative = path.relative_to(root).as_posix()
        parts = relative.lower().split("/")
        name = path.name.lower()
        if path.is_dir():
            if any(part in PYTHON_DIRS for part in parts):
                found.append(relative)
            continue
        if name in PYTHON_FILES or name.endswith(PYTHON_SUFFIXES):
            found.append(relative)
        elif any(part in PYTHON_DIRS for part in parts[:-1]) and name.endswith((".dll", ".exe")):
            found.append(relative)
    return found


def check_python_free(root: Path) -> None:
    found = python_free(root)
    if found:
        raise ValueError("套件含 Python 或 wheel，產品不得內建 Python：" + ", ".join(found[:20]))


def python_runtime_hits(root: Path) -> list:
    """Return shipped PE files that still contain Python runtime code markers."""
    root = Path(root)
    hits = []
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.suffix.lower() not in (".exe", ".dll", ".pyd"):
            continue
        data = path.read_bytes()
        found = [marker.decode("ascii", "replace") for marker in RUNTIME_MARKERS if marker in data]
        if found:
            hits.append(f"{path.relative_to(root).as_posix()}: {', '.join(sorted(set(found)))}")
    return hits


def check_no_python_runtime(root: Path) -> None:
    """Reject a bundle whose binaries still reference the removed interpreter path."""
    hits = python_runtime_hits(root)
    if hits:
        raise ValueError("產品二進位仍含 Python runtime 路徑：" + "; ".join(hits[:20]))


def describe(entry: dict) -> dict:
    """Stable manifest record for release.json."""
    return {
        "platform": "windows-amd64",
        "onnxruntime": entry["onnxruntime"]["version"],
        "msvc_runtime": entry["msvc_runtime"]["version"],
        "files": {name: {"bytes": file["bytes"], "sha256": file["sha256"]}
                  for name, file in sorted(entry["files"].items())},
    }
