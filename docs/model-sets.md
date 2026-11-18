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
- 原生後端支援補標點；Python-free 套件以注音 `zuin-punct-runtime-v1` 或日文 `0c.model.manifest/1` 標點模型執行，不啟動 Python worker。模型不可用時才保留原文。

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

日文不以單一模型檔出貨，而以模型組為單位，綁定同一個日文資料包 manifest。`ja-public-core` 是完整的 reranker 加 punctuation，兩者都是 MIT。`ja-cc-by-sa` 是 reranker-only 實驗組：只有 CC BY-SA 4.0 的 `reranker-extra`，透過 `shared_punctuation` 共用 public-core 的 `punctuation-public-core`（MIT）。

| 模型組 ID | Variant | 授權 | 目前狀態 |
|---|---|---|---|
| `ja-public-core` | `public-core` | MIT | `reranker-public-core` + `punctuation-public-core`，HF 來源，尚未 pin revision |
| `ja-cc-by-sa` | `cc-by-sa` | CC BY-SA 4.0 reranker + MIT punctuation | reranker-only：`reranker-extra` + 共用 `punctuation-public-core`，HF 來源，尚未 pin revision |

混用規則以 artifact 為單位：CC BY-SA 4.0 artifact 不得安裝進 public-core 組；`punctuation` 只允許 public-core，所以不存在 CC BY-SA punctuation。安裝 `reranker-extra` 時必須帶 CC BY-SA 4.0 notice，`punctuation-public-core` 保持 MIT。模型組清單在 `models/pairing.lock.json` 的 `model_sets`，共三個 artifact。

### 在設定程式切換

Windows 設定程式的「Mixed 輸入」頁有「日文模型組」：

- `public-core` 是預設。
- `cc-by-sa` 標示為實驗組，只進 experimental，預設不會載入；它只有 `reranker-extra`，punctuation 共用 public-core。
- 「允許 placeholder 模型」預設關閉；placeholder 只存在 `testdata/models/placeholder/`，正式 lock 不含 placeholder。

設定會寫進 `settings.json`。在日文模型尚未接入推論前，切換模型組不會改變目前日文轉換的結果。

### 命令列安裝與回報

列出模型組：

```sh
go run ./cmd/0c-model-set list
```

只驗證，不搬檔案：

```sh
go run ./cmd/0c-model-set validate -set ja-public-core \
  -from /path/to/reference/models -pack build/ja -allow-placeholders
```

安裝所選的一組，並在 `-to` 寫出 `active.json`：

```sh
go run ./cmd/0c-model-set install -set ja-public-core \
  -from /path/to/reference/models -to ~/.local/share/zuin/models \
  -pack build/ja -allow-placeholders
```

`install` 只放入所選 variant 的檔案，並移除另一組的目錄。同一組重裝要加 `-force`。模型檔不進 Git，工具只搬運已下載或已匯出的檔案。

引擎回報使用中的模型組：

```sh
0c-server -model-report -model-set ja-public-core \
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
- 兩組日文模型尚未 pin 正式 revision，沒有品質數字，不能當成模型比較結果。placeholder 只作測試 fixture，不能進入 release bundle。
- Windows Phase 2 VM 驗收 NOT RUN，模型與候選的實機行為還沒有 Windows 實機通過紀錄。
- 注音模型的下載與配對只在支援的 Linux 系統安裝提供；Windows 套件內附模型，macOS 套件依打包內容提供。
