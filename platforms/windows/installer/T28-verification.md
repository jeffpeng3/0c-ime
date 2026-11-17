# 語言 profile 驗證（P2-40／P2-43）

`verify-profiles.ps1` 在 Windows 10/11 x64 讀取 TSF profile 機碼與 `0c-update status -json`，
輸出 JSON，並且只在「恰好一個 Mixed profile、沒有 ja-JP」時回傳 0。這個工作環境是 Linux，
沒有 Windows 桌面與 PowerShell；以下是可重現的靜態檢查結果，Windows 端請在驗收機執行最後一節。

## 靜態檢查

```text
$ go test ./cmd/0c-update ./internal/update ./internal/neural/...
ok

$ go vet ./...
（無輸出）

$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
（成功；純 Go 統計路徑）

$ python3 -m unittest scripts/test_windows_package.py
（成功；runtime lock、安裝布局、Python-free 邊界）
```

`cmd/0c-update/input_list_test.go` 直接解析 `guids.rs`，確認語言清單只使用 Mixed 的
`LANGID`、`CLSID_ZUIN`、`GUID_PROFILE`；`GUID_PROFILE_JA` 只在升級與解除安裝時移除。

## Windows 驗收（需在 Windows 執行）

安裝後、在同一帳號的一般 PowerShell：

```powershell
pwsh -File platforms/windows/installer/verify-profiles.ps1 `
  -Updater "$env:LOCALAPPDATA\Zuin\bin\0c-update.exe" `
  -Out build/windows/validation/p2-43-profiles.json
```

預期：

1. `profiles` 的 Mixed 是 `"registered": true`，legacy ja-JP 是 `false`。
2. 語言清單只有一個「Mixed」；Win＋Space 不會出現「靈犀日本語」。
3. 從舊版升級後，舊 ja-JP 機碼與 `%LOCALAPPDATA%\Zuin\update\input-list` 的 ja tip 都被清掉。
4. `0c-update rollback`、`0c-update uninstall` 後 Mixed 分別回到上一版與 false。
