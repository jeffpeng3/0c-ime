# 訓練語料授權與 NOTICE

P2-20 產出。範圍是 Phase 2 日文訓練語料、experimental 模型來源與 public core 詞典來源，不含產品詞庫。產品詞庫授權見 `data/sources/ledger.tsv` 與 `data/sources/licenses/`。

本目錄不是法律意見。CC BY-SA 的 share-alike 是否延伸到模型權重，依實際使用與司法管轄而定；本專案採保守政策：experimental 語料與模型都以 CC BY-SA 4.0 釋出，且不與 public core 混用。

## 檔案

| 檔案 | 用途 |
|---|---|
| `README.md` | 授權分軌、署名、修改說明、takedown 與 release 規則 |
| `NOTICE-public-core.md` | public core 資料與模型的 NOTICE 範本 |
| `NOTICE-experimental.md` | CC BY-SA experimental 資料與模型的 NOTICE 範本 |
| `../CC-BY-4.0.txt` | CC BY 4.0 完整條款 |
| `../CC-BY-SA-3.0.txt` | CC BY-SA 3.0 完整條款 |
| `../CC-BY-SA-4.0.txt` | CC BY-SA 4.0 完整條款 |
| `../UNICODE-NOTICE.txt` | Unicode License V3 完整條款（Unihan） |
| `../unidic/` | UniDic 的修正 BSD 全文（網站版與 artifact 版）、COPYING 與著作者表示 |

來源清單、版本、雜湊與逐來源署名在 `data/sources/corpus/`。

## 1. 分軌

| 軌道 | 可用授權 | 釋出 | 不可做的事 |
|---|---|---|---|
| Public core | CC BY 4.0、公共領域、permissive notice-only | 程式 AGPL-3.0-only；資料與模型依各自授權 | 不收 CC BY-SA、non-commercial、ODC-BY、Common Crawl 衍生、Mozc、UniDic 的 CC BY-NC-SA 與古文／方言版本、SudachiDict、NEologin、JUMAN、NEologd |
| Experimental | CC BY-SA 2.5／3.0／4.0 | 衍生語料與模型以 CC BY-SA 4.0 釋出 | 不混進 public core；不共用 manifest、checkpoint 或安裝包 |

Public core 的三類授權：

1. CC BY 4.0：保留署名、授權連結與修改說明。
2. 公共領域：寫明判定依據。
3. permissive notice-only：沒有 copyleft、沒有 non-commercial 限制，但散布時必須保留著作權、許可與免責聲明。2026-11-17 決策允許的來源限 IPADic 2.7.0（NAIST + ICOT）、Unicode Unihan（Unicode License V3）、0c 自有 curated，以及 2026-11-17 第二修訂簽核的現代書き言葉UniDic BSD-3-Clause 分支。這不是通用通行證；新來源仍須逐案審查並在 `data/sources/corpus/public-core.tsv` 留下允許理由。

Public core 與 experimental 必須有不同資料目錄、不同 manifest、不同 checkpoint。安裝包若同時提供兩組模型，必須在設定與檔案層分開標示，不能讓使用者誤以為同一組權重。

## 2. Public core NOTICE

對外散布 public core 資料或模型時，NOTICE 至少要包含：

1. 每個實際使用來源的 `id`、名稱、提供者、URL、版本或快照日期。
2. 帳本 `attribution` 欄的完整署名文字。
3. 授權名稱與全文連結，或隨附 `legal/CC-BY-4.0.txt`、`legal/UNICODE-NOTICE.txt`。
4. 修改說明：正規化、去重、過濾、抽樣、斷詞、tokenization、模型訓練。
5. 免責聲明：來源方未背書本專案，也未參與模型訓練。
6. 若來源是公共領域，寫明判定依據，例如作品著作權已消滅或法令不受著作權保護。
7. 若來源是 permissive notice-only，附完整著作權、許可與免責聲明。IPADic 的 NAIST 聲明與 ICOT NO WARRANTY 不得刪除；Unicode 要保留 copyright 與 permission notice。

`NOTICE-public-core.md` 是範本；實際 release 只列這次真正使用的來源。没有使用的來源不要放進 NOTICE，避免暗示使用範圍大於事實。

## 3. Experimental NOTICE

除 public core 的 1 到 6 點，還要：

1. 寫明 experimental 資料與模型以 CC BY-SA 4.0 釋出。
2. 保留來源的原始授權版本，例如 Wikipedia 條目文字是 CC BY-SA 4.0，LLM-jp `ja_wiki` 是 CC BY-SA 3.0。
3. Stack Exchange 需要逐帖作者與原問題 URL。若格式無法保留，不能只寫「Stack Exchange Network」就當成滿足署名。
4. 標明修改，並保留先前修改的指示。
5. 不得加上限制條款或有效技術措施，妨礙他人行使 CC BY-SA 授予的權利。
6. 不得暗示 Wikipedia、Stack Exchange、azooKey 或 Miwa-Keita 背書。

## 4. 逐來源重點

| 來源 | 重點 |
|---|---|
| 青空文庫 | 只取無著作權作品。保留作品 ID、図書カード URL、底本、入力者、校正者。 |
| e-Gov | 出典：e-Gov 法令検索（デジタル庁）。法令本文另依著作権法第13条不受著作權保護。 |
| KAKEN | 只取研究課題概要。2015 年度以前部分研究報告的著作權屬作者，不取。不得暗示 NII 背書。 |
| 特許庁 | 網站內容依 PDL1.0，與 CC BY 4.0 互換。個別圖表或第三人權利另有標示時，不取。 |
| SIP3 | 提供者以 CC BY 4.0 釋出。保留網站來源與 takedown 流程；只取 level0 與 pdf2text。 |
| IPADic 2.7.0 | NAIST + ICOT，permissive notice-only。保留完整著作權與 ICOT NO WARRANTY；不可只寫 BSD-3-Clause 或 CC BY 4.0。 |
| Unicode Unihan | Unicode License V3，permissive notice-only。保留 copyright 與 permission notice；只取日文讀音與漢字屬性。 |
| 0c curated | 0c 自有。public core 版本以 permissive notice-only 釋出；產品 `ledger.tsv` 的 `curated` 列仍是 CC BY-SA 4.0。 |
| UniDic BSD | `pc-unidic`，`unidic-cwj-202512`，三重授權選 BSD-3-Clause。每個散布的詞典包必附 `legal/unidic/` 的兩份 BSD 與 `AUTHORS.md`。只准現代書き言葉與現代話し言葉；CC BY-NC-SA、古文用與方言用版本不用。 |
| Wikipedia | 文字 CC BY-SA 4.0，另可依 GFDL。不取圖片、商標。 |
| Stack Exchange | CC BY-SA 4.0；2018-05-02 以前投稿可能是 2.5 或 3.0。逐帖署名。 |
| AJIMEE-Bench | 評測資料 CC BY-SA 3.0；程式 CC0-1.0。只供 experimental 評測。 |
| zenz | 模型權重 CC BY-SA 4.0；不是語料。public core 不得使用。 |

## 5. 修改與衍生

每次 release 要能回答：

- 用了哪些來源與版本。
- 做了哪些轉換。
- 哪些資料被移除或過濾。
- 產出的 manifest 與 SHA-256。
- 對應的 NOTICE 與授權全文。

Public core 的模型訓練紀錄要保存來源 manifest 的雜湊。Experimental 的模型卡要寫明來源是 CC BY-SA，並附 `NOTICE-experimental.md`。

## 6. Takedown 與 opt-out

Public core 與 experimental 都要有來源移除流程：

1. 收到權利人通知後，先停止散布相關資料或模型。
2. 在對應帳本列標記 `status=blocked`，或移到 `excluded.tsv`；保留通知與處理紀錄。
3. 重新產生 manifest 與 NOTICE；不要只刪 NOTICE 的文字。
4. 若模型已散布，評估是否重新訓練或下架；不要聲稱刪除 NOTICE 就等於清除資料。

## 7. 不變更既有授權

本目錄不撤回先前版本已有效授予的權利，也不改變 `LICENSE`、`legal/README.md` 或第三方元件的授權。第三方資料與模型依各自條款，不因本專案程式碼採 AGPL-3.0-only 而改變。
