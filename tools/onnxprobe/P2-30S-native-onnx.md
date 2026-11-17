# P2-30S：Go 原生 ONNX 推論（不帶 Python）

狀態：量測 spike 完成，**GO**。
分支：`p2/T30s-native-onnx`（基於 `main`，基準 commit `ccd4e18`）。
本 spike 沒有改動正式 runtime 路徑；`cmd/0c-server` 與產品建置完全不引用新增的程式。

## 1. 結論

**GO。** 一個沒有 Python 的 Go 程式可以載入 pip wheel 內的 `libonnxruntime`、跑
`reranker-public-core.placeholder.int8.onnx`、輸出有限分數；分詞與分數和 Python
worker 路徑一致。延遲與記憶體不比 Python 路徑差，Windows 也實機跑過。

關鍵數字：

| 項目 | Go 原生 | Python 參考 |
|---|---:|---:|
| 冷啟動到第一次推論完成（Linux，3 次） | 217.4 / 220.9 / 232.4 ms | 程序總 wall 450 ms（載入＋1 次推論，含直譯器） |
| 冷啟動到第一次推論完成（Windows 11） | 221.8 ms | 未量 |
| batch 1、seq 32、100 次 median（Linux） | 7.42 / 7.87 / 7.95 ms | 8.53 / 7.64 ms |
| 同上 p95 | 8.81 / 10.92 / 10.91 ms | 11.26 / 9.49 ms |
| 載入後 RSS（Linux） | 125.1 / 133.3 / 129.0 MiB | 程序峰值 151.7 MiB |
| 200 句 token id 完全相符 | 200 / 200 | 基準 |
| 240 句（整份 sample） | 240 / 240 | 基準 |
| 200 句分數最大絕對差 | 5.93e-8 | 基準 |

## 2. 新增的程式

`tools/onnxprobe/` 是獨立 Go module（`github.com/0c-biggo/0c-ime/tools/onnxprobe`），
因此上層 module 不會新增 CGO 依賴，`go build ./...`／`go test ./...` 也不受影響。

| 檔案 | 作用 |
|---|---|
| `main.go` | `tokenize`／`bench` 兩個子命令；量冷啟動、延遲、RSS，輸出 JSON |
| `spm/model.go` | 最小 protobuf 讀取器，解析 `SentencePiece` ModelProto |
| `spm/encode.go` | identity normalizer、Viterbi unigram、byte fallback |
| `spm/spm_test.go` | 合成模型單元測試＋200 句 oracle 黃金測試（`SPM_MODEL`／`SPM_TEXTS` 控制） |
| `spm/testdata/tokens-200.json` | Python oracle 產生的 token id 與模型 sha256（不含語料文字） |
| `reference/oracle.py` | Python sentencepiece＋numpy＋onnxruntime 參考輸出 |
| `reference/compare.py` | 逐句比對 id、比對分數最大絕對差 |
| `reference/worker_latency.py` | Python 路徑延遲參考 |
| `README.md` | 建置、執行與 Windows 指令 |
| `P2-30S-native-onnx.md` | 本報告的 repo 副本 |

綁定用 `github.com/yalue/onnxruntime_go v1.38.0`：只需要執行期的共享函式庫，C 標頭
已隨套件提供，不必另外安裝 onnxruntime 開發包。

## 3. 環境與資產

| 項目 | 值 |
|---|---|
| 主機 | WSL2，Fedora，x86-64，Go 看到 12 個 CPU |
| Go | go1.27.3-X:nodwarf5 linux/amd64 |
| ONNX Runtime | 1.32.0（pip `onnxruntime==1.32.0`，只取共享函式庫） |
| Python 參考 | 3.13.12、sentencepiece 0.2.3、numpy 2.5.5、onnxruntime 1.32.0 |
| Windows 驗證 | 同一台機器的 Windows 11，透過 WSL interop 執行 PE 檔 |
| 模型 | `reranker-public-core.placeholder.int8.onnx`，55,361,701 bytes，sha256 `8e4a354c…96cd`（與 manifest 相符） |
| 分詞器 | `public_core_ja.model`，614,451 bytes，sha256 `b25260c9…e2078` |
| 語料 | `sample-ja.txt` 前 200 行（比較集），另補 240 行全量 |

模型是 placeholder（隨機初始化、untrained），所以分數沒有品質意義；這裡只驗證
「載得起來、算得出來、與 Python 路徑一致」。manifest 的 `production_eligible: false`
維持不變。

pip wheel 與 venv 都放在 repo 外（`/tmp/opencode/ort`、`/tmp/opencode/spm`）；
`.onnx`、`.so`、`.dll`、`.exe` 都沒有進 Git。

## 4. 結果

### 4.1 載入與有限分數

`InitializeEnvironment` → 建立 session → 第一次 `Run` 全部成功。輸出 `scores`
為 `float32`，batch 1 形狀 `[1]`；200 句全部分數有限（`all_finite: true`）。
`scores` 範圍 −1.2931 到 0.3904。

### 4.2 冷啟動

定義：行程開始（Go 套件變數初始化最早時點）到第一次推論成功回傳。包含 Go runtime
啟動、分詞器載入、200 句分詞、ORT 環境初始化、session 建立與第一次推論。

| 階段 | Linux 第 1 次 | 第 2 次 | 第 3 次 |
|---|---:|---:|---:|
| 分詞器載入＋200 句分詞 | 44.3 | 41.5 | 39.1 |
| ORT 環境初始化 | 18.8 | 17.8 | 24.1 |
| session 建立（讀 55 MB 模型＋最佳化） | 137.4 | 147.6 | 150.5 |
| 第一次推論 | 8.8 | 7.9 | 11.4 |
| 其餘（Go runtime、旗標解析等） | 8.1 | 5.9 | 7.2 |
| **合計** | **217.4** | **220.9** | **232.4** |

產品啟動不需要對 200 句分詞，扣掉分詞後約 **173–193 ms**。Windows 11 同流程
221.8 ms（分詞 22.8、環境 52.9、session 131.4、第一次推論 7.4）。

Python 對照：`oracle.py` 載入＋1 次推論的程序總 wall time 450 ms、峰值 RSS
155,320 KB。這個數字包含直譯器啟動，不含 product worker 的協定處理，只當量級參考。

### 4.3 延遲

batch 1、seq 32、intra-op 2 threads、inter-op 1、sequential、關閉 spinning，
暖機 10 次後量 100 次。p95 取排序後索引 `int(0.95*n)`（Go 與 Python 相同定義）。

| 路徑 | min | median | p95 | max |
|---|---:|---:|---:|---:|
| Go Linux #1 | 6.48 | **7.42** | 8.81 | 10.11 |
| Go Linux #2 | 6.49 | **7.87** | 10.92 | 17.32 |
| Go Linux #3 | 6.63 | **7.95** | 10.91 | 13.10 |
| Python Linux #1 | 6.63 | **8.53** | 11.26 | 15.49 |
| Python Linux #2 | 6.75 | **7.64** | 9.49 | 11.45 |
| Go Windows 11 | 5.00 | **7.53** | 11.02 | 12.53 |

Go 路徑與 Python 路徑在同一個數量級，Go 沒有額外推論負擔。這台機器是 WSL2，
數字比 manifest 的 aarch64 主機（median 4.66 ms）慢，屬於環境差異；跨路徑比較
在同機同設定下才有效。

### 4.4 RSS

從 `/proc/self/status` 讀 `VmRSS`：

| 時點 | Linux 第 1 次 | 第 2 次 | 第 3 次 |
|---|---:|---:|---:|
| session 建立後 | 128,108 KB | 136,516 KB | 132,060 KB |
| 100 次推論後 | 129,564 KB | 137,840 KB | 134,660 KB |

成長約 1–1.5 MB，來自 ORT arena，沒有持續泄漏跡象。Go 進程總 RSS 比 Python
參考程序的峰值（155 MB）低。Windows 的 probe 沒有實作 RSS 讀取（回報 −1），
該欄位只適用 Linux。

### 4.5 分詞器

選擇：**自己實作純 Go unigram**（`spm/`），沒有引入第三方分詞器。

- 這個模型用 identity normalizer（`precompiled_charsmap` 為空）、unigram、
  `byte_fallback=true`、vocab 32,768。實作範圍剛好覆蓋這些；讀到含 charsmap
  的模型會在載入時拒絕，不會靜默分錯。
- 200 句固定比較集：**200/200 token id 完全相同**，沒有 mismatch。共 9,064 個 id。
- 額外跑完整 240 行：**240/240 完全相同**。
- 只比較 id；`EncodeAsPieces` 的表面字串沒有另外比。

第三方方案評估：

| 套件 | 結果 |
|---|---|
| `github.com/tggo/goSentencePiece v1.1.0`（純 Go，MIT） | 200/200 完全相同，可用 |
| `github.com/eliben/go-sentencepiece v0.7.0`（純 Go） | 載入即失敗：`model type UNIGRAM not supported` |

選自製而非 tggo 的理由：這個模型只需要 unigram＋identity normalizer，自製版本約
350 行、零新依賴、遇到不支援的 normalizer 直接 fail closed；tggo 會多帶
`google.golang.org/protobuf`，且它的 proto 註冊檔名與其他套件衝突（兩個分詞器
同時 import 時 init panic，單獨使用則正常）。若之後要支援 precompiled charsmap／
NFKC 或 BPE，tggo 是已驗證的替代方案，屆時可換。

### 4.6 分數 parity

同一批 200 句、同一組 token id（`id % 14080`，見下）、同樣 batch 1／seq 32、
同樣 session 設定，比對 Go 與 Python 的 `scores`：

| 指標 | 值 |
|---|---:|
| 配對數 | 200 |
| 最大絕對差 | **5.93e-8**（第 124 句，Go −1.293109，Python −1.2931090593338013） |
| 平均絕對差 | 7.75e-9 |
| 非有限值 | 0 |

差異量級就是 float32 輸出經 JSON 轉成 float64 的捨入，沒有語意差異。Windows
建置的輸出與 Linux、Python 相同（200/200、同一個最大差）。

**vocab 落差**：placeholder reranker 的 embedding 只有 14,080 列，tokenizer 有
32,768 個 piece。原始 id 直接送進模型會讓 `Gather` 越界（實測錯誤：
`idx=26188 must be within the inclusive range [-14080,14079]`）。parity 測試在
兩邊都用 `id % 14080` 映到合法範圍；這只影響這個 placeholder 模型，不影響
「Go 原生跑 ONNX」的結論。正式模型必須讓 tokenizer 與 embedding 對齊。

## 5. Windows 可行性

### 5.1 建置

| 項目 | 結果 |
|---|---|
| 指令 | `GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build` |
| 結果 | **成功**，8,004,183 bytes PE32+ console x86-64 |
| 工具鏈 | mingw-w64 gcc 16.2.1（Fedora 交叉編譯）；匯入只有 `KERNEL32.dll`、`msvcrt.dll` |
| `CGO_ENABLED=0` | **失敗**：`build constraints exclude all Go files in …/onnxruntime_go@v1.38.0` |

`onnxruntime_go` 全部檔案都靠 cgo，所以產品若走這條路，`GOOS=windows` 建置就
不能再是 `CGO_ENABLED=0`。目前 CI 的兩個 job 都假設無 CGO：

- `windows` job（`windows-2022`）以 `CGO_ENABLED=0` 建 `0c-server.exe`；
- `cross-build` job（`ubuntu-24.04`）同樣 `CGO_ENABLED=0 GOOS=windows go build ./...`。

`windows-2022` 映像沒有預裝 mingw，需要 `choco install mingw`（或改用 MSYS2、
llvm-mingw）；`ubuntu-24.04` 需要 `apt-get install mingw-w64`。兩者都可行，
但都是 CI 變更。另一個方向是改用 purego 動態載入 ORT C API，維持
`CGO_ENABLED=0`；這是尚未驗證的工程項目，不改這次的 GO 結論。

### 5.2 執行

把 `onnxruntime.dll` 與 exe 放在同一個 Windows 資料夾，用 Windows 11 實際執行：

| 項目 | 值 |
|---|---:|
| 冷啟動到第一次推論 | 221.8 ms |
| median／p95（100 次） | 7.53 / 11.02 ms |
| 200 句 token id | 200/200 與 Python 相同 |
| 分數最大絕對差 | 5.93e-8 |
| `all_finite` | true |

### 5.3 要放進安裝包的檔案

| 檔案 | 大小 |
|---|---:|
| `onnxruntime.dll`（win_amd64 wheel 內） | 22,668,640 bytes（21.62 MiB） |
| `onnxruntime_providers_shared.dll` | 21,816 bytes |
| 整個 `onnxruntime-1.32.0-cp311…win_amd64.whl`（壓縮） | 16,695,880 bytes |
| Linux `libonnxruntime.so.1.32.0` | 34,824,680 bytes（33.21 MiB） |
| Linux `libonnxruntime_providers_shared.so` | 14,632 bytes |

`onnxruntime.dll` 目前本來就隨 onnxruntime wheel 進安裝包，改成原生路徑不會讓
ORT 本體變大；省下的是 CPython 3.14、numpy 與 `.pyd` 載入機制。

DLL 匯入：`MSVCP140.dll`、`MSVCP140_1.dll`、`VCRUNTIME140.dll`、
`VCRUNTIME140_1.dll`、`KERNEL32.dll`、`ADVAPI32.dll` 等。現有安裝包已經處理
`msvcp140.dll`／`msvcp140_1.dll`（`scripts/prepare_neural.py` 的 `crt` 條目），
但 `vcruntime140*.dll` 目前是由 embeddable CPython 附帶。移除 Python 後要改成
直接從同一個 `msvc_runtime` wheel（或 VC++ 可轉散發套件）取這 4 個 DLL，
`scripts/package_windows.py` 的 `NEURAL_CRT` 檢查也要跟著調整。

## 6. 為什麼是 GO

1. 技術可行性已用同一份真實 artifact 證明：載入、推論、有限分數、Windows 實機執行。
2. 行為等價：200 句（另加 240 句）token id 完全相同；分數差在 float32 捨入等級。
3. 效能不倒退：同機同設定下 Go 的 median／p95 與 Python 路徑同級或略好。
4. 冷啟動沒有變差：Go 的路徑約 0.22 s（含 200 句分詞）；Python 對照程序 0.45 s。
5. 記憶體沒有變差，且少了一個直譯器與 numpy 的攻擊面。
6. Windows 只多一個 21.6 MiB 的 DLL 與既有的 VC runtime 需求，沒有新的系統依賴。

NO-GO 的唯一合理理由是「不想要 CGO」，但那可以用 purego 綁定解決，不影響
「產品不需要 Python」這個命題。

## 7. 若 GO，剩下的工程工作

1. **綁定策略**：決定接受 CGO＋mingw（三個建置路徑都要改），或改用 purego 動態
   載入 ORT C API 以維持 `CGO_ENABLED=0`。建議先做 purego 可行性 spike，因為
   現有 CI 與安裝腳本都建立在無 CGO 上。
2. **ORT 共享函式庫出貨**：Linux `.so` 33.2 MiB、Windows `.dll` 21.6 MiB。
   要固定版本與 sha256、放進 release assets（不進 Git）、加進 `pairing.lock`
   或等價清單、走 `0c-update` 更新路徑，並確認 ORT 的 MIT 授權與
   `THIRD_PARTY_NOTICES.txt`。
3. **把 sequence worker 的輸入組裝搬成 Go**：`sequence_runtime.py` 不是只有分數，
   還有前綴樹、`bases/tones/types/positions/mask/proj_pos`、輕聲分支與候選上限。
   這是比 reranker 大得多的移植面，必須逐項對照現有 Python 行為與測試。
4. **分詞器**：本 spike 只涵蓋 identity normalizer＋unigram＋byte fallback。若正式
   模型改用 charsmap（NFKC）或 BPE，要擴充或改用已驗證的 tggo 套件。
5. **Threading 與遙測**：沿用 worker 的 intra=2／inter=1／sequential／
   `allow_spinning=0`，並在 ORT 初始化前設定 `ORT_DISABLE_TELEMETRY=1`、
   `ORT_RUNNING_UNIT_TESTS=1`（`onnxruntime_go` 另有 `DisableTelemetry()`）。
6. **安裝包**：移除 `neural/python`、numpy 與 wheel 解包流程；保留並改為直接放置
   ORT DLL 與 4 個 VC runtime DLL；更新 `package_windows.py`、
   `prepare_neural.py`、`collect_notices.py` 與相關 `test_release.py` 斷言。
7. **CI**：新增能建 CGO（或 purego）Windows 目標的 job、下載 ORT DLL 的步驟，
   以及用外部模型跑 token／分數黃金測試的 job。目前 probe 是獨立 module，
   上層 CI 不會自動跑到它。
8. **遷移與回退**：用 feature flag 切換 native／Python worker，保留一個版本的回退
   路徑，確認模型缺檔、DLL 載入失敗、CPU 不支援時能退回統計選字。
9. **平台覆蓋**：Linux x86-64 與 Windows x86-64 已驗證；macOS（arm64）、
   Windows ARM64 尚未處理，wheel 內有對應的 ORT 執行檔但沒量測。
10. **正式模型量測**：這次只有 untrained placeholder；換成真模型後要重跑延遲、
    RSS 與分數 parity。

## 8. 風險與限制

- 這台機器是 WSL2／12 vCPU，絕對延遲不能直接和 manifest 的 aarch64 數字比；
  跨路徑比較才是有效訊號。
- 只有一個 reranker placeholder 模型，沒有覆蓋 sequence 模型與其他架構。
- placeholder 的 embedding（14,080）與 tokenizer（32,768）不一致，parity 測試
  用 `id % 14080`；正式出貨必須對齊，否則會是執行期錯誤。
- `onnxruntime_go` 需要 CGO；purego 替代方案未驗證，這是 GO 之後最大的工程不確定性。
- Windows 只在一台 Windows 11 機器驗證，沒有覆蓋 Windows 10、ARM64、無 AVX 的 CPU。
- probe 的 RSS 量測只支援 Linux；Windows 回報 −1。

## 9. 重現指令

```sh
# 1. venv（repo 外）
uv venv /tmp/opencode/ort && VIRTUAL_ENV=/tmp/opencode/ort uv pip install onnxruntime==1.32.0
uv venv /tmp/opencode/spm && VIRTUAL_ENV=/tmp/opencode/spm uv pip install sentencepiece numpy onnxruntime==1.32.0

# 2. 建置 probe
cd tools/onnxprobe && go build -o /tmp/onnxprobe .

# 3. Go 分詞與量測
/tmp/onnxprobe tokenize -spm $REF/tokenizer/public_core_ja.model \
  -texts $REF/tokenizer/sample-ja.txt -limit 200 -out /tmp/go-tokens.json
/tmp/onnxprobe bench -onnx $REF/models/reranker-public-core/reranker-public-core.placeholder.int8.onnx \
  -lib /tmp/opencode/ort/lib/python3.13/site-packages/onnxruntime/capi/libonnxruntime.so.1.32.0 \
  -spm $REF/tokenizer/public_core_ja.model -texts $REF/tokenizer/sample-ja.txt \
  -limit 200 -seq 32 -runs 100 -threads 2 -vocab-cap 14080 -out /tmp/go-bench.json

# 4. Python 參考與比對
python reference/oracle.py --spm $REF/tokenizer/public_core_ja.model \
  --onnx $REF/models/reranker-public-core/reranker-public-core.placeholder.int8.onnx \
  --texts $REF/tokenizer/sample-ja.txt --limit 200 --seq 32 --vocab-cap 14080 --out /tmp/oracle.json
python reference/compare.py --go /tmp/go-bench.json --oracle /tmp/oracle.json

# 5. Windows 交叉編譯
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build -o /tmp/onnxprobe.exe .
```

`$REF` 是 `/home/fedora/.opencode/plan/0c-ja/phase2/reference`。

## 10. 檔案

- `tools/onnxprobe/go.mod`、`go.sum`
- `tools/onnxprobe/main.go`
- `tools/onnxprobe/spm/model.go`、`spm/encode.go`、`spm/spm_test.go`
- `tools/onnxprobe/spm/testdata/tokens-200.json`
- `tools/onnxprobe/reference/oracle.py`、`compare.py`、`worker_latency.py`
- `tools/onnxprobe/README.md`
- `tools/onnxprobe/P2-30S-native-onnx.md`（本報告副本）

沒有 commit：`.onnx`、`libonnxruntime.so`、`onnxruntime.dll`、`.exe`、任何 venv
或語料文字。報告本身的最終 commit SHA 見 `git log -1`（本分支未 push）。
