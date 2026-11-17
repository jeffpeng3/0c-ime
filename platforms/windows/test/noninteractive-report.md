# T41 非互動驗證總巡檢報告

- 日期：2026-11-17（Asia/Taipei）
- Commit：`2b5e4265becad736cc027037c9e10ee1acead7f8`
- 遠端 `origin/main` 同一 commit
- 執行分支：`ja/T41-noninteractive-sweep`，worktree `/home/fedora/0c-wt-T41`
- Linux：WSL2 `6.18.58.1-microsoft-standard-WSL2`，`go1.27.3`，Python 3.15.0
- Windows 主機：`JEFFPC`，Windows 10 Pro 26H2（build 26300.9711），Windows PowerShell 5.1.26100.9711，非管理員

## 總結

| 區域 | 結果 |
|---|---|
| Linux 非互動矩陣 | 12／12 指令 exit 0 |
| Windows CI | run `44013257956` success，指定步驟全部 success |
| T29b Windows runner | 4 pass、1 info、0 fail、0 skip（exit 0） |
| 需要人工互動 | 6 個 `ja-*` 桌面 suite，見文末清單 |

沒有失敗需要修。這次只新增本報告，production code 未修改。

## 1. Linux 非互動矩陣

全部在 commit `2b5e426` 的乾淨 worktree 執行。逐項指令與結果：

| 檢查 | 指令 | exit | 秒 | 證據 |
|---|---|---|---|---|
| Go tests | `go test ./...` | 0 | 173 | 所有 package `ok`，含 `cmd/0c-server`（90.7s）與 `internal/*` |
| Go vet | `go vet ./...` | 0 | 2 | 無輸出 |
| 注音 golden | `go run ./cmd/0c-golden` | 0 | <1 | `注音 baseline 一致（45 個案例）` |
| 日文 lexicon | `go test -tags ja_lexicon ./internal/compile` | 0 | 6 | `ok ... internal/compile 5.696s` |
| 日文 eval gate | `go run ./cmd/0c-eval-ja -gate` | 0 | 1 | `gate: 沒有品質退步`，4 個 corpus 均無 timeout |
| Fcitx5 shell | `make -C platforms/linux-fcitx5 test` | 0 | 50 | C++ 單元測試 `PASS` |
| IBus | `go test ./cmd/0c-ibus` | 0 | <1 | 快取通過；另以 `go test -count=1 ./cmd/0c-ibus` 重跑，101 tests passed |
| Release tests | `make test-release` | 0 | 12 | `test_release.py` 52 tests OK，`l3_result_test.py` OK，source-release 子項 OK |
| Source release | `make test-source-release` | 0 | 5 | `check_public_tree.py`、`test_source_release.py`、`test_copyright.py`（4 tests OK） |
| Upgrade tests | `make test-upgrade` | 0 | 1 | `test_upgrade.py` 34 tests OK |
| Public tree | `python3 scripts/check_public_tree.py` | 0 | <1 | `Public source paths verified.` |
| Windows cross build | `GOOS=windows GOARCH=amd64 go build ./...` | 0 | 4 | 無編譯錯誤 |

## 2. Windows CI

- Workflow：`Windows`
- Run ID：`44013257956`
- URL：https://github.com/jeffpeng3/0c-ime/actions/runs/44013257956
- Job：`windows`（job ID `133805022868`）
- Head SHA：`2b5e4265becad736cc027037c9e10ee1acead7f8`，與本次巡檢 commit 相同
- 結論：`success`，2026-11-17T03:52:49Z 建立，04:01:28Z 完成
- `main` 上沒有更新的 Windows run

必要步驟：

| 步驟 | 結論 |
|---|---|
| Test Go code | success |
| Test Rust | success |
| Build Rust x64（x64 `0c_tsf.dll`） | success |
| Build the 32-bit TSF DLL（x86 `0c_tsf.dll`） | success |
| Verify build outputs | success |

Artifact：

- `windows-binaries`：ID `14298057387`
- `windows-logs`：ID `14297683207`
- 已下載 `windows-binaries` 到 `/mnt/e/freerun/0c-wincheck/artifact`，取代舊的 13,010,944-byte `0c-server.exe`
- 新 `0c-server.exe` 為 13,458,432 bytes，SHA-256 `60fc154c3848f07c1253557ca3034f5373f8a723bb722bc902f6e15a783627c7`
- 內容：x64 `0c-server.exe`、x64 `0c_tsf.dll`、x86 `0c_tsf.dll`、x64 `0c-ui.exe`；CI 未上傳資料包

## 3. T29b Windows 非互動 runner

從 WSL 對同一 commit 執行：

```bash
cd platforms/windows/test
/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe \
  -NoProfile -NonInteractive -ExecutionPolicy Bypass \
  -File "$(wslpath -w run-from-wsl.ps1)" \
  -RepoRevision "$(git rev-parse HEAD)" \
  -DataDir "$(wslpath -w ../../../testdata/toy)"
```

主要結果目錄：`E:\freerun\0c-wincheck\results\20261117-203014-01847d1d`
WSL 路徑：`/mnt/e/freerun/0c-wincheck/results/20261117-203014-01847d1d`

| 檢查 | 狀態 | exit | 秒 | 結果 |
|---|---|---|---|---|
| parser | pass | 0 | 2 | 9 個 T29 腳本剖析通過，0 parse errors |
| interop | pass | 0 | 1.8 | Add-Type 可在 Windows PowerShell 5.1 編譯 `JaAcceptanceProbe` |
| profiles | info | 1 | 1.6 | 0C 未安裝；zh-TW 與 ja-JP 都未註冊，不算 runner 失敗 |
| capabilities | pass | 0 | 1.9 | 非管理員；0C 未安裝；Windows 10 Pro 26H2 (build 26300)；缺少 Go、Rust |
| artifact | pass | 0 | 1.6 | `0c-server.exe -check-data` exit 0，117ms，資料包 `testdata/toy` |

Runner 結束碼 0。

另有一次不加 `-DataDir` 的預設執行，結果目錄
`E:\freerun\0c-wincheck\results\20261117-202937-1c638f55`：
parser、interop、capabilities pass，profiles info，artifact `skip`。
原因：CI `windows-binaries` 只含 4 個執行檔與 DLL，沒有 `data` 目錄；runner 改跑 `0c-server.exe -h`，exit 0。這不是失敗，補上 `-DataDir testdata/toy` 後即為上面的 pass。

## 4. 已知環境限制

1. Windows 主機目前是非管理員（`JEFFPC\user`），無法註冊 TSF、寫 HKLM 或建立排程工作。
2. Windows 主機未安裝 0C，zh-TW／ja-JP profile 都未註冊，因此 T29b `profiles` 依設計記為 `info`。
3. CI `windows-binaries` artifact 不含資料包，`-check-data` 必須另指定 `-DataDir testdata/toy`；不指定時 `artifact` 為 `skip`。
4. Windows 主機 PATH 缺少 Go、Rust、git；MSBuild、signtool、dotnet 有找到。因此 Windows 原生建置只能在 CI 驗證。
5. 本次沒有互動桌面自動化：WPF、記事本、Terminal、Edge、SearchHost、WebView2、Win+Space、TSF 註冊、截圖目視都沒有執行。
6. 安裝、升級、rollback、uninstall 依 T41 範圍不執行。
7. Windows 交叉編譯只證明可編譯，沒有在 Windows 原生執行所有產物；`0c-server.exe` 由 T29b artifact 檢查實際啟動並載入資料包。

## 5. 手動驗收清單（只剩互動才能做）

以下都需要已登入的互動桌面、提升權限的 Windows PowerShell 5.1，並先關閉日常 `0c-server`／`0c-ui`。使用 Windows x64 bundle，於 repo 的 `platforms\windows\test` 執行：

```powershell
.\acceptance-test.ps1 -BundleDir C:\path\to\bundle `
  -Suite ja-smoke,ja-candidate,ja-profile,ja-private,ja-sandbox,ja-32bit
```

| 項目 | 為什麼只能人工 | 觀察重點 |
|---|---|---|
| `ja-smoke`（WPF、記事本、Windows Terminal） | 需要互動桌面與三個真實宿主，總腳本要安裝測試 bundle | 羅馬字→かな、Space 変換、Enter、Escape、Tab／Shift+Tab、変換／無変換、F6–F8、F9／F10、三個宿主都送出日文 |
| `ja-candidate`（WPF） | 候選窗與滑鼠點選只能看實際畫面 | 候選窗內容、文節底線、目前文節移動、Space／PageUp／PageDown、滑鼠點第二列、送出後關窗 |
| `ja-profile`（WPF、記事本） | Win+Space 與語言列切換無法在非互動工作階段驗證 | zh-TW／ja-JP 兩個 TIP、Win+Space、各程式記住語言、新程式從系統語言開始 |
| `ja-private`（WPF、原生 EDIT） | 密碼／PIN 欄位必須由真人輸入觀察 | 原樣收鍵、不開候選、不送引擎；`IS_PRIVATE` 可組字但不讀前文、不學習 |
| `ja-sandbox`（Edge、Windows 搜尋框、WebView2） | 需要 Edge、SearchHost 與 WebView2 runtime 的實際桌面 | Chromium 組字／変換／送出、候選窗位置、搜尋框數字選字、跨 `msedgewebview2.exe` 的候選窗 |
| `ja-32bit`（SysWOW64） | 由 `acceptance-test.ps1` 啟動 32 位元 PowerShell，需互動工作階段 | x86 行程載入 `x86\0c_tsf.dll`、DLL 為 i386、日文組字／変換／送出 |

每個 suite 內的截圖與目視項目也要人工確認：候選窗位置、文節底線粗細與配色、`wpf-ja.png`、Edge／搜尋框／WebView2 截圖。腳本只以畫面雜湊判斷「有移動」，不判斷視覺品質。

WebView2 第一次執行可能需要網路下載 SDK；SearchHost 只在 Windows 11 有沙箱資料夾時受測。這兩項缺少環境時，suite 會記為 skipped，仍要人工確認是否符合預期。
