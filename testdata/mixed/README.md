# 混合語言 benchmark（P2-61）

這個目錄是情境 A/B/C 的可重現混合語言題庫。`internal/eval/mixed.go` 定義格式與
指標，`internal/eval/mixed_session.go` 用真正的 profile-free session 驅動
注音、日文羅馬字與 Latin，`internal/eval/mixed_bench.go` 負責固定種子順序、
manifest 與跨情境彙總。

題庫由 0c 撰寫，授權 CC0-1.0。每個檔案約 3–5 KB，足以人工檢查與逐題重現。

## 題庫

| 檔案 | 情境 | 主要語言 | 案例 | 內容 |
|---|---|---|---:|---|
| `scenario_a.json` | A | `ja` | 5 | 日文為主，混合注音與英文；含規格範例 `watashiwa` 與長詞合併 |
| `scenario_b.json` | B | `zh` | 4 | 中文為主，Latin 仍可走日文，英文保留原文；含注音長詞合併 |
| `scenario_c.json` | C | `en` | 4 | 命令列工具、產品名、大小寫與數字 |

13 個案例、33 個片段。`noisy:*` 標籤把雜訊變體獨立分組：

| tag | 意義 |
|---|---|
| `clean` | 沒有額外雜訊的句子 |
| `noisy:no-space` | 注音與英文之間沒有空白 |
| `noisy:mixed-case` | 英文大小寫混用，不得被改成小寫或另一語言 |
| `noisy:digits` | 數字與英數版本字串保留原文 |
| `noisy:long-match` | 長詞跨過 input 片段合併；用 `merged_segments`／`missing_segments` 觀察 |

`noisy:long-match` 不是「應該兩段都對」的題目。詞庫把 `麻婆豆腐`、`東京大学`
合成一個片段時，後一個 input 沒有自己的輸出片段；benchmark 如實計入合併與
缺段，而不是把合併後的字串重複算兩次。

## Schema

`schema` 固定為 `1`。一個檔案只放一種情境，並記錄固定種子 `seed: 61`。

```json
{
  "schema": 1,
  "name": "mixed-scenario-a",
  "scenario": "A",
  "primary_language": "ja",
  "seed": 61,
  "license": "CC0-1.0",
  "source": "0c phase2 P2-61 fixture (extended from P2-16 skeleton)",
  "count": 1,
  "cases": [
    {
      "id": "a-canonical-mixed",
      "tags": ["clean"],
      "input": [
        {
          "script": "latin",
          "text": "kyou",
          "reading": ["き", "ょ", "う"],
          "language_hint": "ja",
          "composition": "ja-1"
        }
      ],
      "expect": [
        { "input_index": 0, "surface": "今日", "language": "ja", "variant": "ja" }
      ]
    }
  ]
}
```

- `input[].script`：`zhuyin`、`kana`、`latin`、`digit`、`symbol`、`han`、`mixed`。
- `input[].text`：使用者看到的按鍵或文字。
- `input[].reading`：正規化讀音單位。注音片段必填，並由標準排列推出按鍵；
  羅馬字片段填かな讀音，作為片段對齊的單位。
- `input[].keys`：實際按鍵。只有 `kana` 這類沒有通用鍵盤對應的片段必填
  （填羅馬字）；其餘片段可由 `reading` 或 `text` 推出。
- `input[].composition`：同一個非空名稱的連續片段一起打完才送出。空字串
  代表該片段自己送出。目前產品在換語言時以 Enter 結束前一段組字，題庫用
  composition 表達這個邊界；名稱必須連續。
- `input[].language_hint`：只是提示，runner 與引擎都不得用它阻擋其他語言候選。
- `case.tags`：至少一個，必須是上面列出的標籤。
- `expect[]`：一對一對應 input，`input_index` 必須依序是 `0..n-1`。
- `expect[].accept`：詞面的其他可接受寫法；語言標籤與輸出變體仍須完全一致。
- `expect[].variant` 必須與 `language` 相容，例如 `zh` 配 `zh-Hant`、`ja` 配 `ja`、`en` 配 `en`。

載入時會檢查 schema、情境代號、主要語言、種子、tag、composition 連續性、
按鍵推導條件、案例 ID 唯一性、`count`、input/expect 對齊、讀音單位、語言
標籤、變體與空詞面。壞題庫直接載入失敗，不會產生全 0 報告。

## 驅動方式

每個 case 一個新的 engine／session；同一個 case 只在一個 session 內送按鍵，
不呼叫 `SetLanguage`、不切換模式。runner：

1. 依 `keys`／`reading`／`text` 送實際按鍵；
2. 每段 composition 結束時讀取畫面；日文片段送一次 `Convert`，讓輸出是
   統計轉換首選而不是かな；再把尚未定案的中英按鍵 flush 成片段；
3. `Enter` 送出，記錄實際 commit 文字；
4. 依**讀音單位**（`reading` 長度；字面片段用 rune 數）把引擎片段對回
   input 索引。

沒有設定 `OC_JADIC_REFERENCE_PACK` 時，runner 仍開啟日文 runtime，羅馬字會
組成かな，但沒有漢字候選；報告會標記 `pack=none`。這只是 wiring 檢查，不是
日文轉換品質。

## 指標

| 指標 | 定義 |
|---|---|
| `top1` / `segment_top1_rate` | 引擎實際顯示／送出的那條路徑，詞面等於 `surface` 或 `accept` 的比例。 |
| `lang`、`var` | 語言標籤、輸出變體完全相同的比例。 |
| `joint` | 詞面、語言、變體三項都對的比例；這是片段的正確率。 |
| `swP`、`swR`、`swF1` | 以 input 片段邊界計算的語言切換 precision／recall／F1。沒有預期切換時顯示 `n/a`。 |
| `swEx` | 整句切換邊界完全相同的案例數。 |
| `sent` | 整句送出文字完全等於參考文字（`whole-sentence exact`）。`accept` 不影響這一項。 |
| `runs` | 成功跑完的案例數；runner 回傳錯誤或 panic 都算失敗。 |
| `done` | 每個 input 恰好一個預測片段、沒有重複／未知 index 的案例數。 |
| `miss` | 沒有預測片段的 input 數。 |
| `merged` | 一個輸出片段跨過多個 input 邊界的次數。 |

`by_language` 另外依參考語言分組同一組片段指標。`invalid_segments` 檢查空
詞面、無效語言、無效變體與語言／變體不相容。

## Manifest 與重現

`bench.manifest.json` 由 `cmd/0c-eval-mixed build` 產生，記錄：

- `seed` 與每份題庫的檔名、名稱、情境、案例數、SHA-256；
- `case_order_sha256`：固定種子排序後的案例順序；
- `pack`：日文參考 pack 的檔名與 SHA-256（目前是
  `8e57a18deb64d0fdc8147b53b5e234ecda412fa49feb0e8096addc4349582840`）；
- `data`：注音 fixture 資料的 `lexicon.tsv`／`lm.tsv` SHA-256；
- `models.used` 固定為 `false`。

題庫或 pack 換了就要故意重跑 build；測試會逐位元比對 committed manifest。
報告沒有延遲欄位，同一份題庫與 runner 連跑兩次必須完全相同。

## 模型與品質聲明

這份 benchmark **不載入任何模型**。參考資料夾的 int8 ONNX 檔是 placeholder；
任何載入它們的執行都只能標成 `wiring_only`，不得當作品質數字。manifest 的
`models.used` 一旦是 true，報告與表格都會標記 wiring-only。

日文參考答案是統計轉換的首選，不是人工標註；注音 fixture 使用 repo 內固定
的 `testdata/golden/zh/data`，不是完整產品詞庫。因此這些數字是 baseline 與
改進依據，不是上線門檻。

## 已知限制

- 目前產品在一個組字內從日文切回注音不會重新推斷語言；題庫因此把語言切換
  放在 composition 邊界（使用者實際會按 Enter）。
- `kubectl` 不在英文原型詞表，單獨打會被日文 runtime 當成原文並吃掉尾端
  空白；情境 C 用 `kubernetes`，並在檔案備註記錄這個限制，而不是調整評分。

## 執行

```sh
go test ./internal/eval -run 'Mixed'

# 重建 manifest（題庫改動後）
go run ./cmd/0c-eval-mixed build

# 沒有參考 pack：kana-only wiring
go run ./cmd/0c-eval-mixed run -twice

# 有參考 pack：lexicon + 統計轉換的 baseline
OC_JADIC_REFERENCE_PACK=/path/to/ja-lexicon-public-core.pack \
  go run ./cmd/0c-eval-mixed run -twice -out /tmp/mixed.json
```

測試涵蓋：題庫載入與壞題庫拒絕、完美 runner、錯字、語言標錯、缺段、重複／
未知 index、runner panic、nil runner、`accept` 詞面、固定種子順序、manifest
釘選與重建、真引擎 runner（有／無 pack）、長詞合併觀察、wiring-only 標記，
以及同一份題庫連跑兩次的逐位元可重現性。
