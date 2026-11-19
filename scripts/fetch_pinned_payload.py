#!/usr/bin/env python3
"""下載 Windows 安裝包需要的固定輸入，逐檔核對大小與 SHA-256。

這些檔案不在 Hugging Face，也不進 Git：日文 public-core pack、它的 manifest，以及
日文 reranker／補標點模型共用的 SentencePiece tokenizer。來源固定在本 repo 的 assets
release，記錄在 models/payload-assets.lock.json；字典本身仍由 scripts/fetch_assets.py
從 pinned 的 upstream source release 取得。

把 tokenizer 放進 0c-model-set 的下載快取（<repo>/<revision>/<檔名>），後續
`0c-model-set fetch` 會依 pairing lock 核對它，其他日文模型檔才從 Hugging Face 下載。
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
LOCK = ROOT / "models/payload-assets.lock.json"
PAIRING = ROOT / "models/pairing.lock.json"


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def load_lock(path=LOCK):
    lock = json.loads(Path(path).read_bytes())
    if lock.get("schema") != "0c.payload-assets/1":
        raise ValueError(f"payload assets lock schema 不符：{path}")
    for key, spec in lock.get("assets", {}).items():
        for field in ("name", "url", "sha256"):
            if not str(spec.get(field, "")).strip():
                raise ValueError(f"{key} 缺少 {field}")
        if len(spec["sha256"]) != 64 or spec.get("bytes", 0) <= 0:
            raise ValueError(f"{key} 沒有固定大小與 SHA-256")
    for key in ("ja_pack", "ja_manifest", "ja_tokenizer"):
        if key not in lock.get("assets", {}):
            raise ValueError(f"payload assets lock 缺少 {key}")
    return lock


def check(path, spec, label):
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"{label} 不存在或不是一般檔案：{path}")
    if path.stat().st_size != spec["bytes"] or digest(path) != spec["sha256"]:
        raise ValueError(f"{label} 與 payload assets lock 不符：{path}")
    return path


def fetch(url, target, spec, label):
    target = Path(target)
    if target.exists() and not target.is_symlink():
        try:
            return check(target, spec, label)
        except ValueError:
            target.unlink()
    target.parent.mkdir(parents=True, exist_ok=True)
    stream = tempfile.NamedTemporaryFile(dir=target.parent, prefix=".download-", delete=False)
    pending = Path(stream.name)
    try:
        h = hashlib.sha256()
        size = 0
        with stream, urllib.request.urlopen(url, timeout=180) as response:
            while block := response.read(1 << 20):
                stream.write(block)
                size += len(block)
                h.update(block)
        if size != spec["bytes"] or h.hexdigest() != spec["sha256"]:
            raise ValueError(f"{label} 與 payload assets lock 不符：{url}")
        os.replace(pending, target)
    finally:
        pending.unlink(missing_ok=True)
    return target


def download_assets(lock, out):
    out = Path(out)
    out.mkdir(parents=True, exist_ok=True)
    result = {}
    for key, spec in lock["assets"].items():
        target = out / spec.get("download_name", spec["name"])
        result[key] = str(fetch(spec["url"], target, spec, key))
    return result


def tokenizer_targets(pairing=PAIRING):
    """pairing lock 內每個需要 tokenizer.model 的 artifact：快取路徑與預期檔名。"""
    lock = json.loads(Path(pairing).read_bytes())
    pkg = lock.get("model_package") or {}
    out = []
    for artifact in pkg.get("artifacts", []):
        hf = artifact.get("hf") or {}
        asset = (hf.get("assets") or {}).get("tokenizer.model") or {}
        name = str(asset.get("name", "")).strip()
        if not name:
            continue
        repository, revision = str(hf.get("repository", "")).strip(), str(hf.get("revision", "")).strip()
        if not repository or not revision:
            raise ValueError(f"{artifact.get('artifact_id')} 的 tokenizer 缺少 repo 或 revision")
        out.append((repository, revision, name))
    if not out:
        raise ValueError("pairing lock 沒有任何 tokenizer.model 來源")
    return out


def seed_tokenizer(tokenizer, cache, pairing=PAIRING):
    """把已核對的 tokenizer 放進 0c-model-set 的快取；回傳寫入的路徑。"""
    tokenizer = Path(tokenizer)
    written = []
    for repository, revision, name in tokenizer_targets(pairing):
        target = Path(cache) / repository / revision / name
        target.parent.mkdir(parents=True, exist_ok=True)
        if not target.exists() or digest(target) != digest(tokenizer):
            shutil.copy2(tokenizer, target)
        written.append(target)
    return written


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--lock", type=Path, default=LOCK)
    parser.add_argument("--pairing", type=Path, default=PAIRING)
    parser.add_argument("--out", type=Path, default=ROOT / "build/ja-assets",
                        help="pack 與 manifest 的目錄（manifest 一律命名為 manifest.json）")
    parser.add_argument("--model-cache", type=Path, default=ROOT / "build/cache/ja-models",
                        help="0c-model-set 的日文模型快取；tokenizer 會放進對應的 repo/revision")
    args = parser.parse_args()
    try:
        lock = load_lock(args.lock)
        files = download_assets(lock, args.out)
        seeded = seed_tokenizer(files["ja_tokenizer"], args.model_cache, args.pairing)
        print(json.dumps({"files": files, "tokenizer_cache": [str(p) for p in seeded]}, ensure_ascii=False, indent=2))
    except (OSError, ValueError) as error:
        print(f"下載固定輸入失敗：{error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
