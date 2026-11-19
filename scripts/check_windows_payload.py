#!/usr/bin/env python3
"""檢查 Windows 安裝包是否完整，並驗證設定程式解析到的路徑真的在包裡。

P2-50：先前的預覽包缺 0c-settings.exe 與 bin\\ 的 0c-server.exe，模型頁因此顯示
「找不到 0c-server」或載入不存在的 %LOCALAPPDATA%\\Zuin\\data。這個檢查把安裝包當成
一次完整的安裝：解壓到 versions\\<版本>-<雜湊>\\、寫入收據，執行套件內的
0c-update status -json，確認它回報的 server 與 data 都存在，再用那個 data 執行
0c-server -check-data。任何缺少的檔、Python 檔或不該出現的 x86 執行檔都會讓打包失敗。
"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import struct
import subprocess
import sys
import tempfile
import zipfile

from install_package import digest
from windows_runtime import DLLS, PYTHON_DIRS, PYTHON_FILES, PYTHON_SUFFIXES

ROOT = Path(__file__).resolve().parents[1]

EXPECTED = (
    "0c-server.exe", "0c-ui.exe", "0c-settings.exe", "0c_tsf.dll", "x86/0c_tsf.dll",
    "0c-update.exe", "0c-updatew.exe", "0c-mcp.exe",
    *DLLS,
    "data/manifest.json", "data/lexicon.bin", "data/lm.bin",
    "data/NOTICE.txt", "data/report.txt",
    "data/english/manifest.json", "data/english/words.bin",
    "data/ja/manifest.json", "data/ja/lexicon.bin", "data/ja/lm.bin",
    "data/ja/lexicon/words.tsv", "data/ja/connection.tsv",
    "neural/select/manifest.json", "neural/select/model.onnx",
    "neural/punct/manifest.json", "neural/punct/model.onnx",
    "models/active.json", "models/pairing.lock.json",
    "models/reranker-extra/model.int8.onnx", "models/reranker-extra/manifest.json",
    "models/reranker-extra/vocab.json", "models/reranker-extra/tokenizer.model",
    "models/punctuation-public-core/punctuation-public-core.int8.onnx",
    "models/punctuation-public-core/manifest.json",
    "models/punctuation-public-core/vocab.json",
    "models/punctuation-public-core/tokenizer.model",
    "MODEL-PACKAGE.txt", "MODEL-PACKAGE-CC-BY-SA-4.0.txt", "MODEL-PACKAGE-NOTICES.txt",
    "LICENSE", "TERMS.md", "THIRD_PARTY_NOTICES.txt", "legal/README.md",
    "INSTALL.md", "PRIVACY.md", "UPDATING.md", "install.ps1", "release.json",
)

# 只允許 32 位元的 TSF DLL；其他 PE 必須是 x64。
X86_ALLOWED = {"x86/0c_tsf.dll"}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def pe_machine(data):
    if len(data) < 0x40 or data[:2] != b"MZ":
        raise ValueError("不是 PE 檔")
    offset = struct.unpack_from("<I", data, 0x3C)[0]
    if data[offset:offset + 4] != b"PE\0\0" or len(data) < offset + 6:
        raise ValueError("PE 標頭不完整")
    return struct.unpack_from("<H", data, offset + 4)[0]


def peek_archive(archive):
    """回傳 (root, files, release)：root 是 release.json 所在的目錄前綴。"""
    with zipfile.ZipFile(archive) as package:
        entries = [info for info in package.infolist() if not info.is_dir()]
        names = [info.filename for info in entries]
        if len(entries) > 20000:
            raise ValueError("安裝包檔案數過多")
        if sum(info.file_size for info in entries) > 4 << 30:
            raise ValueError("安裝包解壓後過大")
        releases = [name for name in names if name.endswith("/release.json")]
        if len(releases) != 1:
            raise ValueError("安裝包必須有唯一一份 release.json")
        root = releases[0].rsplit("/", 1)[0] + "/"
        seen = set()
        files = {}
        for info in entries:
            name = info.filename
            parts = PurePosixPath(name).parts
            if ("\\" in name or ":" in name or name.startswith("/") or ".." in parts
                    or not name.startswith(root) or name.casefold() in seen):
                raise ValueError(f"安裝包路徑無效或重複：{name}")
            seen.add(name.casefold())
            files[name[len(root):]] = info
        release = json.loads(package.read(releases[0]))
        for rel in files:
            if rel in ("release.json", "SHA256SUMS"):
                continue
            if rel not in release.get("files", {}):
                raise ValueError(f"安裝包含 release.json 未列出的檔案：{rel}")
        for rel, want in release.get("files", {}).items():
            info = files.get(rel)
            if info is None:
                raise ValueError(f"release.json 列出的檔案不存在：{rel}")
            with package.open(info) as stream:
                if sha(stream.read()) != want:
                    raise ValueError(f"套件雜湊不符：{rel}")
    return root, files, release


def check_entries(files, release):
    missing = [name for name in EXPECTED if name not in files]
    if missing:
        raise ValueError("安裝包缺少必要檔案：" + ", ".join(missing))
    if "data/lexicon.tsv" in files:
        raise ValueError("安裝包不應再附 data/lexicon.tsv；執行期使用 data/lexicon.bin")
    for name in files:
        low = name.casefold()
        parts = low.split("/")
        if low.rsplit("/", 1)[-1] in PYTHON_FILES or low.endswith(PYTHON_SUFFIXES):
            raise ValueError(f"安裝包含 Python 或 wheel：{name}")
        if any(part in PYTHON_DIRS for part in parts[:-1]):
            raise ValueError(f"安裝包含 Python 目錄：{name}")
        if "x86" in parts[:-1] and name not in X86_ALLOWED:
            raise ValueError(f"安裝包含未預期的 x86 檔案：{name}")
    if release.get("os") != "windows" or release.get("arch") != "amd64":
        raise ValueError("安裝包不是 Windows amd64")
    native = release.get("native_runtime") or {}
    if native.get("platform") != "windows-amd64" or not native.get("onnxruntime"):
        raise ValueError("安裝包沒有原生 ONNX Runtime 記錄")
    if not (release.get("model_package") or {}).get("version"):
        raise ValueError("安裝包沒有日文模型包記錄")


def check_pe(files, package_archive, root):
    with zipfile.ZipFile(package_archive) as package:
        for name, info in files.items():
            if info.filename.rsplit(".", 1)[-1].casefold() not in ("exe", "dll", "pyd"):
                continue
            machine = pe_machine(package.read(info))
            if name in X86_ALLOWED:
                if machine != 0x14C:
                    raise ValueError(f"{name} 必須是 x86 PE")
            elif machine != 0x8664:
                raise ValueError(f"{name} 必須是 x64 PE（machine={machine:#x}）")


def check_data_manifests(package_archive, root, files):
    with zipfile.ZipFile(package_archive) as package:
        for prefix, language in (("data/", "zh"), ("data/ja/", "ja")):
            manifest = json.loads(package.read(root + prefix + "manifest.json"))
            for name, info in manifest.get("files", {}).items():
                path = prefix + name
                if path not in files:
                    raise ValueError(f"{prefix}manifest.json 列出的檔案不存在：{name}")
                if sha(package.read(root + path)) != info.get("sha256"):
                    raise ValueError(f"{prefix}{name} 與 manifest 不符")
            if prefix == "data/ja/":
                if manifest.get("format") != 2 or manifest.get("language") != language:
                    raise ValueError("日文資料包不是 format 2 的 ja 套件")
                for name in ("lexicon/words.tsv", "connection.tsv"):
                    if prefix + name not in files:
                        raise ValueError(f"日文執行期布局缺少 {name}")


def install_layout(archive, local_app_data):
    """把安裝包當成安裝結果放進 versions/<目錄>，寫入收據；回傳 (zuin, version, directory)。"""
    root, files, release = peek_archive(archive)
    version = release["version"]
    toucher = digest(archive)[:12]
    directory = f"versions/{version}-{toucher}"
    zuin = Path(local_app_data) / "Zuin"
    target = zuin / directory
    if target.exists():
        shutil.rmtree(target)
    with zipfile.ZipFile(archive) as package:
        for name in files:
            destination = target / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(package.read(root + name))
    (zuin / "install-receipt.json").write_text(json.dumps({
        "schema": 1, "product": "0c", "version": version, "directory": directory,
        "active": True, "files": release.get("files", {}),
    }, indent=2) + "\n", encoding="utf-8")
    return zuin, version, directory


def check_resolution(archive, updater, local_app_data):
    """以套件內的 0c-update status 驗證設定程式會拿到的 server 與 data 都存在。"""
    zuin, version, directory = install_layout(archive, local_app_data)
    env = {"LOCALAPPDATA": str(Path(local_app_data)), "ZC_NO_SCHEDULE": "1"}
    result = subprocess.run([str(updater), "status", "-json"], capture_output=True, text=True,
                            encoding="utf-8", env={**os.environ, **env}, timeout=120)
    if result.returncode != 0:
        raise ValueError(f"0c-update status 失敗（結束碼 {result.returncode}）：{result.stderr.strip()}")
    status = json.loads(result.stdout)
    installs = status.get("installs") or []
    active = next((i for i in installs if i.get("active")), installs[0] if installs else None)
    if not active:
        raise ValueError("0c-update status 沒有回報使用中的安裝")
    server, data = active.get("server", ""), active.get("data", "")
    if not server or not Path(server).is_file():
        raise ValueError(f"設定程式解析到的 0c-server 不存在：{server!r}")
    if not data or not (Path(data) / "manifest.json").is_file():
        raise ValueError(f"設定程式解析到的資料目錄不完整：{data!r}")
    want = zuin / directory
    if Path(server).parent != want or Path(data) != want / "data":
        raise ValueError(f"設定的 server/data 不在使用中的版本目錄：{server} {data} != {want}")
    if Path(data).joinpath("lexicon.bin").stat().st_size == 0:
        raise ValueError("資料目錄的 lexicon.bin 是空檔")
    if Path(data).joinpath("lexicon.tsv").exists():
        raise ValueError("資料目錄不應依賴 lexicon.tsv")
    # 用同一個 data 參數驗證資料包真的可載入；不執行任何 ONNX 推論。
    check = Path(server).with_name("0c-server.exe")
    checked = subprocess.run([str(check), "-check-data", "-data", str(data)], capture_output=True,
                             text=True, encoding="utf-8", env={**os.environ, **env}, timeout=300)
    if checked.returncode != 0:
        raise ValueError(f"0c-server -check-data 失敗：{checked.stderr.strip()}")
    return {"version": version, "directory": directory, "server": server, "data": data}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--archive", required=True, type=Path, help="package_windows.py 產生的 ZIP")
    parser.add_argument("--updater", type=Path, help="套件內的 0c-update.exe；指定時執行路徑解析檢查（Windows）")
    parser.add_argument("--local-app-data", type=Path, help="模擬安裝用的 LOCALAPPDATA（預設暫存目錄）")
    parser.add_argument("--json", action="store_true", help="輸出解析結果 JSON")
    args = parser.parse_args()
    try:
        root, files, release = peek_archive(args.archive)
        check_entries(files, release)
        check_pe(files, args.archive, root)
        check_data_manifests(args.archive, root, files)
        resolved = None
        if args.updater:
            local = args.local_app_data or Path(tempfile.mkdtemp(prefix="0c-payload-"))
            resolved = check_resolution(args.archive, args.updater, local)
        if args.json:
            print(json.dumps({"ok": True, "files": len(files), "resolved": resolved}, ensure_ascii=False, indent=2))
        else:
            print(f"payload ok：{len(files)} 個檔案" + (f"，server={resolved['server']} data={resolved['data']}" if resolved else ""))
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, struct.error, subprocess.SubprocessError) as error:
        print(f"payload 檢查失敗：{error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
