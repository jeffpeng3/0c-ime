# 中文模型語言標籤 adapter（P2-26）

既有中文模型 `0c-sequence-d512` 不改權重、vocab、manifest 或配對，只加一層
語言標籤 adapter，把多語言候選介面轉成原本的 sequence_runtime 請求。

- Go 端：`core/score/zh_adapter.go` 的 `ZhAdapter` 實作 `score.Scorer`。
- Runtime 端：`zh_adapter.py` 是 stdio worker，重用 `../runtime/sequence_runtime.py`。
- 模型套件：`models/select`，版本 `d512-cv2-20261111`，`model.onnx` SHA-256
  `4a80caf18acbaddde10c908dcbf35919908d616e94618cdde71fc3409c969261`。
  本目錄不會複製、改寫或重新匯出任何模型檔。

## 行為

1. 候選帶 `language` 標籤。`zh` 與省略標籤的舊呼叫端會送進中文模型；`ja`、
   `en` 與其他語言的候選保留原順序，回報中性分數 `0`。
2. 同一個讀音的中文候選合成一次正式模型請求。同詞面重複候選只送一次，分數
   套回所有重複項。
3. 讀音必須是合法注音，詞面長度必須與音節數相同。かな、空詞面、控制字元與
   長度不符的候選不送模型。
4. 前文只保留最後 64 字，與既有模型窗口相同。隱私欄位仍由呼叫端負責，不得
   送進 `context`。
5. 模型不可用、請求形狀不支援、分數數量不符或非有限值時一律回退。Go 端回
   `score.ErrUnavailable`，Python 端回 `fallback`，呼叫端保留統計轉換結果。
6. 單音節候選預設不評分，因為既有中文模型一次至少需要兩個候選。Go adapter
   可用 `TextMode` 搭配前文使用 text task；套件沒有 `text_weight` 時仍會回退。

Go adapter 沒有接進注音 session 的既有 rerank 路徑，因此注音 golden 不變。

## 啟動

```sh
python3 platforms/neural/zh/zh_adapter.py \
  --package models/select \
  --dictionary <dictionary_manifest_sha256> \
  --threads 2
```

沿用 `platforms/neural/runtime/sequence_runtime.py` 的模型套件檢查、ONNX
Runtime 設定與遙測關閉。`--runtime` 可指定另一個位置的既有 worker，但模型仍
必須符合原本的 manifest 與字典配對。

## 協定

請求是一行一個 JSON，最多 64 KiB：

```json
{
  "id": 1,
  "revision": 3,
  "schema": "zh-language-adapter-v1",
  "context": "前文",
  "candidates": [
    {
      "surface": "今天",
      "reading": ["ㄐㄧㄣ", "ㄊㄧㄢ"],
      "language": {"language": "zh", "confidence": 0.9},
      "variant": "zh-Hant"
    },
    {
      "surface": "金田",
      "reading": ["ㄐㄧㄣ", "ㄊㄧㄢ"],
      "language": {"language": "zh"}
    },
    {
      "surface": "きょう",
      "reading": ["き", "ょ", "う"],
      "language": {"language": "ja"}
    }
  ]
}
```

成功回覆與 `candidates` 等長、同順序：

```json
{"id": 1, "revision": 3, "schema": "zh-language-adapter-v1", "scores": [1.2, 0.8, 0.0]}
```

回退回覆：

```json
{"id": 1, "revision": 3, "schema": "zh-language-adapter-v1", "fallback": "unsupported_request"}
```

## 測試

```sh
go test ./core/score -count=1
python3 -m unittest discover -s platforms/neural/zh/tests -v
go run ./cmd/0c-golden
```

Python 測試不需要 numpy、onnxruntime 或模型檔；它只驗證語言標籤、候選轉換、
分組、去重、前文截斷與回退。
