#!/usr/bin/env python3
"""Assemble the shipped neural/ directory: model packages only.

The product runtime contains no CPython, no numpy and no onnxruntime Python
package. This script downloads and verifies only the ONNX model files and pairs
their manifests/vocab with the dictionary that ships beside them. Inference is
performed by the Go native backend.

模型是 models/models.lock.json 記的 Hugging Face 公開版（#180）：由
scripts/hf_models.py 取得原檔（--models 指定的目錄、快取或下載，逐檔核對
models.lock），依 --dictionary 配對；結果等於這份字典的 pairing.lock.json。
"""

import argparse
import hashlib
from pathlib import Path
import shutil
import sys
import tempfile

import hf_models

ROOT = Path(__file__).resolve().parents[1]

# Accepted target names for build-script compatibility. The contents are the
# same model data on every platform; no interpreter is fetched or installed.
PLATFORMS = ("linux-amd64", "macos-arm64", "macos-amd64", "windows-amd64")


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def build(args):
    dictionary = digest(args.dictionary / "manifest.json")
    weights = hf_models.fetch(args.models, args.models_cache, log=sys.stderr)
    out = args.out
    if out.exists():
        raise ValueError(f"輸出已存在：{out}")
    out.parent.mkdir(parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix=".neural-", dir=out.parent))
    try:
        hf_models.write_packages(weights, dictionary, stage)
        (stage / "README.txt").write_text(
            "靈犀注音內建的神經選字與提交時補標點模型。產品不含 CPython、numpy 或 "
            "onnxruntime Python 套件；推論由原生 ONNX Runtime 執行。\n"
            "切換：0c-update set selection statistical|neural、0c-update set punctuation on|off。\n",
            encoding="utf-8")
        stage.replace(out)
    except BaseException:
        shutil.rmtree(stage, ignore_errors=True)
        raise
    return out


def main():
    parser = argparse.ArgumentParser(description="組出套件內附的 neural/ 目錄（只有模型，沒有 Python runtime）")
    parser.add_argument("--platform", required=True, choices=PLATFORMS)
    parser.add_argument("--models", type=Path, help="Hugging Face 原檔目錄（<dir>/<select|punct>/model.onnx、LICENSE、README.md，"
                        "例如 0c-ime 的 models/）；省略則用 --models-cache 或從 Hugging Face 下載。一律核對 models.lock")
    parser.add_argument("--models-cache", type=Path, default=hf_models.CACHE, help="模型下載快取")
    parser.add_argument("--dictionary", required=True, type=Path, help="將一起出貨的系統字典目錄（模型配對到它）")
    parser.add_argument("--out", required=True, type=Path, help="輸出目錄（不可已存在）")
    args = parser.parse_args()
    try:
        print(build(args))
    except (OSError, ValueError, KeyError) as error:
        print(f"準備神經套件失敗：{error}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
