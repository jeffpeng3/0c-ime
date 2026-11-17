# 資料來源

`data/sources/` 有兩套帳本，用途不同：

| 路徑 | 範圍 |
|---|---|
| `ledger.tsv`、`ja-sources.tsv`、`ja-files.tsv` | 產品詞庫、中文資料與既有日文詞典來源 |
| `licenses/` | 產品資料包 NOTICE 用的授權全文與聲明 |
| `corpus/` | Phase 2 日文訓練語料：public core 與 CC BY-SA experimental 分開列帳 |

訓練語料的授權政策與 NOTICE 在 `legal/corpus/`。Public core 只收 CC BY 4.0、公共領域與經簽核的 permissive notice-only（目前限 IPADic、Unihan、0c curated）；CC BY-SA 只進 experimental；Mozc、UniDic 預設分支、SudachiDict、NEologin、JUMAN、Common Crawl 與 NEologd 不進 public core，UniDic BSD 分支另案審查。

新增或修改訓練語料來源時，先讀 `corpus/README.md`，不要直接改 `ledger.tsv`。產品詞庫與訓練語料是不同交付，不要互相混用來源 ID。
