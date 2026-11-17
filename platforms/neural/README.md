# 神經模型 runtime

完整桌面套件於系統字典旁提供 `neural/`，由 `0c-server` 依設定啟動 worker。神經選字預設開啟；補標點為 Beta、預設關閉。

`runtime/sequence_runtime.py` 與 `runtime/punct_runtime.py` 是 stdio worker，依賴 numpy 與 onnxruntime。固定模型版本、授權及系統字典配對要求見 [模型說明](../../models/README.md)。

日文神經路徑已在 P2-01 移除：正式產品只用統計轉換，不再啟動外部
推論後端。日文 ONNX 模型由 P2-25 匯出、P2-30 接上；在那之前 `ja/` 只有
移除說明，沒有可執行的 worker。

## 使用發行版提供的 Python

從完整 tarball 根目錄啟動已編譯的 server：

```sh
build/0c-server -data assets/data \
  -neural-python /usr/bin/python3 \
  -neural-package models/select -neural-worker platforms/neural/runtime/sequence_runtime.py \
  -punct-package models/punct -punct-worker platforms/neural/runtime/punct_runtime.py
```

系統 Python 須已安裝 numpy 與 onnxruntime；server 依使用者設定啟動模型。補標點仍預設關閉。安裝時依發行版目錄配置調整上述路徑，並保留字典與模型的固定配對。

## 官網可攜套件

```sh
python3 scripts/prepare_neural.py --platform linux-amd64 \
  --dictionary assets/data --models models \
  --out build/neural-linux-amd64
```

Windows 使用 `windows-amd64`，Apple Silicon 使用 `macos-arm64`（onnxruntime 需 macOS 14 以上）。Intel Mac 使用 `macos-amd64`：onnxruntime 1.30.0 沒有 Intel Mac 版，改用最後一版有的 1.23.2 與它支援的 CPython 3.13.15；兩個模型的輸出與 1.30.0 逐請求比對過相同。工具下載並校驗固定版本的 Python 與 wheel，快取在 `build/cache/neural`。

`windows-amd64` 的 `neural/` 要在 Linux x86-64 上準備，再把輸出目錄複製到 Windows 建置機交給 `scripts/package_windows.py --neural`：工具用同一版 CPython 的 Linux 版（python-build-standalone 的 `x86_64-unknown-linux-gnu`）把 numpy、onnxruntime 編成 bytecode，這個直譯器在 Windows 上跑不起來。同一版 CPython 的 bytecode 不分平台，Windows 內附 Python 的標準函式庫本來就是 `python314.zip` 裡的 bytecode，所以不必在 Windows 上編。macOS 的兩個平台在 Linux 或 Mac 上準備都可以。

模型是 `models/models.lock.json` 記的 Hugging Face 公開版（#180，0.2.3 起；0.2.2 以前是權重相同的內部匯出，見 `models/equivalent.json`）。`--models` 指定的目錄須有 `<select|punct>/model.onnx`、`LICENSE`、`README.md`，完整 tarball 的 `models/` 即可；省略時用 `build/cache/models`，缺的從 Hugging Face 下載，一律逐檔核對 `models.lock`。`manifest.json`、`vocab.json` 由 `models/upstream` 配對到 `--dictionary`，結果等於這份字典的 `pairing.lock.json`。只換字典或只換模型、沿用現有的可攜 Python 時用 `scripts/hf_models.py neural`。

以 `--neural` 將輸出目錄交給平台打包工具，並用同一份字典作為 `--data`。發行版離線建置應自行提供相依套件，不直接使用會下載 runtime 的可攜套件流程。

worker 必須以 `-I -B -X utf8` 執行；安裝後檔案不得在執行時改變。換模型須同步更換配對字典並重新產生 runtime，不手動改寫 manifest。公開包打包時核對 `neural/` 就是公開版模型配對出貨字典的結果（`hf_models.check_shipped`）。

## 模型常駐與 CPU／GPU provider（P2-30）

`0c-server` 以一個模型管理器管理 sequence 選字與補標點，兩者共用同一個有界執行器；模型的 manifest 與雜湊同步核對，模型本身一律在背景載入，第一個按鍵不必等 Python。

- `-model-mode auto`：主要語言先背景載入，其他語言第一次用到才載入（預設）。
- `-model-mode all`：所有模型背景載入，仍不阻塞第一個按鍵。
- `-model-mode primary`：只常駐 `-primary-language` 指定的語言。
- `-model-mode none`：不載入模型，只用統計引擎。
- 模型缺失、manifest 版本或字典配對不符、檔案雜湊不符、ONNX 內容損壞、Python 或 onnxruntime 不可用時，按鍵與提交照統計結果繼續；health 的 `selection_state`、`punctuation_state` 與 `*_reason` 會回報狀態。
- `model_resident_bytes`、`model_peak_bytes`、`model_in_flight`、`model_residency` 由 health 回報，供記憶體量測；不含輸入文字。
- manifest 帶 `placeholder: true` 時，health 另回報 `model_placeholder=true`，啟動紀錄也會標明 PLACEHOLDER；placeholder 模型不得當成已訓練模型或進入正式安裝包。

onnxruntime 預設只用 `CPUExecutionProvider`。需要 GPU 時用 `ZUIN_ORT_PROVIDERS` 指定逗號分隔的 provider（例如 `CUDAExecutionProvider`）：只有 onnxruntime 實際回報可用時才加入，建立 session 失敗或 provider 不存在都會回退 CPU，不影響啟動。

`runtime/onnx_bundle_probe.py` 是 P2-25P placeholder 的驗證工具，會核對 `0c.model.manifest/1` 的檔案大小與 SHA-256、載入 int8 ONNX、跑一次推論並輸出 placeholder、provider、載入時間與第一次推論時間。它不進正式安裝包。
