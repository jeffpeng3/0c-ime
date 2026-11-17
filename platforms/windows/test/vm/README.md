# P2-44 Windows VM 驗收 harness

這組工具從 Linux（或任何有 `ssh`／`scp` 的機器）操作一台 Windows 10／11 x64 VM：
上傳目前建置的 Windows bundle、在已登入的互動桌面安裝測試版本、跑 Phase 2 Mixed
profile 驗收與既有宿主 suite、把日誌與截圖抓回來。它只回報 product bug，不改
`core/`、`platforms/windows/tsf/`、`platforms/windows/ui/` 或 `cmd/`。

## 這台機器（Linux）的狀態

本 worktree 沒有可連線的 Windows VM，因此 live run 標為 **NOT RUN**；可重現的
self-test、注音 golden 與 dry-run 已在本機跑過。完整紀錄見 [LIVE-RUN.md](LIVE-RUN.md)。

## 需求

- VM：Windows 10 1803 以上或 Windows 11，x64；Windows PowerShell 5.1；系統管理員帳號。
- VM 必須**已登入且未鎖屏**。`sshd` 以該帳號金鑰登入；SSH 工作階段本身不是互動桌面，
  harness 會用 `Register-ScheduledTask -LogonType Interactive -RunLevel Highest`
  把驗收放進目前使用者的桌面工作階段。
- 受測 bundle：`0c-<版本>-windows-amd64-preview.zip` 或 public-beta zip，內含
  `release.json`、`0c-update.exe`、x64／x86 DLL、`0c-server.exe`、`0c-ui.exe`、資料與模型。
- VM 上不要有日常的 `0c-server`／`0c-ui` 在跑；harness 不會替你砍掉它們，preflight 會
  直接報錯並列出 PID。測試安裝由 `acceptance-test.ps1` 負責，結束時還原 TSF 註冊、
  收據、`user.json`、字典 manifest、語言清單與固定路徑 `Zuin\tsf`。
- 目標宿主：記事本、Windows Terminal、Edge；WebView2 runtime 與 Windows 11 搜尋框
  缺少時記為 skipped，不算失敗。32 位元宿主需要 bundle 的 `x86\0c_tsf.dll`。
- 第一次跑 WebView2 suite 需要網路從 NuGet 下載固定版本 SDK；離線時該 suite skipped。

## 快速開始

```sh
# 1) 不需要 VM 的自我測試（Linux 可跑；含注音 golden）
python3 platforms/windows/test/vm/p2vm.py selftest

# 2) 只印出會執行的命令，不連 VM
python3 platforms/windows/test/vm/p2vm.py plan \
  --host 192.0.2.10 --user tester --key ~/.ssh/0c-win-vm \
  --bundle /path/to/0c-0.3.0-windows-amd64-preview.zip

# 3) 只做 VM 能力檢查：SSH、互動桌面、OS、宿主、磁碟、日常引擎
python3 platforms/windows/test/vm/p2vm.py preflight \
  --host 192.0.2.10 --user tester --key ~/.ssh/0c-win-vm

# 4) 完整驗收；結果在 build/windows/validation/acceptance-*
python3 platforms/windows/test/vm/p2vm.py run \
  --host 192.0.2.10 --user tester --key ~/.ssh/0c-win-vm \
  --bundle /path/to/0c-0.3.0-windows-amd64-preview.zip
```

主機、使用者與金鑰也可以用環境變數 `P2VM_HOST`、`P2VM_USER`、`P2VM_KEY`、
`P2VM_BUNDLE`。harness 不會把金鑰寫進報告；命令紀錄中的金鑰路徑會換成
`<ssh-key>`。

常用的 `run` 選項：

| 選項 | 作用 |
|---|---|
| `--suite p2-mixed,wpf,notepad,...` | 只跑指定 suite；預設就是 Phase 2 七類目標宿主加 `p2-mixed` |
| `--result-root DIR` | 本機結果目錄，預設 `build/windows/validation` |
| `--remote-root C:\0c-p2vm` | VM 上的工作目錄；每次開始前清空，不能有空白 |
| `--timeout-minutes N` | acceptance 總逾時，預設 150 分鐘 |
| `--keep-remote` | 保留 VM 上的 `C:\0c-p2vm`（預設抓回結果後刪除） |
| `--skip-local-golden` | 不跑本機注音 golden（不建議） |
| `--port`／`--ssh`／`--scp`／`--ssh-option` | SSH 連線細節 |

## 流程

1. **本機檢查**：`git rev-parse HEAD`、bundle SHA-256／`release.json`、`go run ./cmd/0c-golden`。
2. **SSH preflight**：`ssh` 連線與身分。
3. **上傳**：整個 `platforms/windows/test/` 與 bundle 到 `C:\0c-p2vm`。每次先刪掉
   harness 自己的工作目錄，避免上次的腳本或 bundle 被重用。
4. **互動工作**：`Invoke-Interactive.ps1` 建立具名排程工作，在目前登入者桌面以提升權限
   執行 `Remote-P2Acceptance.ps1`。它會：
   - 檢查 OS、build、x64、管理員、WTS session 是否 Active、explorer、磁碟、宿主；
   - 記錄 `environment.json`（版本、宿主版本、WebView2 runtime、搜尋框可用性）；
   - 驗 bundle SHA-256、解壓、找 `release.json`；
   - 執行 `acceptance-test.ps1 -BundleDir ... -Suite ...`；
   - Windows 10 自動略過 `search`（Windows 11 才有 SearchHost 沙箱）；
   - 失敗時擷取桌面截圖 `desktop-failure.png`。
5. **抓回**：`logs/`、`validation/`、suite 的 PNG 與 `tsf.log` 尾端。
6. **判讀**：Python 分析 `p2-mixed-result.json` 的原始 trace，產生 `summary.json`、
   `summary.md`；有失敗時產生 `bug-report.md`。

## 自動檢查

`p2-mixed` suite 由 `platforms/windows/test/p2-mixed-test.ps1` 執行。它先連上
`acceptance-test.ps1` 已啟動的隔離引擎（預設 `\\.\pipe\0c-<SID>`），把每個請求與
完整快照寫進 trace，再用 WPF 宿主做兩項實機檢查。最後的判定在 Linux 的
`p2vm/checks.py`，因此失敗的 trace 可以重播、也可以在 Linux 上自我測試。

| 檢查 ID | 驗什麼 | 判定方式 |
|---|---|---|
| `profile.registered_single_mixed` | 只註冊一個 Mixed TSF profile | 列舉 `HKLM\SOFTWARE\Microsoft\CTF\TIP\<CLSID>\LanguageProfile`，必須只有一個 profile，GUID 是 `EF62F743-...`，顯示名稱含 `Mixed` |
| `profile.no_legacy_ja` | 沒有殘留 ja-JP profile | 不得出現舊 GUID `3D6A8B21-...` |
| `profile.language_list_single_tip` | 語言清單只有一個靈犀 TIP | `Get-WinUserLanguageList` 的 `InputMethodTips` 只能有一個含本 CLSID，且不能是 ja-JP |
| `candidate.no_language_labels` | 候選 UI 輸出沒有語言標籤 | 每個快照的候選 `label` 只能是空、`common`、`related`、`personal`；`note` 不能是「中文／日文／日本語／注音／英文」；`title`／`suggestion`／`notice` 不能有 `〔日文〕` 這類標記。候選**文字**本身不列入（「日文」可以是正常詞） |
| `candidate.segment_switch` | 游標移動換片段 | 打 `麻婆豆腐` 後把游標移到 0：候選 `start=0,length=4`；移到 2：`start=2,length=2`（豆腐），且兩次都有候選 |
| `candidate.long_word_one` | 長詞 match 是一項候選 | 游標在 0 時，候選清單中 `麻婆豆腐` 恰好出現一次，`length=4` |
| `input.english_direct` | Mixed profile 直接打英文 | 送 `hello`＋Enter，commit 必須是 `hello`，且 session 沒有切到 `english` 模式 |
| `input.zhuyin_commit` | 注音外殼路徑沒退化 | 送 `su3cl3`＋Enter，commit 是 `你好` |
| `input.segment_fixture` | 混合組字 fixture | `麻婆豆腐` 的組字文字正確；資料包缺少 `麻婆豆腐` 時會明確失敗 |
| `app.wpf_english_direct` | WPF 直接打英文 | WPF TextBox 不打 Shift、不切模式，輸入 `hello` 後內容是 `hello` |
| `app.wpf_candidate_visible` | 候選窗真的出現 | WPF 宿主按下候選鍵後找到可見的 `ZuinCandidateWindow`，截圖 `p2-wpf-candidate.png` |
| `app.wpf_segment_switch_visible` | P2-41 外殼換片段 | 在 WPF 打 `麻婆豆腐`，沒開候選時按 Home／End；外殼要自動為新片段重開候選。兩次只擷取候選窗，PNG 雜湊必須不同；截圖 `p2-wpf-segment-start.png`／`-end.png` |
| `local.zhuyin_golden` | 注音 golden 不變 | 本機 `go run ./cmd/0c-golden`，`testdata/golden/zh` 的 45 個案例 |

### 目標宿主

| 需求 | suite | 驗收內容 |
|---|---|---|
| WPF | `wpf`（`smoke-test.ps1`） | 組字、提交、候選、混打、密碼欄位、DLL 版本 |
| 記事本 | `notepad` | 存檔內容、取消、滑鼠選字、Shift、快速連打 |
| Windows Terminal | `terminal` | 專用視窗、提交不送 Enter、候選、混打 |
| Edge | `browser` | 專用設定目錄、本機頁面、候選、密碼框 |
| WebView2 | `webview2` | WinForms＋WebView2、跨 `msedgewebview2.exe` 候選窗 |
| 搜尋框 | `search` | Windows 11 SearchHost 沙箱、數字選字；Windows 10 記 skipped |
| 32 位元 | `wpf32`、`native32` | SysWOW64 PowerShell 載入 `x86\0c_tsf.dll`、32 位元登錄視圖 |

每個 suite 各自的結果 JSON、`tsf.log`、`server.err` 與截圖都留在
`validation/acceptance-*/<suite>/`。suite 失敗時 `acceptance-test.ps1` 會停止後續
suite，因此後面的宿主會是 `NOT RUN`，不是被誤判成產品失敗。

## 結果目錄

```text
build/windows/validation/acceptance-<時間>-<bundle-sha8>/
  summary.json                 # 機器可讀：checks、suites、failures、環境、bundle
  summary.md                   # 人看的總表與重跑命令
  bug-report.md                # 只有失敗時：失敗檢查、trace、日誌尾端、截圖、重跑命令
  logs/
    01-local-golden.log
    02-ssh-probe.log
    ...                        # 每個 ssh／scp 的完整命令、exit code、stdout/stderr
    remote/
      environment.json         # VM OS、build、宿主版本、session 狀態
      remote-summary.json      # 遠端流程、acceptance exit、suite 清單
      remote.log
      acceptance.out.log/.err.log
      desktop-failure.png      # 失敗時的桌面截圖
  validation/
    acceptance-<時間>/
      acceptance-result.json
      acceptance-summary.md
      p2-mixed/p2-mixed-result.json      # 原始 trace、profile、WPF 檢查
      p2-mixed/p2-wpf-english.png
      p2-mixed/p2-wpf-candidate.png
      p2-mixed/p2-wpf-segment-start.png
      p2-mixed/p2-wpf-segment-end.png
      wpf/desktop-result.json、*.png
      notepad/...
      terminal/...
      browser/...
      webview2/...
      search/...
      wpf32/...、native32/...
```

`summary.json` 的 `live_run.status` 只會是 `PASS`、`FAIL` 或 `NOT RUN`。
`NOT RUN` 表示 harness 到不了 VM 或互動工作啟動不了，不是產品結論。

## 失敗回報

`bug-report.md` 會帶：

- commit、bundle SHA-256／版本／channel；
- VM 的 Windows 版本、build、架構、session 狀態、宿主版本；
- 每個失敗檢查的 ID、detail 與 trace step 證據；
- 失敗 suite 的 `error`；
- `tsf.log`、`server.err`、acceptance stdout/stderr 的尾端；
- 截圖清單（suite 自己的 PNG、失敗桌面）；
- 同一條重跑命令（金鑰路徑已替換）。

把整個結果目錄附到 issue 即可。harness 只收集與判定，不會修改 product code。

## 冪等與安全

- 每次 `run` 使用新的本機結果目錄，不覆蓋舊結果。
- VM 上的 `C:\0c-p2vm` 在每次開始前由 harness 刪除重建；只碰這個目錄。
- 排程工作名稱固定為 `0c-P2VM-Acceptance`，開始前會先移除同名舊工作。
- 不停止、不刪除日常引擎或候選窗；preflight 發現它們在跑就報錯並列出 PID。
- `acceptance-test.ps1` 自己負責測試安裝與還原 TSF 註冊、收據、`user.json`、字典
  manifest、語言清單、`InputMethodOverride` 與 `input-list` 記號。
- 不 merge、不 push；不在 CI 自動執行。

## 已知限制

- 這台 Linux 機器沒有可用的 Windows VM，live run 本次是 `NOT RUN`。
- 候選「無語言標籤」是掃描實際送給 UI 的欄位（label／note／title／suggestion／
  notice）與快照；沒有做 OCR。UI 若自行畫出語言字樣，仍需人眼看截圖；截圖會保留。
- 長詞檢查依賴隨附資料包有 `麻婆豆腐`。資料包缺詞時會失敗並附完整候選清單，這是
  資料或產品問題，不是 harness 自行跳過。
- 搜尋框只在 Windows 11 跑；Windows 10 記 skipped。WebView2 沒有 runtime 或離線
  下載不到 SDK 時 skipped。
- Win＋Space、語言列名稱、候選窗像素主題、多螢幕／高 DPI 仍要人工看；本 harness
  只自動化可重複的協定與宿主文字檢查。
- 32 位元 suite 需要 x64 VM 的 SysWOW64 與 bundle 的 x86 DLL；ARM64 不在範圍。

## 開發與自我測試

```sh
python3 platforms/windows/test/vm/p2vm.py selftest     # 46 個 Python 測試＋注音 golden
python3 -m unittest discover -s platforms/windows/test/vm/tests -v
```

自我測試涵蓋：profile 數量／舊 ja、候選語言標籤、逐片段 range、長詞單一候選、
英文直接輸入、zhuyin commit、suite skip／NOT RUN、結果報告與金鑰遮蔽、PowerShell
靜態契約與 `p2-mixed` 接線。沒有 PowerShell 的 Linux 也能跑。

靜態檢查另外守住：

- PowerShell 5.1 需要 UTF-8 BOM 的中文 `.ps1`；
- trace step 名稱必須和 Python 判定一致；
- harness 不寫入 `core/`、`platforms/windows/tsf|ui|installer`、`cmd/`；
- repo 不 bake 主機、使用者或金鑰。

## 檔案

| 檔案 | 位置 | 用途 |
|---|---|---|
| `p2vm.py` | 本目錄 | CLI 入口 |
| `p2vm/` | 本目錄 | 參數、SSH／SCP、trace 判定、報告 |
| `tests/` | 本目錄 | Linux 自我測試 |
| `Invoke-Interactive.ps1` | VM | 把腳本放進互動桌面執行 |
| `Remote-P2Acceptance.ps1` | VM | preflight、解壓 bundle、跑 acceptance、收集 |
| `Capture-Desktop.ps1` | VM | 失敗桌面截圖 |
| `../p2-mixed-test.ps1` | VM | Phase 2 協定 trace 與 WPF 宿主檢查 |
| `../acceptance-test.ps1` | VM | 安裝、逐 suite 排程、還原 |
