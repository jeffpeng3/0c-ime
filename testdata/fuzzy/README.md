# 模糊輸入評測題庫（P2-62）

這裡凍結從既有單語言題庫衍生的合成錯鍵題庫：

- 注音：`testdata/eval/zhuyin-golden.json` 的 92 個詞片段。
- 日文：`testdata/eval/ja-lexicon-ambiguous.json` 保留切片中，以固定 seed
  `62` 取樣的 200 個讀音。

每個來源案例只做一種擾動。題庫不訓練、不學習、不改產品排序預設，也不載入
任何模型；參考資料夾的 int8 placeholder 不會出現在這條路徑。

## 擾動種類

| 種類 | 注音 | 日文 |
|---|---|---|
| `wrong-key` | 把一個標準注音按鍵換成實體相鄰鍵，且換出的符號不同。 | 把一個羅馬字按鍵換成 QWERTY 相鄰字母。 |
| `missing-key` | 刪掉一個按鍵。 | 刪掉一個羅馬字按鍵。 |
| `doubled-key` | 重複一個按鍵。靜態注音排列會把同一個鍵重按視為同一符號，所以多數案例的輸入與原輸入相同；這是實際的冪等行為。 | 重複一個羅馬字按鍵（可能形成促音或無效前綴）。 |
| `near-sound` | 用 `core/fuzzy` 的近音配對（ㄗ／ㄓ、ㄘ／ㄔ、ㄙ／ㄕ、ㄣ／ㄥ）換一個音節，聲調不變。 | 不適用。 |

位置由 `SHA-256(seed + 用途 + 來源 ID)` 決定，不依 map 走訪、時間或 Go
版本；同一份來源與參數重建會逐位元相同。

## 指標

每個語言×擾動種類都有同一組分母 `n`：

| 指標 | 定義 |
|---|---|
| `raw.top1_rate` | 直接打模糊輸入後，預期文字在產品候選清單第一名的比例。 |
| `raw.top5_rate` | 預期文字在候選前五名的比例。 |
| `raw.intended_rate` | 不選字直接提交時，提交文字等於預期文字的比例。 |
| `fallback.top1_rate` / `fallback.top5_rate` | 同一批候選交給 `score.Ranker` 的統計回退角色排序後，預期文字的名次。 |
| `fallback.intended_rate` | 回退角色的第一名等於預期文字的比例；與 `raw.intended_rate` 的差額就是「直接提交」與「回退解析」的差。 |

注音候選在產品預設的 `select` 模糊音模式下取得（開候選時合併近音，不改
自動顯示），和設定檔的預設一致。日文候選由同一個統計轉換器的 N-best 取得，
再交給沒有模型的 `score.Ranker` 回退排序。

## 分母與降級

兩個角色都使用完整 `n`。下列情況只記在 `degraded`，不會把案例移出分母：

- 打字／轉換／送出錯誤、`no_session`、`ja_session`
- 沒有讀音（`no_reading`）
- 沒有日文參考 pack（`no_decoder`）
- 候選清單是空的（`no_candidates`）
- 提交是空的（`no_commit`）
- 羅馬字按鍵沒有被 session 接收（`unhandled_keys`）

沒有日文 pack 時，日文案例仍然計入 `n`，`no_decoder` 會增加，候選指標全部
算未命中。這些數字不可引用為日文品質；有 pack 才能比較 top-1／top-5。

`score.Ranker` 的 `fallback`、`timeout`、`privacy` 都留在請求分母裡；報告的
`ranker_counters` 會顯示 `requests == model + fallback + timeout + privacy`。
本題庫沒有模型，正常執行的 `model` 是 0，所有有候選的請求都由統計回退服務。

## 檔案與重現

- `fuzzy-input.json`：919 題（注音 319、日文 600）。
- `fuzzy-input.manifest.json`：題庫 SHA-256、來源題庫 SHA-256、seed、種類與
  語言計數，以及 `models.used=false`。

```sh
go run ./cmd/0c-eval-fuzzy build
go run ./cmd/0c-eval-fuzzy run -languages zh
OC_JADIC_REFERENCE_PACK=/path/to/ja-lexicon-public-core.pack \
  go run ./cmd/0c-eval-fuzzy run -twice -out /tmp/fuzzy-report.json
go test ./internal/eval -run 'TestFuzzy'
```

`run` 會先比對 manifest 與題庫雜湊；不符就直接失敗，不會默默跑別份資料。
`-twice` 比較所有非延遲欄位，兩次必須逐位元相同。

## 授權

注音來源是專案自有的 CC0-1.0 golden fixture。日文的讀音與詞面沿用
`ja-lexicon-ambiguous.json` 的授權與出處標示；題庫只保存衍生後的短讀音、
詞面與按鍵，不重新散布詞典檔。
