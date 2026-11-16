# 日文資料與授權

T02 的結論：日文資料可以在 AGPL-3.0-only 的 0c 產品裡散布，條件是每個來源照自己的授權附聲明，且不把混合授權的 Mozc 詞典整體當成 BSD-3-Clause。

逐檔清單在 [ja-files.tsv](../sources/ja-files.tsv)，來源摘要與邊界在 [ja-sources.tsv](../sources/ja-sources.tsv)，完整授權全文在 [sources/licenses/ja-*.txt](../sources/licenses/)。

## 建議來源

| 用途 | 首選 | 備案 | 理由 |
|---|---|---|---|
| 日文詞典與連接成本 | `ja-mozc` | `ja-sudachidict` | Mozc 直接提供詞條、品詞、詞成本與連接矩陣，符合契約的 Mozc 式 Converter。SudachiDict 授權較單純，但只有詞條與品詞，沒有 IME 用的詞成本，T13 需要另外估成本。 |
| 讀音與斷詞 | `ja-unidic` | `ja-sudachidict` 內附的 UniDic 詞條 | UniDic 選 BSD-3-Clause，商用與再散布都可以；用來標注 Wikipedia 語料與補讀音。 |
| 詞頻與 n-gram | `ja-wiki` | 0c 自有日文語料 | Wikipedia 授權明確，版本可固定；衍生統計維持 CC BY-SA 4.0。 |
| 神經重排 | `ja-zenz` | 無 | zenz-v3.1-small GGUF 是已量化的 73.9 MB 模型，CC BY-SA 4.0，適合當第一版 baseline。 |
| 轉換評測 | `ja-anthy-corpus` 的 corpus.1.txt | `ja-ajimee` | Anthy corpus.1 明示 public domain；AJIMEE 資料是 CC BY-SA 3.0，適合放 testdata。 |

`ja-sudachidict` 當備案的具體做法：用 SudachiDict Core 的詞條與 UniDic 讀音建立候選，再用 Wikipedia 詞頻估成本。這樣可以避開 Mozc 的混合授權，但轉換品質與成本曲線要重新驗證。

## 逐項結論

| 來源 | 版本 | 授權 | 可散布 | 結論 |
|---|---|---|---|---|
| Mozc `dictionary_oss` | `a4541df` | BSD-3-Clause + NAIST/IPAdic + ICOT + 沖繩辭書 Public Domain | 是 | 可散布，必須附完整混合聲明。不可整體標成 BSD-3-Clause。 |
| SudachiDict Core | `v20261015` | Apache-2.0，內含 UniDic BSD 與 NEologd 詞條 | 是 | 可散布，附 Apache-2.0 全文與 LEGAL。 |
| 現代書き言葉UniDic | `unidic-cwj-202512` | GPL-2.0／LGPL-2.1／BSD-3-Clause 三選一 | 是 | 選 BSD-3-Clause。CC BY-NC-SA 版本不可用。 |
| zenz-v3.1-small GGUF | `ddf6e44` | CC-BY-SA-4.0 | 是 | 模型檔獨立散布，附作者、來源與 CC BY-SA 4.0 全文。 |
| Anthy corpus | `fc2260c` | Public Domain，僅 corpus.1.txt 明示 | 是，限 corpus.1.txt | corpus.2、corpus.3、corpus.5 沒有授權聲明，corpus.5 出處不明，不得散布。 |
| AJIMEE-Bench | `401666c` | 資料 CC-BY-SA-3.0；程式 CC0-1.0 | 是 | 可放 testdata。資料檔保留 CC BY-SA 3.0，不改成 AGPL。 |
| 日本語版 Wikipedia | `jawiki-20261101` | CC-BY-SA-4.0，另可依 GFDL | 是 | 衍生詞頻與 n-gram 維持 CC BY-SA 4.0，附作者、版本與修改說明。 |

## Mozc 混合授權邊界

`dictionary_oss` 目錄不能只用 Mozc 的 BSD-3-Clause 交代。Mozc 的 `LICENSE` 對 `src/data/dictionary*` 另外列出三段條款：

1. 詞條、品詞與連接成本衍生自 `mecab-ipadic-2.7.0-20070801`，適用 NAIST 授權。
2. 大部分詞條來自 ICOT Free Software，散布時必須保留 ICOT 的 NO WARRANTY 段落。
3. 沖繩辭書是 Public Domain。

因此 `dictionary00.txt` 到 `dictionary09.txt`、`id.def`、`connection_single_column.txt`、`suffix.txt` 都屬於混合檔案。實際使用時把整份 `ja-mozc.txt` 附進 NOTICE，不要逐檔猜測哪些詞條屬於哪個來源。`README.txt`、`collocation*.txt`、`dictionary_filter.tsv`、`reading_correction.tsv`、`suggestion_filter.txt`、`aux_dictionary.tsv` 是 Google 撰寫的設定與追加詞，可用 BSD-3-Clause，但整包散布仍建議附完整 `ja-mozc.txt`，避免漏掉詞典檔的 NAIST 與 ICOT 聲明。

NAIST/IPAdic 與 ICOT 都是保留聲明的寬鬆授權，沒有 copyleft。它們可以和 AGPL-3.0 程式一起散布，只要聲明完整。這也是 T02 最重要的判斷：Mozc 詞典可用，但授權標示必須是混合，不能寫成單一 BSD-3-Clause。

## 模型與程式分開

`ja-zenz` 的 GGUF 是獨立的資料檔，不編進 Go 執行檔，也不放進 manifest 的 `files`。契約第 4 節已經規定日文 neural 權重由 `models/ja/` 管理，這個邊界要維持。

- 程式碼：AGPL-3.0-only。
- 模型權重：CC-BY-SA-4.0，附作者三輪敬太、模型頁連結、完整授權與修改說明。
- 量化、轉檔、再訓練後的權重：仍是 CC-BY-SA-4.0。
- 模型輸出的文字：不因模型授權而變成 CC BY-SA；產品仍可正常使用輸出。

## 公開資料包 NOTICE 草稿

`0c-compile -public` 會逐來源檢查名稱、網址、版本、授權、署名與 `sources/licenses/<ID>.txt`。以下是日文資料包要附的 NOTICE 骨架；完整授權全文由編譯器從 `data/sources/licenses/ja-*.txt` 接在各段之後。

```text
靈犀注音（0c）日文資料包使用下列來源，各來源依其授權條款使用。

== Mozc OSS 日文詞典（ja-mozc）==
授權：BSD-3-Clause AND NAIST-IPAdic AND ICOT-Free-Software；沖繩辭書 Public Domain
版本：commit a4541df29db443b49f81cf54665873b68fde9efa（2026-11-09）
網址：https://github.com/google/mozc/tree/a4541df29db443b49f81cf54665873b68fde9efa/src/data/dictionary_oss
姓名標示：Copyright 2010-2018 Google Inc.；Copyright 2000, 2001, 2002, 2003 Nara Institute of Science and Technology；ICOT Free Software；沖繩辭書 Public Domain
[附 ja-mozc.txt 全文]

== SudachiDict Core（ja-sudachidict）==
授權：Apache-2.0（含 UniDic BSD-3-Clause 與 NEologd 衍生詞條）
版本：v20261015（commit 830bf9e1313f6f75235ba87ee041e144dbcad8c1）
網址：https://github.com/WorksApplications/SudachiDict
姓名標示：Copyright (c) 2017-2023 Works Applications Co., Ltd.；The UniDic Consortium；Toshinori Sato 與 NEologd 資料提供者
[附 ja-sudachidict.txt 全文]

== 現代書き言葉UniDic（ja-unidic）==
授權：BSD-3-Clause（自 GPL-2.0／LGPL-2.1／BSD-3 三選一）
版本：unidic-cwj-202512（2025-12-31）
網址：https://clrd.ninjal.ac.jp/unidic/
姓名標示：The UniDic Consortium，National Institute for Japanese Language and Linguistics
[附 ja-unidic.txt 全文與 AUTHORS]

== 日本語版 Wikipedia 文字衍生 n-gram 與詞頻（ja-wiki）==
授權：CC-BY-SA-4.0
版本：jawiki-20261101-pages-articles.xml.bz2（md5 1b04b3aa8e790294af720112660ae0a8）
網址：https://dumps.wikimedia.org/jawiki/
姓名標示：Wikipedia contributors
[附 ja-wiki.txt 全文]

== zenz-v3.1-small GGUF（ja-zenz）==
授權：CC-BY-SA-4.0
版本：ddf6e44b2e05ab7ea9a3e31559c5e7948761365c
網址：https://huggingface.co/Miwa-Keita/zenz-v3.1-small-gguf
姓名標示：三輪敬太（Miwa-Keita）
[附 ja-zenz.txt 全文；模型檔不進 manifest files]
```

評測資料不進公開資料包時，NOTICE 不必列 `ja-anthy-corpus` 與 `ja-ajimee`。若 testdata 隨 release 散布，另外在 testdata 目錄附上 `ja-anthy-corpus.txt` 與 `ja-ajimee.txt`。

## T13 編譯指引

1. 詞典用 `ja-mozc`，讀音與斷詞用 `ja-unidic`，詞頻用 `ja-wiki`。`ja-sudachidict` 只當備案或補洞來源。
2. 使用 `ja-mozc` 時，`data/sources/licenses/ja-mozc.txt` 必須完整附進 NOTICE。不要刪掉 NAIST、ICOT 或沖繩辭書段落。
3. 使用 `ja-sudachidict` 時，附 `ja-sudachidict.txt`，內含 Apache-2.0、LEGAL、UniDic BSD 與 NEologd 聲明。
4. 使用 `ja-wiki` 時，附 `ja-wiki.txt`，並在 manifest 記錄 dump 版本與 md5。
5. 使用 `ja-zenz` 時，模型放 `models/ja/`，不進 manifest `files`；資料包只記模型名稱與版本。
6. `0c-compile -public` 前確認實際使用的來源都在 `ledger.tsv` 有 `yes`，且 `sources/licenses/<ID>.txt` 存在。
7. 不要把 Anthy corpus.2、corpus.3、corpus.5 放進 testdata 或資料包。
8. AJIMEE 資料檔保留 CC BY-SA 3.0 授權檔與出處，不要改標成 AGPL。

## 與 AGPL-3.0 的相容性

| 來源 | 與 AGPL-3.0 產品一起散布 | 條件 |
|---|---|---|
| Mozc 詞典 | 可以 | NAIST/IPAdic、ICOT、BSD-3 都是保留聲明的寬鬆授權；附完整聲明。 |
| SudachiDict | 可以 | Apache-2.0 與 GPLv3／AGPLv3 相容；附授權與 NOTICE。 |
| UniDic | 可以 | 選 BSD-3-Clause；附 BSD 全文與作者。 |
| zenz 模型 | 可以 | 模型是獨立資料檔，依 CC BY-SA 4.0 散布，不併入程式碼。 |
| Anthy corpus.1 | 可以 | Public Domain；只限 corpus.1.txt。 |
| AJIMEE | 可以 | 資料依 CC BY-SA 3.0 獨立散布，程式 CC0；不併入 AGPL 程式碼。 |
| Wikipedia 衍生統計 | 可以 | 資料依 CC BY-SA 4.0 獨立散布，附署名與修改說明。 |

這張表的前提是資料與程式分開授權。若把 CC BY-SA 資料直接併進 AGPL 程式碼或把模型權重編進執行檔，授權邊界會變得難以交代，T13 不要這樣做。

## 檔案

- [ja-sources.tsv](../sources/ja-sources.tsv)：來源摘要、版本、授權、可散布、署名、用途與邊界。
- [ja-files.tsv](../sources/ja-files.tsv)：Mozc 逐檔清單，以及 SudachiDict、UniDic、zenz、Anthy、AJIMEE、Wikipedia 的檔案層級結論。
- [ledger.tsv](../sources/ledger.tsv)：已追加 `ja-*` 七列，既有列未改。
- [sources/licenses/ja-*.txt](../sources/licenses/)：各來源的完整授權全文或聲明。
