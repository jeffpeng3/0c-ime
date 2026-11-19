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

OUT=${1:-build/mozc-${TAG}}
mkdir -p "$OUT"

# 檔名與 SHA-256。只列匯入與授權確認會用到的檔案。
FILES='
aux_dictionary.tsv 8103d07c4eb18892282983299cbb20284a0f74bc596dda63831787b69aa7ce6c
connection_single_column.txt 68823a1f6617372ccb89afdbcb334d7a0bcfa82c9c5dda03fc1f80f1cab5c48e
dictionary00.txt 9e07ce292932c4fff327ed8cb06274c09acca556bd8bdf7c7ef1f6173eae3b61
dictionary01.txt 836fd5fd8399510de0d8ef7b33be913cca6bbaee7b712cc8045fba03f4c75a89
dictionary02.txt 93943858d0cfd092b849a78def6901eaf228177e1ac5663544e865a3a1e45a9e
dictionary03.txt cb9f1b0b4a7550d682d86244a021cd17e0b15366328819e69135b5f183a1a24b
dictionary04.txt 9fdc4e3948d03463de13ab5782adffbc11dd06e934cb80138c5920029cd3d2f6
dictionary05.txt 199e3b0f050b691823ae7ba943090d437f51af6436b27c5f3dc69cafa12000e4
dictionary06.txt b95b14f7770ae2a824bc8e9fad8b03c1bc45c504f93a36554b8b56daa0368d2a
dictionary07.txt 7f53649b529d4c8c495d3a8dd9c4d489dfe7696080f7423107ff01331174be48
dictionary08.txt 50a31bb5fc1646721b00be371a5695dffab1292bda143e1e59cc70cbbdd8643f
dictionary09.txt 4e5fcd4cbfbf11395166a37b506f547fc54d20edc0884f5184ecd3fc705f0d9a
dictionary_filter.tsv 802f2c76516bf1bbb30cd501cc343abf59aa179d3f98917297d961fb9921a2fb
id.def 07a05a268c3783b7e02b36e4aa591555a2dd19a1f3c93891576740761c154f2d
LICENSE 44cdd923b91ea9199293abecc2762c70c87dbf1e581c027a94c416368d1a648c
README.txt 6b1de66bc6fa30e0b45dc45b8c8f6c57bd78aff923c261a3efffac8eb86f7bac
'

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
	echo "下載 $name"
	curl -fsSL "$BASE/$name" -o "$path.tmp"
	got=$(sha256sum "$path.tmp" | awk '{print $1}')
	if [ "$got" != "$want" ]; then
		echo "SHA-256 不符：$name（got $got, want $want）" >&2
		rm -f "$path.tmp"
		exit 1
	fi
	mv "$path.tmp" "$path"
done

# 留下可核對的清單，不把原始資料放進 Git。
printf '%s\n' "$FILES" | awk 'NF==2{print $2"  "$1}' >"$OUT/SHA256SUMS"
printf 'Mozc OSS dictionary_oss\nrepo: https://github.com/%s\ncommit: %s\ntag: %s\n' "$REPO" "$COMMIT" "$TAG" >"$OUT/UPSTREAM.txt"
echo "Mozc dictionary_oss $TAG ($COMMIT) 已驗證：$OUT"
