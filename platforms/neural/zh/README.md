# 中文模型語言標籤 adapter（P2-26）

既有中文模型 `0c-sequence-d512` 不改權重、vocab、manifest 或配對，只加一層
語言標籤 adapter，把多語言候選介面轉成 sequence 模型請求。正式執行者是
Go 原生 ONNX 後端；產品不含 Python worker。

- Go 端：`core/score/zh_adapter.go` 的 `ZhAdapter` 實作 `score.Scorer`。
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
   `score.ErrUnavailable`，呼叫端保留統計轉換結果。
6. 單音節候選預設不評分，因為既有中文模型一次至少需要兩個候選。Go adapter
   可用 `TextMode` 搭配前文使用 text task；套件沒有 `text_weight` 時仍會回退。

Go adapter 沒有接進注音 session 的既有 rerank 路徑，因此注音 golden 不變。

## 協定

adapter 的請求模型延續 P2-26 的欄位：一行一個 JSON，最多 64 KiB，包含
`id`、`revision`、`schema`、`context` 與 `candidates`。候選帶 `surface`、
`reading`、`language` 與 `variant`。成功回覆與候選等長、同順序並帶 `scores`；
不支援的請求回報 `fallback`。

P2-30D 起 stdio worker 與測試 fixture 已刪除。需要比對舊行為時，請從
P2-30C 以前的 checkout 取 reference 實作，依開發用 parity gate 執行。

## 測試

```sh
go test ./core/score -count=1
go run ./cmd/0c-golden
```
