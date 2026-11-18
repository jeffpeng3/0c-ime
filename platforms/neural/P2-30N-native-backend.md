# P2-30N：原生 ONNX 後端

狀態：實作完成。P2-30D 起這是產品唯一的推論執行路徑；執行階段沒有 Python。
分支：`p2/T30n-native-backend`，後續整合於 `p2/T30D-drop-python`。

## 1. 怎麼用

```sh
# 預設：有原生 runtime 就用 native，否則退回統計
0c-server ...

# 明確指定（開發／診斷）
0c-server -neural-backend native -neural-ort-lib /path/to/libonnxruntime.so
```

- `-neural-backend`：`auto`（預設）或 `native`。未知值會記錄警告並回到
  `auto`；`python` 已移除，指定時視為未知值。
- `-neural-ort-lib`：ORT 共享函式庫路徑；空值先找執行檔旁的
  `onnxruntime.dll`／`onnxruntime.so` 與 `runtime/` 子目錄，也可用
  `ZUIN_ORT_LIBRARY`。
- native 接到 sequence／reranker 與補標點。補標點支援注音
  `zuin-punct-runtime-v1` 套件與日文 `0c.model.manifest/1` 標點模型。
- 預設建置（`CGO_ENABLED=0`）不編譯 ORT binding；`auto` 會回報
  `native_unavailable` 並退回統計引擎。
- native 建置：`CGO_ENABLED=1 go build -tags onnxruntime ./...`；Windows 需要
  mingw 的 gcc。

residency manager 只有一個 executor：`ModelSpec.Backend` 決定 native
`Backend`；manifest、`vocab.json`、字典 SHA-256、檔案雜湊與 placeholder
標記都共用同一份核對結果。

## 2. 檔案

| 檔案 | 作用 |
|---|---|
| `internal/spm/` | 從 `tools/onnxprobe/spm` 移出的純 Go SentencePiece unigram（identity normalizer、byte fallback）。無 CGO。 |
| `internal/neural/native/input/` | 參考 sequence `prepare()` 的逐行 Go 移植：前綴樹、候選 slot、positions、mask、proj_pos、target rows。無 CGO。 |
| `internal/neural/native/backend_ort.go` | `cgo && onnxruntime` 的 ORT 後端：sequence 9 輸入與 reranker 2 輸入。 |
| `internal/neural/native/backend_stub.go` | 預設建置的 stub；`Available()==false`。 |
| `internal/neural/native/manifest.go` | manifest／vocab 契約核對。 |
| `internal/neural/native/punct_plan.go` | 無 CGO 的補標點 manifest 核對、視窗、合併、插入與注音 `eligible_boundaries` 移植。 |
| `internal/neural/native/punct_ort.go` | `cgo && onnxruntime` 的補標點 scorer：注音 ids／types 與日文 input_ids／attention_mask 兩種契約。 |
| `internal/neural/manifest.go` | Go manifest、套件雜湊、字典配對與 rerank 選項。 |
| `internal/neural/residency.go` | `Backend` 型別、`ParseBackend`、`ModelSpec.Backend`、`loadNative`、`loadNativePunct`。 |
| `cmd/0c-server/main.go` | `-neural-backend`、`-neural-ort-lib`，health 增加 `neural_backend`、`neural_native_available`；native 也建立補標點 spec。 |

已刪除：`platforms/neural/runtime/*.py`、`internal/neural/worker.go`、
`supervisor.go`、stdio protocol 與 Python-only 測試 fixture。

## 3. 輸入組裝對照

### 3.1 Sequence／reranker

Go `input.Prepare` 逐一對照參考實作：

- 前文取最後 64 個 Unicode code point；字表外字元用 UNK id 4。
- `[BOS]=1`、`[SEP]=2`、`[OUT]=3`；base id 從 2 起、character id 從 8 起。
- 讀音：聲調符號 `ˊˇˋ˙`，無符號為一聲；`tone_known=false` 且套件允許時
  tone id 0。
- 分支：text task 只有 OUT；phonetic 為「最後音節＋OUT」；`neutral_last`
  另加「最後音節改輕聲」分支。
- 候選樹：共同前綴共用節點；`parent` 鏈、`mask` 左下三角與祖先關係、
  `positions` 完全一致。
- `proj_pos` 取所有分支最後節點去重後排序；`target_node` 含 i=0 的 `()`
  根；`target_ids = char id - 6`。
- 分數：輸出列依 branch 排列，`reshape(-1, count).max(0)`。
- 驗證錯誤代碼：`unsupported_shape`、`duplicate_candidates`、
  `unknown_character`、`unsupported_segment` 等。

`internal/neural/native/input/testdata/golden.json` 是參考
`prepare()` 產生的黃金向量，涵蓋一般、輕聲分支、text task、未知聲調、超過
64 字前文、共同前綴與 partial segment。`TestPythonGolden` 在預設
`go test ./...` 就會跑，不需要 CGO 或模型。

### 3.2 補標點輸入與插入

日文 `0c.model.manifest/1` 標點模型的契約直接取自 manifest：

- 輸入：`input_ids` 與 `attention_mask`，都是 `int64 [batch, seq]`。
- 輸出：`logits`，`float32 [batch, seq, 4]`，順序固定
  `none`／`comma`／`period`／`question`。
- 標記：manifest 的 `architecture.marks` 是 `["", "、", "。", "？"]`；空白與
  append 的 `<eos>` 不補。
- 分詞：`internal/spm` 讀同一個 `public_core_ja.model`，identity normalizer、
  unigram、byte fallback；tokenizer 路徑由 `ModelSpec.Tokenizer`、manifest 的
  tokenizer dataset 或套件內的 `tokenizer.model` 取得。
- 視窗：訓練 `seq_len`（這顆模型是 256）為窗口、步長 128；長文用
  `range(0, n-window, stride) + [n-window]` 滑動，每個位置取離窗口邊緣最遠的
  那一窗。
- padding／truncation：每個窗口補到 256，`pad_id=0`、`attention_mask=0`；
  padding 位置不產生插入。超過窗口時只滑動，不截掉任何原字。
- 插入：逐 token `argmax`，只把 manifest 的標點字元插在 token 結尾對應的原字
  邊界；token 表面是空白、byte fallback 或 dummy prefix 時跳過。輸出新字串由
  原字元加上插入的標點組成，原字元、順序、長度都不變。

注音套件（`zuin-punct-runtime-v1`）走同一個 native scorer：character id 從 8
起、UNK 4、`[BOS]=1`、`[SEP]=2`、`[OUT]=7`，echo 需要 `ids`／`types`，
bidirectional 只送 `ids`；`threshold`、`window`、`stride`、`input` 全部照
manifest，插入規則與 `eligible_boundaries`、段尾不補、開頭列舉數字不補頓號
都逐一移植。

## 4. Parity 結果

### 4.1 中文選字（真實模型）

環境：Linux x86-64、onnxruntime 1.30.0、`Funmula/0c-sequence-d512` int8
（`d512-cv2-20261111`）、真實注音 golden 與衍生案例。

| 指標 | 值 |
|---|---:|
| 案例數 | 265 |
| 分數比較數 | 1239 |
| 分數不一致 | **0** |
| 排序不一致 | **0** |
| 選字不一致 | **0** |
| 組裝張量不一致 | **0** |

### 4.2 日文 reranker（ONNX parity）

同一顆 ONNX 模型、同一組 token id，native 與參考實作逐請求比對分數與排序。
placeholder 只有 `input_ids`／`attention_mask` 兩個輸入，oracle 直接比較
`input.Prepare` 與 `native.ScoreTokens`；9 輸入的 `native.Score` 走同一份
`input.Prepare`。

### 4.3 補標點（真實訓練 int8 模型、240 窗）

`punctuation-public-core.int8.onnx`（32.7 MB，SHA-256
`2860267d…f051b`）、`public_core_ja.model`、`sample-ja.txt` 前 240 行；窗口
256、步長 128、intra-op 8。

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
| 同機參考 ORT 每窗 median／p95 | 34.0 ms／40.1 ms |

## 5. 測試

```sh
# 預設建置：不需 CGO、不需 ORT、不需 Python
CGO_ENABLED=0 go test ./...

# 編譯 native 後端（執行測試才需要 ORT 共享函式庫）
CGO_ENABLED=1 go build -tags onnxruntime ./...

# 中文選字 parity gate（開發用；環境變數見 select_e2e_test.go）
CGO_ENABLED=1 go test -tags onnxruntime ./internal/neural/native -run TestSelectParityGate -v

# 日文 reranker parity gate（開發用）
CGO_ENABLED=1 go test -tags onnxruntime ./internal/neural/native -run TestParity200 -v

# 240 窗真實補標點 parity 與延遲（開發用）
CGO_ENABLED=1 go test -tags onnxruntime ./internal/neural/native -run TestPunctuationParity200 -v

# 補標點視窗、截斷、masked／padding、空輸入與插入的無 CGO 單元測試
CGO_ENABLED=0 go test ./internal/neural/native -run 'Punct|Window|TokenEdits|Merge|Empty|Character|Truncation|Eligible'

# 既有 zhuyin golden 與 neural 測試
go test ./core/... ./internal/neural/... ./cmd/0c-server
```

真實模型 gates 需要 `NEURAL_SELECT_*`、`NEURAL_PARITY_*`、
`NEURAL_PUNCT_PARITY_*` 環境變數，指向模型、ORT 與外部參考實作；未設定時
跳過。repo 不含 worker，測試不是執行階段相依。

## 6. 已接上與限制

- 補標點 native 已接上注音與日文兩種 manifest。
- `ZUIN_ORT_PROVIDERS` GPU provider 選擇（CUDA／CoreML 等）尚未移植。
- ORT `Run` 本身不可中斷；呼叫前會檢查 context。
- `tone_known` 桌面請求目前不帶；Go 的輸入組裝已支援，但 native `Score` 還沒
  有對外欄位。
- 未知聲調的完整 v2 套件行為只有輸入組裝層對齊，尚未用 v2 模型實測。
- SentencePiece 只支援 identity normalizer＋unigram＋byte fallback；
  charsmap（NFKC）、BPE 會 fail closed。
- 模型檔案驗證做 manifest／hash，`native.Open` 會再核對一次。
- Linux x86-64 與 Windows x86-64 曾在 P2-30S 驗證；macOS arm64、
  Windows ARM64、無 AVX CPU 未測。補標點 parity 已用真實模型跑過 240 窗；
  sequence 已用真實中文模型跑過 265 案例。
