# 混合語言 benchmark skeleton（P2-16）

這個目錄放情境 A/B/C 的小型可重現題庫。評分程式在 `internal/eval/mixed.go`，
目前只建立格式與指標，不執行引擎，也不設準確度門檻。P2-61 會接上公開語料與
真正的轉換器。

## 題庫

| 檔案 | 情境 | 主要語言 | 案例 | 內容 |
|---|---|---|---:|---|
| `scenario_a.json` | A | `ja` | 2 | 日文為主，混合注音與英文；含規格範例 |
| `scenario_b.json` | B | `zh` | 2 | 中文為主，Latin 仍可走日文，英文保留原文 |
| `scenario_c.json` | C | `en` | 2 | `kubectl`、`pCloud`、`GitHub`、個人詞彙 |

全部由 0c 撰寫，授權 CC0-1.0。每個檔案約 2 KB，足以人工檢查與逐題重現。

## Schema

`schema` 固定為 `1`。一個檔案只放一種情境。

```json
{
  "schema": 1,
  "name": "mixed-scenario-a",
  "scenario": "A",
  "primary_language": "ja",
  "license": "CC0-1.0",
  "source": "0c phase2 P2-16 hand-written fixture",
  "count": 2,
  "cases": [
    {
      "id": "a-canonical",
      "input": [
        { "script": "latin", "text": "watashiwa", "reading": ["わ", "た", "し", "は"], "language_hint": "ja" }
      ],
      "expect": [
        { "input_index": 0, "surface": "私わ", "language": "ja", "variant": "ja" }
      ]
    }
  ]
}
```

- `input[].script`：`zhuyin`、`kana`、`latin`、`digit`、`symbol`、`han`、`mixed`。
- `input[].text`：使用者實際輸入的按鍵或文字。
- `input[].reading`：正規化讀音單位；拉丁片段可留空，由 converter 決定。
- `input[].language_hint`：只是提示，不得拿來阻擋其他語言候選。
- `expect[]`：一對一對應 input，`input_index` 必須依序是 `0..n-1`。
- `expect[].accept`：詞面的其他可接受寫法。語言標籤與輸出變體仍須完全一致。
- `expect[].variant` 必須與 `language` 相容，例如 `zh` 配 `zh-Hant`、`ja` 配 `ja`、`en` 配 `en`。

載入時會檢查 schema、情境代號、主要語言、案例 ID 唯一性、`count`、input/expect
對齊、讀音單位、語言標籤、變體與空詞面。壞題庫直接載入失敗，不會產生全 0 報告。

## 指標

每個片段分開計分：

- `surface_rate`：詞面等於 `surface` 或 `accept` 之一。
- `language_rate`：語言標籤完全相同。
- `variant_rate`：輸出變體完全相同。
- `joint_rate`：以上三項都對。
- `by_language`：依參考語言分組的同一組數字。

語言切換以 input 片段索引為邊界，不是最終文字位移：

- `expected`、`predicted`：預期與預測的切換邊界數。
- `tp`、`fp`、`fn`：邊界集合的比較。
- `precision`、`recall`、`f1`：micro 平均。
- `exact_cases`：整句切換邊界完全相同的案例數。

功能 smoke 只回答「有沒有正常跑完」：

- `runs`、`failed_runs`、`panics`：runner 是否成功，panic 會被捕捉成失敗。
- `complete_cases`：每個 input 恰好一個預測片段。
- `text_correct`：最終文字等於參考文字。
- `text_matches_segments`：最終文字等於預測片段依序串接。
- `missing_segments`、`duplicate_segments`、`unknown_index_segments`、`invalid_segments`：
  結構錯誤計數。`invalid_segments` 檢查空詞面、無效語言、無效變體與語言/變體不相容。

`internal/eval/mixed.go` 的 `RunMixed` 接受 `MixedRunner`，P2-61 只要替換 runner
就能用同一份題庫比較不同引擎。報告含題庫 SHA-256，沒有延遲欄位；同一份題庫
與 runner 連跑兩次，`MixedDeterminismSignature` 必須完全相同。

## 執行

```sh
go test ./internal/eval -run Mixed
```

測試涵蓋：題庫載入、完美 runner、錯字、語言標錯、缺段、重複 index、未知 index、
runner panic、nil runner、`accept` 詞面、可重現性與壞題庫拒絕。

目前沒有硬性準確度門檻。等功能與語料穩定後，P2-61 再決定是否凍結 baseline。
