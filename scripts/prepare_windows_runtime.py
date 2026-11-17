#!/usr/bin/env python3
"""Materialise the pinned native ONNX Runtime DLLs for the Windows bundle.

Reads models/runtime.lock.json, takes the two pinned wheels from --cache or
downloads them, and writes only onnxruntime.dll plus the four MSVC runtime
DLLs. No CPython, numpy or onnxruntime Python package is extracted.
"""

import argparse
from pathlib import Path
import sys

import windows_runtime


def main():
    parser = argparse.ArgumentParser(description="從固定 wheel 取出 Windows 原生 ONNX Runtime DLL")
    parser.add_argument("--out", required=True, type=Path, help="輸出目錄（不可已存在）")
    parser.add_argument("--cache", type=Path, help="wheel 快取（預設 build/runtime-cache）")
    parser.add_argument("--lock", type=Path, default=windows_runtime.LOCK)
    parser.add_argument("--onnxruntime-wheel", type=Path, help="本機的 onnxruntime wheel（須符合 lock）")
    parser.add_argument("--msvc-wheel", type=Path, help="本機的 msvc_runtime wheel（須符合 lock）")
    args = parser.parse_args()
    wheels = {}
    if args.onnxruntime_wheel:
        wheels["onnxruntime"] = args.onnxruntime_wheel
    if args.msvc_wheel:
        wheels["msvc_runtime"] = args.msvc_wheel
    try:
        lock = windows_runtime.load_lock(args.lock)
        out = windows_runtime.extract(lock, out=args.out, cache=args.cache, wheels=wheels)
    except (OSError, ValueError) as error:
        print(f"準備原生執行階段失敗：{error}", file=sys.stderr)
        return 1
    entry = windows_runtime.platform_entry(lock)
    print(f"{out}：onnxruntime {entry['onnxruntime']['version']}、"
          f"MSVC {entry['msvc_runtime']['version']}，{len(entry['files'])} 個 DLL")
    return 0


if __name__ == "__main__":
    sys.exit(main())
