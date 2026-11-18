# 授權頁

Copyright (C) 2026 Funmula Co., Limited

這頁整理使用者需要的授權資訊，完整條款與逐檔清單仍以 [legal/README.md](../legal/README.md)、[LICENSE](../LICENSE)、各資料包的 `NOTICE.txt` 與模型目錄的 `LICENSE` 為準。這頁不增加或限制任何授權已授予的權利。

## 程式

自有程式碼、建置腳本、測試程式與文件採 **GNU Affero General Public License version 3 only（AGPL-3.0-only）**。商業使用不需要另外付費；需要以不符合 AGPL 的方式閉源整合或散布時，請洽談另行書面商業授權。

自有圖示、標誌與圖片採 **CC BY-SA 4.0**，署名 Funmula Co., Limited。範圍見 [legal/README.md](../legal/README.md)。

## 公開核心資料與模型

公開核心（public core）的資料與模型只用這三類授權：

1. **CC BY 4.0**：保留署名、授權連結與修改說明。
2. **公共領域**：在 NOTICE 寫明判定依據。
3. **permissive notice-only**：沒有 copyleft、沒有 non-commercial 限制，散布時保留著作權、許可與免責聲明。

目前經簽核可進公開核心的 notice-only 來源限：

- IPADic 2.7.0（NAIST 與 ICOT）
- Unicode Unihan（Unicode License V3）
- 0c 自有 curated 的公開核心版本
- 現代書き言葉UniDic 的 BSD-3-Clause 分支

這不是通用通行證。新來源要逐案審查，並在 `data/sources/corpus/public-core.tsv` 留下允許理由。

日文產品只有一個固定的模型包：reranker 是 CC BY-SA 4.0，punctuation 是 MIT。公開核心 reranker 只留在訓練 repo 作評估參考，不出現在產品 lock、文件或安裝路徑。注音 `select/`、`punct/` 與日文模型的檔案與授權以各模型 `LICENSE` 與 [models/README.md](../models/README.md) 為準。

公開核心不使用 CC BY-SA、non-commercial、ODC-BY、Common Crawl 衍生、Mozc、UniDic 的 CC BY-NC-SA 與古文／方言版本、SudachiDict、NEologin、JUMAN 或 NEologd 來源。

## CC BY-SA 實驗資料與模型

CC BY-SA 資料與模型以 **CC BY-SA 4.0** 釋出，與公開核心分開 manifest、checkpoint 與安裝包：

- 日文模型包的 `reranker-extra` 使用 Wikipedia 與 Stack Exchange 的 CC BY-SA 實驗語料，衍生模型以 CC BY-SA 4.0 釋出。
- 保留來源的原始授權版本與署名，例如 Wikipedia 文字是 CC BY-SA 4.0、Stack Exchange 需要逐帖作者與原問題 URL，部分 LLM-jp 語料是 CC BY-SA 3.0。
- 安裝或散布日文模型包時一定帶 `legal/CC-BY-SA-4.0.txt`，打包後的套件另附 `MODEL-PACKAGE-CC-BY-SA-4.0.txt` 與 `MODEL-PACKAGE-NOTICES.txt`。
- 同一模型包的 punctuation 保持 MIT，不會因與 CC BY-SA reranker 一起出貨而變成 CC BY-SA。它附 UniDic BSD 與 IPADic 的 notice，詳見下一節。

產品不安裝只供評估的 reranker，也不提供其他模型包選擇。

## 個別來源的散布義務

### IPADic

使用 IPADic 2.7.0 的詞典包必須保留完整聲明：

- NAIST 的著作權聲明與免責條款。
- ICOT Free Software 的 **NO WARRANTY** 段落。

不能只寫 `BSD-3-Clause` 或 `CC BY 4.0`。完整文字在 [legal/corpus/NOTICE-public-core.md](../legal/corpus/NOTICE-public-core.md) 第 7 節。

### UniDic

使用現代書き言葉UniDic 的 BSD-3-Clause 分支時，每一個散布的詞典包都要同時附上：

- [legal/unidic/BSD.txt](../legal/unidic/BSD.txt)
- [legal/unidic/BSD-unidic-cwj-202512.txt](../legal/unidic/BSD-unidic-cwj-202512.txt)
- [legal/unidic/AUTHORS.md](../legal/unidic/AUTHORS.md)

兩份 BSD 的著作權行不同：

- 網站版：`Copyright (c) 2011-2021, The UniDic Consortium`
- artifact 版：`Copyright (c) 2023 National Institute for Japanese Language and Linguistics`

不能只附一份、改寫或合併成單一 `BSD-3-Clause` 標籤。CC BY-NC-SA 版本、古文用與方言用字典不在核准範圍。

### Unicode Unihan

散布含 Unihan 資料的產出時，保留 `Copyright © 1991-2026 Unicode, Inc.` 與 Unicode License V3 全文。全文在 [legal/UNICODE-NOTICE.txt](../legal/UNICODE-NOTICE.txt)。

### CC BY 與 CC BY-SA

- CC BY 4.0：保留作者、來源、授權連結與修改說明。
- CC BY-SA 4.0：保留作者、來源、原始授權版本與修改說明；衍生作品以 CC BY-SA 4.0 釋出。
- 不暗示來源方背書本專案。
- 不加上限制條款或有效技術措施妨礙他人行使授權。

## 產品詞庫與日文資料包

產品注音詞庫的 `curated` 詞條與 `0c_collect` 統計採 **CC BY-SA 4.0**。這是產品資料，與上面「公開核心訓練資料」是不同軌道；公開核心版本另外以 permissive notice-only 授權，不撤回產品資料的 CC BY-SA。

日文資料包逐來源標示。`data/ja/` 的詞條、連接成本與詞頻目前採 CC BY-SA 4.0，記号與顔文字辭典採 AGPL-3.0-only；編譯後的 `data/ja/NOTICE.txt` 列出實際來源、版本、授權與姓名標示。日後匯入 Mozc、SudachiDict 或 Wikipedia 時維持各自的混合授權，不整體改標。

英文詞表授權在 `data/english/LICENSE.txt`，編譯後的通知在 `data/english/NOTICE.txt`。

## Windows 產品的執行環境

Windows 產品**不內建 CPython、numpy、onnxruntime Python 套件或 wheel**。`0c-server.exe` 旁出貨：

| 檔案 | 元件 | 授權 |
|---|---|---|
| `onnxruntime.dll` | ONNX Runtime 1.30.0 | MIT，Copyright (c) Microsoft Corporation |
| `vcruntime140.dll`、`vcruntime140_1.dll`、`msvcp140.dll`、`msvcp140_1.dll` | Microsoft Visual C++ 2022 Runtime 14.44.35112 | Microsoft Visual Studio 可轉散佈條款 |

版本、大小與 SHA-256 固定在 [models/runtime.lock.json](../models/runtime.lock.json)。打包工具會拒絕任何 `.py`、`.pyc`、`.pyd`、wheel、numpy 或 onnxruntime Python 套件。原生後端支援選字／reranker 與補標點；Python-free 套件以 `native.OpenPunct` 執行補標點，不會啟動 Python worker。

Linux 與 macOS 的神經推論使用系統或發行版提供的 Python、NumPy 與 ONNX Runtime，這個「不內建 Python」的敘述只適用 Windows 產品。兩個原始碼與模型 tarball 都不攜帶可攜 Python、wheel、訓練 checkpoint 或訓練環境。

## 其他第三方元件

安裝包的第三方元件聲明在 `THIRD_PARTY_NOTICES.txt`。逐檔著作權人與授權以 Debian DEP-5 格式列在 [legal/copyright](../legal/copyright)，可作為 `debian/copyright` 的基礎。UniDic 的完整說明在 [legal/unidic/README.md](../legal/unidic/README.md)。

## 品質與驗證狀態

- 日文模型包的 reranker 與 punctuation 已 pin HF revision 與逐檔 SHA-256。模型仍在評測，沒有品質承諾。placeholder 只留在 `testdata/models/placeholder/`，不進正式 lock 或 release bundle。
- Windows Phase 2 VM 驗收是 **NOT RUN**，不能寫成已通過。
- 注音、混合語言 benchmark 與延遲數字只在有量測來源時引用；沒有數字的項目代表未驗證。
