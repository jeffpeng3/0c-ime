# T28 語言 profile 驗證紀錄

`verify-profiles.ps1` 在 Windows 10/11 x64 讀取 TSF profile 機碼與 `0c-update status -json`，
輸出 JSON 並在兩個 profile 都註冊時回傳 0。這個工作環境是 Linux，沒有 Windows 桌面與 PowerShell；
以下是可重現的靜態檢查結果，Windows 端請在驗收機執行最後一節。

## 靜態檢查（Linux，2026-11-17）

```text
$ go test ./cmd/0c-update ./internal/update
ok  	github.com/0c-biggo/0c-ime/cmd/0c-update
ok  	github.com/0c-biggo/0c-ime/internal/update

$ go vet ./cmd/0c-update ./internal/update
（無輸出）

$ GOOS=windows GOARCH=amd64 go build ./...
（成功）

$ cd platforms/windows && cargo check -p zuin-tsf --target x86_64-pc-windows-gnu --offline
（成功，只剩既有的 keys.rs dead_code 警告）

$ cargo test -p zuin-tsf --target x86_64-pc-windows-gnu --offline --no-run
（成功）
```

`cmd/0c-update/input_list_test.go` 直接解析 `guids.rs`，確認兩個輸入清單識別與 DLL 註冊的
`LANGID`／`LANGID_JA`、`CLSID_ZUIN`、`GUID_PROFILE`／`GUID_PROFILE_JA` 一致。

## Windows 驗收（需在 Windows 執行）

安裝後、在同一帳號的一般 PowerShell：

```powershell
pwsh -File platforms/windows/installer/verify-profiles.ps1 `
  -Updater "$env:LOCALAPPDATA\Zuin\bin\0c-update.exe" `
  -Out build/windows/validation/t28-profiles.json
```

預期：

1. `profiles` 的 `zh-TW` 與 `ja-JP` 都是 `"registered": true`。
2. `status_profiles` 與 `0c-update status -json` 的 `profiles` 相同，兩個 `registered` 都是 `true`。
3. Win＋Space 可在「靈犀注音」與「靈犀日本語」之間切換；在兩個程式分別選不同語言，切回來仍記得。
4. `0c-update rollback` 後再執行腳本：`zh-TW` 為 true、`ja-JP` 為 false。
5. `0c-update uninstall` 後再執行腳本：兩者都是 false，`%LOCALAPPDATA%\Zuin\update\input-list` 不存在。

把 `build/windows/validation/t28-profiles.json` 附在驗收紀錄。
