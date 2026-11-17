# Windows 日文驗收（T29）

這頁說明日文 TSF 驗收怎麼跑、哪些由 CI 自動測、哪些一定要在互動桌面手動看。腳本只操作自己的測試視窗、暫存引擎與專用瀏覽器設定目錄；不讀寫日常 `user.json`。

## Phase 2 Mixed profile VM harness（P2-44）

`vm/` 是從 Linux 以 SSH／SCP 操作 Windows 10/11 x64 VM 的 harness：上傳目前 bundle、在已登入的互動桌面安裝、跑 `acceptance-test.ps1` 與 Phase 2 的 `p2-mixed` suite，抓回日誌與截圖。`p2-mixed-test.ps1` 檢查單一 Mixed profile、候選無語言標籤、游標換片段、長詞單一候選、英文直接輸入與注音 smoke；WPF／記事本／Terminal／Edge／WebView2／搜尋框／32 位元沿用既有 suite。Linux 端有 46 個自我測試，不需要 Windows。用法、結果格式與限制見 [vm/README.md](vm/README.md)，本機執行紀錄見 [vm/LIVE-RUN.md](vm/LIVE-RUN.md)。

```sh
python3 platforms/windows/test/vm/p2vm.py selftest
python3 platforms/windows/test/vm/p2vm.py run --host <VM> --user <admin> --key <key> --bundle <windows-x64.zip>
```

## CI 與手動驗收的邊界

CI（T05 的 `windows-2022` workflow）只跑功能性的部分：

- `go test ./...`、`go vet ./...`、`go build ./...`
- Rust 的 `cargo build`／`cargo test`（含 T15 按鍵協定、T26 文節樣式與能力旗標、T28 profile 註冊的單元測試）
- x64 與 x86（SysWOW64）DLL 編譯、`0c-ui.exe`、`0c-server.exe` 建置

CI **不**註冊 TSF、不開 WPF／記事本／Terminal／Edge／WebView2／搜尋框，也不做像素、語言列或 Win+Space 檢查。下面每一支 `ja-*.ps1` 都需要已登入的互動桌面與實際宿主；`acceptance-test.ps1` 會用排程工作在目前使用者的工作階段啟動它們。

## 腳本

| 腳本 | 宿主 | 自動檢查 | 需要 |
|---|---|---|---|
| `ja-smoke-test.ps1` | WPF、記事本、Windows Terminal | 羅馬字→かな、Space 変換、Enter、Escape 回かな、Tab／Shift+Tab、変換／無変換、F6–F8、F9／F10 有送進引擎且不先送出、三個宿主都送出日文 | 互動桌面、系統管理員（由總腳本安裝測試 bundle） |
| `ja-candidate-test.ps1` | WPF | 変換後候選窗、文節底線／目前文節移動（Tab／Shift+Tab 前後組字區畫面改變）、Space 換候選、PageUp／PageDown 不送出、滑鼠點第二列選字、送出後關窗 | 互動桌面 |
| `ja-profile-test.ps1` | WPF、記事本 | zh-TW／ja-JP profile 已註冊、語言清單有兩個 TIP、Win+Space 切換、每個程式記住自己的語言、新程式從系統設定的語言開始 | 互動桌面；Win+Space 熱鍵不可被關閉 |
| `ja-private-test.ps1` | WPF、原生 EDIT | 密碼與 PIN 欄位原樣收鍵、不開候選、不送引擎；`IS_PRIVATE` 欄位可組字／変換但不讀前文、不學習 | 互動桌面；等引擎寫入 `user.json`（約 12 秒 ×2） |
| `ja-sandbox-test.ps1` | Edge、Windows 搜尋框、WebView2 | Chromium 宿主組字／変換／送出、候選窗在輸入框附近；SearchHost 沙箱內組字與數字選字；WebView2 跨 `msedgewebview2.exe` 行程的候選窗與送出 | Windows 11（搜尋框）、Edge、WebView2 執行階段；第一次跑 WebView2 需要網路下載 SDK |
| `ja-32bit-test.ps1` | SysWOW64 Windows PowerShell | x86 行程載入 `x86\0c_tsf.dll`、DLL 是 i386、日文組字／変換／送出 | 由 `acceptance-test.ps1` 以 32 位元 PowerShell 啟動 |

`ja-common.ps1` 是共用工具：從登錄查日文 profile GUID、用 `SendInput` 送按鍵、找候選窗、算畫面雜湊。它不是獨立測試。

## 執行

先關掉日常的 `0c-server`／`0c-ui`，用一個 Windows x64 preview 或 public-beta bundle，從**目前登入的桌面使用者**開提升權限的 Windows PowerShell：

```powershell
cd <repo>\platforms\windows\test
.\acceptance-test.ps1 -BundleDir C:\path\to\bundle `
  -Suite ja-smoke,ja-candidate,ja-profile,ja-private,ja-sandbox,ja-32bit
```

只跑其中一項也可以，例如 `-Suite ja-smoke`。`-ResultDir` 不指定時，全部都是 `ja-*` 的組合會落在 `build\windows\validation\ja-<時間>-<亂數>`；混合注音與日文則用原本的 `acceptance-*`。總腳本會：

1. 驗 bundle、安裝測試版本（`ZC_NO_SCHEDULE=1`，不建立自動更新排程）。
2. 每個 suite 用自己的目錄與暫存 `test-user.json` 啟動隔離引擎，再用排程工作以目前使用者的互動工作階段執行腳本。
3. 最後還原 TSF 註冊（x64／x86）、install receipt、`user.json`、既有 `data\manifest.json`、固定路徑 `Zuin\tsf`、測試安裝新增的語言清單 TIP、`InputMethodOverride` 與 `update\input-list` 記號。

不要直接對日常引擎執行 `ja-*.ps1`；它們要求 `-ResultDir` 與 `-ExpectedDLL`，沒有隔離引擎時會失敗而不是退回日常資料。

## 從 WSL 跑非互動檢查（T29b）

上面整段需要互動桌面與系統管理員；`run-from-wsl.ps1` 只跑不需要桌面的前置檢查，讓 Linux／WSL 這邊用一條指令確認腳本可剖析、`Add-Type` interop 可編譯、profile 註冊狀態與主機能力。它**不**安裝任何東西、**不**寫登錄、**不**註冊 TSF、**不**建立排程工作，也不需要管理員。

```bash
cd <repo>/platforms/windows/test
/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe \
  -NoProfile -NonInteractive -ExecutionPolicy Bypass \
  -File "$(wslpath -w run-from-wsl.ps1)"
```

預設寫到 `/mnt/e/freerun/0c-wincheck`（Windows 路徑 `E:\freerun\0c-wincheck`），可用 `-ResultRoot` 改位置；路徑可以給 `/mnt/e/...` 或 `E:\...`。加上 `-RepoRevision "$(git rev-parse HEAD)"` 會把 commit 記進 summary。

腳本會：

1. 把 `ja-*.ps1`、`acceptance-test.ps1` 複製到 `<ResultRoot>\scripts`，並記下每個檔案的 SHA-256。`verify-profiles.ps1` 另外複製成 UTF-8 BOM 版本：Windows PowerShell 5.1 在非 UTF-8 ACP 的機器上會把沒有 BOM 的中文 `.ps1` 當成 ANSI 而剖析失敗，來源檔不動。
2. 以 Windows PowerShell 5.1 逐項執行並各自記錄 stdout、stderr、exit code：
   - **parser**：用 `Parser::ParseFile` 剖析所有 `ja-*.ps1`、`acceptance-test.ps1` 與本腳本；失敗時列出 `檔案:行:欄: 訊息`。
   - **interop**：dot-source 複本 `ja-common.ps1`、呼叫 `Initialize-JaProbe`，再查前景視窗與視窗類別，證明 `Add-Type` 在 5.1 可編譯；不送按鍵。
   - **profiles**：執行 `verify-profiles.ps1`。兩個 profile 都註冊為 `pass`；0C 未安裝且兩者都未註冊為 `info`（不算失敗）；已安裝卻缺 profile 為 `fail`。
   - **capabilities**：是否為管理員、0C 是否已安裝、Windows 版本，以及 Go、Rust、MSBuild、signtool 是否在 PATH 或標準路徑；另外記錄 dotnet 與 git。
   - **artifact**：若 `<ResultRoot>\artifact`（或 `-ArtifactDir`）有 `0c-server.exe`，找它旁邊的 `data`（需有 `manifest.json` 或 `lexicon.tsv`）並執行 `0c-server.exe -check-data -data <data>`；沒有資料包時只執行 `-h` 確認執行檔可啟動，記為 `skip`。也可用 `-DataDir` 指定測試資料包。這一步不註冊 TSF、不寫 HKLM、不碰個人詞庫。
3. 寫出 `summary.json`、`summary.md` 與 `run.log`。

CI 有 Windows binary artifact 時，可先在 Linux 下載，runner 會自動使用 `E:\freerun\0c-wincheck\artifact`：

```bash
gh run download <run-id> -n windows-binaries -D /mnt/e/freerun/0c-wincheck/artifact
```

artifact 沒附資料包時，可用 repo 的 toy 資料包確認 `-check-data` 路徑：

```bash
... -File "$(wslpath -w run-from-wsl.ps1)" \
  -DataDir "$(wslpath -w ../../../testdata/toy)"
```

結果格式：

```text
E:\freerun\0c-wincheck\
  scripts\                          ja-*.ps1、acceptance-test.ps1、verify-profiles.ps1、manifest.json
  results\20261117-140000-ab12cd34\
    summary.json
    summary.md
    run.log
    checks\parser\                  stdout.txt、stderr.txt、exit-code.txt、command.txt、result.json
    checks\interop\                 同上
    checks\profiles\                同上，另有 profiles.json
    checks\capabilities\            同上
    checks\artifact\                同上，另有 check-data.* 或 exe-probe.*
```

結束碼：`0` 表示沒有 `fail`（`info`／`skip` 不算失敗）、`1` 有檢查失敗、`2` 參數或環境錯誤、`3` runner 本身例外。互動桌面與 TSF 註冊仍照上面「執行」一節由使用者手動驗收；T28 的 Win＋Space、rollback／uninstall 檢查也仍要人工執行。

## 結果

每個 suite 一個目錄，例如：

```text
build\windows\validation\ja-20261117-143000-ab12cd34\
  acceptance-result.json
  acceptance-summary.md
  original-language-list.json
  ja-smoke\ja-smoke-result.json
  ja-smoke\wpf-ja.png
  ja-candidate\ja-candidate-result.json
  ja-candidate\candidate-clause-1.png
  ja-candidate\candidate-clause-2.png
  ja-sandbox\ja-edge-candidate.png
  ja-sandbox\ja-search-candidate.png
  ja-sandbox\ja-webview2-candidate.png
```

`*-result.json` 的 `checks` 是通過的項目，`error` 是第一個失敗原因；`acceptance-summary.md` 是所有 suite 的總表與還原結果。腳本失敗時 `acceptance-test.ps1` 回傳非零結束碼，排程工作的 `LastTaskResult` 也會是失敗碼。

## 回報給管理中心

把整個 `ja-*` 或 `acceptance-*` 結果目錄壓縮後附上，至少要有：

1. `acceptance-result.json`、`acceptance-summary.md`。
2. 失敗 suite 的 `*-result.json`（含 `error`）。
3. 該 suite 目錄的 `Zuin\tsf.log` 尾端相關行；需要按鍵細節時才附 `tsf-debug`，用完刪除。日誌不含輸入文字。
4. 截圖：`wpf-ja.png`、候選窗、Edge／搜尋框／WebView2 的候選畫面。
5. 環境：Windows 版本（`winver`）、bundle 版本、宿主版本（記事本／Terminal／Edge／WebView2 runtime）、32／64 位元、輸入方式（romaji／kana）、能力旗標 `japanese`／`convert_keys`／`clause_display`。

只記按鍵名稱與結果，不要把密碼、PIN 或私人欄位的內容貼進回報。T29 只負責把問題變成可重複的腳本；TSF 或引擎的行為問題依 `error` 回報給 T26、T20 或 T28，不在這裡修 production 程式。

## 已知限制

- 文節底線粗細用組字區畫面雜湊判斷「有移動」；實際粗細與配色仍要人眼看截圖。
- 候選排序依日文資料包與學習狀態；腳本不寫死「日本語」這類候選字，只要求選到的結果是日文且與かな讀音不同。
- F9／F10 的全角／半角英數表記由核心（T20／T22）決定；這裡只確認按鍵有送進引擎、組字沒被提前送出。
- Win+Space 若被系統設定關閉、或語言清單有其他 IME，`ja-profile-test.ps1` 會按最多 8 次並在到不了時失敗，訊息會提示熱鍵可能被停用。
- WebView2 SDK 下載失敗或沒安裝執行階段時，`ja-sandbox-test.ps1` 記為 skipped，不算失敗；Windows 10 沒有 SearchHost 沙箱資料夾時搜尋框也記為 skipped，Edge 與 WebView2 仍會跑。
