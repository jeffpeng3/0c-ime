#!/usr/bin/env python3
"""Build a Windows x64 bundle on Windows; never install, register, or upload.

The bundle also carries an i686 TSF DLL at x86/0c_tsf.dll for 32-bit applications (#112); the engine,
candidate window and updater stay x64. --ja-data adds an optional data/ja/ Japanese pack; without it the
bundle is byte-for-byte the Chinese-only layout."""
import argparse
import json
import os
from pathlib import Path
import platform
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import zipfile

from install_package import digest, regular
from collect_notices import collect
from package_release import ROOT, DATA_FILES, check_licenses, copy_licenses, check_release_docs, copy_release_docs, check_data, copy_english, copy_ja_data, seal_data
import windows_runtime
import hf_models

# 內嵌 Python 的 ._pth 或 PYTHONPATH 指向另一份原始碼時，會打包出那份程式（2026-10-01 曾因沿用舊建置目錄的
# Python，把 dev.21 的程式標成新版本）。
if ROOT != Path(__file__).resolve().parents[1]:
    raise SystemExit(f"打包模組來自另一份原始碼：{ROOT}；請檢查 Python 的 ._pth 或 PYTHONPATH")

TARGET = "x86_64-pc-windows-msvc"
# 32 位元應用程式（例如 32 位元 Office、HCL Notes 12）只載入 32 位元的 TSF DLL（#112）。只建 DLL：引擎與候選窗
# 由 x64 那份經 named pipe 服務。兩個 target 的 C 執行階段都靜態連結（.cargo/config.toml）：Visual C++ 可轉散發
# 套件不一定有裝，x64 也一樣（#261）。
TARGET32 = "i686-pc-windows-msvc"
DLL32 = "x86/0c_tsf.dll"  # 與 internal/update 的 WindowsDLL32 相同
PLATFORM = ROOT / "platforms/windows"
PE_MACHINE = {"x64": b"\x64\x86", "x86": b"\x4c\x01"}


def check_pe(path, machine="x64"):
    with regular(path).open("rb") as stream:
        if stream.read(2) != b"MZ":
            raise ValueError(f"不是 Windows PE 執行檔：{path}")
        stream.seek(0x3c)
        offset = stream.read(4)
        if len(offset) != 4:
            raise ValueError(f"PE 標頭不完整：{path}")
        stream.seek(struct.unpack("<I", offset)[0])
        if stream.read(6) != b"PE\0\0" + PE_MACHINE[machine]:
            raise ValueError(f"需要 Windows {machine} PE：{path}")


PE_SUBSYSTEM_GUI = 2


def pe_subsystem(path):
    """PE 選用標頭的 Subsystem 欄位（2 是 GUI、3 是主控台）；32／64 位元的位移相同。"""
    data = regular(path).read_bytes()
    if data[:2] != b"MZ":
        raise ValueError(f"不是 Windows PE 執行檔：{path}")
    pe = struct.unpack_from("<I", data, 0x3C)[0]
    if data[pe:pe + 4] != b"PE\0\0":
        raise ValueError(f"PE 標頭不完整：{path}")
    return struct.unpack_from("<H", data, pe + 24 + 68)[0]


def pe_imports(path):
    """PE 匯入表與延遲載入表列出的 DLL 名稱，依出現順序；不在 Windows 上也能讀（只解析檔案）。"""
    data = regular(path).read_bytes()
    try:
        if data[:2] != b"MZ":
            raise ValueError
        pe = struct.unpack_from("<I", data, 0x3C)[0]
        if data[pe:pe + 4] != b"PE\0\0":
            raise ValueError
        count, optional_size = struct.unpack_from("<H", data, pe + 6)[0], struct.unpack_from("<H", data, pe + 20)[0]
        optional = pe + 24
        magic = struct.unpack_from("<H", data, optional)[0]
        directories = optional + {0x10B: 96, 0x20B: 112}[magic]
        image_base = (struct.unpack_from("<I", data, optional + 28) if magic == 0x10B else struct.unpack_from("<Q", data, optional + 24))[0]
        directory_count = struct.unpack_from("<I", data, directories - 4)[0]
        sections = [struct.unpack_from("<IIII", data, optional + optional_size + 40 * i + 8) for i in range(count)]

        def offset(rva):
            for virtual_size, address, raw_size, raw in sections:
                if address <= rva < address + max(virtual_size, raw_size):
                    return raw + rva - address
            raise ValueError

        def name(rva):
            start = offset(rva)
            return data[start:data.index(b"\0", start)].decode("ascii")

        names = []
        # 1：匯入表（每筆 20 位元組，DLL 名稱在 +12）；13：延遲載入表（32 位元組，名稱在 +4，屬性沒有 1 時是 VA）。
        for index, size, field in ((1, 20, 12), (13, 32, 4)):
            if index >= directory_count:
                continue
            rva = struct.unpack_from("<I", data, directories + 8 * index)[0]
            if not rva:
                continue
            at = offset(rva)
            while any(entry := data[at:at + size]):
                if len(entry) < size:
                    raise ValueError
                value = struct.unpack_from("<I", entry, field)[0]
                if index == 13 and not struct.unpack_from("<I", entry, 0)[0] & 1:
                    value -= image_base
                names.append(name(value))
                at += size
        return names
    except (ValueError, KeyError, IndexError, struct.error, UnicodeDecodeError):
        raise ValueError(f"不是可解析的 Windows PE：{path}") from None


# Visual C++ 可轉散發套件的 DLL；UCRT（api-ms-win-crt-*、ucrtbase.dll）是 Windows 10 起內建，不在此列。
VC_REDIST = re.compile(r"^(vcruntime|msvcp|concrt|vccorlib|vcomp|mfc)\d", re.IGNORECASE)


def check_static_crt(path):
    """我們建的執行檔與 DLL 不得依賴 VC++ 可轉散發套件（#261）：沒裝的電腦 regsvr32 載不了 0c_tsf.dll，
    安裝精靈在註冊那步失敗。Rust 由 platforms/windows/.cargo/config.toml 的 +crt-static 靜態連結；設了
    RUSTFLAGS 環境變數會蓋掉那個設定，所以打包時再檢查一次。"""
    needed = [name for name in pe_imports(path) if VC_REDIST.match(name)]
    if needed:
        raise ValueError(f"{Path(path).name} 需要 Visual C++ 可轉散發套件（{', '.join(needed)}），沒裝的電腦載入不了；"
                         "請以 -C target-feature=+crt-static 建置，並確認沒有設定 RUSTFLAGS")


# 檔案內容（說明）出現在檔案內容對話框、工作管理員與 SmartScreen／Defender 的報告；發行者與 Artifact Signing 的
# 法定名稱一致。
PUBLISHER = "FUNMULA PTE. LTD."
PRODUCT = "靈犀注音（0c）"
DESCRIPTIONS = {
    "0c_tsf.dll": "靈犀注音輸入法（TSF 文字服務）",
    "0c-ui.exe": "靈犀注音候選窗",
    "0c-server.exe": "靈犀注音引擎",
    "0c-update.exe": "靈犀注音安裝與更新工具",
    "0c-settings.exe": "靈犀注音設定程式",
    "0c-mcp.exe": "靈犀注音 AI 工具轉接（MCP）",
    "0c-updatew.exe": "靈犀注音背景更新啟動器",
}


def file_version(version):
    """數字版本只取 x.y.z.0（與安裝精靈相同）；預覽序號留在字串欄位，避免正式版的數字反而比預覽版小。"""
    return tuple(int(part) for part in version.split("-")[0].split(".")) + (0,)


def version_info(name, version, dll):
    """VS_VERSIONINFO 資源（RT_VERSION）：固定欄位＋一個 zh-TW／UTF-16 字串表。"""
    def pad(data):
        return data + b"\0" * (-len(data) % 4)

    def node(key, value=b"", text=False, children=b""):
        # 每個節點：wLength、wValueLength（文字以字元計、二進位以位元組計）、wType，鍵名與值各自對齊 4 位元組。
        key_part = pad(b"\0" * 6 + (key + "\0").encode("utf-16-le"))[6:]
        rest = key_part + pad(value) + children
        value_length = len(value) // 2 if text else len(value)
        return pad(struct.pack("<HHH", 6 + len(rest), value_length, 1 if text else 0) + rest)

    def text(key, value):
        return node(key, (value + "\0").encode("utf-16-le"), text=True)

    major, minor, patch, build = file_version(version)
    fixed = struct.pack("<13I", 0xFEEF04BD, 0x00010000, major << 16 | minor, patch << 16 | build,
                        major << 16 | minor, patch << 16 | build, 0x3F, 0, 0x00040004, 2 if dll else 1, 0, 0, 0)
    strings = b"".join(text(k, v) for k, v in (
        ("CompanyName", PUBLISHER), ("FileDescription", DESCRIPTIONS[name]), ("FileVersion", version),
        ("InternalName", name.rsplit(".", 1)[0]), ("LegalCopyright", f"© {PUBLISHER}"),
        ("OriginalFilename", name), ("ProductName", PRODUCT), ("ProductVersion", version)))
    table = node("040404B0", children=strings)
    translation = node("Translation", struct.pack("<HH", 0x0404, 1200))
    children = node("StringFileInfo", children=table) + node("VarFileInfo", children=translation)
    return node("VS_VERSION_INFO", fixed, children=children)


def existing_version_languages(path):
    """檔案內已有的版本資源（RT_VERSION、ID 1）的語言；沒有時為空。Tauri 編出的 exe 已經帶一份（0.1.0、
    識別碼當公司名），不先移除，檔案會同時有兩份不同語言的版本資訊。"""
    import ctypes
    from ctypes import wintypes
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel32.LoadLibraryExW.restype = wintypes.HMODULE
    kernel32.LoadLibraryExW.argtypes = [wintypes.LPCWSTR, wintypes.HANDLE, wintypes.DWORD]
    kernel32.EnumResourceLanguagesW.argtypes = [wintypes.HMODULE, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_ssize_t]
    kernel32.FreeLibrary.argtypes = [wintypes.HMODULE]
    module = kernel32.LoadLibraryExW(str(path), None, 0x2)  # LOAD_LIBRARY_AS_DATAFILE
    if not module:
        raise OSError(ctypes.get_last_error(), f"無法讀取檔案資源：{path}")
    languages = []
    callback_type = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HMODULE, ctypes.c_void_p, ctypes.c_void_p, wintypes.WORD, ctypes.c_ssize_t)

    def collect_language(_module, _kind, _name, language, _param):
        languages.append(language)
        return True

    callback = callback_type(collect_language)
    try:
        kernel32.EnumResourceLanguagesW(module, 16, 1, ctypes.cast(callback, ctypes.c_void_p), 0)  # 沒有這個資源時回傳失敗，languages 維持空
    finally:
        kernel32.FreeLibrary(module)
    return languages


def stamp_version(path, version):
    """以 Win32 UpdateResource 寫入版本資訊；必須在簽章與計算雜湊之前。"""
    import ctypes
    from ctypes import wintypes
    languages = existing_version_languages(path)
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel32.BeginUpdateResourceW.restype = wintypes.HANDLE
    kernel32.BeginUpdateResourceW.argtypes = [wintypes.LPCWSTR, wintypes.BOOL]
    kernel32.UpdateResourceW.argtypes = [wintypes.HANDLE, ctypes.c_void_p, ctypes.c_void_p, wintypes.WORD,
                                         ctypes.c_void_p, wintypes.DWORD]
    kernel32.EndUpdateResourceW.argtypes = [wintypes.HANDLE, wintypes.BOOL]
    data = version_info(path.name, version, path.suffix.lower() == ".dll")
    handle = kernel32.BeginUpdateResourceW(str(path), False)
    if not handle:
        raise OSError(ctypes.get_last_error(), f"無法寫入版本資訊：{path}")
    ok = all(kernel32.UpdateResourceW(handle, 16, 1, language, None, 0) for language in languages if language != 0x0404)
    buffer = ctypes.create_string_buffer(data, len(data))
    ok = ok and kernel32.UpdateResourceW(handle, 16, 1, 0x0404, buffer, len(data))  # RT_VERSION、ID 1
    if not kernel32.EndUpdateResourceW(handle, not ok) or not ok:
        raise OSError(ctypes.get_last_error(), f"無法寫入版本資訊：{path}")


TIMESTAMP_URL = "http://timestamp.acs.microsoft.com"
PE_SUFFIXES = (".exe", ".dll", ".pyd")
# 內附的原生 ONNX Runtime 與 MSVC 執行階段；固定版本與 SHA-256 在 models/runtime.lock.json。
# 產品不內建 CPython、numpy 或 onnxruntime Python 套件。
RUNTIME_DLLS = windows_runtime.DLLS


def signer(path, args):
    """回傳 signtool verify /pa 認可的簽署者（葉憑證的 Issued to）；未簽章或驗證失敗時為 None。"""
    run = subprocess.run([str(args.signtool), "verify", "/pa", "/v", str(path)], capture_output=True, text=True,
                         encoding="utf-8", errors="replace")
    if run.returncode != 0:
        return None
    return leaf_signer(run.stdout)


def leaf_signer(output):
    """signtool verify /v 先列簽章憑證鏈（根到葉），之後才是時間戳的憑證鏈；取時間戳段落之前最後一個 Issued to。"""
    names = []
    for line in output.splitlines():
        line = line.strip()
        if line.startswith("The signature is timestamped") or line.startswith("Timestamp Verified by"):
            break
        if line.startswith("Issued to:"):
            names.append(line.split(":", 1)[1].strip())
    return names[-1] if names else None


def authenticode_sign(paths, args):
    """以 Artifact Signing（signtool＋dlib）簽章並加時間戳，簽完逐檔核對發行者。"""
    if not paths:
        return
    subprocess.run([str(args.signtool), "sign", "/fd", "SHA256", "/tr", TIMESTAMP_URL, "/td", "SHA256",
                    "/dlib", str(args.sign_dlib), "/dmdf", str(args.sign_metadata), *map(str, paths)], check=True)
    for path in paths:
        if signer(path, args) != PUBLISHER:
            raise ValueError(f"簽章後核對失敗（發行者不是 {PUBLISHER}）：{path}")


def unsigned_pe(root, args):
    """內附元件中沒有有效原廠簽章的 PE（例如 numpy 的 .pyd）；已有其他發行者簽章的保留原狀。"""
    return [p for p in sorted(root.rglob("*")) if p.is_file() and p.suffix.lower() in PE_SUFFIXES and signer(p, args) is None]


def check_settings(directory):
    """檢查 scripts/build_settings_windows.py 的輸出（0c-settings.exe 與 NOTICES.txt），回傳 (執行檔, 授權文字)。

    不在打包流程裡建置：Tauri 需要另一個 Rust 工作區與較長的編譯時間，改由事先建好再交給打包；
    這裡只確認它是 Windows x64 的 PE 而不是別的東西。
    """
    program = regular(Path(directory) / "0c-settings.exe")
    check_pe(program)
    notices = regular(Path(directory) / "NOTICES.txt").read_text(encoding="utf-8")
    if not notices.strip():
        raise ValueError("設定程式缺少第三方授權聲明")
    return program, notices


def check_runtime(directory):
    """核對原生 ONNX Runtime 目錄：只有 lock 固定的 onnxruntime.dll 與四個 MSVC DLL。

    版本、大小與 SHA-256 都在 models/runtime.lock.json；回傳 lock 的 platform entry，
    供 release.json 記錄實際出貨的 runtime。
    """
    entry = windows_runtime.verify_tree(directory)
    for dll in RUNTIME_DLLS:
        check_pe(Path(directory) / dll)
    return entry


RUNTIME_NOTICE = """onnxruntime.dll
    ONNX Runtime 1.30.0
    Copyright (c) Microsoft Corporation. MIT License.
    https://github.com/microsoft/onnxruntime/blob/main/LICENSE

vcruntime140.dll, vcruntime140_1.dll, msvcp140.dll, msvcp140_1.dll
    Microsoft Visual C++ 2022 Runtime 14.44.35112
    Redistributable per the Microsoft Visual Studio license terms.
"""


def python_free(stage):
    """產品不得內建 Python：完成品出現 .py／.pyc／.pyd、wheel、numpy 或 onnxruntime Python 套件就失敗。"""
    windows_runtime.check_python_free(stage)


def load_model_sets(path=None):
    """Read models/pairing.lock.json's P2-27 model_sets section."""
    path = Path(path) if path else ROOT / "models" / "pairing.lock.json"
    with open(path, encoding="utf-8") as stream:
        lock = json.load(stream)
    section = lock.get("model_sets")
    if not section or section.get("schema") != "0c.model-sets/1" or not section.get("sets"):
        raise ValueError(f"模型組清單沒有 model_sets：{path}")
    return section


def check_model_set(set_id, format, allow_placeholders, path=None):
    """Validate one pairing-lock model set before staging it.

    Placeholder (untrained) sets are never the default: shipping one requires an
    explicit --model-set plus --allow-placeholders, and the bundle gets a marker.
    """
    section = load_model_sets(path)
    sets = section["sets"]
    if set_id not in sets:
        raise ValueError(f"未知的模型組 {set_id!r}；可用：{', '.join(sorted(sets))}")
    chosen = sets[set_id]
    if chosen.get("variant") not in ("public-core", "cc-by-sa"):
        raise ValueError(f"模型組 {set_id} 的 variant 不正確：{chosen.get('variant')}")
    if chosen.get("placeholder") and not allow_placeholders:
        raise ValueError(f"模型組 {set_id} 是 placeholder（quality={chosen.get('quality')}）；"
                         "要出貨必須明確加 --allow-placeholders")
    for artifact in chosen.get("artifacts", []):
        if artifact.get("variant") != chosen["variant"]:
            raise ValueError(f"模型組 {set_id} 混用 variant")
        if format not in artifact.get("files", {}):
            raise ValueError(f"模型組 {set_id} 沒有 {format} 檔案")
    return chosen


def check_model_pair(directory, data):
    """P2-43：Windows 產品仍出貨配對好的注音 select／punct ONNX，但沒有 Python worker。

    directory 是 hf_models.py pair 的輸出（select/、punct/、pairing.lock.json）。
    """
    directory = Path(directory)
    for role in ("select", "punct"):
        if not regular(directory / role / "manifest.json"):
            raise ValueError(f"模型配對目錄缺少 {role}/manifest.json：{directory}")
    expected = hf_models.check_shipped(directory, Path(data), workers=None)
    lock = directory / "pairing.lock.json"
    if not regular(lock) or lock.read_bytes() != expected:
        raise ValueError(f"模型配對目錄的 pairing.lock.json 與出貨字典不符：{directory}")
    return directory


def copy_model_pair(source, stage):
    """只複製模型檔與清單，不複製任何 Python worker。"""
    source, stage = Path(source), Path(stage)
    for path in sorted(source.rglob("*")):
        if path.is_symlink() or not path.is_file():
            continue
        if path.suffix in (".py", ".pyc", ".pyd", ".whl"):
            raise ValueError(f"模型配對目錄含 Python 檔案：{path}")
        target = stage / "neural" / path.relative_to(source)
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)


def model_set_manifest(set_id, chosen, format):
    """Stable release.json record; makes the placeholder status impossible to miss."""
    return {
        "set_id": set_id,
        "variant": chosen["variant"],
        "version": chosen.get("version", ""),
        "format": format,
        "placeholder": bool(chosen.get("placeholder")),
        "quality": chosen.get("quality", ""),
        "production_eligible": bool(chosen.get("production_eligible")),
        "dictionary": chosen.get("dictionary", {}),
    }


def server_version(args, binary):
    """E2E fault package F: the engine reports a different version, so the post-switch health check fails."""
    if args.e2e_fault == "version-mismatch" and binary == "0c-server":
        return args.version + "-fault"
    return args.version


def runtime_plan(ort_runtime, statistical_only, neural):
    """Validate the shipped inference choice; returns "native" or "statistical".

    The shipped product must carry the native runtime. --statistical-only exists
    for diagnostic previews and --neural is rejected outright.
    """
    if neural:
        raise ValueError("產品不內建 Python；請移除 --neural，改用 --ort-runtime 出貨原生 ONNX Runtime")
    if ort_runtime and statistical_only:
        raise ValueError("--ort-runtime 與 --statistical-only 只能擇一")
    if not ort_runtime and not statistical_only:
        raise ValueError("需要 --ort-runtime（原生 ONNX Runtime）；只有診斷用的純統計套件可加 --statistical-only")
    return "native" if ort_runtime else "statistical"


def build(args):
    if sys.platform != "win32" or platform.machine().lower() not in ("amd64", "x86_64"):
        raise ValueError("請在 Windows x64 的建置環境執行")
    data = args.data.resolve()
    manifest = check_data(data, not args.preview)
    check_licenses()
    release_docs = check_release_docs(getattr(args, "release_docs", None))
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = bool(subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip())
    if dirty and not args.preview:
        raise ValueError("公開包需要乾淨且已提交的程式碼；未提交版本請用 --preview")
    # Keep version syntax identical to the updater before using it in a path.
    from package_release import valid_version
    if not valid_version(args.version):
        raise ValueError("版本請用 0.1.0-beta.1 形式")
    settings, settings_notices = check_settings(args.settings) if args.settings else (None, "")
    runtime_plan(getattr(args, "ort_runtime", None), getattr(args, "statistical_only", False), getattr(args, "neural", None))
    runtime = check_runtime(args.ort_runtime) if getattr(args, "ort_runtime", None) else None
    model_set = None
    if getattr(args, "model_set", None):
        if not getattr(args, "model_source", None):
            raise ValueError("--model-set 需要 --model-source（已匯出的 ONNX 來源目錄）")
        if not args.ja_data:
            raise ValueError("--model-set 需要 --ja-data：模型組綁定日文資料包的 manifest")
        model_set = check_model_set(args.model_set, getattr(args, "model_format", "int8"), getattr(args, "allow_placeholders", False))
    elif getattr(args, "model_source", None) or getattr(args, "allow_placeholders", False):
        raise ValueError("--model-source／--allow-placeholders 必須搭配 --model-set")
    model_pair = None
    if getattr(args, "model_pair", None):
        model_pair = check_model_pair(args.model_pair, data)
    if runtime and not model_pair:
        raise ValueError("原生產品需要 --model-pair：hf_models.py pair 產生的 select/、punct/ 與配對清單")
    if model_pair and not runtime:
        raise ValueError("--model-pair 只能搭配 --ort-runtime（產品不使用 Python worker）")
    args.out.mkdir(parents=True, exist_ok=True)
    name = f"0c-{args.version}-windows-amd64" + ("-preview" if args.preview else "")
    archive = args.out / (name + ".zip")
    checksum = archive.with_suffix(".zip.sha256")
    if archive.exists() or checksum.exists():
        raise ValueError(f"輸出已存在，請使用新版本或新目錄：{archive}")
    with tempfile.TemporaryDirectory(prefix="0c-windows-") as temp:
        stage = Path(temp) / name
        stage.mkdir()
        def copy(src, dst):
            target = stage / dst
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(regular(src), target)
        # Force the documented target even if the shell has another default toolchain. 兩個 target 都由
        # platforms/windows/.cargo/config.toml 靜態連結 C 執行階段（#261），下面 check_static_crt 再核對。
        subprocess.run([args.cargo, "build", "--locked", "--release", "--target", TARGET], cwd=PLATFORM, check=True)
        for binary in ("0c_tsf.dll", "0c-ui.exe"):
            copy(PLATFORM / "target" / TARGET / "release" / binary, binary)
        subprocess.run([args.cargo, "build", "--locked", "--release", "--target", TARGET32, "-p", "zuin-tsf"],
                       cwd=PLATFORM, check=True)
        copy(PLATFORM / "target" / TARGET32 / "release" / "0c_tsf.dll", DLL32)
        env = dict(os.environ, GOOS="windows", GOARCH="amd64", CGO_ENABLED="0")
        # 原生 ONNX 後端：0c-server 以 CGO＋onnxruntime build tag 建置（Windows 需要 mingw 的 gcc）；
        # 只有這一支帶 CGO。更新器、MCP 與啟動器維持純 Go（CGO_ENABLED=0），CGO-off 的伺服器路徑仍可建置。
        server_env = dict(env)
        server_tags = []
        if runtime:
            server_env.update(CGO_ENABLED="1", CC=args.cc)
            server_tags.append("onnxruntime")
        if args.e2etest:
            server_tags.append("e2etest")
        if args.e2etest and not args.preview:
            raise ValueError("--e2etest 只能用於 --preview 套件")
        server_tag_flag = ["-tags", ",".join(server_tags)] if server_tags else []
        subprocess.run([args.go, "build", "-trimpath", *server_tag_flag,
                        "-ldflags", f"-X main.buildVersion={server_version(args, '0c-server')}",
                        "-o", str(stage / "0c-server.exe"), "./cmd/0c-server"],
                       cwd=ROOT, env=server_env, check=True)
        # 0c-mcp：只支援 stdio 的 AI 工具用的轉接（#126）；0c-update 安裝時把它複製到不隨版本變動的
        # %LOCALAPPDATA%\Zuin\bin\0c-mcp.exe（穩定副本），設定程式把那個路徑填進範例。
        for binary in ("0c-update", "0c-mcp"):
            tags = ["-tags", "e2etest"] if args.e2etest and binary == "0c-update" else []
            subprocess.run([args.go, "build", "-trimpath", *tags, "-ldflags", f"-X main.buildVersion={server_version(args, binary)}",
                            "-o", str(stage / (binary + ".exe")), f"./cmd/{binary}"],
                           cwd=ROOT, env=env, check=True)
        # 0c-updatew：排程與通知按鈕執行的啟動器（#289），GUI 子系統、沒有主控台，取代 conhost --headless。
        subprocess.run([args.go, "build", "-trimpath", "-ldflags", "-H windowsgui", "-o", str(stage / "0c-updatew.exe"),
                        "./cmd/0c-updatew"], cwd=ROOT, env=env, check=True)
        if pe_subsystem(stage / "0c-updatew.exe") != PE_SUBSYSTEM_GUI:
            raise ValueError("0c-updatew.exe 必須是 GUI 子系統，否則排程會閃出主控台視窗")
        if settings:
            copy(settings, "0c-settings.exe")
        ours = [stage / b for b in ("0c_tsf.dll", "0c-ui.exe", "0c-server.exe", "0c-update.exe", "0c-mcp.exe", "0c-updatew.exe")
                + (("0c-settings.exe",) if settings else ())]
        for binary in ours:
            check_pe(binary)
        check_pe(stage / DLL32, "x86")
        ours.append(stage / DLL32)
        for binary in ours:
            check_static_crt(binary)
        for binary in ours:
            stamp_version(binary, args.version)
        # 先寫版本資訊再簽章；之後才計算 release.json 與 SHA256SUMS 的雜湊。
        if args.sign:
            authenticode_sign(ours, args)
        for filename in DATA_FILES:
            copy(data / filename, "data/" + filename)
        copy_english(data, stage / "data", args.go)
        if model_pair:
            copy_model_pair(model_pair, stage)
        if args.ja_data:
            copy_ja_data(args.ja_data, stage / "data", public=not args.preview)
        if model_set:
            # P2-27：只安裝 pairing lock 選中的那一組；public-core 與 cc-by-sa 不混用。
            # 0c-model-set 會逐檔核對大小與 SHA-256 並寫出 active.json。
            tool = Path(temp) / "0c-model-set.exe"
            subprocess.run([args.go, "build", "-trimpath", "-o", str(tool), "./cmd/0c-model-set"],
                           cwd=ROOT, env=env, check=True)
            command = [str(tool), "install", "-set", args.model_set, "-from", str(args.model_source.resolve()),
                       "-to", str(stage / "models"), "-format", getattr(args, "model_format", "int8"),
                       "-lock", str(ROOT / "models" / "pairing.lock.json"), "-pack", str(args.ja_data.resolve())]
            if getattr(args, "allow_placeholders", False):
                command.append("-allow-placeholders")
            subprocess.run(command, cwd=ROOT, env=env, check=True)
            shutil.copy2(ROOT / "models" / "pairing.lock.json", stage / "models" / "pairing.lock.json")
            marker = model_set_manifest(args.model_set, model_set, getattr(args, "model_format", "int8"))
            (stage / "MODEL-SET.txt").write_text(
                f"set_id: {marker['set_id']}\nvariant: {marker['variant']}\nversion: {marker['version']}\n"
                f"format: {marker['format']}\nplaceholder: {'yes' if marker['placeholder'] else 'no'}\n"
                f"quality: {marker['quality']}\n", encoding="utf-8")
            if marker["placeholder"]:
                (stage / "MODEL-SET-PLACEHOLDER.txt").write_text(
                    "本套件的模型組是未訓練 placeholder（quality=" + marker["quality"] + "），"
                    "僅供安裝、更新與回退流程驗證，不是正式模型。\n", encoding="utf-8")
        if runtime:
            for dll in RUNTIME_DLLS:
                copy(args.ort_runtime / dll, dll)
            if args.sign:
                for dll in RUNTIME_DLLS:
                    if not (signer(stage / dll, args) or "").startswith("Microsoft"):
                        raise ValueError(f"內附的 {dll} 不是 Microsoft 簽章")
        seal_data(stage, args.go, public=not args.preview)
        copy(PLATFORM / "install.ps1", "install.ps1")
        copy_release_docs(release_docs, stage)
        copy_licenses(stage)
        (stage / "THIRD_PARTY_NOTICES.txt").write_text(
            collect("windows-amd64", None, go=args.go, cargo=args.cargo)
            + ("\n" + RUNTIME_NOTICE if runtime else "")
            + ("\n" + settings_notices if settings_notices else ""), encoding="utf-8")
        if args.preview:
            (stage / "PREVIEW.txt").write_text("內部驗證包，非公開發行版。\n", encoding="utf-8")
        # 最後一道產品邊界：不得有 CPython、numpy、onnxruntime Python 套件、worker 或 wheel。
        python_free(stage)
        subprocess.run([str(stage / "0c-server.exe"), "-check-data", "-data", str(stage / "data")], check=True)
        release = {"schema": 1, "product": "0c", "name": "靈犀注音", "version": args.version,
                   "channel": "internal-preview" if args.preview else "public-beta", "os": "windows", "arch": "amd64",
                   "revision": revision, "dirty": dirty, "build_platform": platform.platform(),
                   "data_format": manifest["format"], "data_manifest_sha256": digest(stage / "data/manifest.json"),
                   "files": {p.relative_to(stage).as_posix(): digest(p) for p in sorted(stage.rglob("*")) if p.is_file()}}
        if runtime:
            release["native_runtime"] = windows_runtime.describe(runtime)
        else:
            release["native_runtime"] = {"backend": "statistical", "onnxruntime": None}
        if model_set:
            release["model_set"] = model_set_manifest(args.model_set, model_set, getattr(args, "model_format", "int8"))
        if model_pair:
            release["model_pair"] = {"dictionary_manifest_sha256": digest(stage / "data/manifest.json"),
                                     "pairing_lock_sha256": digest(stage / "neural" / "pairing.lock.json")}
        if args.ja_data:
            release["ja_data_format"] = 2
            release["ja_data_manifest_sha256"] = digest(stage / "data/ja/manifest.json")
        (stage / "release.json").write_text(json.dumps(release, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        subprocess.run([str(stage / "0c-update.exe"), "verify", "-bundle", str(stage)], check=True)
        (stage / "SHA256SUMS").write_text("".join(f"{digest(p)}  {p.relative_to(stage).as_posix()}\n"
                                                 for p in sorted(stage.rglob("*")) if p.is_file() and p.name != "SHA256SUMS"), encoding="utf-8")
        fd, temporary = tempfile.mkstemp(prefix=".0c-", dir=args.out)
        os.close(fd)
        try:
            with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as zip_file:
                for p in sorted(stage.rglob("*")):
                    if p.is_file():
                        zip_file.write(p, f"{name}/{p.relative_to(stage).as_posix()}")
            os.replace(temporary, archive)
        finally:
            Path(temporary).unlink(missing_ok=True)
        checksum.write_text(f"{digest(archive)}  {archive.name}\n", encoding="utf-8")
    return archive


def main():
    parser = argparse.ArgumentParser(description="建立靈犀注音 Windows x64 完整套件；不安裝、不註冊、不上傳")
    parser.add_argument("--release-docs", required=True, type=Path, help="外部文件目錄，須含 INSTALL.md、PRIVACY.md、UPDATING.md")
    parser.add_argument("--data", required=True, type=Path)
    parser.add_argument("--ja-data", type=Path, help="0c-compile -lang ja -public 產生的日文資料包（含可選 symbols/）；放進 data/ja/")
    parser.add_argument("--version", required=True)
    parser.add_argument("--out", type=Path, default=ROOT / "build/release")
    parser.add_argument("--preview", action="store_true")
    parser.add_argument("--e2e-fault", choices=["version-mismatch"], help="E2E 故障套件 F：引擎回報不符的版本（須搭配 --e2etest）")
    parser.add_argument("--e2etest", action="store_true", help="E2E 測試版：更新器信任測試金鑰／CA，更新器與引擎包含可控同步點；須搭配 --preview")
    parser.add_argument("--go", default="go")
    parser.add_argument("--neural", type=Path, help="已移除：產品不內建 Python，指定時直接失敗")
    parser.add_argument("--ort-runtime", type=Path,
                        help="prepare_windows_runtime.py 產生的原生執行階段目錄（onnxruntime.dll 與四個 MSVC DLL）；必要")
    parser.add_argument("--statistical-only", action="store_true",
                        help="診斷用純統計套件：不建置 CGO 原生後端、不出貨 ONNX Runtime（不得用於正式發行）")
    parser.add_argument("--cc", default="gcc", help="CGO 使用的 C 編譯器（Windows 上是 mingw 的 gcc）")
    parser.add_argument("--model-set", help="pairing.lock 的模型組 ID（ja-public-core、ja-cc-by-sa）；不指定則不出貨日文模型組")
    parser.add_argument("--model-pair", type=Path,
                        help="hf_models.py pair 產生的注音 select/、punct/ 與 pairing.lock.json；原生產品必要")
    parser.add_argument("--model-source", type=Path, help="已匯出／下載的模型根目錄，內含各 artifact 路徑（搭配 --model-set）")
    parser.add_argument("--model-format", choices=("int8", "fp32"), default="int8")
    parser.add_argument("--allow-placeholders", action="store_true",
                        help="明確允許安裝未訓練 placeholder 模型組；完成品會標記 MODEL-SET-PLACEHOLDER.txt")
    parser.add_argument("--cargo", default="cargo")
    parser.add_argument("--settings", type=Path, help="scripts/build_settings_windows.py 產生的目錄（0c-settings.exe 與 NOTICES.txt）；省略則不內附設定程式")
    parser.add_argument("--signtool", type=Path, help="Windows SDK x64 signtool.exe")
    parser.add_argument("--sign-dlib", type=Path, help="Artifact Signing 用戶端的 Azure.CodeSigning.Dlib.dll")
    parser.add_argument("--sign-metadata", type=Path, help="Artifact Signing metadata.json（端點、帳戶、憑證設定檔）")
    try:
        args = parser.parse_args()
        sign = (args.signtool, args.sign_dlib, args.sign_metadata)
        if any(sign) and not all(sign):
            parser.error("簽章需要同時指定 --signtool、--sign-dlib、--sign-metadata")
        args.sign = all(sign)
        if not args.preview and not args.sign:
            parser.error("公開包必須簽章：請指定 --signtool、--sign-dlib、--sign-metadata")
        if args.e2etest and not args.preview:
            parser.error("--e2etest 只能用於 --preview 套件")
        if args.e2e_fault and not args.e2etest:
            parser.error("--e2e-fault 只能用於 --e2etest 套件")
        print(build(args))
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        print(f"打包失敗：{error}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
