# 日文神經驗證 runtime（T14）

T03 的結論：zenz-v3.1-small Q5_K_M 可以在 CPU 載入，但每次按鍵跑完整 free
generation 的 P50 已超過 100 ms。因此這個 runtime 只做 **draft + verify**：

- draft 與候選由 T12 統計轉換產生，runtime 不生成整句。
- 每個候選以 GBNF 固定成候選表記，取得逐 token 的原始 logprob 與 top-1。
- 候選 token 不是該位置 top-1 時，回報到第一個分歧點為止的高機率前綴。

限制式重搜、門檻與權重屬於 T30；這裡只提供推論、逾時與退回。

## 檔案

| 路徑 | 用途 |
|---|---|
| `ja_runtime.py` | llama.cpp CPU 後端 worker；啟動 loopback `llama-server`，說 stdin/stdout 單行 JSON |
| `manifest.example.json` | `models/ja/manifest.json` 範例；T03 pin 的 GGUF 與 llama.cpp commit |
| `tests/mock_runtime.py` | 不需要模型或 llama.cpp 的 mock，給 T30 開發 |
| `tests/test_ja_runtime.py` | 協定、manifest、GBNF、逾時的單元測試 |

Go 端在 `internal/neural/ja*.go`：`OpenJa`、`JaRuntime.Verify`、`WaitReady`、
`Close`、`Stats`。

## 模型配對

模型權重不進 Git，也不進 T13 的資料包 manifest。`models/ja/` 由發佈流程提供：

```text
models/ja/
  manifest.json
  ggml-model-Q5_K_M.gguf
  NOTICE.txt
```

`manifest.json` 必須與 `manifest.example.json` 同 schema，並綁定：

- `backend: "llama.cpp"` 與 `backend_commit`（T03：`2d8a325…`）。
- `model.file`、`model.sha256`、`model.bytes`：Go 與 Python 都會核對實際檔案。
- `context_policy: "confirmed_left_128"`：只收外殼確認過的前文，最多 128 字。
- `limits`：讀音 64 單位、候選 8 條、前文 128 字。
- `context_size`：預設 512，允許 256 到 2048。

`OpenJa` 在啟動 worker 前同步核對 manifest 與模型雜湊；缺檔或雜湊不符回傳
`*neural.JaModelError`，`Reason` 是 `model_missing`、`model_hash_mismatch` 等
固定代碼。呼叫端據此退回統計轉換，不需要停用日文輸入。

正式安裝把 T03 pin 經 `JaConfig` 傳入：

```go
rt, err := neural.OpenJa(neural.JaConfig{
    Python:        "/usr/bin/python3",
    Script:        ".../platforms/neural/ja/ja_runtime.py",
    Package:       ".../models/ja",
    Server:        ".../llama.cpp/build-cpu/bin/llama-server",
    ModelSHA256:   neural.JaZenzModelSHA256,
    BackendCommit: neural.JaLlamaCPPCommit,
    Threads:       6,
    Timeout:       100 * time.Millisecond,
})
```

`OpenJa` 只驗證配對並在背景啟動；引擎啟動路徑不等模型。模型就緒前的
`Verify` 立刻回 `neural.ErrUnavailable`，T30 照統計結果顯示。

## 輸入／輸出協定

worker 的 stdin/stdout 是一行一個 JSON，最大 64 KiB。Go 端已封裝，以下給
mock／除錯參考。

請求：

```json
{
  "id": 1,
  "revision": 7,
  "schema": "ja-zenz-verify-v1",
  "model_sha256": "<manifest.model.sha256>",
  "left_context": "私の名前は",
  "reading": ["き", "ょ", "う", "は", "い", "い", "て", "ん", "き"],
  "draft": {
    "surface": "今日はいい天気",
    "reading": ["き", "ょ", "う", "は", "い", "い", "て", "ん", "き"],
    "clauses": [
      {"reading": ["き", "ょ", "う"], "surface": "今日"},
      {"reading": ["は"], "surface": "は"}
    ]
  },
  "candidates": [
    {"surface": "今日はいい天気", "reading": ["き", "ょ", "う", "は", "い", "い", "て", "ん", "き"]},
    {"surface": "今日はいい天気です", "reading": ["き", "ょ", "う", "は", "い", "い", "て", "ん", "き"]}
  ]
}
```

- `reading` 是 T00 的かな單位，不是 mora；worker 只在送進模型前轉成片假名。
- 每個候選的 `reading` 必須與請求完全相同，否則整筆退回
  `unsupported_request`，不產生不忠實的候選。
- `draft` 可省略；省略時用第一個候選的前綴當約束。
- `clauses` 只用來回報已固定的完整文節數與讀音單位數。

回覆：

```json
{
  "id": 1,
  "revision": 7,
  "schema": "ja-zenz-verify-v1",
  "model_sha256": "<manifest.model.sha256>",
  "prefix": "今日はいい天気",
  "fixed_clauses": 2,
  "fixed_reading_units": 4,
  "draft": {
    "logprob": -3.2,
    "score": -0.45,
    "prefix": "今日はいい天気",
    "passed": true,
    "prefix_ok": true
  },
  "candidates": [
    {
      "surface": "今日はいい天気",
      "logprob": -3.2,
      "score": -0.45,
      "prefix": "今日はいい天気",
      "passed": true,
      "prefix_ok": true
    },
    {
      "surface": "今日はいい天気です",
      "logprob": -9.8,
      "score": -0.98,
      "prefix": "今日はいい天気",
      "passed": false,
      "prefix_ok": true
    }
  ]
}
```

語意：

- `logprob`：候選表記的 token logprob 總和（自然對數，越高越好）。
- `score`：`logprob / 表記字數`，給不同長度候選排序用。
- `prefix`：模型在該表記第一個非 top-1 token 之前的高機率前綴；全部
  token 都是 top-1 時等於 `surface`。最後的 EOS 不計分。
- `passed`：候選每個 token 都是該位置的 top-1，不需要限制式重搜。
- `prefix_ok`：候選表記以 draft 的全域 `prefix` 開頭。T30 可用
  `fixed_clauses`／`fixed_reading_units` 固定已確認的文節再重搜。
- `fallback: "timeout"`：worker 在預算內沒有完成；Go 回
  `neural.ErrTimeout`，這次不用神經結果。為避免 llama-server 還在跑逾時
  請求而污染下一個 slot，worker 會結束，下一次呼叫在冷卻後重啟。
- `fallback: "unsupported_request"`：輸入不合協定；Go 回
  `neural.ErrJaUnsupported`，不殺 worker。

## 逾時與生命週期

- `JaConfig.Timeout` 必須在 1 到 100 ms；預設 100 ms。
- worker 自己留 10 ms 給 JSON 與排程，llama-server 的 HTTP 逾時用剩餘預算。
- Go 的硬逾時到時直接殺掉 worker；Unix 上 worker 自成 process group，
  `llama-server` 一起結束，不會留下孤兒。
- 逾時或 crash 後，下一次 `Verify` 立刻回 `ErrUnavailable`；冷卻期
  （預設 3 秒；缺執行環境或模型配對錯誤 10 分鐘）過後背景重啟。
- 啟動握手最多 30 秒（可調 `StartTimeout`），但不阻塞 `Verify`。

## Mock（T30）

`tests/mock_runtime.py` 說完全相同的協定，固定回傳可預測的分數與前綴，
可選 `--delay-ms` 模擬慢模型：

```go
rt, err := neural.OpenJa(neural.JaConfig{
    Python: "python3",
    Script: ".../platforms/neural/ja/tests/mock_runtime.py",
    Package: ".../platforms/neural/ja/tests/mock-package",
    Server: "mock", // mock 不使用
    Timeout: 100 * time.Millisecond,
})
```

`tests/mock-package/` 已附一個 5 bytes 的假模型與 manifest，T30 可直接使用。
mock 的 `backend_commit` 是 `mock`，所以 `JaConfig.BackendCommit` 留空或設
`mock`；正式安裝才需要 T03 pin。

## 測試

```sh
# Go 協定、逾時、模型配對、mock worker
go test ./internal/neural/ -run TestJa -count=1

# Python 單元測試（不啟動 llama.cpp）
python3 -m unittest discover -s platforms/neural/ja/tests -p 'test_*.py'

# 真模型 smoke；需要 T03 的 llama.cpp 與 zenz GGUF
JA_TEST_REAL=1 \
JA_TEST_PACKAGE=/path/to/models/ja \
JA_TEST_LLAMA_SERVER=/path/to/llama.cpp/build-cpu/bin/llama-server \
go test ./internal/neural/ -run TestJaRealModelSmoke -count=1 -v
```

真模型 smoke 只驗證端到端與分數方向，不當延遲認證。最終 P50／P95／P99
要在沒有其他 agent 或 CI 的安靜時段用 `--isolated` 重跑 T03 的量測。

## 記憶體

模型 70.4 MiB、ctx 512、threads 6。T03 在載入後量到 RSS 約 114 MiB；
T14 的驗證請求沒有 prefix cache 之外的成長。單語言 250 MB 預算以
`llama-server` 的 RSS 為準，Go 端不載入權重。實測與主機負載記在
[SMOKE.md](SMOKE.md)，目前是 preliminary，不是最終認證。

## 不在範圍

- 不接 T12 轉換器（T30）。
- 不訓練模型、不改注音模型。
- 不做 Windows Job Object 的孤兒清理（T34 打包）。
