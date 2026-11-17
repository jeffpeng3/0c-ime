# Public core 資料、模型與詞典 NOTICE 範本

本檔案是 P2-20 的 NOTICE 範本，2026-11-17 依公開核心授權政策修訂。實際 release 只保留這次真正使用的來源，並填入版本、雜湊與修改說明。

## 0. 共通聲明

本產出使用下列公開資料訓練或建置。各來源依其授權提供；來源方未背書本專案，也未參與模型訓練或輸出。

公開核心只使用三類授權：CC BY 4.0、公共領域、permissive notice-only。permissive notice-only 不是 CC0；散布時必須保留各來源的著作權、許可與免責聲明。CC BY-SA、non-commercial、Mozc、UniDic 預設分支、SudachiDict、NEologin、JUMAN、Common Crawl 與 NEologd 來源不得放進本 NOTICE。

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

## 7. IPADic 2.7.0（日文詞典）

- 來源：mecab-ipadic-2.7.0-20070801（2007-08-01），Nara Institute of Science and Technology（NAIST）；部分詞條來自 ICOT Free Software
- 取用形式：官方下載頁 https://taku910.github.io/mecab/#download（Google Drive 資料夾 https://drive.google.com/drive/folders/1-xcDvik4983YEEAOfkWhliDmarfqani_）；GitHub mirror https://github.com/taku910/mecab/tree/master/mecab-ipadic，commit 2fd29256c6d5e1b10211cac838069ee9ede8c77a（2015-04-23）
- 授權類別：Permissive-Notice-Only（NAIST-IPAdic + ICOT Free Software）；授權全文 https://github.com/taku910/mecab/blob/master/mecab-ipadic/COPYING
- 允許理由：NAIST 授權允許使用、重製與散布，只要保留著作權聲明與免責條款；ICOT Free Software 允許原樣或修改後自由散布，但必須保留 NO WARRANTY。兩者都沒有 copyleft 或 non-commercial 限制，符合 2026-11-17 決策的 permissive notice-only 類。
- 邊界：只取詞條、品詞、讀音、活用與連接成本。Google 追加詞與沖繩辭書屬 Mozc 混合授權，不使用；散布時不得只寫 BSD-3-Clause 或 CC BY 4.0。
- 修改：轉成 UTF-8、讀音正規化為平假名、重複移除、成本尺度調整與排序。
- 必須保留的聲明：

```text
Copyright 2000, 2001, 2002, 2003 Nara Institute of Science
and Technology.  All Rights Reserved.

Use, reproduction, and distribution of this software is permitted.
Any copy of this software, whether in its original form or modified,
must include both the above copyright notice and the following
paragraphs.

Nara Institute of Science and Technology (NAIST),
the copyright holders, disclaims all warranties with regard to this
software, including all implied warranties of merchantability and
fitness, in no event shall NAIST be liable for
any special, indirect or consequential damages or any damages
whatsoever resulting from loss of use, data or profits, whether in an
action of contract, negligence or other tortuous action, arising out
of or in connection with the use or performance of this software.

A large portion of the dictionary entries
originate from ICOT Free Software.  The following conditions for ICOT
Free Software applies to the current dictionary as well.

Each User may also freely distribute the Program, whether in its
original form or modified, to any third party or parties, PROVIDED
that the provisions of Section 3 ("NO WARRANTY") will ALWAYS appear
on, or be attached to, the Program, which is distributed substantially
in the same form as set out herein and that such intended
distribution, if actually made, will neither violate or otherwise
contravene any of the laws and regulations of the countries having
jurisdiction over the User or the intended distribution itself.

NO WARRANTY

The program was produced on an experimental basis in the course of the
research and development conducted during the project and is provided
to users as so produced on an experimental basis.  Accordingly, the
program is provided without any warranty whatsoever, whether express,
implied, statutory or otherwise.  The term "warranty" used herein
includes, but is not limited to, any warranty of the quality,
performance, merchantability and fitness for a particular purpose of
the program and the nonexistence of any infringement or violation of
any right of any third party.

Each user of the program will agree and understand, and be deemed to
have agreed and understood, that there is no warranty whatsoever for
the program and, accordingly, the entire risk arising from or
otherwise connected with the program is assumed by the user.

Therefore, neither ICOT, the copyright holder, or any other
organization that participated in or was otherwise related to the
development of the program and their respective officials, directors,
officers and other employees shall be held liable for any and all
damages, including, without limitation, general, special, incidental
and consequential damages, arising out of or otherwise in connection
with the use or inability to use the program or any product, material
or result produced or otherwise obtained by using the program,
regardless of whether they have been advised of, or otherwise had
knowledge of, the possibility of such damages at any time during the
project or thereafter.  Each user will be deemed to have agreed to the
foregoing by his or her commencement of use of the program.  The term
"use" as used herein includes, but is not limited to, the use,
modification, copying and distribution of the program and the
production of secondary products from the program.

In the case where the program, whether in its original form or
modified, was distributed or delivered to or received by a user from
any person, organization or entity other than ICOT, unless it makes or
grants independently of ICOT any specific warranty to the user in
writing, such person, organization or entity, will also be exempted
from and not be held liable to the user for any such damages as noted
above as far as the program is concerned.
```

## 8. Unicode Unihan Database（日文漢字讀音）

- 來源：Unicode Unihan Database，Unicode 18.0.0（UCD 18.0.0，2026-09-01；`Unihan.zip` 2026-08-05，8.0 MB）
- 取用形式：https://www.unicode.org/Public/18.0.0/ucd/Unihan.zip；資料說明 https://www.unicode.org/reports/tr38/
- 授權類別：Permissive-Notice-Only（Unicode License V3）；授權全文 https://www.unicode.org/license.txt
- 允許理由：Unicode License V3 允許免費使用、複製、修改、合併、出版、散布與販售，只需保留著作權與許可聲明，沒有 copyleft 或 non-commercial 限制，符合 permissive notice-only 類。
- 邊界：只取 `kJapaneseOn`、`kJapaneseKun`、部首、筆畫與異體欄位，用來補日文漢字讀音；不取 Unihan 以外的 Unicode 檔案。
- 修改：只抽指定欄位、轉成 UTF-8、與 IPADic／curated 對齊、重複移除與排序。
- 必須保留的聲明：`Copyright © 1991-2026 Unicode, Inc.` 與下列 UNICODE LICENSE V3 全文；release 時一併附上 `legal/UNICODE-NOTICE.txt`。

```text
UNICODE LICENSE V3

COPYRIGHT AND PERMISSION NOTICE

Copyright © 1991-2026 Unicode, Inc.

NOTICE TO USER: Carefully read the following legal agreement. BY
DOWNLOADING, INSTALLING, COPYING OR OTHERWISE USING DATA FILES, AND/OR
SOFTWARE, YOU UNEQUIVOCALLY ACCEPT, AND AGREE TO BE BOUND BY, ALL OF THE
TERMS AND CONDITIONS OF THIS AGREEMENT. IF YOU DO NOT AGREE, DO NOT
DOWNLOAD, INSTALL, COPY, DISTRIBUTE OR USE THE DATA FILES OR SOFTWARE.

Permission is hereby granted, free of charge, to any person obtaining a
copy of data files and any associated documentation (the "Data Files") or
software and any associated documentation (the "Software") to deal in the
Data Files or Software without restriction, including without limitation
the rights to use, copy, modify, merge, publish, distribute, and/or sell
copies of the Data Files or Software, and to permit persons to whom the
Data Files or Software are furnished to do so, provided that either (a)
this copyright and permission notice appear with all copies of the Data
Files or Software, or (b) this copyright and permission notice appear in
associated Documentation.

THE DATA FILES AND SOFTWARE ARE PROVIDED "AS IS", WITHOUT WARRANTY OF ANY
KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT OF
THIRD PARTY RIGHTS.

IN NO EVENT SHALL THE COPYRIGHT HOLDER OR HOLDERS INCLUDED IN THIS NOTICE
BE LIABLE FOR ANY CLAIM, OR ANY SPECIAL INDIRECT OR CONSEQUENTIAL DAMAGES,
OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS,
WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION,
ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THE DATA
FILES OR SOFTWARE.

Except as contained in this notice, the name of a copyright holder shall
not be used in advertising or otherwise to promote the sale, use or other
dealings in these Data Files or Software without prior written
authorization of the copyright holder.
```

## 9. 0c curated 日文種子詞條

- 來源：`data/ja/lexicon/words.tsv`，2026-10-19，137 詞條；sha256 51cc49de9a2a6fa2db1d8157d954ed699499bbff638d81ede51ae26ec5ab7f76
- 授權類別：Permissive-Notice-Only（0c 自有）
- 署名：Funmula Co., Limited（0c）
- 允許理由：這批詞條由 0c 人工維護，著作權屬 Funmula Co., Limited；權利人就是本專案，可直接以 permissive notice-only 納入 public core，不涉及第三方 copyleft。
- 邊界：只收無第三方貢獻的 curated 種子。`data/sources/ledger.tsv` 的產品 `curated` 列仍為 CC BY-SA 4.0；本 NOTICE 的 permissive 授權是權利人對 public core 版本另外授予，不撤回也不取代產品資料包的 CC BY-SA 4.0。
- 修改：正規化、讀音驗證、重複移除、成本調整與排序。

## 10. 授權全文

- CC BY 4.0：`legal/CC-BY-4.0.txt` 或 https://creativecommons.org/licenses/by/4.0/legalcode
- 公共領域：依各來源聲明；本範本第 1 與第 6 節已記錄判定依據。
- IPADic：https://github.com/taku910/mecab/blob/master/mecab-ipadic/COPYING；完整 NAIST 與 ICOT 聲明見第 7 節。
- Unicode License V3：`legal/UNICODE-NOTICE.txt` 或 https://www.unicode.org/license.txt；完整聲明見第 8 節。
- 0c curated：權利人 0c 的 permissive notice-only 授權記錄在本 NOTICE 與 `data/sources/corpus/public-core.tsv`。

## 11. Release 檢查

- [ ] NOTICE 只列實際使用的來源。
- [ ] 每個來源都有版本、雜湊、授權與完整署名。
- [ ] 修改說明與 manifest 一致。
- [ ] 已附 CC BY 4.0 全文或連結。
- [ ] 已附 permissive notice-only 來源的完整著作權與免責聲明：IPADic 的 NAIST 與 ICOT NO WARRANTY、Unicode License V3、0c curated 署名。
- [ ] 沒有 CC BY-SA、non-commercial、Mozc、UniDic 預設分支、SudachiDict、NEologin、JUMAN、Common Crawl、NEologd 來源。
- [ ] UniDic BSD 分支未經 `review.tsv` 的 `rev-unidic-bsd` 簽核前不得列入；簽核後才更新本 NOTICE。
- [ ] 沒有暗示來源方背書。
