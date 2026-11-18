# 神經模型 runtime

完整桌面套件於系統字典旁提供 `neural/`，只有模型資料：`select/` 與 `punct/`
的 `model.onnx`、`vocab.json`、`manifest.json`、`LICENSE`。產品執行階段不含
CPython、NumPy、onnxruntime Python 套件或 stdio worker；`0c-server` 以 Go
原生 ONNX Runtime 後端載入模型。神經選字預設開啟；補標點為 Beta、預設關閉。

固定模型版本、授權及系統字典配對要求見 [模型說明](../../models/README.md)。

日文神經路徑在 P2-01 移除外部後端，P2-25 匯出模型，P2-30 接上原生執行；
`ja/` 只有移除說明，沒有可執行的 worker。

## 啟動

原生後端需要 `CGO_ENABLED=1` 與 `onnxruntime` build tag，以及 ONNX Runtime
共享函式庫。Windows 產品出貨固定版本的 `onnxruntime.dll` 與 MSVC 執行階段，
放在 `0c-server.exe` 旁；Linux／macOS 由發行版提供 `onnxruntime.so`／
`libonnxruntime.dylib`，或用 `-neural-ort-lib`／`ZUIN_ORT_LIBRARY` 指定。

```sh
build/0c-server -data assets/data \
  -neural-package models/select -punct-package models/punct
```

沒有原生後端或共享函式庫時，`-neural-backend auto` 直接退回統計引擎；產品
沒有 Python 執行路徑。`CGO_ENABLED=0` 的建置以 native stub 編譯，功能照常，
模型不載入。

## 官網可攜套件

`scripts/prepare_neural.py` 只取得並核對模型，輸出的 `neural/` 不含任何
runtime：

```sh
python3 scripts/prepare_neural.py --platform linux-amd64 \
  --dictionary assets/data --models models \
  --out build/neural-linux-amd64
```

Windows 產品由 `scripts/prepare_windows_runtime.py` 從
[models/runtime.lock.json](../../models/runtime.lock.json) 固定的 wheel 取出
`onnxruntime.dll` 與四個 MSVC 執行階段 DLL，再交給
`scripts/package_windows.py --ort-runtime`。版本、SHA-256 與打包邊界見
[打包與發佈](../../PACKAGING.md)。

模型是 `models/models.lock.json` 記的 Hugging Face 公開版（#180，0.2.3 起；
0.2.2 以前是權重相同的內部匯出，見 `models/equivalent.json`）。`--models`
指定的目錄須有 `<select|punct>/model.onnx`、`LICENSE`、`README.md`，完整
tarball 的 `models/` 即可；省略時用 `build/cache/models`，缺的從 Hugging Face
下載，一律逐檔核對 `models.lock`。`manifest.json`、`vocab.json` 由
`models/upstream` 配對到 `--dictionary`，結果等於這份字典的
`pairing.lock.json`。只換字典或只換模型時用 `scripts/hf_models.py neural`；
它只輸出模型，不複製舊 `neural/` 的 runtime。

換模型須同步更換配對字典並重新產生模型目錄，不手動改寫 manifest。公開包
打包時核對 `neural/` 就是公開版模型配對出貨字典的結果
（`hf_models.check_shipped`）。

## 模型常駐與 CPU／GPU provider（P2-30）

`0c-server` 以一個模型管理器管理 sequence 選字與補標點，兩者共用同一個有界
執行器；模型的 manifest 與雜湊同步核對，模型本身一律在背景載入，第一個
按鍵不必等模型。

- `-model-mode auto`：主要語言先背景載入，其他語言第一次用到才載入（預設）。
- `-model-mode all`：所有模型背景載入，仍不阻塞第一個按鍵。
- `-model-mode primary`：只常駐 `-primary-language` 指定的語言。
- `-model-mode none`：不載入模型，只用統計引擎。
- `-neural-backend auto`（預設）：建置含 CGO＋`onnxruntime` tag 且執行檔旁有
  ONNX Runtime 時用 native；DLL 不在或載入失敗時退回統計。
- `-neural-backend native`：明確要求 native。sequence 選字與補標點都走 Go
  原生 ONNX；需 `CGO_ENABLED=1 -tags onnxruntime` 建置與 ORT 共享函式庫
  （執行檔旁的 `onnxruntime.dll`／`onnxruntime.so`、`runtime/` 子目錄、
  `-neural-ort-lib` 或 `ZUIN_ORT_LIBRARY`）。補標點支援注音
  `zuin-punct-runtime-v1` 套件與日文 `0c.model.manifest/1` 標點模型。模型
  形狀不受支援時回報 `unsupported_model`。
- 模型缺失、manifest 版本或字典配對不符、檔案雜湊不符、ONNX 內容損壞、
  ONNX Runtime 不可用時，按鍵與提交照統計結果繼續；health 的
  `selection_state`、`punctuation_state` 與 `*_reason` 會回報狀態。
- `-neural-python`、`-system-python`、`-neural-worker`、`-punct-worker` 與
  `-neural-backend python` 已移除；指定時由 Go flag 解析直接報錯。
- `model_resident_bytes`、`model_peak_bytes`、`model_in_flight`、
  `model_residency` 由 health 回報，供記憶體量測；不含輸入文字。
- manifest 帶 `placeholder: true` 時，health 另回報
  `model_placeholder=true`，啟動紀錄也會標明 PLACEHOLDER；placeholder 模型
  不得當成已訓練模型或進入正式安裝包。

onnxruntime 預設只用 `CPUExecutionProvider`。需要 GPU 時用
`ZUIN_ORT_PROVIDERS` 指定逗號分隔的 provider（例如
`CUDAExecutionProvider`）：只有 onnxruntime 實際回報可用時才加入，建立
session 失敗或 provider 不存在都會回退 CPU，不影響啟動。

真實模型的 parity gates 保留為開發用測試：它們有 `cgo && onnxruntime` build
tag，並要求 `NEURAL_*` 環境變數指向模型、ORT 與外部參考實作；未設定時跳過。
repo 不包含 Python worker，測試也不是執行階段相依。細節見
[P2-30N](P2-30N-native-backend.md)。
