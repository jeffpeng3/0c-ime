# Phase 2 訓練語料來源帳本

P2-20 產出。這張帳本管日文訓練語料、評測語料與 public core 詞典來源，不管產品詞庫。產品詞庫、中文資料與既有日文詞典來源仍以 `data/sources/ledger.tsv`、`data/sources/ja-sources.tsv`、`data/sources/ja-files.tsv` 為準。

## 1. 兩條軌道

Public core 與 CC BY-SA experimental 是兩個獨立資料集，不共用輸出目錄、manifest、checkpoint 或安裝包。

| 軌道 | 允許授權 | 用途 | 檔案 |
|---|---|---|---|
| Public core | CC BY 4.0、公共領域、permissive notice-only | 公開核心 reranker、標點模型與日文詞典 | `public-core.tsv` |
| Experimental | CC BY-SA 2.5、3.0、4.0 | 獨立實驗模型與評測 | `experimental.tsv` |
| 暫緩 | 宣稱可用，但 upstream 條款或適用範圍待確認 | 法律審查，暫不進任何一軌 | `review.tsv` |
| 排除 | 不是 CC BY 4.0、公共領域或 permissive notice-only，或已明確禁止 | 不進 public core | `excluded.tsv` |

規則：

1. Public core 只使用 `public-core.tsv` 中 `status=approved` 或 `approved-with-caveat` 的來源。`kind=lexicon` 的列由 P2-22 詞典管線使用，不進 P2-21 語料 tokenization。
2. CC BY-SA 資料只進 experimental；不得與 public core 混用、去重後合併或共用 tokenized binary。
3. `review.tsv` 與 `excluded.tsv` 的來源不得進 public core。要改狀態，先改帳本並留下審查依據。
4. Mozc、SudachiDict、NEologin、JUMAN、Common Crawl 與 NEologd 不進 public core。Common Crawl 的衍生資料（CC-100、OSCAR、FineWeb-2、LLM-jp `ja_cc`／`ja_fineweb-2`）同樣不進。UniDic 只准 `pc-unidic` 的 BSD-3-Clause 分支，而且只限現代書き言葉（`cwj`）與現代話し言葉（`csj`）；CC BY-NC-SA 版本、古文用與方言用版本列 `excluded.tsv` 的 `x-unidic`。
5. 新聞、5ch、Twitter/X 沒有逐站書面授權前，public core 與 experimental 都不用。
6. permissive notice-only 只適用經 2026-11-17 決策簽核的來源：IPADic 2.7.0、Unicode Unihan、0c 自有 curated，以及 2026-11-17 第二修訂簽核的現代書き言葉UniDic BSD-3-Clause 分支。散布時必須保留完整著作權、許可與免責聲明；這不是新的通用授權類別。

## 2. 檔案

| 檔案 | 內容 |
|---|---|
| `public-core.tsv` | 已核准的 public core 語料、評測與詞典來源 |
| `experimental.tsv` | CC BY-SA 語料、評測與模型來源 |
| `review.tsv` | 授權宣告或適用範圍待確認，暫緩使用 |
| `excluded.tsv` | 明確排除的來源與理由 |
| 本檔 | 欄位定義、雜湊規則、P2-21 入口條件與調查結論 |

授權全文與 NOTICE 在 `legal/corpus/`；CC BY 4.0 與 CC BY-SA 3.0 全文在 `legal/`。

## 3. 欄位

`public-core.tsv` 與 `experimental.tsv`：

| 欄位 | 說明 |
|---|---|
| `id` | 帳本內唯一 ID；P2-21 manifest 直接引用 |
| `kind` | `corpus`、`eval`、`model`、`lexicon` |
| `name` | 來源名稱 |
| `provider` | 資料提供者與上游 |
| `url` | 主要散布網址；多個網址用 ` ; ` 分隔 |
| `version` | 快照、commit、dump 名稱、檔案大小、token 數 |
| `hash` | 上游版本 ID 或檔案雜湊；逐檔 SHA-256 在取得時補 |
| `license` | 帳本判定的授權類別：`CC-BY-4.0`、`Public-Domain`、`Permissive-Notice-Only`，後者註明來源條款 |
| `license_url` | 授權全文或官方條款 |
| `attribution` | NOTICE 必須出現的署名文字 |
| `scope` | `train`、`eval`、`model`、`lexicon` |
| `status` | 只用於 public core：`approved`、`approved-with-caveat`、`blocked` |
| `distributable` | `yes`、`yes-with-conditions`、`yes-share-alike` |
| `release` | 只用於 experimental：衍生語料與模型的釋出授權 |
| `notes` | 邊界、排除項目、轉換與風險 |

`experimental.tsv` 沒有 `status` 欄；所有列都是 experimental，不得進 public core。`release` 欄記錄 experimental 模型與衍生語料的釋出授權。

`review.tsv` 與 `excluded.tsv` 的欄位見各檔開頭。

## 4. 版本與雜湊

本階段不下載語料。帳本先固定上游版本 ID：

- Hugging Face dataset：repo revision SHA。
- LLM-jp Corpus v4：Git tree ID。這是 Git 物件 ID，不是檔案內容的 SHA-256。
- Wikipedia dump：檔名、大小、MD5。
- Stack Exchange dump：檔名、大小、MD5、SHA-1。
- 模型：revision 與檔案 SHA-256。
- 詞典：Git commit 或上游版本頁；IPADic 的 tarball、Unihan 的 `Unihan.zip` 與 curated 檔案的 SHA-256 由 P2-22 取得時寫入 manifest。

P2-21A／P2-21B 下載後，必須逐檔計算 SHA-256，寫進各自的 `manifest.json`。manifest 至少要有：帳本 `id`、實際 URL、下載日期、位元組數、SHA-256、授權、署名、轉換步驟、去重與 holdout 規則。沒有 SHA-256 的檔案不得進訓練。

## 5. 目前調查結論

Public core 可用來源：

- 青空文庫，經 `globis-university/aozorabunko-clean`，CC BY 4.0；16,951 件、241 MB。
- e-Gov 法令，經 `nlp-waseda/e_gov`，CC BY 4.0；11,347 法令、433 MB，2024-10-20 下載。
- KAKEN 研究課題概要，LLM-jp `ja_kaken`，CC BY 4.0；0.9B tokens。只取課題概要，不取作者保留著作權的研究報告。
- 特許公報文字，LLM-jp `ja_patent`，CC BY 4.0；68B tokens。
- SIP3 日本語汎用ウェブコーパス，CC BY 4.0；HTML 11B tokens、PDF 30B tokens。只取 `level0` 與 `pdf2text`。
- Anthy `corpus.1.txt`，公共領域；只供評測。

這些來源合計遠超過 10 到 20 GB 目標。P2-21A 應抽樣與去重，不需要整包下載。

Public core 詞典來源（P2-22）：

- mecab-ipadic-2.7.0-20070801，NAIST + ICOT Free Software，permissive notice-only；詞條、品詞、讀音、活用與連接成本。官方下載頁 https://taku910.github.io/mecab/#download；GitHub mirror commit 2fd29256c6d5e1b10211cac838069ee9ede8c77a。
- Unicode Unihan Database 18.0.0（UCD 18.0.0，2026-09-01），Unicode License V3，permissive notice-only；只取日文讀音與漢字屬性。`Unihan.zip` 在 https://www.unicode.org/Public/18.0.0/ucd/Unihan.zip。
- 0c curated 日文種子詞條，`data/ja/lexicon/words.tsv`，137 詞條，0c 自有，permissive notice-only。產品 `ledger.tsv` 的 `curated` 列仍為 CC BY-SA 4.0，本列不撤回該授權。
- 現代書き言葉UniDic `unidic-cwj-202512`（2025-12-31），NINJAL／The UniDic Consortium，三重授權選 BSD-3-Clause，permissive notice-only；只取詞條、品詞、讀音、活用與成本。散布時每個詞典包必附 `legal/unidic/BSD.txt`、`legal/unidic/BSD-unidic-cwj-202512.txt` 與 `legal/unidic/AUTHORS.md`。NINJAL 沒有獨立 `AUTHORS` 下載，artifact 內也沒有 `AUTHORS`。

UniDic BSD 分支（`unidic-cwj-202512`）在 2026-11-17 第二修訂簽核通過，改列 `public-core.tsv` 的 `pc-unidic`；`review.tsv` 的 `rev-unidic-bsd` 已結案移除。CC BY-NC-SA 版本、古文用與方言用版本仍留在 `excluded.tsv` 的 `x-unidic`。

Experimental 可用來源：

- 日本語版 Wikipedia dump，CC BY-SA 4.0（文字另可依 GFDL）。
- LLM-jp `ja_wiki`，CC BY-SA 3.0，2023-07-20 版。
- Stack Exchange `ja.stackoverflow.com` 與 `japanese.stackexchange.com`，CC BY-SA 4.0。
- AJIMEE-Bench JWTD，CC BY-SA 3.0，只供評測。
- zenz-v3.1-small GGUF，CC BY-SA 4.0，只供 experimental 模型。

## 6. 關鍵發現

1. **LLM-jp Corpus v4 不是整體 CC BY 4.0。** 同一包內有 CC BY 4.0、ODC-BY、CC BY-SA 3.0 與 Common Crawl 條款。只能逐 subset 引用，不能整包當 public core。
2. **KAKEN 有例外。** 研究課題概要可用 CC BY 4.0；2015 年度以前部分研究報告的著作權屬作者，不能一併收。
3. **國會會議紀錄與 WARP 的 upstream 條款不支持 blanket CC BY 4.0。** 國會會議紀錄個別發言著作權屬發言者；WARP 保存內容著作權屬原網站。兩者都放 `review.tsv`。
4. **NWC2010 原頁沒有明示授權。** LLM-jp 的 `ja_nwc2010` 標 CC BY 4.0，但 upstream 沒有對應授權文字。放 `review.tsv`。
5. **SudachiDict 是 Apache-2.0，不屬於 public core 三類。** 且含 UniDic 衍生詞條。只供 P2-22 詞典評估，不進 public core corpus。
6. **Mozc 是混合授權。** 不可只看 Mozc 程式碼的 BSD-3-Clause。P2-20 明確排除。
7. **Common Crawl 衍生資料全部排除。** 即使 dataset card 標 ODC-BY 或 CC BY-SA，也不符合 public core 的授權條件。
8. **LLM-jp 自己也拿掉不能重散布的來源。** NWJC 與 Ceek.jp News 不在 Corpus v4 公開資料集；兩者列 `excluded.tsv`。不要從其他管道補回來。
9. **IPADic 與 Unihan 是 permissive notice-only，不是 CC BY 4.0 或公共領域。** 2026-11-17 決策單獨允許這兩類與 0c curated 進 public core；條件是保留完整著作權、許可與免責聲明。IPADic 不可只寫 BSD-3-Clause；Unihan 不可只寫「Unicode」。
10. **UniDic BSD 分支已簽核，但範圍很窄。** NINJAL FAQ 明示授權附著於解析用UniDic 自体，可在 GPL／LGPL／BSD 三者擇一；本專案選 BSD-3-Clause，只准 `unidic-cwj-202512` 與同系列的現代話し言葉。CC BY-NC-SA 版本、古文用與方言用字典仍在 `x-unidic`，不得混用。BSD 的義務是保留著作權、條件與免責聲明，並且每次都附 `BSD` 與著作者表示。

## 7. P2-21 入口條件

P2-21A（public core）：

1. 只讀 `public-core.tsv` 的 `approved` 與 `approved-with-caveat`，並跳過 `kind=lexicon`；詞典由 P2-22 處理。
2. 每個來源產生獨立 manifest；記錄 SHA-256。
3. 正規化、去重、document-group holdout 後才 tokenize。
4. 不把 experimental 資料寫進同一輸出目錄。
5. 公開產出附 `legal/corpus/NOTICE-public-core.md` 產生的 NOTICE。

P2-21B（experimental）：

1. 只讀 `experimental.tsv`。
2. 獨立目錄、獨立 manifest、獨立 checkpoint。
3. 保留逐帖或逐條目的署名資訊；Stack Exchange 需要作者與原問題 URL。
4. 衍生語料與 experimental 模型以 CC BY-SA 4.0 釋出。
5. 公開產出附 `legal/corpus/NOTICE-experimental.md` 產生的 NOTICE。

## 8. 變更流程

新增來源時：

1. 先在對應 TSV 加一列，填齊 URL、版本、授權、署名、邊界。
2. 授權不是 CC BY 4.0、公共領域或經簽核的 permissive notice-only，不能進 `public-core.tsv`。permissive notice-only 目前只限 IPADic、Unihan、0c curated 與 UniDic 的 BSD-3-Clause 分支。
3. 授權有疑問，先放 `review.tsv`，不要先用再補。UniDic BSD 分支就是先放 `rev-unidic-bsd`、簽核後才進 `public-core.tsv` 的例子。
4. 改動 `public-core.tsv` 或 `experimental.tsv` 後，重跑 P2-21 的 manifest 檢查。
5. 不把語料、壓縮檔或 tokenized binary 放進 Git。
