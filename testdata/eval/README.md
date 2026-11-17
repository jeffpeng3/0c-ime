# 單語言評測題庫（P2-60）

這裡凍結 Phase 2 的兩個單語言評測來源。兩份題庫都只讀固定的輸入，不會訓練、
學習或改變產品的排序預設：

- `zhuyin-golden.json`：從 `testdata/golden/zh` 的注音 golden 案例凍結而來。
- `ja-lexicon-ambiguous.json`：從真實 `ja-lexicon-public-core.pack` 取樣有多個
  漢字候選的讀音。

`baseline.json` 同時記錄注音的「不能退步」門檻與日文的 baseline 數字；日文
不是目標，也不是上線門檻。

## 指標

| 指標 | 定義 |
|---|---|
| `segment_top1_rate` | 注音：每個詞的候選第一名等於 golden 詞面的比例。 |
| `segment_mrr` | 注音：每個詞的參考詞面在候選清單中的 `1/rank` 平均；找不到記 0。 |
| `sentence_exact_rate` | 注音：整句提交文字完全等於 golden 的比例。 |
| `dictionary_top1_rate` / `dictionary_mrr` | 日文：只依 pack 的 signed i16 成本排序。參考答案依定義是成本最低的詞面，所以這組恆為 1；用途是驗證題庫、分母與參考規則，不是品質成就。 |
| `ranker_top1_rate` / `ranker_mrr` | 日文：`decode.Decoder.NBest` 產生統計候選後，交給 `score.Ranker` 在沒有模型時的特徵式回退排序。 |

日文的參考答案是**詞典偏好**（同讀音中成本最低的漢字詞面），不是人工標註。
題庫備註與 baseline 都明寫這一點，所以 `ranker_*` 只能當 baseline，不能宣稱
是日文轉換的準確率。

## 目前 baseline

注音（17 句、92 個詞）：

| 指標 | 數值 |
|---|---|
| `segment_top1_rate` | 1.0000（92/92） |
| `segment_mrr` | 1.0000 |
| `sentence_exact_rate` | 1.0000（17/17） |

日文（seed 60、取樣 1200、保留 400）：

| 切分 | n | dictionary top-1 | dictionary MRR | ranker top-1 | ranker MRR |
|---|---:|---:|---:|---:|---:|
| holdout（報告用） | 400 | 1.0000 | 1.0000 | 0.5000 | 0.6916 |
| dev（之後校準用） | 800 | 1.0000 | 1.0000 | 0.5025 | 0.6914 |
| all | 1200 | 1.0000 | 1.0000 | 0.5017 | 0.6915 |

`ranker_*` 的每一筆請求都是 `fallback`：目前沒有接任何日文模型，`score.Ranker`
以詞典成本特徵排序。基線與題庫都由同一份 pack 重建，沒有使用參考資料夾裡的
未訓練 placeholder 模型。

## 回歸門檻

- 注音：`gated: true`，三個指標的容許下降都是 0；題庫、詞庫或語言模型雜湊
  變動也直接失敗。任何下降都要先解釋，再故意重寫 baseline。
- 日文：`gated: false`。題庫與 pack／連接表雜湊不符仍會失敗（代表跑到的不是
  同一份題庫）；指標上升或下降只印出 `+/-` 差值。等有獨立標註的日文題庫後，
  才適合討論目標或硬性門檻。

`data/ja/connection.tsv` 是 repo 內固定的連接成本。lexicon pack 不含連接矩陣，
所以日文報告與 baseline 另外記錄它的 SHA-256；換連接表就要重新凍結 baseline。

## 重現

注音題庫不需要外部檔案：

```sh
go run ./cmd/0c-eval-single build-zh
go run ./cmd/0c-eval-single zh -gate
go test ./internal/eval -run 'TestZhGolden'
```

日文題庫需要參考 pack。`OC_JADIC_REFERENCE_PACK` 指向
`ja-lexicon-public-core.pack`；manifest 預設讀同目錄的 `manifest.json`：

```sh
export OC_JADIC_REFERENCE_PACK=/path/to/ja-lexicon-public-core.pack
go run ./cmd/0c-eval-single build-ja
go run ./cmd/0c-eval-single ja -gate
go run ./cmd/0c-eval-single ja -out /tmp/ja-report.json
go test ./internal/eval -run 'TestJALex'
```

`build-ja` 只從 pack 取樣，不從時間、環境或檔案走訪順序推導任何值：

- 取樣鍵：`SHA-256(seed + "\0sample\0" + reading)`，排序後取前 `sample` 個。
- 保留切片：同一批讀音再以 `SHA-256(seed + "\0holdout\0" + reading)` 排序，
  取前 `holdout` 個。
- 每個讀音只留全漢字詞面（允許 々、〆、ヶ、ヵ、〇），最多 `candidate_cap=6` 個，依 `(cost, surface)`
  排序；`reference` 是第一個。
- 讀音長度限制在 1–8 個かな。

同一份 pack 與參數產生的 JSON 必須逐位元相同；
`TestJALexBuildDeterministic` 會比對重建結果與提交的題庫雜湊。

## 雜湊與 manifest

`*.manifest.json` 是題庫的 sidecar：

- `zhuyin-golden.manifest.json`：題庫 SHA-256、`cases.json` 與參與案例的
  `expected/*.jsonl` 合併 SHA-256。
- `ja-lexicon-ambiguous.manifest.json`：題庫 SHA-256、取樣參數，以及 pack
  的檔案 SHA-256 `8e57a18deb64d0fdc8147b53b5e234ecda412fa49feb0e8096addc4349582840`
  和 `manifest.json` 的 SHA-256。

`baseline.json` 另外釘住題庫雜湊、注音資料的 lexicon/lm 雜湊、日文 pack 與
連接表雜湊；任何一項換了都不會默默沿用舊數字。

## 更新 baseline

只有在**故意**更換題庫、資料或接受已解釋的行為改變時才重寫：

```sh
go run ./cmd/0c-eval-single zh -write-baseline
OC_JADIC_REFERENCE_PACK=... go run ./cmd/0c-eval-single ja -write-baseline
```

`-write-baseline` 只覆寫對應語言的區段，不會動到另一個語言。PR 必須說明為什麼
舊 baseline 不再適用。

## Placeholder 模型

參考的 int8 placeholder 只在
`TestPlaceholderModelWiringOnly` 裡使用：測試用 `internal/neural.ValidateArtifact`
核對真實檔案的雜湊與 `placeholder=true`、`quality=untrained` 標記，再用中性分數
替身證明 `score.Ranker` 的模型路徑可以走通。推論本身另由既有的
`internal/neural -run TestPlaceholderModelProbe` 驗證；本測試不計算、也不寫入任何
品質指標。日文 benchmark 的兩組分數都不含模型。

```sh
OC_PLACEHOLDER_MODELS=/path/to/reference/models \
  go test ./internal/eval -run TestPlaceholderModelWiringOnly
```

## 檔案

- `zhuyin-golden.json`、`zhuyin-golden.manifest.json`
- `ja-lexicon-ambiguous.json`、`ja-lexicon-ambiguous.manifest.json`
- `baseline.json`
- 產生器與 runner：`cmd/0c-eval-single`、`internal/eval/zh_gold.go`、
  `internal/eval/ja_lexicon.go`、`internal/eval/single_baseline.go`
