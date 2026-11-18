# 模型與模型包

模型分成兩類：注音的選字／補標點模型，以及日文模型包。兩者的檔案與授權不同，不能混在一起看。

## 注音模型

| 模型 | 目錄 | 用途 | 授權 |
|---|---|---|---|
| 選字 | `models/select/` | 依前文重排注音候選 | MIT |
| 補標點 | `models/punct/` | 送出時補標點（Beta） | MIT |

完整套件會內附已配對的模型。沒有模型時引擎仍可用統計選字；補標點關閉或模型不可用時保留原文。

### Windows

產品出貨或使用原生 ONNX Runtime，執行階段不含 Python。`0c-server` 預設在有 `onnxruntime.dll` 時使用 native 後端：

- 支援選字／reranker 的模型形狀。
- 模型形狀不受支援、DLL 不在或載入失敗時，回報原因並保留統計排序。
- 原生後端支援補標點：注音 `zuin-punct-runtime-v1` 或日文 `0c.model.manifest/1` 標點模型都由 `native.OpenPunct` 執行。模型不可用時才保留原文。

### Linux 與 macOS

Linux 與 macOS 的神經推論使用原生 ONNX Runtime；沒有共享函式庫時退回統計引擎。Linux 系統安裝不隨套件帶模型，使用者在設定程式的「模型」頁按「下載模型」才會連到 Hugging Face：

```sh
0c-update models status -json
0c-update models download
0c-update models cancel
0c-update models remove
```

下載與配對需要字典符合 `models/pairing.lock.json`。字典不是清單上的那一份時，設定程式會說明不能使用下載的模型。

## 日文模型包

產品只有一個日文模型包，沒有其他模型包可選。模型包綁定一個日文資料包 manifest，內含兩個 artifact：

| 角色 | 授權 | 來源 |
|---|---|---|
| 候選重排 reranker | CC BY-SA 4.0 | `jeffpeng3/0c-ja-reranker`，revision `7450e10fe8b6` |
| 標點 punctuation | MIT | `jeffpeng3/0c-ja-punct`，revision `ff9166abc620` |

兩個 artifact 的 fp32／int8 大小與 SHA-256 都在 `models/pairing.lock.json` 的 `model_package`。reranker 的舊 repo 名稱 `0c-ja-reranker-extra` 只作暫時 redirect，產品與文件一律使用 `jeffpeng3/0c-ja-reranker`。reranker 來自 Wikipedia 與 Stack Exchange 的 CC BY-SA 實驗語料，安裝或散布時一定帶 `legal/CC-BY-SA-4.0.txt` 與打包產生的 `MODEL-PACKAGE-CC-BY-SA-4.0.txt`、`MODEL-PACKAGE-NOTICES.txt`；punctuation 保持 MIT，附 UniDic 與 IPADic 的 notice。公開核心 reranker 只留在訓練 repo 作評估參考，不出現在產品 lock、文件或安裝路徑。

### 部署設定

`model_package.deployment` 固定出貨的推論設定：

| 設定 | 值 | 說明 |
|---|---:|---|
| `sequence_length` | 124 | ONNX 匯出與執行的 seq 長度（P2-25C／P2-63） |
| `context_tokens_per_side` | 60 | 候選左側與右側各最多 60 個 token |
| `gating_top_k` | 3 | 只把統計前 3 名候選送進 reranker，其餘保留字典序 |
| `format` | `int8` | 預設出貨量化格式；`fp32` 只作對照 |

`gating_policy` 也記在 lock：逾時、取消或模型不可用時保留目前字典序，不閃動。改模型或改 seq 時要同時更新 lock 與文件。

### 設定程式

設定程式的「Mixed 輸入」頁只顯示模型常駐模式、常駐預算與是否允許 placeholder，沒有模型包選項。模型包固定為上述組合；「允許 placeholder 模型」預設關閉，placeholder 只存在 `testdata/models/placeholder/`，正式 lock 不含 placeholder。

### 命令列安裝與回報

列出模型包：

```sh
go run ./cmd/0c-model-set list
```

只驗證，不搬檔案：

```sh
go run ./cmd/0c-model-set validate \
  -from /path/to/reference/models -pack build/ja
```

安裝模型包，並在 `-to` 寫出 `active.json`：

```sh
go run ./cmd/0c-model-set install \
  -from /path/to/reference/models -to ~/.local/share/zuin/models \
  -pack build/ja
```

正式發行前跑 release 檢查；任何 revision 或逐檔 SHA-256 缺少都會失敗：

```sh
go run ./cmd/0c-model-set check -lock models/pairing.lock.json -release
```

同一份模型包重裝要加 `-force`。模型檔不進 Git，工具只搬運已下載或已匯出的檔案。

引擎回報使用中的模型包：

```sh
0c-server -model-report \
  -model-package-root ~/.local/share/zuin/models \
  -model-package-lock models/pairing.lock.json
```

沒有 `active.json` 時回報 `not_configured`。報告會列出模型包版本、placeholder、字典包雜湊是否相符、模型檔是否通過大小與 SHA-256 核對。報告有問題時只影響報告與模型載入，不影響注音與日文統計轉換。

## 常駐與退回

- `model-mode`：`auto` 先載入主要語言、其他語言延遲；`all` 全部載入；`primary` 只載入主要語言；`none` 不載入模型。
- `model-residency-mb`：`auto` 依單語言／雙語言選 250 MB／400 MB，也可指定上限。
- 超過預算、模型缺少、形狀不受支援或載入失敗時，保留統計轉換。
- 私人欄位不送模型。

## 目前限制

- 日文 reranker／punctuation 尚未接入產品推論，模型包目前只影響安裝與模型報告。這項接線是 P2-30 之後的工作。
- 模型包的 reranker 與 punctuation 已 pin HF revision、大小與 SHA-256，但還沒有產品驗收的品質承諾。placeholder 只作測試 fixture，不能進入 release bundle。
- Windows Phase 2 VM 驗收 NOT RUN，模型與候選的實機行為還沒有 Windows 實機通過紀錄。
- 注音模型的下載與配對只在支援的 Linux 系統安裝提供；Windows 套件內附模型，macOS 套件依打包內容提供。
