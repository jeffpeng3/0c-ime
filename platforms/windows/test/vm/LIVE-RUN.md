# P2-44 執行紀錄

- 日期：2026-11-18（Asia/Taipei）
- Worktree：`/home/fedora/0c-wt-P244`
- 分支：`p2/T44-vm-harness`
- 基底：`62e9482` (`main`，P2-17 整合後)
- 主機：Linux（WSL2 核心），Python 3.15.0、Go 1.27.3
- Windows VM：**沒有可連線的 VM**（沒有提供 host／金鑰；本機為 Linux）

## 結果

| 項目 | 結果 | 證據 |
|---|---|---|
| Python self-test | PASS，46 tests | `python3 platforms/windows/test/vm/p2vm.py selftest` |
| 協定 trace 整合測試 | PASS | 同一 self-test 會建置 `0c-server`、用 `testdata/golden/zh/data` 重跑 `hello`／`su3cl3`／`麻婆豆腐` 的候選 range 與長詞檢查 |
| 注音 golden | PASS，45 個案例 | 同上的 `local.zhuyin_golden`，`go run ./cmd/0c-golden` |
| dry-run／plan | PASS | `p2vm.py plan --host 192.0.2.10 ...` 印出上傳與互動排程命令，未連線 |
| SSH 可達性 | NOT RUN | 對 RFC 5737 測試位址執行，10 秒 `Connection timed out`；報告 `live_run.status = NOT RUN` |
| VM preflight | NOT RUN | 無 VM |
| bundle 安裝 | NOT RUN | 無 VM |
| `p2-mixed` 協定 trace | NOT RUN | 無 VM |
| WPF／記事本／Terminal／Edge／WebView2／搜尋框／32 位元 | NOT RUN | 無 VM |

本次沒有把任何 live 檢查標成 PASS。`p2vm.py run` 在 SSH 失敗時寫出
`summary.json`／`summary.md`，`live_run.reason` 為
`no reachable Windows VM over SSH; live checks were not run`，結束碼 3。
`build/windows/validation/` 沒有提交的結果目錄；重跑會產生新的
`acceptance-<時間>-<bundle-sha8>`。

## 要跑 live 需要什麼

1. 一台 Windows 10 1803 以上或 Windows 11 x64 VM，已開機並以測試帳號登入、
   螢幕未鎖定，`explorer.exe` 在目前 session。
2. VM 的 OpenSSH Server 已啟動，測試帳號是系統管理員，且 harness 主機的私鑰可登入。
3. 同一帳號下沒有日常 `0c-server`／`0c-ui`／`zuin-server`／`zuin-ui` 在跑。
4. Windows 10/11 preview 或 public-beta bundle（含 `release.json`、x64 與 x86 DLL、
   `0c-update.exe`、`0c-server.exe`、`0c-ui.exe`、資料與模型）。
5. 目標宿主：記事本、Windows Terminal、Edge；WebView2 runtime；Windows 11 才有搜尋框。
   第一次 WebView2 suite 需要網路下載固定版本 SDK。
6. 從 Linux 執行：

   ```sh
   python3 platforms/windows/test/vm/p2vm.py run \
     --host <VM-IP> --user <admin-user> --key <ssh-private-key> \
     --bundle <0c-...-windows-amd64-preview.zip>
   ```

   先跑 `preflight` 可以只確認 VM 能力；`--keep-remote` 保留 VM 上的工作目錄。

## 已知的基底差異（只回報，不修）

本次基底 `62e9482` 的 `main` 還沒合併 P2-41 的 `platforms/windows/tsf/src/candidate_model.rs`
（`p2/T41-candidate-ui` 分支）。harness 的 `candidate.segment_switch`、
`candidate.long_word_one` 與 `app.wpf_segment_switch_visible` 是照 P2-41 的完成條件
寫的；引擎端 fixture 已在 Linux 對真的 `0c-server` 驗過（0/4 → 2/2、`麻婆豆腐`
一項），但 WPF 外殼的「游標移動自動重開候選」要等 P2-41 合入才會 pass。live 跑在
目前基底上會如實回報這個缺口；harness 不會自行修 product code。P2-40 的單一 Mixed
profile 已在基底；`cmd/0c-update` 仍留著舊的 ja-JP profile 狀態與語言清單移除邏輯，
live 檢查會分辨「只註冊一個 Mixed」與「語言清單只剩一個 TIP」。
