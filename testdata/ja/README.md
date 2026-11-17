# 日文轉換評測（T33）

這個目錄是凍結的日文評測題庫。評分程式在 `internal/eval/ja*.go`，命令列在
`cmd/0c-eval-ja/`。同一份資料與同一份資料包會得到相同的品質數字；延遲只在同一台機器上比較。

## 題庫

| 檔案 | 題數 | 邊界題數 | 來源 | 授權 |
|---|---:|---:|---|---|
| `anthy_corpus1.json` | 200 | 199 | Anthy 9100h `calctrans/corpus.1.txt`，固定 seed `20261117` | Public Domain |
| `ajimee_200.json` | 200 | 0 | [AJIMEE-Bench](https://github.com/azooKey/AJIMEE-Bench) JWTD_v2/v1，commit `401666cd` | CC-BY-SA-3.0 |
| `modern_500.json` | 500 | 500 | 為 0c 撰寫的樣板現代日文句 | CC0-1.0 |
| `hard_cases.json` | 35 | 35 | 為 0c 撰寫的同音詞與文節案例 | CC0-1.0 |

Anthy 題庫只讀 `corpus.1.txt`。`corpus.2.txt`、`corpus.3.txt`、`corpus.5.txt`
沒有授權聲明，`corpus.4.txt` 是已知錯誤轉換而非正解，四個檔案都沒有進入任何 fixture。
`cmd/0c-eval-ja/tools/build_anthy_fixture.py` 會拒絕 `corpus.1.txt` 以外的檔名，並檢查
前 5 行含 `public domain`；題庫內容也必須完全等於 T03 的
`scripts/spike/ja-zenz/fixtures/anthy_sample200.json`。`internal/eval/ja_test.go`
的 provenance 測試會重跑這兩個檢查。

Anthy 有一行 `|とあるばしょで|...` 的讀音與詞面文節數不同，保留在 top-1/top-5
題庫內，但不計入文節邊界，所以邊界題數是 199。

AJIMEE 的輸入是片假名，fixture 把片假名轉成平假名後交給 0c 的讀音介面；預期輸出逐字保留，
第二個以後的答案放在 `accept`，top-1 與 top-5 都接受。AJIMEE 沒有文節標注，不計邊界。
授權全文在 `LICENSE-ajimee.txt`。

`modern_500.json` 是五個句型各 100 句的樣板語料，不是自然語料抽樣。讀音與文節由同一個
slot 機械產生。它的用途是穩定回歸，不是宣稱代表現代日文分布。

`hard_cases.json` 覆蓋 橋/箸、雨/飴、髪/紙、花/鼻、雲/蜘蛛、撮/取、帰/変 等
同音詞，以及動詞、助詞、補助動詞的文節邊界。

## 凍結規則

`rules.json` 在跑之前凍結。它釘住每個題庫檔的 SHA-256、題數、邊界題數、授權與來源，
也定義指標和 CI 門檻。`baseline.json` 記錄 `rules.json` 的雜湊與當時的資料包雜湊。
只要題庫、規則或資料包換了，gate 會要求刻意更新 baseline，不會默默把門檻搬走。

指標：

- `top1_rate`：排名第 1 的結果等於正解或 `accept` 其中之一。
- `top5_rate`：前 5 個不重複結果中任一個等於正解或 `accept` 其中之一。
- `boundary_exact_rate`：第 1 名結果的文節邊界集合與正解完全相同。句尾不是邊界；
  標點前的邊界不計，因為標點不是文節。
- `boundary_precision`、`boundary_recall`、`boundary_f1`：以邊界位置為單位的 micro 平均。
- `latency_p50_ms`、`latency_p95_ms`：每題一次 `NBest(5)` 的 nearest-rank 百分位。
- `timeout_rate`：單題轉換超過 `timeout_ms`（100 ms）的比例。這是計數，不會取消轉換。

同一份資料連跑兩次，非延遲結果必須完全相同。`-determinism` 會直接比對兩次報告的
品質欄位與逐題輸出。

## 目前 baseline

資料包是 repo 內的 `data/ja` 種子包（144 詞條），不是 Mozc 全量包。因此 Anthy 與
AJIMEE 的分數很低，這是冷啟動數字，不是轉換器上限。T13 換成全量資料包後要重跑並
更新 baseline。

| 題庫 | n | top1 | top5 | 邊界 exact | 邊界 F1 |
|---|---:|---:|---:|---:|---:|
| `ajimee-200` | 200 | 1.5% | 1.5% | 不計 | 不計 |
| `anthy-corpus1` | 200 | 2.5% | 2.5% | 1.5% | 0.253 |
| `modern-500` | 500 | 23.2% | 23.2% | 14.4% | 0.445 |
| `hard-cases` | 35 | 14.3% | 14.3% | 14.3% | 0.238 |

延遲只記在 `baseline.json` 的 `*_informational` 欄位，不進 CI 門檻；共享 runner 的
延遲不穩定。品質門檻的允許跌幅全部是 0：任何一個題庫的 top-1、top-5、邊界 exact
或邊界 F1 下降，`go test ./internal/eval` 就會失敗。

T03 spike 另有兩個外部參考數字，不由這個 harness 產生，也不進 gate（T03 是非正式 spike，不是產品路徑）：

| 引擎 | 資料 | top1 |
|---|---|---:|
| Anthy 9100h，每段 candidate 0 | corpus.1 200 題 | 80.0% |
| zenz-v3.1-small Q5_K_M，free greedy | corpus.1 200 題 | 47.5% |

Anthy 的 80.0% 是它在訓練語料上的分數，明顯偏高；zenz 的 47.5% 沒有 Zenzai 限制式
重搜，不是神經轉換上限。兩者與本目錄的 0c 種子包不可直接比較。

## 執行

```sh
# 摘要
go run ./cmd/0c-eval-ja

# 完整逐題 JSON
go run ./cmd/0c-eval-ja -out /tmp/ja-report.json

# 連跑兩次，確認非延遲結果相同
go run ./cmd/0c-eval-ja -determinism

# CI gate
go run ./cmd/0c-eval-ja -gate

# 刻意更新 baseline（換題庫、換資料包或規則後）
go run ./cmd/0c-eval-ja -write-baseline testdata/ja/baseline.json
```

Go 測試：

```sh
go test ./internal/eval ./cmd/0c-eval-ja
```

`TestJACorpusProvenance`、`TestJAFixtureReusesT03Corpus1`、
`TestJARunDeterministic`、`TestJARegressionGate` 分別檢查授權邊界、T03 重用、
可重現性與品質門檻。

## 重建題庫

Anthy（只讀 `corpus.1.txt`）：

```sh
python3 cmd/0c-eval-ja/tools/build_anthy_fixture.py \
  --corpus /path/to/anthy-9100h/calctrans/corpus.1.txt \
  --t03-fixture scripts/spike/ja-zenz/fixtures/anthy_sample200.json \
  --out testdata/ja/anthy_corpus1.json
```

AJIMEE：

```sh
python3 cmd/0c-eval-ja/tools/build_ajimee_fixture.py \
  --items /path/to/AJIMEE-Bench/JWTD_v2/v1/evaluation_items.json \
  --out testdata/ja/ajimee_200.json
```

自建語料與困難案例：

```sh
python3 cmd/0c-eval-ja/tools/build_modern_fixture.py --out testdata/ja/modern_500.json
python3 cmd/0c-eval-ja/tools/build_hard_fixture.py --out testdata/ja/hard_cases.json
```

重建後 `rules.json` 的 SHA-256 會變，gate 會失敗；這是刻意的。確認新題庫後再更新
`rules.json` 與 `baseline.json`。

## 環境與對照

這次沒有 Mozc 或 Zenzai 可執行環境，所以沒有跑即時對照；T03 的 Anthy 與 zenz
數字已保存在上面（T03 spike，非正式產品路徑）。`-data` 可以指向任何具有 `lexicon/words.tsv` 與 `connection.tsv`
的資料包，P2-30 接上 ONNX 重排後可用同一份題庫比較，不需要改評分規則。
