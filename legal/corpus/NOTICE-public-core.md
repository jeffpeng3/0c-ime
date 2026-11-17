# Public core 資料與模型 NOTICE 範本

本檔案是 P2-20 的 NOTICE 範本。實際 release 只保留這次真正使用的來源，並填入版本、雜湊與修改說明。

## 0. 共通聲明

本產出使用下列公開資料訓練或建置。各來源依其授權提供；來源方未背書本專案，也未參與模型訓練或輸出。

修改說明：資料經過正規化、重複與近重複移除、文件群組 holdout、抽樣、斷詞與 tokenization；模型由本專案從零訓練。實際步驟見訓練 manifest。

## 1. 青空文庫

- 來源：青空文庫（https://www.aozora.gr.jp/）
- 取用形式：globis-university/aozorabunko-clean（https://huggingface.co/datasets/globis-university/aozorabunko-clean），revision 42a9c9c0f1d67e6a5554d9bea4201973dc9b049c
- 授權：CC BY 4.0（https://creativecommons.org/licenses/by/4.0/）；原典作品為公共領域
- 署名：青空文庫（https://www.aozora.gr.jp/）與入力・校正ボランティア；globis-university/aozorabunko-clean
- 邊界：只使用無著作權作品。保留作品 ID、図書カード URL、底本、入力者與校正者。不包含圖片、書影或另有權利的內容。

## 2. e-Gov 法令

- 來源：e-Gov 法令検索（デジタル庁）（https://laws.e-gov.go.jp/）
- 取用形式：nlp-waseda/e_gov（https://huggingface.co/datasets/nlp-waseda/e_gov），revision 08e57e367a9f79436e4ce87adcca261f33ece6f9，2024-10-20 下載
- 授權：CC BY 4.0（https://creativecommons.org/licenses/by/4.0/）；法令本文另依著作権法第13条不受著作權保護
- 署名：出典：e-Gov 法令検索（デジタル庁）（https://laws.e-gov.go.jp/）；nlp-waseda/e_gov
- 邊界：只使用法令本文與 metadata。不包含圖示、商標、圖表。

## 3. KAKEN

- 來源：KAKEN: Grants-in-Aid for Scientific Research Database（National Institute of Informatics）（https://kaken.nii.ac.jp/）
- 取用形式：LLM-jp Corpus v4 `ja_kaken`（https://gitlab.llm-jp.nii.ac.jp/datasets/llm-jp-corpus-v4/-/tree/main/ja/ja_kaken），tree 383ff015c70adeac987251590e6e800049188b20
- 授權：CC BY 4.0（https://creativecommons.org/licenses/by/4.0/）
- 署名：Source: KAKEN: Grants-in-Aid for Scientific Research Database (The National Institute of Informatics)（https://kaken.nii.ac.jp/）；edited by LLM-jp
- 邊界：只使用研究課題概要。不包含作者保留著作權的研究報告，特別是 2015 年度以前部分報告。本產出是基於 KAKEN 的編輯物，不代表 NII 的見解，也不表示 NII 背書。

## 4. 特許公報

- 來源：特許庁ウェブサイト（https://www.jpo.go.jp/）
- 取用形式：LLM-jp Corpus v4 `ja_patent`（https://gitlab.llm-jp.nii.ac.jp/datasets/llm-jp-corpus-v4/-/tree/main/ja/ja_patent），tree 879e3d6c2337e70547dc6c97145ef014c1dab619
- 授權：CC BY 4.0（https://creativecommons.org/licenses/by/4.0/）；特許庁網站內容依公共データ利用規約 1.0（https://www.digital.go.jp/resources/open_data/public_data_license_v1.0）
- 署名：出典：特許庁ウェブサイト（https://www.jpo.go.jp/）；LLM-jp Corpus v4 ja_patent
- 邊界：只使用文字。個別圖表或第三人權利另有標示時不使用。

## 5. SIP3 日本語汎用ウェブコーパス

- 來源：SIP3「統合型ヘルスケアシステムの構築における生成AI活用」で構築された SIP3 日本語汎用ウェブコーパス
- 取用形式：LLM-jp Corpus v4 `ja_sip_comprehensive_html` 與 `ja_sip_comprehensive_pdf`（trees 46485ced3d6542bc4e1836b37ad21c0f07db8ffb、789686e2f850474e5bc65f8f1da31b36b8ff1a6d）
- 授權：CC BY 4.0（https://creativecommons.org/licenses/by/4.0/）
- 署名：SIP3「統合型ヘルスケアシステムの構築における生成AI活用」；LLM-jp Corpus v4
- 邊界：只使用 HTML `level0` 與 PDF `pdf2text`。不使用 `surya`。保留網站來源與 takedown 流程。

## 6. Anthy corpus（評測）

- 來源：Anthy Unicode project，`calctrans/corpus.1.txt`，commit fc2260cf60100bb868c20f7ca493e8c2e01dcf11
- 授權：Public Domain（檔案開頭明示 copying: public domain）
- 署名：Anthy Unicode project 與 corpus 作者
- 邊界：只供評測。`corpus.2`、`corpus.3`、`corpus.5` 沒有授權聲明，`corpus.4` 是錯誤示例，均不使用。

## 7. 授權全文

- CC BY 4.0：`legal/CC-BY-4.0.txt` 或 https://creativecommons.org/licenses/by/4.0/legalcode
- 公共領域：依各來源聲明；本範本第 1 與第 6 節已記錄判定依據。

## 8. Release 檢查

- [ ] NOTICE 只列實際使用的來源。
- [ ] 每個來源都有版本、雜湊、授權與完整署名。
- [ ] 修改說明與 manifest 一致。
- [ ] 已附 CC BY 4.0 全文或連結。
- [ ] 沒有 CC BY-SA、ODC-BY、Common Crawl、Mozc、UniDic 來源。
- [ ] 沒有暗示來源方背書。
