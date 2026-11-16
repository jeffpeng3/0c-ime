# T14 真模型 smoke（preliminary）

狀態：**功能 smoke 通過，不是延遲或記憶體認證。** 量測時主機仍有其他 agent／
建置工作在跑（load average 約 0.2 到 1.0 / 12 vCPU，中間曾升到 5），所以數字
只能當方向。最終 P50／P95／P99 要照 T03 的 `--isolated` 流程在安靜時段重跑。

## 環境

| 項目 | 值 |
|---|---|
| 日期 | 2026-11-17 |
| 平台 | WSL2，Fedora 45 |
| CPU | AMD Ryzen 5 5600X，6 核 12 執行緒 |
| 模型 | `Miwa-Keita/zenz-v3.1-small-gguf` revision `ddf6e44b…`，`ggml-model-Q5_K_M.gguf`，73,871,968 bytes |
| 模型 SHA-256 | `4de930c06bef8c263aa1aa40684af206db4ce1b96375b3b8ed0ea508e0b14f6c` |
| llama.cpp | `2d8a32502909ca065c41c4cf5f2fb1c427c047fc`（v1.0.0），CPU build |
| 設定 | threads 6、ctx 512、`-np 1`、loopback、`--no-webui --no-slots --log-disable` |

## 指令

```sh
JA_TEST_REAL=1 \
JA_TEST_PACKAGE=/tmp/opencode/ja-zenz-package \
JA_TEST_LLAMA_SERVER=/tmp/opencode/llama.cpp/build-cpu/bin/llama-server \
JA_TEST_PYTHON=python3 \
go test ./internal/neural/ -run TestJaRealModelSmoke -count=1 -v
```

`JA_TEST_PACKAGE` 內是 `manifest.json` 與 T03 的 GGUF；manifest 綁定上表的
模型雜湊與 llama.cpp commit。

## 結果

輸入：左文脈 `私の名前は`、讀音 `きょうはいいてんき`，draft 與第一個候選是
`今日はいい天気`，第二個候選是 `今日はいい天気です`。

| 表記 | logprob | score（每字） | passed | prefix |
|---|---:|---:|---|---|
| `今日はいい天気`（draft／候選 1） | -0.35295 | -0.05042 | true | `今日はいい天気` |
| `今日はいい天気です`（候選 2） | -13.54636 | -1.50515 | false | `今日はいい天気で` |

- 整筆 `Verify`（draft、2 個候選、快取重用、worker JSON、loopback HTTP）單次
  耗時 **70.9 到 77.0 ms**。
- 低負載樣本（`JA_TEST_SMOKE_N=20`，主機 load average 約 0.2 到 1.0）：
  **timeouts 0、P50 71.62 ms、P95 87.51 ms、max 87.64 ms**。
- 同一測試在 load average 約 1.4 到 2.4 重跑時，有部分請求超過 worker 內部
  的 90 ms 截止而回 `timeout`；下一次呼叫會重啟 worker。這是預期的退回行為，
  也說明 T30 在忙碌時應先送 2 到 3 條候選，不要一次送滿 8 條。
- 這些都是 preliminary 樣本，不是隔離後的 P50／P95／P99 認證。
- 第二個候選在 `す` 分歧，top-1 是 `で`；`prefix` 與 `passed=false` 符合
  Zenzai 的 fixRequired 形狀。
- 量測期間有其他 agent 活動，所以只把這些數字當方向；最終認證仍要 `--isolated`。

## 記憶體

模型載入並暖機後，`llama-server` 的 `VmRSS` 是 **117,328 KiB（約 114.6 MiB）**，
threads 6、ctx 512。Go 端不載入權重，worker 只做 JSON 與 HTTP。單語言 250 MB
預算還有餘裕；這是 preliminary，長時間 soak 與大量請求後的成長仍要在安靜時段量。

## 生命週期

- 測試結束後 `pgrep -ax llama-server` 沒有殘留；Unix 上 worker 自成 process
  group，Go 硬逾時或測試結束會一起收掉 `llama-server`。
- mock worker 的逾時測試（40 ms 硬逾時、worker 死亡後下一次立刻
  `ErrUnavailable`）與 Python fake server 的內部逾時測試都在一般 `go test`／
  `unittest` 內，不需要模型。

## 待安靜時段補測

1. 100 次以上 verify 的 P50／P95／P99，分開記 prompt eval 與 decode。
2. 2／3／4 個候選的邊際成本，決定 T30 每次最多送幾條。
3. 連續請求與 prefix cache 命中率。
4. 長時間 RSS 與 worker 重啟次數。
