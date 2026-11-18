# 發佈模型

每個版本的 Release 另附 `0c-ime-models-<版本>.tar.xz`，內含兩個可直接搭配 `assets/data/` 使用的模型，解壓到原始碼 tarball 的同一處即補上本目錄的 `select/` 與 `punct/`（0.2.2 以前內附在原始碼 tarball）。模型沒有公開訓練程式與訓練資料，不屬於 DFSG-free 的原始碼 tarball，見 [DFSG 檢查](../legal/DFSG-free.md)。模型權重與版本固定，建置輸入法不需要訓練模型或重新產生字典；沒有模型時輸入法以統計選字運作。使用者版授權整理見 [docs/licensing.md](../docs/licensing.md)。

| 目錄 | 模型 | 授權 |
|---|---|---|
| `select/` | [0c-sequence-d512](https://huggingface.co/Funmula/0c-sequence-d512)，D512 PF512／2026-09-26 | MIT |
| `punct/` | [0c-punct-d512](https://huggingface.co/Funmula/0c-punct-d512)，D512 e60k／2026-09-30 | MIT |

每個目錄包含 `model.onnx`、`vocab.json`、`manifest.json`、`LICENSE` 與上游 `README.md`。Git 只保存固定清單，權重由發佈工具加入模型 tarball，不使用 Git LFS。

## 原生 ONNX Runtime（P2-43）

產品執行階段不內嵌 Python，也不出貨 wheel。`runtime.lock.json` 固定 `onnxruntime` 1.30.0 與 `msvc_runtime` 14.44.35112 兩個 wheel 的 URL、SHA-256，以及實際出貨的五個 DLL（`onnxruntime.dll`、`vcruntime140.dll`、`vcruntime140_1.dll`、`msvcp140.dll`、`msvcp140_1.dll`）的大小與 SHA-256。`scripts/prepare_windows_runtime.py` 只從 wheel 取出這些 PE 檔，`scripts/package_windows.py --ort-runtime` 逐檔核對後放在 `0c-server.exe` 旁，並拒絕任何 Python 檔案、wheel、numpy 或 onnxruntime Python 套件。`0c-server` 預設在有 DLL 時使用 native 後端；DLL 不在或模型形狀不受支援時退回統計轉換，原因寫在 health 的 `neural_backend_reason`。runtime 檔不進 Git。

## 字典配對

[models.lock.json](models.lock.json) 記錄上游公開套件的 commit、檔案大小與 SHA-256；[pairing.lock.json](pairing.lock.json) 固定本版字典 manifest 的 SHA-256，以及配對後的模型 metadata 雜湊。模型 tarball 中的模型已完成配對，使用者不用執行配對工具。

發佈組裝時只更新 `dictionary_sha256` 與對應 vocab 檔案雜湊；ONNX 權重、token 順序、注音映射與推論參數均維持固定上游版本。模型卡與授權保留上游內容，本版字典綁定以配對清單為準。來源字典不符合公開檢查、固定雜湊或配對清單時即拒絕交付。

字典或模型更新，須由維護者重新驗證並更新固定清單；不在一般建置或安裝時任意改寫綁定。遇到模型不支援的字或讀音，仍依 runtime 的既有規則退回統計選字。

推論由 Go 原生 ONNX Runtime 後端執行，沒有 Python worker。Windows 產品出貨固定版本的 `onnxruntime.dll` 與 MSVC 執行階段（[models/runtime.lock.json](runtime.lock.json)）；其他平台有共享函式庫時使用 native，缺少時退回統計引擎。兩個 tarball 都不攜帶可攜 Python、wheel、訓練 checkpoint 或訓練環境。啟動方式見 [神經推論](../platforms/neural/README.md)。

## 日文模型包（P2-27）

日文不以可選模型組出貨，而是**一個固定模型包**，綁定同一個日文詞典包的 manifest SHA-256。模型包直接包含 `reranker`（CC BY-SA 4.0，HF `jeffpeng3/0c-ja-reranker` revision `7450e10f…`）與 `punctuation-public-core`（MIT，HF `jeffpeng3/0c-ja-punct` revision `ff9166ab…`）。安裝 `reranker` 時必須帶 CC BY-SA 4.0 notice；punctuation 保持 MIT 並附 UniDic／IPADic notice。公開核心 reranker 只留在訓練 repo 作評估參考，不出現在產品 lock、文件或安裝路徑。清單在 [pairing.lock.json](pairing.lock.json) 的 `model_package` 區段；外層既有的注音字典配對欄位不變，舊版 `0c-server`／`0c-update` 會忽略新區段。舊的 `0c-ja-reranker-extra` repo 名稱只作暫時 redirect，產品一律使用新名稱 `jeffpeng3/0c-ja-reranker`。

產品只有這一個模型包，沒有其他模型組或 variant。兩個模型分別來自 `jeffpeng3/0c-ja-reranker`（CC BY-SA 4.0）與 `jeffpeng3/0c-ja-punct`（MIT）。每個 artifact 的 `hf` 記錄 repo、revision、授權與逐檔名稱／大小／SHA-256。Git 只保存 lock；`0c-model-set fetch` 從 HF 下載權重、`manifest.json`、`vocab.json` 與 `tokenizer.model`，逐檔核對後放進安裝包。每個 artifact 是獨立目錄，manifest、字表、分詞器與權重必須同在一處。`revision` 與大小／雜湊尚未 pin 時，development 驗證只警告，`-release` 與正式打包直接失敗。placeholder 只在 `testdata/models/placeholder/pairing.lock.json` 作測試 fixture，不在正式 lock；`-allow-placeholders` 仍可用來驗證或安裝這份 fixture。

**Tokenizer 來源註記（2026-11-19）**：`jeffpeng3/0c-ja-reranker` 與 `jeffpeng3/0c-ja-punct` 在鎖定的 revision 只公開 `manifest.json`、`vocab.json` 與 ONNX 權重，沒有公開 `tokenizer.model`。套件仍依 lock 的 SHA-256 `b25260c9c3d90a754c719e62eea6b8125f08448d48dd4d1755c18edbd00e2078` 放入 614,451 bytes 的 `tokenizer.model`，內容取自 pinned training reference；重跑 `fetch` 前必須把同名檔案補到 HF repo 或改用 `--model-source`，否則下載會在 tokenizer 得到 HTTP 404。

產品側 manifest schema 在 [model-package.schema.json](model-package.schema.json)（JSON Schema 2020-12）。每個模型記錄：

- `artifact_id`、`kind`（`reranker`／`punctuation`）、`variant`、版本與參數數。正式 lock 共兩個 artifact：`reranker`（cc-by-sa）與 `punctuation-public-core`（public-core）。
- 每個檔案格式（`int8`、`fp32`）的相對路徑、大小、SHA-256 與量化方式。
- `io`：`input_ids`／`attention_mask` 都是 `int64 [batch, seq]`；reranker 輸出 `scores` 為 `float32 [batch]`，punctuation 輸出 `logits` 為 `float32 [batch, seq, 4]`。
- `license`、`notice`、`attribution` 參考（授權全文與來源帳本）。reranker 的 attribution 含 Wikipedia 與 Stack Exchange 的 experimental 語料；punctuation 的 attribution 含 UniDic 與 IPADic notice。
- `source`（訓練資料、generator、fingerprint）與 `placeholder`／`quality`／`production_eligible`。
- `hf`：Hugging Face repo、commit `revision`、授權，以及每個格式的檔名、大小與 SHA-256。

`dictionary.manifest_sha256` 是安裝後 `<data>/ja/manifest.json` 的 SHA-256。驗證時必須等於實際安裝的日文包；字典重建或換版就要重新審查並更新這份清單。placeholder 模型（`placeholder: true`、`quality: untrained`）只有在 `allow_placeholders` 開啟時才能通過驗證或安裝；預設拒絕，且 `production_eligible` 必須是 `false`。檔名含 `.placeholder.` 的檔案不能拿掉標記。

### Pin、下載與驗證

HF 的 revision 與逐檔雜湊用 `pin` 寫進 lock。`pin` 會下載該 repo 的每個模型檔、計算大小與 SHA-256；可重跑，離線時只接受 cache：

```sh
go run ./cmd/0c-model-set pin \
  --lock models/pairing.lock.json --repo jeffpeng3/0c-ja-reranker \
  --revision <commit> --cache build/cache/ja-models
```

`check` 只驗 lock schema 與雜湊欄位，不連網；development 模式對未 pin 的 revision 印警告，`-release` 直接失敗：

```sh
go run ./cmd/0c-model-set check --lock models/pairing.lock.json
go run ./cmd/0c-model-set check --lock models/pairing.lock.json -release
```

`fetch` 從 HF 下載模型包，逐檔核對大小與 SHA-256，並把結果放到 lock 的相對路徑；`-offline` 只用 cache，缺檔或雜湊不符都直接失敗：

```sh
go run ./cmd/0c-model-set fetch -format int8 \
  -out build/ja-models -cache build/cache/ja-models
```

### 安裝

```sh
# 列出模型包（含 placeholder 狀態）
go run ./cmd/0c-model-set list

# 只驗證，不搬檔案
go run ./cmd/0c-model-set validate \
  -from /path/to/reference/models -pack build/ja

# 安裝模型包；-to 會寫出 active.json
go run ./cmd/0c-model-set install \
  -from /path/to/reference/models -to ~/.local/share/zuin/models \
  -pack build/ja
```

`install` 只會放入模型包的兩個 artifacts：`reranker` 與 `punctuation-public-core`。同一模型包重裝要加 `-force`。模型檔不進 Git（`.gitignore` 也排除 `*.onnx`），工具只搬運已下載或已匯出的檔案。

### 引擎回報

`0c-server -model-report` 沿用 P2-33 的 JSON 輸出，另外加上 `model_package` 區段：

```sh
0c-server -model-report \
  -model-package-root ~/.local/share/zuin/models \
  -model-package-lock models/pairing.lock.json
```

沒有 `active.json` 時 `model_package.active` 為 `false`。報告會列出模型包版本、placeholder、字典包雜湊是否相符、模型檔是否通過大小與 SHA-256 核對；設定或檔案有問題時只在報告裡說明，不影響注音與日文統計轉換。日文 reranker 與標點已接入引擎；沒有 `active.json` 或檔案核對失敗時，日文轉換保留統計順序。
