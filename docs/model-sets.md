# 模型與模型組

模型分成兩類：注音的選字／補標點模型，以及日文的模型組。兩者的檔案、授權與切換方式不同，不能混在一起看。

## 注音模型

| 模型 | 目錄 | 用途 | 授權 |
|---|---|---|---|
| 選字 | `models/select/` | 依前文重排注音候選 | MIT |
| 補標點 | `models/punct/` | 送出時補標點（Beta） | MIT |

完整套件會內附已配對的模型。沒有模型時引擎仍可用統計選字；補標點關閉或模型不可用時保留原文。

### Windows

Windows 產品出貨原生 ONNX Runtime，不內建 Python。`0c-server` 預設在有 `onnxruntime.dll` 時使用 native 後端：

- 支援選字／reranker 的模型形狀。
- 模型形狀不受支援、DLL 不在或載入失敗時，回報原因並保留統計排序。
- 原生後端目前不支援補標點，Python-free 套件提交時保留原文。設定程式的「提交時補標點」在這種套件不會實際補字。

### Linux 與 macOS

Linux 與 macOS 的神經推論使用系統或發行版提供的 Python、NumPy 與 ONNX Runtime。Linux 系統安裝不隨套件帶模型，使用者在設定程式的「模型」頁按「下載模型」才會連到 Hugging Face：

```sh
0c-update models status -json
0c-update models download
0c-update models cancel
0c-update models remove
```

下載與配對需要字典符合 `models/pairing.lock.json`。字典不是清單上的那一份時，設定程式會說明不能使用下載的模型。

## 日文模型組

日文不以單一模型檔出貨，而以模型組為單位，綁定同一個日文資料包 manifest。預設安裝組是 `ja-cc-by-sa`：CC BY-SA 4.0 的 `reranker-extra`，加上透過 `shared_punctuation` 共用的 MIT `punctuation-public-core`。`ja-public-core` 是 MIT-only 替代組，保留 `reranker-public-core` 與 `punctuation-public-core`；它的 reranker HF repo 尚未發布，因此 revision 未 pin，不是 release 候選。

| 模型組 ID | Variant | 授權 | 目前狀態 |
|---|---|---|---|
| `ja-cc-by-sa`（預設） | `cc-by-sa` | CC BY-SA 4.0 reranker + MIT punctuation | reranker-only：`reranker-extra` + 共用 `punctuation-public-core`。`reranker-extra` pin `7450e10fe8b6`，`punctuation-public-core` pin `ff9166abc620`；fp32／int8 大小與 SHA-256 都已寫入 lock |
| `ja-public-core` | `public-core` | MIT | MIT-only 替代組：`reranker-public-core` + `punctuation-public-core`。HF `jeffpeng3/0c-ja-reranker` 尚未發布、revision 未 pin，`production_eligible` 為 false |

`model_sets.default_set` 明示預設是 `ja-cc-by-sa`；`0c-model-set` 的 `install`、`fetch`、`show` 省略 `-set` 時用它，清單也會標示 default。混用規則以 artifact 為單位：CC BY-SA 4.0 artifact 不得安裝進 public-core 組；`punctuation` 只允許 public-core，所以不存在 CC BY-SA punctuation。安裝 `reranker-extra` 時一定帶 `legal/CC-BY-SA-4.0.txt` 與打包產生的 `MODEL-SET-CC-BY-SA-4.0.txt`／`MODEL-SET-NOTICES.txt`，`punctuation-public-core` 保持 MIT。模型組清單在 `models/pairing.lock.json` 的 `model_sets`，共三個 artifact。正式 release 的 `check -release` 只驗 `default_set`；替代組可以留在 lock，但不得讓未 pin 的 revision 進入安裝包。

### 部署設定

模型組 metadata 的 `model_sets.deployment` 固定這組預設模型一起出貨的推論設定：

| 設定 | 值 | 說明 |
|---|---:|---|
| `sequence_length` | 124 | ONNX 匯出與執行的 seq 長度（P2-25C／P2-63） |
| `context_tokens_per_side` | 60 | 候選左側與右側各最多 60 個 token |
| `gating_top_k` | 3 | 只把統計前 3 名候選送進 reranker，其餘保留字典序 |
| `format` | `int8` | 預設出貨量化格式；`fp32` 只作對照 |

`gating_policy` 也記在 lock：逾時、取消或模型不可用時保留目前字典序，不閃動。這些值跟著 `default_set` 一起審查，改模型或改 seq 時要同時更新 lock 與文件。

### 在設定程式切換

Windows 設定程式的「Mixed 輸入」頁有「日文模型組」：

- `cc-by-sa` 是預設，包含 `reranker-extra` 與共用的 public punctuation；安裝時帶 CC BY-SA 4.0 notice。
- `public-core` 是 MIT-only 替代組，只在它的 HF reranker 發布並 pin 後才能成為 release 選項。
- 「允許 placeholder 模型」預設關閉；placeholder 只存在 `testdata/models/placeholder/`，正式 lock 不含 placeholder。

設定會寫進 `settings.json`。在日文模型尚未接入推論前，切換模型組不會改變目前日文轉換的結果。

### 命令列安裝與回報

列出模型組：

```sh
go run ./cmd/0c-model-set list
```

只驗證預設組，不搬檔案：

```sh
go run ./cmd/0c-model-set validate \
  -from /path/to/reference/models -pack build/ja
```

安裝預設組，並在 `-to` 寫出 `active.json`：

```sh
go run ./cmd/0c-model-set install \
  -from /path/to/reference/models -to ~/.local/share/zuin/models \
  -pack build/ja
```

`-set` 可指定 `ja-public-core` 檢查替代組。正式發行前跑 release 檢查；它只驗 `default_set`，任何 revision 或逐檔 SHA-256 被移除都會失敗：

```sh
go run ./cmd/0c-model-set check -lock models/pairing.lock.json -release
```

`install` 只放入所選 variant 的檔案，並移除另一組的目錄。同一組重裝要加 `-force`。模型檔不進 Git，工具只搬運已下載或已匯出的檔案。

引擎回報使用中的模型組：

```sh
0c-server -model-report -model-set ja-cc-by-sa \
  -model-set-root ~/.local/share/zuin/models \
  -model-set-lock models/pairing.lock.json
```

沒有指定 `-model-set` 時會讀 `<model-set-root>/active.json`。報告會列出使用中的 set／variant／placeholder、字典包雜湊是否相符、模型檔是否通過大小與 SHA-256 核對。報告有問題時只影響報告與模型載入，不影響注音與日文統計轉換。

## 常駐與退回

- `model-mode`：`auto` 先載入主要語言、其他語言延遲；`all` 全部載入；`primary` 只載入主要語言；`none` 不載入模型。
- `model-residency-mb`：`auto` 依單語言／雙語言選 250 MB／400 MB，也可指定上限。
- 超過預算、模型缺少、形狀不受支援或載入失敗時，保留統計轉換。
- 私人欄位不送模型。

## 目前限制

- 日文 reranker／punctuation 尚未接入產品推論，`model-set` 目前只影響設定與模型報告。這項接線是 P2-30 之後的工作。
- 預設組的 `reranker-extra` 與 `punctuation-public-core` 已 pin HF revision、大小與 SHA-256，但還沒有產品驗收的品質承諾；`ja-public-core` 的 MIT reranker 尚未發布，不能當成 release 結果。placeholder 只作測試 fixture，不能進入 release bundle。
- Windows Phase 2 VM 驗收 NOT RUN，模型與候選的實機行為還沒有 Windows 實機通過紀錄。
- 注音模型的下載與配對只在支援的 Linux 系統安裝提供；Windows 套件內附模型，macOS 套件依打包內容提供。
