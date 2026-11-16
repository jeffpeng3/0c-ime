# 注音 golden baseline（T01）

這份 baseline 凍結注音輸入的協定層級行為：`cases.json` 的按鍵／請求序列經由正式引擎
（`engine.Handle` → `session.Handle`）執行，`expected/<case>.jsonl` 是每一步的 `proto.Snapshot` JSON。
T10 與 T20 重構後必須讓這裡的 diff 只剩預期的欄位變動。

## 檔案

- `cases.json`：案例、每一步的按鍵、每個案例的引擎設定。
- `data/lexicon.tsv`、`data/lm.tsv`：固定詞庫與語言模型。與 `build/data`、`data/` 無關，
  所以資料包改版不會讓行為 baseline 失敗；反過來說，這裡只證明行為沒變，不證明選詞品質。
- `expected/`：baseline 本體，`-update` 產生。

## 執行

```sh
go run ./cmd/0c-golden            # 只比對（CI 跑這個）
go run ./cmd/0c-golden -update    # 以目前行為重寫 expected
go test ./internal/golden/        # 同一份比對，含案例數與決定性檢查
```

`-update` 後必須在 PR 說明為什麼行為改變是預期的；沒有說明就代表退步。

## 案例寫法

`cases.json` 的每個 action 只能填一種：

| 欄位 | 意義 |
|---|---|
| `syllables` | 以標準排列的完整按鍵打出每個音節（含聲調），一步一個音節 |
| `bare` | 逐符號打，空白是空白鍵；給聲調選填與分隔鍵 `'` 使用 |
| `keys` | 逐字元送按鍵，例如 `ji3hellot ` |
| `key` | 送一個具名鍵（`Enter`、`Down`、`ShiftDelete`、`Ctrl+,`） |
| `request` | 直接送協定請求（`select`、`page`、`forget`、`toggle`） |

`options` 對應外殼在 `OpenSession` 前的設定；虛線功能用套件內固定的替身：

- `rerank`：固定評分器，候選文字含 `prefer` 任一項就加分，用來測重排呼叫路徑與否決規則。
- `punct`：固定補標點器，在最後一個漢字前插入 `mark`，用來測 `punct.Edit` 的驗證與套用。

`seed` 只在需要可重現的個人資料起點時使用（例如自訂詞）。神經與標點的替身不是模型，
不反映品質，只證明契約有被走過。
