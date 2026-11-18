# 打包與發佈

給發行版維護者與本專案的發佈者。一般使用者請到 [官網下載](https://0c.biggo.com/download/)，逐步說明見 [使用者文件](docs/README.md)。

## 原始碼與模型 tarball

每個版本的 [GitHub Release](https://github.com/0c-biggo/0c-ime/releases) 提供 `0c-ime-<版本>.tar.xz`、`0c-ime-models-<版本>.tar.xz` 與 `SHA256SUMS`。

- `0c-ime-<版本>.tar.xz` 交付程式原始碼與可直接使用的統計字典，符合 DFSG（見 [DFSG 檢查](legal/DFSG-free.md)），可編譯出與官網同版本的輸入法，不需要重新產生統計資料。
- `0c-ime-models-<版本>.tar.xz` 是兩個已配對的神經模型。模型沒有公開訓練程式與訓練資料，不屬於 DFSG-free 的內容，因此分開提供；解壓到同一處即補上 `models/select/` 與 `models/punct/`。沒有模型時輸入法以統計選字運作。

| tarball 路徑 | 內容 |
|---|---|
| `assets/data/` | `lexicon.bin`、`lm.bin`、manifest、NOTICE、資料報告與英文字典子包 |
| `SOURCE-RELEASE.json` | 檔案清單、tag／commit 與固定清單 |
| 其他目錄 | Go 核心、平台外殼、推論 worker、建置工具與測試 |
| 模型 tarball：`models/select/`、`models/punct/` | 已配對的 ONNX 權重、vocab、manifest、模型卡與授權 |
| 模型 tarball：`MODELS-RELEASE.json` | 模型檔案清單，以及對應的 tag／commit、模型與配對 lock、字典 manifest 雜湊 |

```sh
sha256sum -c SHA256SUMS
tar -xJf 0c-ime-<版本>.tar.xz
tar -xJf 0c-ime-models-<版本>.tar.xz   # 選用：神經選字與補標點
```

0.2.2 以前的版本只有一個 tarball，模型內附在 `models/` 裡；0.2.1 以前以 `ime-oss-<版本>.tar.xz` 為名。從 git clone 或 Source code 壓縮檔開始時，`make assets` 會下載同版本的 tarball，驗證後把 `assets/data/` 與兩個模型放進原始碼目錄；`make assets MODELS=0` 只取字典。大型 bigram TSV 另見 [從原始碼重建字典](#從原始碼重建字典)；原始語料、checkpoint 與訓練環境不附入。

## 語言資產

注音與日文的資料包分開。注音包從 Release tarball 取得；日文包目前由 repo 內的 `data/ja` 以公開模式編譯，輸出到 `build/ja`。兩者都不會互相覆蓋。

| 指令 | 結果 |
|---|---|
| `make assets` | 注音字典與配對模型放在 `assets/data/`、`models/select/`、`models/punct/` |
| `make assets MODELS=0` | 只放注音字典，不加模型 |
| `make assets MODELS_ONLY=1` | 只換 `models/select/`、`models/punct/`；`assets/data/` 必須已存在，配對清單要與目前字典相符 |
| `make assets LANG=ja` | 以 `0c-compile -lang ja -public` 編譯 `data/ja`，輸出 `build/ja`，並複製 `symbols/` |
| `make assets LANG=all` | 同時處理注音與日文 |
| `make assets LANG=ja JA_OUT=<目錄>` | 指定日文包輸出位置 |

日文包沒有另附神經模型 tarball。P2-01 起正式產品不 import 或啟動 llama.cpp、GGUF、zenz，日文只用統計詞典轉換。日文 ONNX 模型會在 P2-25 匯出、P2-30 接上，屆時再依新的 manifest 與打包流程出貨；目前的移除狀態見 [日文 runtime 說明](platforms/neural/ja/README.md)。T03 的實驗程式只留在 `scripts/spike/ja-zenz/`，不是產品路徑。**載入接線注意**：`0c-server` 的日文載入路徑目前讀 `data/ja/lexicon/words.tsv` 與 `data/ja/connection.tsv`，編譯後 `data/ja` 的 `lexicon.bin`／`lm.bin` 尚未接上；在整合完成前，`--ja-data` 只證明打包與雜湊正確，不能宣稱 shipped 日文轉換可用。

日文包的 `manifest.json` 是 format 2、`language: ja`，`lexicon.bin`、`lm.bin`、`lexicon.tsv`、`NOTICE.txt`、`report.txt` 由編譯器產生；`symbols/` 是 T22 的記号與顔文字 TSV。編譯器不寫時間戳，同一份 `data/ja` 連跑兩次得到相同雜湊。

## 發行版整合

解壓原始碼 tarball，在根目錄執行：

```sh
sha256sum -c SHA256SUMS
tar -xJf 0c-ime-<版本>.tar.xz
cd 0c-ime-<版本>
mkdir -p build
go build -trimpath -ldflags '-X main.buildVersion=<版本>' -o build/0c-server ./cmd/0c-server
go build -trimpath -o build/0c-cli ./cmd/0c-cli
build/0c-server -check-data -data assets/data
build/0c-cli -data assets/data
```

Go module 相依須預先備妥，之後可用 `GOPROXY=off GOSUMDB=off` 離線建置；不需要 `.git` 或重新編譯 bigram。平台外殼依各平台 README 建置。

要把外殼（Fcitx5、IBus）、引擎、設定程式與字典整套裝到系統目錄時，用根目錄的 `make build PREFIX=/usr VERSION=<版本>` 與 `make install PREFIX=/usr VERSION=<版本> DESTDIR=<暫存根目錄>`；這種安裝由套件管理員更新，不會自己檢查或下載更新。變數、安裝路徑與建置／執行相依見 [Linux 系統安裝](platforms/linux-system/README.md)。

神經選字與補標點使用模型 tarball 的 `models/select/`、`models/punct/`，可另外打包成選用套件（例如 Debian 的 non-free）；版本與配對檢查見 [模型說明](models/README.md)，worker 與系統 Python、NumPy、ONNX Runtime 的整合方式見 [神經推論](platforms/neural/README.md)。工具鏈與執行相依套件由發行版自行提供。逐檔的著作權人與授權以 DEP-5 格式列在 [legal/copyright](legal/copyright)，可直接作為 `debian/copyright` 的基礎。

## 從原始碼重建字典

內附的 `lexicon.bin`、`lm.bin` 可以從公開內容重建：Git 內的詞庫與詞頻，加上 [0c-ime-data](https://github.com/0c-biggo/0c-ime-data) Release 的四個詞對統計檔（約 307 MB）。

```sh
tar -xJf 0c-ime-bigram-20261019.tar.xz
python3 scripts/build_release_data.py --bigram-dir 0c-ime-bigram-20261019/bigram --out build/release-data
```

輸出的 `build/release-data/data/` 與 `assets/data/` 位元組相同。各版本使用的統計檔版本見 0c-ime-data 的 README。

## 組裝 Release tarball

Git repo 保存 [字典 lock](data/dictionary.lock.json)、[上游模型 lock](models/models.lock.json) 與 [配對 lock](models/pairing.lock.json)；二進位字典由發佈工具加入原始碼 tarball，配對後的模型另外打成模型 tarball。`models/pairing.lock.json` 的 `model_package` 是唯一的日文模型包（CC BY-SA reranker + public punctuation）；模型檔不進 Git，安裝器以 `cmd/0c-model-set` 安裝這一個模型包，`0c-server -model-report` 會回報使用中的版本。

先提交原始碼與固定清單、建立版本 tag，再用已驗證的現成字典組裝。輸出目錄須尚未存在：

```sh
python3 scripts/source_release.py build --tag v<版本> --data-dir /path/to/compiled-data \
  --out build/source-release/<版本>
python3 scripts/source_release.py verify build/source-release/<版本>/0c-ime-<版本>.tar.xz \
  --models build/source-release/<版本>/0c-ime-models-<版本>.tar.xz
(cd build/source-release/<版本> && sha256sum -c SHA256SUMS)
```

工具只讀取 tag 的 Git archive，不納入未提交改動。字典必須符合固定清單與公開來源檢查，並與模型配對清單一致；模型權重依上游 commit、大小與 SHA-256 取得，只依已驗證的配對清單產生綁定 metadata。模型快取完整時可加 `--offline`，不連網組裝。

上一版的原始碼 tarball 可用 `source_release.py extract-data` 取出字典重用。repo variable `DICTIONARY_SOURCE_URL` 設定後，tag 的 CI 會從該網址取回字典、依當前 tag 的 lock 校驗並組裝兩個 tarball，解壓後離線建置與載入字典成功，才一起上傳到 Release 草稿；沒有設定時這個工作會略過。

## Windows 安裝、移除與回復

Windows 套件由 `scripts/package_windows.py` 在 Windows x64 建置，再用 `scripts/package_windows_installer.py` 包成 NSIS 安裝精靈。**產品不內嵌 CPython、numpy 或 onnxruntime Python 套件**；推論使用原生 ONNX Runtime，`onnxruntime.dll` 與 `vcruntime140.dll`、`vcruntime140_1.dll`、`msvcp140.dll`、`msvcp140_1.dll` 放在 `0c-server.exe` 旁。版本、大小與 SHA-256 固定在 [models/runtime.lock.json](models/runtime.lock.json)，由 `scripts/prepare_windows_runtime.py` 從固定的 wheel 取出；打包時逐檔核對，完成品再檢查一次沒有任何 `.py`、`.pyc`、`.pyd`、wheel、numpy 或 onnxruntime Python 套件。授權邊界與第三方聲明見 [授權頁](docs/licensing.md)。

`0c-server` 預設 `-neural-backend auto`：建置含 CGO＋`onnxruntime` tag 且執行檔旁有 `onnxruntime.dll` 時使用 native；DLL 不在或載入失敗時直接退回統計轉換，不會啟動 Python。原生後端支援選字／reranker 與補標點的模型形狀；模型形狀不受支援時回報 `unsupported_model` 並保留統計結果。Python-free 套件的補標點也走 native，只有建置完全沒有原生後端時才退回 Python worker。

完整套件需要外部文件目錄的 `INSTALL.md`、`PRIVACY.md`、`UPDATING.md`：

```powershell
make assets LANG=ja
python scripts\prepare_windows_runtime.py --out build\runtime
python -B scripts\hf_models.py pair --data assets\data --out build\models-pair
python scripts\package_windows.py --data assets\data --ja-data build\ja --ort-runtime build\runtime `
  --model-pair build\models-pair --ja-model --model-source build\models-export `
  --release-docs <文件目錄> --version <版本> --out build\release
python scripts\package_windows_installer.py --archive build\release\<ZIP> --out build\release
```

`--ja-data` 是選用的。有指定時，日文包以 `data/ja/` 放進 ZIP，`release.json` 多了 `ja_data_format` 與 `ja_data_manifest_sha256`；安裝後位於 `%LOCALAPPDATA%\Zuin\versions\<版本>-<雜湊>\data\ja\`。沒有指定時，ZIP 與原本的注音套件相同。日文包會經過 `check_ja_data`：format 2、`language: ja`、manifest 內的檔案雜湊、公開包的來源聲明與 `symbols/*.tsv` 都會檢查。日文資料不經過 `-seal`，因為 `0c-compile -seal` 目前只驗證注音 manifest；ZIP 的逐檔 SHA-256 與安裝收據仍會保護它。

`release.json` 的 `native_runtime` 記錄出貨的 ONNX Runtime／MSVC 版本與五個 DLL 的 SHA-256。`--statistical-only` 只供診斷，不建置原生後端、不出貨 DLL；公開套件必須用 `--ort-runtime`。`--neural` 已移除，指定時直接失敗。

注音 select／punct ONNX 由 `--model-pair` 出貨（`hf_models.py pair` 的輸出；不含 Python worker），打包時核對 `models.lock` 與 `pairing.lock`。原生產品沒有 `--model-pair` 會拒絕出貨。

日文模型包由 `--ja-model` 出貨，讀取 [models/pairing.lock.json](models/pairing.lock.json) 的 `model_package`。產品只有這一個模型包：reranker（CC BY-SA 4.0）加 punctuation（MIT），沒有其他模型包可選。`--model-source` 可指向已下載的 ONNX；省略時依 lock 的 `hf.repository` 與 `hf.revision` 從 Hugging Face 下載到 `--model-cache`，逐檔核對大小與 SHA-256，`--model-offline` 只用快取。`--model-format` 選 `int8`（預設）或 `fp32`。打包時以 `cmd/0c-model-set` 再核對一次並綁定日文資料包 manifest，只放入模型包的兩個 artifacts。`model_package.deployment` 記錄 seq 124、候選每側 60 tokens、只對統計前 3 名 gating，逾時或模型不可用時保留字典序；改模型或 seq 時要一起更新 lock 與 [模型文件](docs/model-sets.md)。`active.json`、`pairing.lock.json` 放在套件的 `models/`。正式 release 要求模型包的 revision 與逐檔雜湊已 pin，缺少任何一個都直接失敗；`go run ./cmd/0c-model-set check -release` 會驗證唯一模型包。placeholder 只允許出現在明確加 `--allow-placeholders` 的 preview 套件，正式 release 一律拒絕。reranker 安裝時一定帶 `MODEL-PACKAGE-CC-BY-SA-4.0.txt` 與 `MODEL-PACKAGE-NOTICES.txt`，punctuation 保持 MIT 並附 UniDic／IPADic notice，授權義務見 [授權頁](docs/licensing.md)。

`0c-update` 安裝時逐檔核對 `release.json`，把整個版本目錄換上去；升級保留上一版，`install.ps1 -Rollback` 或 `0c-update rollback` 回到上一版。`0c-update rollback -restore-user` 額外還原升級前的個人詞庫；`install.ps1` 沒有 `-RestoreUser` 參數。`install.ps1 -Uninstall` 或 `0c-update uninstall` 取消 TSF 註冊並停用收據，**不刪**版本目錄、字典、備份或 `%LOCALAPPDATA%\Zuin\user.json`；要清掉所有本機檔案時，先解除安裝，再自行刪除 `%LOCALAPPDATA%\Zuin`。回復到不含日文包的版本時，使用中的版本目錄沒有 `data/ja/`，不會從別版補上；想移除日文包請換成不含 `--ja-data` 的套件再安裝或回復，不要手動刪除版本目錄裡的檔案，收據核對會失敗。

Linux 與 macOS 套件仍出注音字典與既有模型；日文資料包目前只接入 Windows 套件，等各平台外殼正式載入日文詞典後再擴充。這樣沒有日文包時，注音安裝、更新與回復都不受影響。

## 更新字典

詞庫、編譯參數或 `0c-compile` 的排序改變時，內附字典要重建，兩個 lock 一起更新：

1. 以 `scripts/build_release_data.py --bigram-dir <符合 bigram lock 的統計檔>` 重建字典。
2. 更新 `data/dictionary.lock.json` 的檔案大小與 SHA-256。
3. 更新 `models/pairing.lock.json` 的字典 manifest 雜湊與配對 metadata，再以 `scripts/pair_models.py` 確認配對；ONNX 權重、token 順序與推論設定不變。
4. 以新字典在本機組裝 Release tarball，`DICTIONARY_SOURCE_URL` 改指向新的原始碼 tarball。
