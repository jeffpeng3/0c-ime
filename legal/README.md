# 授權

Copyright (C) 2026 Funmula Co., Limited

本專案的自有程式碼、建置腳本、測試程式與文件，著作權屬 Funmula Co., Limited，除另有聲明外，依 **GNU Affero General Public License version 3 only（AGPL-3.0-only）** 授權。完整條款見 [LICENSE](../LICENSE)。

使用與授權說明見 [TERMS.md](TERMS.md)，不影響 LICENSE 已授予的權利；隱私與資料的說明見官網 [隱私與資料](https://0c.biggo.com/privacy/)。使用者版授權整理見 [docs/licensing.md](../docs/licensing.md)，內容與本文件及 [PACKAGING.md](../PACKAGING.md) 對齊；逐檔與逐來源的權威清單仍是本目錄。依 Debian 自由軟體指導方針（DFSG）的逐項檢查見 [DFSG-free.md](DFSG-free.md)；逐檔的著作權人與授權以 Debian 的 DEP-5 格式列在 [copyright](copyright)。

## 圖示與圖片

本專案自有的圖示、標誌與圖片，著作權屬 Funmula Co., Limited，依 **Creative Commons Attribution-ShareAlike 4.0 International（CC BY-SA 4.0）** 授權，署名為 Funmula Co., Limited，完整條款見 [CC-BY-SA-4.0.txt](CC-BY-SA-4.0.txt)。範圍包括：

- `.github/assets/`
- `internal/settingsapp/icon.ico`、`internal/settingsapp/icon.png`
- `platforms/linux-fcitx5/icons/`
- `platforms/macos/Icon/`
- `platforms/settings/icons/`、`platforms/settings/ui/icon.png`

## 商業授權

AGPL-3.0 允許研究、個人及商業使用。若需要以不符合 AGPL 條件的方式進行閉源整合或散布，請聯絡專案維護者洽談另行書面商業授權。未取得書面授權前，仍適用 AGPL-3.0。

另行授權僅涵蓋授權方有權授權且書面約定納入的內容；外部貢獻不自動取得閉源重授權權利。

## 資料、模型與第三方元件

第三方程式與相依元件、詞庫、語料、模型、字型及圖片依各自授權或權利聲明處理，不因本專案的 LICENSE 而變更。0c 自有的詞條與詞頻統計（`curated`、`0c_collect` 系列）著作權屬 Funmula Co., Limited，依 CC BY-SA 4.0 授權。

- 詞庫與詞頻：原始碼中的 `data/sources/ledger.tsv` 與 `data/sources/licenses/`；編譯後資料包中的 `data/NOTICE.txt`。
- 訓練語料：public core 與 CC BY-SA experimental 分開列帳，來源在 `data/sources/corpus/`，授權政策與 NOTICE 在 [legal/corpus/](corpus/README.md)。公開核心只用 CC BY 4.0、公共領域與經簽核的 permissive notice-only（目前限 IPADic、Unihan、0c curated 與 UniDic 的 BSD-3-Clause 分支）；CC BY-SA 只進 experimental，並以 CC BY-SA 4.0 釋出衍生語料與模型。Mozc、UniDic 的 CC BY-NC-SA 與古文／方言版本、SudachiDict、NEologin、JUMAN、Common Crawl 與 NEologd 不進公開核心。UniDic 的 BSD 全文與著作者表示在 [legal/unidic/](unidic/README.md)。
- 英文詞表：原始碼中的 `data/english/LICENSE.txt`；編譯後資料包中的 `data/english/NOTICE.txt`。
- 日文資料包：repo 內的 `data/ja/` 種子詞條、連接成本與詞頻採 CC BY-SA 4.0，記号與顔文字辭典採 AGPL-3.0-only；正式日文詞典由 Mozc OSS `dictionary_oss`（tag 3.35.6354、commit 0136c357）匯入，授權是 BSD-3-Clause AND NAIST-IPAdic AND ICOT-Free-Software，沖繩辭書 Public Domain。編譯後的 `data/ja/NOTICE.txt` 逐來源列出授權與姓名標示。Mozc 資料不整體改標成 Apache-2.0 或單一 BSD-3-Clause；完整條款在 [data/sources/licenses/ja-mozc.txt](../data/sources/licenses/ja-mozc.txt)。來源判斷見 [data/ja/README.md](../data/ja/README.md)。
- 神經模型：各模型套件隨附的 `LICENSE`；目前選字與補標點的固定 D512 模型採 MIT。日文產品只有一個模型包：reranker（CC BY-SA 4.0，HF revision 已 pin，Wikipedia 與 Stack Exchange attribution）加 punctuation（MIT，UniDic／IPADic notice）；安裝時帶 `CC-BY-SA-4.0` notice。只供評估的 reranker 留在訓練 repo，不出現在產品 lock、文件或安裝路徑。
- ONNX Runtime：`onnxruntime.dll` 隨 Windows 產品出貨，採 MIT，Copyright (c) Microsoft Corporation；版本、大小與 SHA-256 固定在 `models/runtime.lock.json`。
- 產品執行階段沒有 CPython、NumPy 或 onnxruntime Python 套件；Python 只用於建置與測試工具。
- Unicode：[授權聲明](UNICODE-NOTICE.txt)。
- UniDic：[修正 BSD 全文與著作者表示](unidic/README.md)。
- 其他隨安裝包出貨的元件：`THIRD_PARTY_NOTICES.txt`。

授權全文保留在各元件或資料所在目錄，供編譯、打包時一併附上。本文件不額外授予資料或素材的散布權，也不撤回先前版本已有效授予的權利。
