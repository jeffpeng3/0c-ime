#!/bin/sh
# 下載並驗證固定版本的 Mozc OSS dictionary_oss。
#
# 用法：scripts/fetch_mozc_dictionary.sh <輸出目錄>
#
# 只下載詞典匯入實際使用的檔案與授權檔；原始資料只放在輸出的 build/ 目錄，
# 不進 Git。Mozc 上游的 dictionary_oss 是混合授權：Google 的 BSD-3-Clause、
# NAIST/IPAdic、ICOT Free Software，以及沖繩辭書 Public Domain。上游沒有把
# 這份資料標成 Apache-2.0；實際條款以 LICENSE 與 README.txt 為準。
set -eu

REPO="google/mozc"
TAG="3.35.6354"
COMMIT="0136c35782536cb43d710575c311cd4185982738"
BASE="https://raw.githubusercontent.com/${REPO}/${COMMIT}/src/data/dictionary_oss"

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
HASHES="$ROOT/data/sources/ja-mozc-${TAG}.sha256"
OUT=${1:-build/mozc-${TAG}}
mkdir -p "$OUT"

# 檔名與 SHA-256 來自 Git 內的固定清單；只列匯入與授權確認會用到的檔案。
FILES=$(awk 'NF == 2 && $1 !~ /^#/ {print $2, $1}' "$HASHES")

command -v curl >/dev/null 2>&1 || { echo "需要 curl" >&2; exit 2; }
command -v sha256sum >/dev/null 2>&1 || { echo "需要 sha256sum" >&2; exit 2; }

echo "$FILES" | while read -r name want; do
	[ -n "$name" ] || continue
	path="$OUT/$name"
	if [ -f "$path" ]; then
		got=$(sha256sum "$path" | awk '{print $1}')
		if [ "$got" = "$want" ]; then
			continue
		fi
	fi
	# LICENSE 在 Mozc 的 repo 根目錄，不在 src/data/dictionary_oss。
	if [ "$name" = "LICENSE" ]; then
		url="https://raw.githubusercontent.com/${REPO}/${COMMIT}/LICENSE"
	elif [ "$name" = "README.txt" ]; then
		url="$BASE/README.txt"
	else
		url="$BASE/$name"
	fi
	echo "下載 $name"
	curl -fsSL "$url" -o "$path.tmp"
	got=$(sha256sum "$path.tmp" | awk '{print $1}')
	if [ "$got" != "$want" ]; then
		echo "SHA-256 不符：$name（got $got, want $want）" >&2
		rm -f "$path.tmp"
		exit 1
	fi
	mv "$path.tmp" "$path"
done

# 留下可核對的清單，不把原始資料放進 Git。
echo "$FILES" | awk '{print $2"  "$1}' >"$OUT/SHA256SUMS"
printf 'Mozc OSS dictionary_oss\nrepo: https://github.com/%s\ncommit: %s\ntag: %s\n' "$REPO" "$COMMIT" "$TAG" >"$OUT/UPSTREAM.txt"
echo "Mozc dictionary_oss $TAG ($COMMIT) 已驗證：$OUT"
