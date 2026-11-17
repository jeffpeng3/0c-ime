# onnxprobe（P2-30S 實驗工具）

證明 Go 程式可以直接載入 `libonnxruntime` 共享函式庫、跑 ONNX 推論、並用純 Go 做
SentencePiece unigram 編碼，整個路徑沒有 Python。**這是量測 spike，不是產品路徑**：
`tools/onnxprobe` 是獨立 Go module，`cmd/0c-server` 與正式建置完全不引用它。

- 綁定：`github.com/yalue/onnxruntime_go v1.38.0`（CGO，只需要執行期的共享函式庫，
  編譯期標頭檔已隨套件提供）。
- 分詞器：`spm/` 是純 Go 的 SentencePiece unigram 實作，支援 identity normalizer、
  Viterbi、byte fallback。讀到含 precompiled charsmap 的模型會在載入時直接拒絕。
- 報告：`P2-30S-native-onnx.md`（同目錄），完整量測與 GO/NO-GO 結論。

## 建置與執行

```sh
cd tools/onnxprobe
go build -o /tmp/onnxprobe .

# 只做分詞，輸出 JSON
/tmp/onnxprobe tokenize \
  -spm  public_core_ja.model \
  -texts sample-ja.txt -limit 200 -out go-tokens.json

# 載入模型、量冷啟動／延遲／RSS，並對 200 句打分
/tmp/onnxprobe bench \
  -onnx reranker-public-core.placeholder.int8.onnx \
  -lib  /path/to/libonnxruntime.so.1.32.0 \
  -spm  public_core_ja.model \
  -texts sample-ja.txt -limit 200 \
  -seq 32 -runs 100 -threads 2 -vocab-cap 14080 \
  -out go-bench.json
```

`-vocab-cap 14080` 只用於這個 placeholder 模型：它的 embedding 表是 14080 列，
而 tokenizer 有 32768 個 piece，原始 id 會讓 `Gather` 越界。兩邊都用 `id % 14080`
做 parity 比較，不影響「Go 能否原生跑 ONNX」的結論。

## Python 參考

`reference/` 內的三個腳本只是 oracle，不會進產品：

```sh
uv venv /tmp/spm && uv pip install sentencepiece numpy onnxruntime==1.32.0

python reference/oracle.py --spm ... --onnx ... --texts ... \
  --limit 200 --seq 32 --vocab-cap 14080 --out oracle.json
python reference/worker_latency.py --onnx ... --seq 32 --runs 100 --out py-latency.json
python reference/compare.py --go go-bench.json --oracle oracle.json
```

## 測試

```sh
go test ./...
# 200 句 oracle 比對（語料不進 repo，需自行指定路徑）
SPM_MODEL=public_core_ja.model SPM_TEXTS=sample-ja.txt go test ./spm -run TestGolden200 -v
```

`spm/testdata/tokens-200.json` 只存 token id 與模型 sha256，不存語料文字。

## Windows

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
  CC=x86_64-w64-mingw32-gcc go build -o onnxprobe.exe .
```

`CGO_ENABLED=0` 會失敗（binding 全部檔案都有 cgo build constraint）。執行時把
`onnxruntime.dll` 與 exe 放在一起，或透過 `-lib` 指到絕對路徑。
