# CC BY-SA Experimental 資料與模型 NOTICE 範本

本檔案是 P2-20 的 NOTICE 範本。Experimental 資料與模型與 public core 分開管理；實際 release 只保留這次真正使用的來源。

## 0. 共通聲明

本產出是 experimental 版本，使用下列 CC BY-SA 資料。衍生語料與模型以 CC BY-SA 4.0 釋出。來源方未背書本專案，也未參與模型訓練或輸出。

修改說明：資料經過正規化、重複與近重複移除、文件群組 holdout、抽樣、斷詞與 tokenization；模型由本專案從零訓練。實際步驟見 experimental manifest。

## 1. 日本語版 Wikipedia

- 來源：日本語版 Wikipedia（https://ja.wikipedia.org/）
- 取用形式：`jawiki-20261101-pages-articles.xml.bz2`（https://dumps.wikimedia.org/jawiki/），4,782,309,586 bytes，md5 1b04b3aa8e790294af720112660ae0a8
- 授權：CC BY-SA 4.0（https://creativecommons.org/licenses/by-sa/4.0/）；文字另可依 GFDL
- 署名：Wikipedia contributors；https://dumps.wikimedia.org/jawiki/
- 邊界：只使用條目文字。不包含圖片、商標、圖表。
- 修改：移除 wiki 標記、模板與參考資料格式，保留條目文字；去重與過濾。

## 2. LLM-jp Corpus v4 ja_wiki

- 來源：LLM-jp Corpus v4 `ja_wiki`（https://gitlab.llm-jp.nii.ac.jp/datasets/llm-jp-corpus-v4/-/tree/main/ja/ja_wiki），2023-07-20 版，tree 68ea5eafa001f1aefed8c392e26103925a51f7f5
- 授權：CC BY-SA 3.0（https://creativecommons.org/licenses/by-sa/3.0/）
- 署名：Wikipedia contributors；LLM-jp Corpus v4
- 邊界：與第 1 節的 Wikipedia dump 重疊；二選一並去重，不重複計入。
- 修改：沿用 LLM-jp 的清理與去重，本專案再做文件群組 holdout 與 tokenization。

## 3. Stack Exchange：ja.stackoverflow.com

- 來源：Stack Exchange Data Dump（https://archive.org/details/stackexchange），2024-04-02 dump
- 檔案：`ja.stackoverflow.com.7z`，80,753,098 bytes，md5 be742e1f4ff4a3e98c76ac9c9dab738a，sha1 9585321d29a7c7f3ed4a79e5de4bb7b7281c3475
- 授權：CC BY-SA 4.0（https://creativecommons.org/licenses/by-sa/4.0/）；2018-05-02 以前投稿可能是 CC BY-SA 2.5 或 3.0
- 署名：Stack Exchange Network；各投稿の著者名と元質問 URL（`https://ja.stackoverflow.com/questions/...`）
- 邊界：逐帖保留作者與原問題 URL。若無法保留，不能只寫「Stack Exchange Network」就當成滿足署名。
- 修改：只取問題與答案文字；移除投票、標籤與使用者個人資料；去重與過濾。

## 4. Stack Exchange：japanese.stackexchange.com

- 來源：Stack Exchange Data Dump（https://archive.org/details/stackexchange），2024-04-02 dump
- 檔案：`japanese.stackexchange.com.7z`，71,906,152 bytes，md5 9e903379b6848d1e5af9d00b46176bab，sha1 6d322bd6436ae2b59b68c7a4e810556e5db21285
- 授權：CC BY-SA 4.0（https://creativecommons.org/licenses/by-sa/4.0/）；2018-05-02 以前投稿可能是 CC BY-SA 2.5 或 3.0
- 署名：Stack Exchange Network；各投稿の著者名と元質問 URL（`https://japanese.stackexchange.com/questions/...`）
- 邊界：與第 3 節分開 manifest；可合併訓練但保留來源。
- 修改：只取問題與答案文字；移除投票、標籤與使用者個人資料；去重與過濾。

## 5. AJIMEE-Bench（評測）

- 來源：azooKey/AJIMEE-Bench（https://github.com/azooKey/AJIMEE-Bench），commit 401666cd56d1a570c2021798b64b6da4396bfd45，JWTD_v2/v1
- 授權：資料 CC BY-SA 3.0（https://creativecommons.org/licenses/by-sa/3.0/）；程式 CC0-1.0
- 署名：azooKey Project；日本語Wikipedia入力誤りデータセット v2 作者與 Wikipedia contributors
- 邊界：只供 experimental 評測，不進訓練，不放進 public core testdata。
- 修改：轉成評測格式；保留出處、授權與修改說明。

## 6. zenz-v3.1-small GGUF（模型）

- 來源：Miwa-Keita/zenz-v3.1-small-gguf（https://huggingface.co/Miwa-Keita/zenz-v3.1-small-gguf），revision ddf6e44b2e05ab7ea9a3e31559c5e7948761365c
- 檔案：`ggml-model-Q5_K_M.gguf`，sha256 4de930c06bef8c263aa1aa40684af206db4ce1b96375b3b8ed0ea508e0b14f6c
- 授權：CC BY-SA 4.0（https://creativecommons.org/licenses/by-sa/4.0/）
- 署名：三輪敬太（Miwa-Keita）
- 邊界：experimental 模型，不是語料；public core 不得使用。
- 修改：無；沿用上游權重。

## 7. 授權全文

- CC BY-SA 4.0：`legal/CC-BY-SA-4.0.txt` 或 https://creativecommons.org/licenses/by-sa/4.0/legalcode
- CC BY-SA 3.0：`legal/CC-BY-SA-3.0.txt` 或 https://creativecommons.org/licenses/by-sa/3.0/legalcode
- CC0 1.0：https://creativecommons.org/publicdomain/zero/1.0/

## 8. Release 檢查

- [ ] 已標明 experimental，與 public core 分開。
- [ ] 衍生語料與模型以 CC BY-SA 4.0 釋出。
- [ ] 每個來源都有版本、雜湊、授權與完整署名。
- [ ] Stack Exchange 有逐帖作者與原問題 URL。
- [ ] 已附 CC BY-SA 4.0 全文或連結，以及來源的原始授權版本。
- [ ] 沒有 public core 資料、Mozc、UniDic、Common Crawl 來源混入。
- [ ] 沒有加上限制條款或有效技術措施。
- [ ] 沒有暗示來源方背書。
