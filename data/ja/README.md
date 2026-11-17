# 日文資料與授權

T02 的結論：日文資料可以在 AGPL-3.0-only 的 0c 產品裡散布，條件是每個來源照自己的授權附聲明，且不把混合授權的 Mozc 詞典整體當成 BSD-3-Clause。

P2-01 起，正式產品不 import 或啟動 llama.cpp、GGUF、zenz；日文轉換只用本目錄編譯出的統計詞典。日文 ONNX 模型會在 P2-25 匯出、P2-30 接上。T03 的 zenz 實驗只保留在 `scripts/spike/ja-zenz/`，不是產品路徑。

逐檔清單在 [ja-files.tsv](../sources/ja-files.tsv)，來源摘要與邊界在 [ja-sources.tsv](../sources/ja-sources.tsv)，完整授權全文在 [sources/licenses/ja-*.txt](../sources/licenses/)。

## 建議來源

| 用途 | 首選 | 備案 | 理由 |
|---|---|---|---|
| 日文詞典與連接成本 | `ja-mozc` | `ja-sudachidict` | Mozc 直接提供詞條、品詞、詞成本與連接矩陣，符合契約的 Mozc 式 Converter。SudachiDict 授權較單純，但只有詞條與品詞，沒有 IME 用的詞成本，T13 需要另外估成本。 |
| 讀音與斷詞 | `ja-unidic` | `ja-sudachidict` 內附的 UniDic 詞條 | UniDic 選 BSD-3-Clause，商用與再散布都可以；用來標注 Wikipedia 語料與補讀音。 |
| 詞頻與 n-gram | `ja-wiki` | 0c 自有日文語料 | Wikipedia 授權明確，版本可固定；衍生統計維持 CC BY-SA 4.0。 |
| 神經重排 | 日文 ONNX reranker（P2-25 匯出、P2-30 接上） | 無 | P2-01 起正式產品沒有 llama.cpp、GGUF、zenz 路徑，目前只用統計詞典。新模型由訓練 repo 匯出，授權待 P2-20 帳本定案。 |
| 轉換評測 | `ja-anthy-corpus` 的 corpus.1.txt | `ja-ajimee` | Anthy corpus.1 明示 public domain；AJIMEE 資料是 CC BY-SA 3.0，適合放 testdata。 |

`ja-sudachidict` 當備案的具體做法：用 SudachiDict Core 的詞條與 UniDic 讀音建立候選，再用 Wikipedia 詞頻估成本。這樣可以避開 Mozc 的混合授權，但轉換品質與成本曲線要重新驗證。

## 逐項結論

| 來源 | 版本 | 授權 | 可散布 | 結論 |
|---|---|---|---|---|
| Mozc `dictionary_oss` | `a4541df` | BSD-3-Clause + NAIST/IPAdic + ICOT + 沖繩辭書 Public Domain | 是 | 可散布，必須附完整混合聲明。不可整體標成 BSD-3-Clause。 |
| SudachiDict Core | `v20261015` | Apache-2.0，內含 UniDic BSD 與 NEologd 詞條 | 是 | 可散布，附 Apache-2.0 全文與 LEGAL。 |
| 現代書き言葉UniDic | `unidic-cwj-202512` | GPL-2.0／LGPL-2.1／BSD-3-Clause 三選一 | 是 | 選 BSD-3-Clause。CC BY-NC-SA 版本不可用。 |
| zenz-v3.1-small GGUF（僅 T03 spike，非正式） | `ddf6e44` | CC-BY-SA-4.0 | 僅供歷史參考 | P2-01 起不進正式產品；實驗程式留在 `scripts/spike/ja-zenz/`。 |
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

P2-01 起正式產品沒有 llama.cpp、GGUF、zenz 路徑，也不從 `models/ja/` 載入權重。日文轉換只用本目錄編譯出的統計詞典。日文 ONNX 模型會在 P2-25 匯出、P2-30 接上；屆時模型權重仍與程式碼分開授權，模型不進資料包 manifest 的 `files`。

T03 的 zenz 權重是 CC-BY-SA-4.0，只留在 `scripts/spike/ja-zenz/` 的歷史實驗，不是產品路徑。模型輸出的文字不因模型授權而變成 CC BY-SA；產品仍可正常使用輸出。

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
```

評測資料不進公開資料包時，NOTICE 不必列 `ja-anthy-corpus` 與 `ja-ajimee`。若 testdata 隨 release 散布，另外在 testdata 目錄附上 `ja-anthy-corpus.txt` 與 `ja-ajimee.txt`。

## T13 編譯指引

1. 詞典用 `ja-mozc`，讀音與斷詞用 `ja-unidic`，詞頻用 `ja-wiki`。`ja-sudachidict` 只當備案或補洞來源。
2. 使用 `ja-mozc` 時，`data/sources/licenses/ja-mozc.txt` 必須完整附進 NOTICE。不要刪掉 NAIST、ICOT 或沖繩辭書段落。
3. 使用 `ja-sudachidict` 時，附 `ja-sudachidict.txt`，內含 Apache-2.0、LEGAL、UniDic BSD 與 NEologd 聲明。
4. 使用 `ja-wiki` 時，附 `ja-wiki.txt`，並在 manifest 記錄 dump 版本與 md5。
5. 日文神經模型不進資料包 manifest `files`；P2-01 起也沒有外部模型路徑。ONNX 模型由 P2-25 匯出、P2-30 接上，屆時再更新打包與配對流程。
6. `0c-compile -public` 前確認實際使用的來源都在 `ledger.tsv` 有 `yes`，且 `sources/licenses/<ID>.txt` 存在。
7. 不要把 Anthy corpus.2、corpus.3、corpus.5 放進 testdata 或資料包。
8. AJIMEE 資料檔保留 CC BY-SA 3.0 授權檔與出處，不要改標成 AGPL。

## 與 AGPL-3.0 的相容性

| 來源 | 與 AGPL-3.0 產品一起散布 | 條件 |
|---|---|---|
| Mozc 詞典 | 可以 | NAIST/IPAdic、ICOT、BSD-3 都是保留聲明的寬鬆授權；附完整聲明。 |
| SudachiDict | 可以 | Apache-2.0 與 GPLv3／AGPLv3 相容；附授權與 NOTICE。 |
| UniDic | 可以 | 選 BSD-3-Clause；附 BSD 全文與作者。 |
| zenz 模型（僅 T03 spike） | 不進正式產品 | P2-01 起不隨產品散布；實驗程式在 `scripts/spike/ja-zenz/`。 |
| Anthy corpus.1 | 可以 | Public Domain；只限 corpus.1.txt。 |
| AJIMEE | 可以 | 資料依 CC BY-SA 3.0 獨立散布，程式 CC0；不併入 AGPL 程式碼。 |
| Wikipedia 衍生統計 | 可以 | 資料依 CC BY-SA 4.0 獨立散布，附署名與修改說明。 |

這張表的前提是資料與程式分開授權。若把 CC BY-SA 資料直接併進 AGPL 程式碼或把模型權重編進執行檔，授權邊界會變得難以交代，T13 不要這樣做。

## 檔案

- [ja-sources.tsv](../sources/ja-sources.tsv)：來源摘要、版本、授權、可散布、署名、用途與邊界。
- [ja-files.tsv](../sources/ja-files.tsv)：Mozc 逐檔清單，以及 SudachiDict、UniDic、Anthy、AJIMEE、Wikipedia 與歷史 zenz spike 的檔案層級結論。
- [ledger.tsv](../sources/ledger.tsv)：已追加 `ja-*` 七列，既有列未改。
- [sources/licenses/ja-*.txt](../sources/licenses/)：各來源的完整授權全文或聲明。

## T13 編譯器與資料包

T13 把日文詞典來源編成 `build/ja/` 的資料包。注音流程 `0c-compile`（`-lang zh`，預設）完全不變；日文走 `-lang ja` 分支。

```sh
# 開發包
go run ./cmd/0c-compile -lang ja -out build/ja

# 公開包（逐來源檢查帳本、可散布、授權全文）
go run ./cmd/0c-compile -lang ja -public -out build/ja

# 由辭書形展開五段、一段、サ変、カ変、形容詞、形容動詞的常見活用
go run ./cmd/0c-compile -lang ja -ja-conjugate -out build/ja
```

沒有指定 `-out` 時，`-lang ja` 預設寫到 `build/ja`，避免覆蓋 `build/data` 的注音包。

### 目錄與來源格式

```text
data/ja/
  lexicon/words.tsv      curated 詞條（詞面、讀音、成本、詞性、來源、活用型）
  import/<origin>.mozc.tsv      Mozc dictionary_oss TSV
  import/<origin>.sudachi.csv   SudachiDict Core CSV
  import/<origin>.id.def        Mozc id.def（可選，補沒有 comment 的詞性）
  connection.tsv         詞性連接成本
  lm/unigram.tsv         詞面次數（可選，用來估 unigram 成本）
  lm/bigram.tsv          詞面 bigram 機率（可選）
```

`words.tsv` 的欄位是 `詞面<TAB>讀音<TAB>成本<TAB>詞性<TAB>來源<TAB>活用型`。來源可逗號分隔；活用型可用 `godan`、`ichidan`、`suru`、`kuru`、`i-adjective`、`na-adjective`，或 Mozc／UniDic 的日文名稱。詞性名稱與 `core/ja/lexicon.POS` 相同。

詞成本是浮點數，越小越優先，負值合法。合法範圍是 `-32.767`～`32.767`，對應 `0c-jadic` pack 的 signed i16 成本乘上 `0.001`；NaN、Inf 與超出範圍的值會被 `core/ja/lexicon.Load` 與 `internal/compile.parseJAWords` 拒絕。成本只約束單詞，連接成本 `connection.tsv` 仍要求非負。

`import/` 的檔名前綴就是來源 ID，必須在 `data/sources/ledger.tsv` 有紀錄：

- `ja-mozc.mozc.tsv`：Mozc 的「讀音、左 ID、右 ID、成本、詞面、MeCab 詞性 CSV」。成本預設乘以 `1/1000`（`-ja-mozc-scale` 可改），與 curated 詞條的 1～3 同一尺度。
- `ja-sudachidict.sudachi.csv`：SudachiDict Core 的 18 欄 CSV。Sudachi 沒有 IME 詞成本，編譯器用詞性與長度估成本；有 Wikipedia 詞頻時以詞頻為準。

讀音一律正規化成平假名單位：片假名轉平假名、空白忽略，長音符與小撮かな保留，所以讀音長度不必等於詞面字數。

讀音只能使用かな。每個讀音單位都必須通過 `reading.Unit.Valid`，標點、記号、英數字與其他符號一律拒絕；`internal/jaimport.NormalizeReading` 在匯入時就報錯，`BuildJA` 出版前會再驗一次詞條。像 `、`、`。`、`！`、`？`、`・` 這類記号候選屬於 T22 的 `data/ja/symbols/`，由 `core/ja/rewrite.LoadSymbolsFS` 提供，不要放進語音詞典 `lexicon/words.tsv`。

### 0c-jadic 匯入契約

P2-22E 固定了 `internal/jadic` 的讀音與排序契約，完整量測與成本建議見 plan 的 `P2-22E-contracts.md`。

- 讀音：`jadic.Load`／`Parse` 只接受平假名與長音符 `ー`。片假名、片假名與平假名混用、以及其他字元會讓載入失敗，不會留到 `Convert` 才正規化。manifest 的 `reading_script` 可以記錄建置來源，但產品匯入路徑要求 hiragana。
- 同 surface 排序：成本（換算後，升冪）、讀音、詞性、活用型、活用形。五欄完全相同時保留 pack 內的原始順序。同一份 pack 的 `words.tsv` 因此位元組固定。
- 成本：P2-22F 起採用 P2-22E 的 mapping D，`words.tsv` 的成本是 `pack_i16_cost * 0.001`，保留正負號，不 clamp、不 offset。單位是 0.001／MeCab・Sudachi 成本單位，越小越優先；負成本是原資料的語意，不是錯誤。114 筆負成本（最低 `-8.313`）原樣寫入，例如「研究所」的 `けんきゅうじょ -4.215` 排在 `けんきゅうしょ -3.955` 之前。
- 與連接成本結合：解碼器對每條路徑累加「每個詞的詞成本 + 相鄰詞性的 `connection.tsv` 成本」，再取最低總成本。詞成本與連接成本共用同一尺度，可以直接相加；mapping D 不改變任何一筆的相對權重，也不引入每詞固定偏移。connection 的中位數是 `0.4`，若用 `+8.313` offset 會是它的 20.8 倍，會系統性偏向短分割。

### 資料包內容

```text
lexicon.bin    詞條、讀音 trie、詞面索引、詞性連接成本（二進位）
lm.bin         unigram 與 bigram 成本（二進位）
lexicon.tsv    core/ja/lexicon.Load 可直接載入的中介格式
manifest.json  format 2、language、input_methods、output_variants、limits、雜湊
NOTICE.txt     實際使用來源的授權與姓名標示全文
report.txt     詞條數、連接數、檔案大小、常駐記憶體估計
```

manifest v2 的固定欄位：

```json
{
  "format": 2,
  "language": "ja",
  "input_methods": ["romaji", "kana"],
  "output_variants": ["ja"],
  "limits": {"max_reading_units": 64, "max_word_units": 32, "max_candidates": 16},
  "files": {},
  "counts": {},
  "sources": [],
  "inputs": {},
  "source_hash": "...",
  "runtime_hint": {"encoded_bytes": 0, "estimated_bytes": 0}
}
```

沒有 `language` 或 `format: 1` 一律視為注音包；`internal/compile.ManifestLanguage` 就是這個判斷。一個資料包只屬於一種語言。

`lexicon.bin` 的格式：magic `0CJLEX01`、詞條（詞面、かな單位、成本、詞性、送り仮名長度、活用型）、詞面索引、讀音 trie、連接成本矩陣。所有整數用無號 varint，浮點數固定 little-endian float64；詞條、詞面、trie 子節點、連接成本都先排序。`lm.bin` 用 `0CJLM001`，同樣排序後輸出。解碼器在 `internal/jaimport.DecodeLexicon` 與 `DecodeLM`，T12 的 `core/ja/lexicon` 可直接接上，或先用 `lexicon.tsv` 的 `Load`。

### 可重現建置

- 不寫時間戳、主機名稱或絕對路徑；manifest 的 `inputs` 用相對 `data/` 路徑。
- 詞條、詞面、bigram、連接成本全部排序，map 不直接影響輸出順序。
- `source_hash` 只由輸入檔內容雜湊與大小組成，換目錄不變。
- 同一份 `data/ja` 連跑兩次，`lexicon.bin`、`lm.bin`、`lexicon.tsv`、`manifest.json`、`NOTICE.txt`、`report.txt` 六個檔案的 SHA-256 相同；`internal/compile` 的測試會逐檔比對。
- `-public` 在建立輸出前檢查：每個實際使用來源都在 `ledger.tsv`、可散布為 `yes`、名稱／網址／版本／授權／署名不缺、`data/sources/licenses/<ID>.txt` 存在且非空。失敗時不動既有資料包。

### 量測

目前 repo 內的 curated 種子（137 詞條、82 連接成本、15 bigram）：

| 項目 | 數值 |
|---|---:|
| `lexicon.bin` | 8,412 bytes（約 61.4 bytes／詞條） |
| `lm.bin` | 2,387 bytes |
| `lexicon.tsv` | 3,979 bytes |
| 解碼後常駐估計 | 53,486 bytes（約 0.05 MiB） |

`internal/jaimport` 另有 20,000 筆合成詞條的量測測試：`lexicon.bin` 1,107,733 bytes（55.39 bytes／詞條），解碼後 heap 約 5.25 MB，常駐估計 4.56 MB；`BenchmarkDecodeLexicon` 約 8 ms／次。實際 Mozc 全量詞典的數字待 T13 在具備 dictionary_oss 的環境重跑，格式與量測程式不變。

### 與 core/ja/lexicon 的介接

T13 分支不含 T12 的 `core/ja/lexicon`，因此一般測試不 import 它。`internal/compile/ja_lexicon_integration_test.go` 用 `ja_lexicon` build tag 提供合併後的驗證：

```sh
go test -tags ja_lexicon ./internal/compile
```

它會把產出的 `lexicon.tsv` 交給真正的 `core/ja/lexicon.Load`，逐筆比對詞面、讀音與詞性。T12 的二進位解碼器接上後，同一測試可改讀 `lexicon.bin`。
