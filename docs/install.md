# 安裝、升級、回復與移除

先選平台。三個平台的套件都不會上傳打字內容；個人詞庫與設定保留原則相同。實際檔案位置與外殼行為以各平台 README 為準。

## Windows

### 安裝

一般使用者雙擊 `0c-<版本>-windows-amd64-setup.exe`，依精靈完成安裝。安裝程式會：

1. 把程式、字典與所選模型放進 `%LOCALAPPDATA%\Zuin\versions\`。
2. 註冊單一的 Mixed TSF profile。
3. 把「靈犀注音」加進目前帳號的中文（台灣）語言清單。
4. 建立登入排程啟動引擎。

個人詞庫在 `%LOCALAPPDATA%\Zuin\user.json`，設定在 `%LOCALAPPDATA%\Zuin\settings.json`。安裝不需要 Python、numpy 或 onnxruntime Python 套件；Windows 產品出貨原生 `onnxruntime.dll` 與 MSVC 執行階段，詳見 [授權頁](licensing.md)。

安裝後如果語言列找不到，到 Windows 設定 → 時間與語言 → 語言與地區 → 中文（台灣）→ 新增鍵盤 →「Mixed」。舊版的「靈犀日本語」ja-JP profile 會在升級時移除，不需要重新加入。

### 升級

直接執行新版的 setup.exe，或讓設定程式的「更新」頁下載並安裝。升級會：

- 保留 `%LOCALAPPDATA%\Zuin\user.json`，並在版本備份另存一份升級前的詞庫。
- 把新版本放進新的版本目錄，保留上一版作為 rollback 目標。
- 重新指向固定的 `Zuin\tsf\0c_tsf.dll`，已開著的應用程式要重新開啟才換新版。
- 通常不需要登出。

從 0.1.0-dev.15 以前的 `zuin-*` 版本升級時，請用新套件內附的 `0c-update.exe`。舊版工具不認得新檔名，會在變更註冊前拒絕。

### 回復上一版

在解壓後的套件目錄，以同一位桌面使用者的系統管理員 PowerShell 執行：

```powershell
.\install.ps1 -Rollback
```

或直接使用套件內的更新器：

```powershell
.\0c-update.exe rollback
```

升級前的個人詞庫預設保留。要一併還原升級前保存的詞庫，使用套件內的更新器；`install.ps1` 沒有 `-RestoreUser` 參數：

```powershell
.\0c-update.exe rollback -restore-user
```

回復前使用的詞庫會另存成新備份，可以再 rollback 撤回。回復後已經開著的應用程式要重新開啟；如果舊版讀不懂新版詞庫格式，引擎會照常輸入但不存檔，直到換回新版或執行 `-restore-user`。

### 移除

一般移除：Windows 設定 → 應用程式 → 已安裝的應用程式 → 靈犀注音 → 解除安裝。

命令列：

```powershell
.\install.ps1 -Uninstall
# 或
.\0c-update.exe uninstall
```

解除安裝會取消 TSF 註冊、移除登入排程並停用安裝收據。**不會刪除**版本目錄、字典、備份或 `%LOCALAPPDATA%\Zuin\user.json`。要清掉所有本機檔案時，先解除安裝，再自行刪除 `%LOCALAPPDATA%\Zuin`。

### 驗收狀態

Windows 舊版安裝、升級、回復流程曾在 Windows 11 x64 實機驗收。Phase 2 的單一 Mixed profile、逐片段候選與長詞候選，VM 驗收是 **NOT RUN**；`p2-mixed` harness 與 Linux 端自我測試存在，但不能當成 Windows 實機通過。

## macOS

### 安裝

一般使用者雙擊 `.pkg`，依畫面安裝。套件只裝到目前帳號，不需要系統管理員密碼。安裝器會把 `0c.app` 放到 `~/Library/Input Methods/`，第一次安裝需要在系統設定允許第三方輸入來源；從沒有模式的舊版升級時，系統可能要求重新登入。

終端機安裝：

```sh
shasum -a 256 -c 0c-<版本>-macos-arm64-preview.tar.gz.sha256
tar -xzf 0c-<版本>-macos-arm64-preview.tar.gz
cd 0c-<版本>-macos-arm64-preview
./0c-update install -bundle . -channel internal-preview
```

個人資料在 `~/Library/Application Support/0c/`：

- `user.json`：個人學習。
- `settings.json`：設定。
- `install-receipt.json`、`backups/`：安裝收據與版本備份。

### 升級、回復與移除

```sh
./0c-update rollback     # 換回上一個 App，目前的 App 移入 backups/
./0c-update uninstall    # App 移入 backups/；個人詞庫、設定與備份保留
```

回復後可以再 rollback 撤回。移除後如果還要清乾淨，自行刪除 `~/Library/Application Support/0c`。

### 驗收狀態

macOS 的安裝與更新流程有自己的驗收紀錄；Phase 2 的單一 Mixed profile 與逐片段候選是 Windows 外殼功能，macOS 沒有對應的 Phase 2 實機數字。

## Linux

Linux 有兩種安裝方式，更新責任不同：

| 方式 | 更新 | 移除 |
|---|---|---|
| 官網下載的家目錄安裝 | 設定程式的「更新」頁或 `0c-update` | Fcitx5 用 `make -C platforms/linux-fcitx5 uninstall`，或 `0c-update uninstall`；IBus 另用 `0c-ibus unregister` |
| 發行版系統套件 | 系統套件管理員 | 系統套件管理員 |

系統安裝由 `make install PREFIX=/usr` 建置與安裝，更新與移除都交給套件管理員；`0c-update` 在這個模式會拒絕 `install`、`rollback`、`uninstall` 等命令，回報 `package_managed`。詳細變數、路徑與執行相依見 [Linux 系統安裝](../platforms/linux-system/README.md)。

原始碼的 Fcitx5 家目錄安裝：

```sh
make -C platforms/linux-fcitx5 test
make -C platforms/linux-fcitx5
make -C platforms/linux-fcitx5 install
make upgrade       # 已安裝後從原始碼更新
make -C platforms/linux-fcitx5 uninstall
```

根目錄的 `make install PREFIX=/usr`／`make uninstall` 是系統安裝，不是家目錄 Fcitx5 的安裝流程。

IBus 外殼另外登錄與取消登錄：

```sh
~/.local/bin/0c-ibus register
~/.local/bin/0c-ibus unregister
```

Linux 的個人資料在 `~/.local/share/zuin/user.json`，設定在 `~/.local/share/0c/settings.json`。神經模型在系統安裝時由設定程式下載到 `~/.local/share/zuin/models/`，需要系統的 `python3`、`python3-numpy`、`python3-onnxruntime`。這與 Windows 的 Python-free 套件不同。

### 驗收狀態

Linux 的 Fcitx5 與 IBus 有各自的容器或桌面驗收，範圍見各平台 README。Phase 2 的單一 Mixed profile 只在 Windows 外殼；Linux 仍依設定檔的中英混打開關，沒有「Windows Mixed 驗收已通過」這種說法。

## 共通原則

- 升級保留個人詞庫與設定；版本備份只用於 rollback。
- 移除不刪個人詞庫。要清乾淨時先移除，再手動刪個人資料目錄。
- 神經模型缺少、形狀不支援或載入失敗時，引擎退回統計轉換，打字不會卡住。
- 沒有量測過的平台與流程一律標成未驗證，不從其他平台推論通過。
