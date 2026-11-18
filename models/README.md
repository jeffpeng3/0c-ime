# 發佈模型

每個版本的 Release 另附 `0c-ime-models-<版本>.tar.xz`，內含兩個可直接搭配 `assets/data/` 使用的模型，解壓到原始碼 tarball 的同一處即補上本目錄的 `select/` 與 `punct/`（0.2.2 以前內附在原始碼 tarball）。模型沒有公開訓練程式與訓練資料，不屬於 DFSG-free 的原始碼 tarball，見 [DFSG 檢查](../legal/DFSG-free.md)。模型權重與版本固定，建置輸入法不需要訓練模型或重新產生字典；沒有模型時輸入法以統計選字運作。使用者版授權整理見 [docs/licensing.md](../docs/licensing.md)。

| 目錄 | 模型 | 授權 |
|---|---|---|
| `select/` | [0c-sequence-d512](https://huggingface.co/Funmula/0c-sequence-d512)，D512 PF512／2026-09-26 | MIT |
| `punct/` | [0c-punct-d512](https://huggingface.co/Funmula/0c-punct-d512)，D512 e60k／2026-09-30 | MIT |

每個目錄包含 `model.onnx`、`vocab.json`、`manifest.json`、`LICENSE` 與上游 `README.md`。Git 只保存固定清單，權重由發佈工具加入模型 tarball，不使用 Git LFS。

## 原生 ONNX Runtime（P2-43）

Windows 產品不內嵌 Python，也不出貨 wheel。`runtime.lock.json` 固定 `onnxruntime` 1.30.0 與 `msvc_runtime` 14.44.35112 兩個 wheel 的 URL、SHA-256，以及實際出貨的五個 DLL（`onnxruntime.dll`、`vcruntime140.dll`、`vcruntime140_1.dll`、`msvcp140.dll`、`msvcp140_1.dll`）的大小與 SHA-256。`scripts/prepare_windows_runtime.py` 只從 wheel 取出這些 PE 檔，`scripts/package_windows.py --ort-runtime` 逐檔核對後放在 `0c-server.exe` 旁，並拒絕任何 Python 檔案、wheel、numpy 或 onnxruntime Python 套件。`0c-server` 預設在有 DLL 時使用 native 後端；DLL 不在或模型形狀不受支援時退回統計轉換，原因寫在 health 的 `neural_backend_reason`。runtime 檔不進 Git。

## 字典配對

[models.lock.json](models.lock.json) 記錄上游公開套件的 commit、檔案大小與 SHA-256；[pairing.lock.json](pairing.lock.json) 固定本版字典 manifest 的 SHA-256，以及配對後的模型 metadata 雜湊。模型 tarball 中的模型已完成配對，使用者不用執行配對工具。

發佈組裝時只更新 `dictionary_sha256` 與對應 vocab 檔案雜湊；ONNX 權重、token 順序、注音映射與推論參數均維持固定上游版本。模型卡與授權保留上游內容，本版字典綁定以配對清單為準。來源字典不符合公開檢查、固定雜湊或配對清單時即拒絕交付。

字典或模型更新，須由維護者重新驗證並更新固定清單；不在一般建置或安裝時任意改寫綁定。遇到模型不支援的字或讀音，仍依 runtime 的既有規則退回統計選字。

推論 worker 在 `platforms/neural/runtime/`。Linux 與 macOS 的發行版提供 Python、NumPy 與 ONNX Runtime 執行環境；Windows 產品不內建 Python，改出貨固定版本的 `onnxruntime.dll` 與 MSVC 執行階段（[models/runtime.lock.json](runtime.lock.json)）。兩個 tarball 都不攜帶可攜 Python、wheel、訓練 checkpoint 或訓練環境。啟動方式見 [神經推論](../platforms/neural/README.md)。

## 日文模型組與版本分離（P2-27）

日文模型不以「單一模型檔」出貨，而是以**模型組**為單位：一組固定一個 reranker 與一個 punctuation，兩者同 variant，並綁定同一個日文詞典包的 manifest SHA-256。`public-core` 與 `cc-by-sa` 是兩個獨立的組，不得混用、不得配成 mixed set。`ja-public-core` 的模型標示 CC BY 4.0，資料依公開核心的 CC BY 4.0／公共領域／permissive notice-only 三類授權；`ja-cc-by-sa` 的模型與資料標示 CC BY-SA 4.0，只進 experimental。模型組清單在 [pairing.lock.json](pairing.lock.json) 的 `model_sets` 區段；外層既有的注音字典配對欄位不變，舊版 `0c-server`／`0c-update` 會忽略新區段。

產品側 manifest schema 在 [model-set.schema.json](model-set.schema.json)（JSON Schema 2020-12）。每個模型記錄：

- `artifact_id`、`kind`（`reranker`／`punctuation`）、`variant`、版本與參數數。
- 每個檔案格式（`int8`、`fp32`）的相對路徑、大小、SHA-256 與量化方式。
- `io`：`input_ids`／`attention_mask` 都是 `int64 [batch, seq]`；reranker 輸出 `scores` 為 `float32 [batch]`，punctuation 輸出 `logits` 為 `float32 [batch, seq, 4]`。
- `license`、`notice`、`attribution` 參考（授權全文與來源帳本）。
- `source`（訓練資料、generator、fingerprint）與 `placeholder`／`quality`／`production_eligible`。

`dictionary.manifest_sha256` 是安裝後 `<data>/ja/manifest.json` 的 SHA-256。驗證時必須等於實際安裝的日文包；字典重建或換版就要重新審查並更新這份清單。placeholder 模型（`placeholder: true`、`quality: untrained`）只有在 `allow_placeholders` 開啟時才能通過驗證或安裝；預設拒絕，且 `production_eligible` 必須是 `false`。檔名含 `.placeholder.` 的檔案不能拿掉標記。

### 選擇與安裝

```sh
# 列出模型組（含 placeholder 狀態）
go run ./cmd/0c-model-set list

# 只驗證，不搬檔案
go run ./cmd/0c-model-set validate -set ja-public-core \
  -from /path/to/reference/models -pack build/ja -allow-placeholders

# 安裝所選的一組；-to 會寫出 active.json
go run ./cmd/0c-model-set install -set ja-public-core \
  -from /path/to/reference/models -to ~/.local/share/zuin/models \
  -pack build/ja -allow-placeholders
```

`install` 只會放入所選 variant 的模型檔，並移除清單中另一組的目錄，避免同一個安裝目錄留下 public-core 與 cc-by-sa 混用的檔案。同一組重裝要加 `-force`。模型檔不進 Git（`.gitignore` 也排除 `*.onnx`），工具只搬運已下載或已匯出的檔案。

### 引擎回報

`0c-server -model-report` 沿用 P2-33 的 JSON 輸出，另外加上 `model_set` 區段：

```sh
0c-server -model-report -model-set ja-public-core \
  -model-set-root ~/.local/share/zuin/models -model-set-lock models/pairing.lock.json
```

沒有指定 `-model-set` 時會讀 `<model-set-root>/active.json`；兩者都沒有時 `model_set.active` 為 `false`。報告會列出使用中的 set／variant／placeholder、字典包雜湊是否相符、模型檔是否通過大小與 SHA-256 核對；設定或檔案有問題時只在報告裡說明，不影響注音與日文統計轉換。日文模型正式接上推論仍是 P2-30 之後的工作。
