# 資料來源

`data/sources/` 有兩套帳本，用途不同：

| 路徑 | 範圍 |
|---|---|
| `ledger.tsv`、`ja-sources.tsv`、`ja-files.tsv` | 產品詞庫、中文資料與既有日文詞典來源 |
| `ja-mozc-3.35.6354.sha256` | Mozc OSS dictionary_oss 固定版本的逐檔 SHA-256；原始資料不進 Git |
| `licenses/` | 產品資料包 NOTICE 用的授權全文與聲明 |
| `corpus/` | Phase 2 日文訓練語料：public core 與 CC BY-SA experimental 分開列帳 |

訓練語料的授權政策與 NOTICE 在 `legal/corpus/`。Public core 只收 CC BY 4.0、公共領域與經簽核的 permissive notice-only（目前限 IPADic、Unihan、0c curated 與 UniDic 的 BSD-3-Clause 分支）；CC BY-SA 只進 experimental；Mozc、UniDic 的 CC BY-NC-SA 與古文／方言版本、SudachiDict、NEologin、JUMAN、Common Crawl 與 NEologd 不進 public core。UniDic 的授權全文在 `legal/unidic/`。

新增或修改訓練語料來源時，先讀 `corpus/README.md`，不要直接改 `ledger.tsv`。產品詞庫與訓練語料是不同交付，不要互相混用來源 ID。
