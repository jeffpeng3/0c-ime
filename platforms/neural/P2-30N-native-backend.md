# P2-30N：原生 ONNX 後端（Windows 產品預設）

狀態：實作完成。Windows 產品由 P2-43 改為預設 native 並出貨固定 runtime；Linux／macOS 沒有原生後端時仍保留 Python worker。
分支：`p2/T30n-native-backend`（基於 `p2/T30s-native-onnx`）。補標點由 P2-30P 接上 native，見第 3.2 與 4.2 節。

## 1. 怎麼用

```sh
# 預設：有原生 runtime 就用 native，否則退回統計（不會啟動 Python）
0c-server ...

# 明確指定（開發／診斷）
0c-server -neural-backend native -neural-ort-lib /path/to/libonnxruntime.so.1.32.0
0c-server -neural-backend python
```

- `-neural-backend`：`auto`（預設）、`native` 或 `python`。未知值會記錄警告並回到 `auto`。
- `-neural-ort-lib`：ORT 共享函式庫路徑；空值先找執行檔旁的 `onnxruntime.dll`／`onnxruntime.so` 與 `runtime/` 子目錄，也可用 `ZUIN_ORT_LIBRARY`。
- native 接到 sequence／reranker 與補標點。補標點支援注音 worker 套件（`zuin-punct-runtime-v1`）與日文 `0c.model.manifest/1` 標點模型；native 模式不啟動 Python，模型不可用時才保留原文。
- 只有建置完全沒有 native 後端時，補標點才會退回 Python worker；native 套件本身載入失敗會回報實際原因，不會偷偷啟動 Python。
- 預設建置（`CGO_ENABLED=0`）不編譯 ORT binding；Windows 的 `auto` 會回報 `native_unavailable` 並退回統計引擎。
- native 建置：`CGO_ENABLED=1 go build -tags onnxruntime ./...`；Windows 需要 mingw 的 gcc。

residency manager 對同一個 manifest 可選任一後端：`ModelSpec.Backend` 決定 `LoadModel`
走 `Supervisor`（Python）或 `native.Backend`。manifest、`vocab.json`、字典 SHA-256、
檔案雜湊與 placeholder 標記都共用同一份核對結果。

## 2. 檔案

| 檔案 | 作用 |
|---|---|
| `internal/spm/` | 從 `tools/onnxprobe/spm` 移出的純 Go SentencePiece unigram（identity normalizer、byte fallback）。無 CGO。 |
| `internal/neural/native/input/` | `sequence_runtime.prepare()` 的逐行 Go 移植：前綴樹、候選 slot、positions、mask、proj_pos、target rows。無 CGO。 |
| `internal/neural/native/backend_ort.go` | `cgo && onnxruntime` 的 ORT 後端：sequence 9 輸入與 reranker 2 輸入。 |
| `internal/neural/native/backend_stub.go` | 預設建置的 stub；`NativeAvailable()==false`。 |
| `internal/neural/native/manifest.go` | 與 Python worker 相同的 manifest／vocab 契約核對。 |
| `internal/neural/native/punct_plan.go` | 無 CGO 的補標點 manifest 核對、視窗、合併、插入與注音 `eligible_boundaries` 移植。 |
| `internal/neural/native/punct_ort.go` | `cgo && onnxruntime` 的補標點 scorer：注音 ids／types 與日文 input_ids／attention_mask 兩種契約。 |
| `cmd/0c-server/main.go` | `-neural-backend`、`-neural-ort-lib`，health 增加 `neural_backend`、`neural_native_available`；native 也建立補標點 spec。 |
| `internal/neural/residency.go` | `Backend` 型別、`ParseBackend`、`ModelSpec.Backend`、`loadNative`、`loadNativePunct`。 |
| `internal/neural/native/testdata/parity_oracle.py` | 200 句 parity oracle：Python `prepare()`＋同一顆 int8 placeholder；另有補標點 oracle。 |

## 3. 輸入組裝對照

### 3.1 Sequence／reranker

Go `input.Prepare` 逐一對照 `sequence_runtime.py`：

- 前文取最後 64 個 Unicode code point；字表外字元用 UNK id 4。
- `[BOS]=1`、`[SEP]=2`、`[OUT]=3`；base id 從 2 起、character id 從 8 起。
- 讀音：聲調符號 `ˊˇˋ˙`，無符號為一聲；`tone_known=false` 且套件允許時 tone id 0。
- 分支：text task 只有 OUT；phonetic 為「最後音節＋OUT」；`neutral_last` 另加「最後音節改輕聲」分支。
- 候選樹：共同前綴共用節點；`parent` 鏈、`mask` 左下三角與祖先關係、`positions` 完全同 Python。
- `proj_pos` 取所有分支最後節點去重後排序；`target_node` 含 i=0 的 `()` 根；`target_ids = char id - 6`。
- 分數：輸出列依 branch 排列，`reshape(-1, count).max(0)`。
- 驗證錯誤代碼沿用 Python：`unsupported_shape`、`duplicate_candidates`、`unknown_character`、`unsupported_segment` 等。

`internal/neural/native/input/testdata/golden.json` 是 Python `prepare()` 產生的黃金向量，
涵蓋一般、輕聲分支、text task、未知聲調、超過 64 字前文、共同前綴與 partial segment。
`TestPythonGolden` 在預設 `go test ./...` 就會跑，不需要 CGO 或模型。

### 3.2 補標點輸入與插入

日文 `0c.model.manifest/1` 標點模型的契約直接取自 manifest：

- 輸入：`input_ids` 與 `attention_mask`，都是 `int64 [batch, seq]`。
- 輸出：`logits`，`float32 [batch, seq, 4]`，順序固定 `none`／`comma`／`period`／`question`。
- 標記：manifest 的 `architecture.marks` 是 `["", "、", "。", "？"]`；空白與 append 的 `<eos>` 不補。
- 分詞：`internal/spm` 讀同一個 `public_core_ja.model`，identity normalizer、unigram、byte fallback；tokenizer 路徑由 `ModelSpec.Tokenizer`、manifest 的 tokenizer dataset 或套件內的 `tokenizer.model` 取得。
- 視窗：訓練 `seq_len`（這顆模型是 256）為窗口、步長 128；長文用 `range(0, n-window, stride) + [n-window]` 滑動，每個位置取離窗口邊緣最遠的那一窗，與 `punct_runtime.py` 的滑動視窗 margin 規則逐行相同。
- padding／truncation：每個窗口補到 256，`pad_id=0`、`attention_mask=0`；padding 位置不產生插入。超過窗口時只滑動，不截掉任何原字。
- 插入：逐 token `argmax`，只把 manifest 的標點字元插在 token 結尾對應的原字邊界；token 表面是空白、byte fallback 或 dummy prefix 時跳過。輸出新字串由原字元加上插入的標點組成，原字元、順序、長度都不變。

注音 worker 套件（`zuin-punct-runtime-v1`）走原本的 `punct_runtime.py` 契約：character id 從 8 起、UNK 4、`[BOS]=1`、`[SEP]=2`、`[OUT]=7`，echo 需要 `ids`／`types`，bidirectional 只送 `ids`；`threshold`、`window`、`stride`、`input` 全部照 manifest，插入規則與 `eligible_boundaries`、段尾不補、開頭列舉數字不補頓號都逐一移植。

## 4. Parity 結果

### 4.1 Sequence／reranker（200 句真實日文）

環境：Linux x86-64、Go 1.27.3、onnxruntime 1.32.0、`reranker-public-core.placeholder.int8.onnx`、
`public_core_ja.model`、`sample-ja.txt` 前 200 行；batch 1、seq 32、intra-op 2。

| 指標 | 值 |
|---|---:|
| 案例數 | 200 |
| 組裝陣列不一致 | 0 |
| token id 不一致（含 SentencePiece） | 0／200 |
| 最大絕對分數差 | **0** |
| 平均絕對分數差 | 0 |
| Go 延遲（組裝＋推論，每句） | **7.77 ms** |
| Python 延遲（`prepare()`＋推論，每句） | **8.42 ms** |

分數差為 0 是因為兩邊都是同一顆 float32 模型、同一組輸入；先前 P2-30S 的 5.93e-8
是 JSON float64 轉換造成的差，這次 parity harness 直接比 float。測試會在 token id
不同或分數差超過 `1e-4` 時失敗。

placeholder 只有 `input_ids`／`attention_mask` 兩個輸入，不能直接餵給 worker 的
9 輸入 session。因此 oracle 直接 import `sequence_runtime.prepare()`，把組裝後的
`ids` 壓成同一顆 placeholder 的 32 token 輸入；這驗證的是同一份 Python 組裝程式與
同一顆模型，Go 端則用 `input.Prepare` 與 `native.ScoreTokens`。9 輸入的
`native.Score` 走同一份 `input.Prepare`，但目前沒有真 sequence 模型可做端到端比對。

重現：

```sh
REF=/home/fedora/.opencode/plan/0c-ja/phase2/reference
NEURAL_PARITY_PYTHON=/path/to/python \
NEURAL_PARITY_SPM=$REF/tokenizer/public_core_ja.model \
NEURAL_PARITY_TEXTS=$REF/tokenizer/sample-ja.txt \
NEURAL_PARITY_ONNX=$REF/models/reranker-public-core/reranker-public-core.placeholder.int8.onnx \
NEURAL_PARITY_LIB=/path/to/libonnxruntime.so.1.32.0 \
CGO_ENABLED=1 go test -tags onnxruntime ./internal/neural/native -run TestParity200 -v
```

placeholder 的 embedding 只有 14,080 列、tokenizer 有 32,768 個 piece，所以兩邊都用
`id % 14080`；正式模型必須讓 tokenizer 與 embedding 對齊。placeholder 沒有品質意義。

### 4.2 補標點（真實訓練 int8 模型、240 窗）

環境：Linux x86-64（AMD Ryzen 5 5600X、12 threads）、Go 1.27.3、onnxruntime 1.32.0、
`punctuation-public-core.int8.onnx`（32.7 MB，SHA-256 `2860267d…f051b`）、
`public_core_ja.model`、`sample-ja.txt` 前 240 行；窗口 256、步長 128、intra-op 8。

| 指標 | 值 |
|---|---:|
| 視窗數 | 240 |
| token id 不一致（SentencePiece） | 0／240 |
| 每位置 argmax 不一致 | 0 |
| 全文合併決策不一致 | 0／240 |
| 插入 edit 不一致 | 0／240 |
| 插入後全文不一致 | 0／240 |
| 移除插入標點後與原文不同 | **0 字元** |
| native int8 每窗 median／p95 | **34.1 ms／40.2 ms** |
| 同機 Python ORT 每窗 median／p95 | 34.0 ms／40.1 ms |

窗口拉長到 256 後，這台機器的絕對延遲比 manifest 的 aarch64 主機（11.9 ms）慢，
但 native 與 Python ORT 同機同執行緒設定下相同。插入等於 0 差異代表兩邊的
SentencePiece span 對應、邊緣 margin 選擇與 none 決策完全一致；`no_edit` 檢查逐一
移除插入的標點後仍等於原文，原字元沒有被改寫、刪除或重排。

重現：

```sh
REF=/home/fedora/.opencode/plan/0c-ja/phase2/reference
NEURAL_PUNCT_PARITY_PYTHON=/path/to/python-with-sentencepiece \
NEURAL_PUNCT_PARITY_MANIFEST=$REF/models/trained/punctuation-public-core/manifest.json \
NEURAL_PUNCT_PARITY_TOKENIZER=$REF/tokenizer/public_core_ja.model \
NEURAL_PUNCT_PARITY_TEXTS=$REF/tokenizer/sample-ja.txt \
NEURAL_PUNCT_PARITY_LIB=/path/to/libonnxruntime.so.1.32.0 \
CGO_ENABLED=1 go test -tags onnxruntime ./internal/neural/native -run TestPunctuationParity200 -v
```

## 5. 測試

```sh
# 預設建置：不需 CGO、不需 ORT、不需 Python
CGO_ENABLED=0 go test ./...

# 編譯 native 後端（執行測試才需要 ORT 共享函式庫）
CGO_ENABLED=1 go build -tags onnxruntime ./...

# 200 句 parity（環境變數見上）
CGO_ENABLED=1 go test -tags onnxruntime ./internal/neural/native -run TestParity200 -v

# 240 窗真實補標點 parity 與延遲（環境變數見 4.2）
CGO_ENABLED=1 go test -tags onnxruntime ./internal/neural/native -run TestPunctuationParity200 -v

# 補標點視窗、截斷、masked／padding、空輸入與插入的無 CGO 單元測試
CGO_ENABLED=0 go test ./internal/neural/native -run 'Punct|Window|TokenEdits|Merge|Empty|Character|Truncation|Eligible'

# 既有 zhuyin golden 與 neural 測試
go test ./core/... ./internal/neural/... ./cmd/0c-server
```

## 6. Windows 打包（P2-43 已完成）與尚未移植的功能

1. **Windows MSVC runtime（已完成）**：`models/runtime.lock.json` 固定從 `msvc_runtime`
   wheel 取 `MSVCP140.dll`、`MSVCP140_1.dll`、`VCRUNTIME140.dll`、`VCRUNTIME140_1.dll`，
   與 `onnxruntime.dll` 一起放在 `0c-server.exe` 旁；`scripts/prepare_windows_runtime.py`
   取出，`scripts/package_windows.py` 逐檔核對 SHA-256。
2. **ORT 共享函式庫出貨（已完成）**：`onnxruntime.dll` 取自固定 `onnxruntime` 1.30.0
   wheel；大小、SHA-256、授權與 `THIRD_PARTY_NOTICES.txt` 由 runtime lock 與打包流程處理。
3. **CI 的 mingw（已完成）**：`windows-2022` 的 `windows-cgo-native` job 安裝 mingw、
   以 `CGO_ENABLED=1 -tags onnxruntime` 建置並跑測試；`CGO_ENABLED=0` 的純 Go job 保留。
4. **已接上與仍未移植的 worker 功能**：
   - 補標點 native 已接上：注音 `zuin-punct-runtime-v1` 與日文 `0c.model.manifest/1` 兩種 manifest 都可由 `native.OpenPunct` 執行；Windows Python-free 套件的補標點不再強制保留原文。
   - `ZUIN_ORT_PROVIDERS` GPU provider 選擇（CUDA／CoreML 等）尚未移植。
   - worker 的 stdio 協定、逾時後殺 worker 重啟、`Supervisor` 的每小時重啟上限：native
     沒有獨立行程可殺，目前只在呼叫前檢查 context；ORT `Run` 本身不可中斷。
   - `tone_known` 桌面請求目前不帶；Go 的輸入組裝已支援，但 native `Score` 還沒有對外欄位。
   - 未知聲調的完整 v2 套件行為只有輸入組裝層對齊，尚未用 v2 模型實測。
   - SentencePiece 只支援 identity normalizer＋unigram＋byte fallback；charsmap（NFKC）、
     BPE 會 fail closed。
   - 模型檔案驗證只做 manifest／hash；native 仍會在 `native.Open` 重新核對，尚未與
     `validateONNXFile` 的 corrupt 分類合併。
5. **平台與模型覆蓋**：Linux x86-64 與 Windows x86-64 曾在 P2-30S 驗證；macOS arm64、
   Windows ARM64、無 AVX CPU 未測。補標點 parity 已用真實 `punctuation-public-core`
   int8 模型跑過 240 窗；sequence 仍只有 untrained placeholder，換正式 sequence 模型後
   要重跑分數 parity、延遲與 RSS。
6. **打包與回退**：Windows 打包由 P2-43 放入 ORT 與 VC runtime，不再帶 Python；補標點
   與選字走同一個 native 建置。回退路徑是 `-neural-backend python`（有 Python 的環境），
   或在 native 載入失敗時由 residency manager 回報 `runtime_missing`／`failed` 並保留
   統計引擎；只有建置完全沒有 native 後端時才會改走 Python worker。
