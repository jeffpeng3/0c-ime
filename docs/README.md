# 靈犀注音（0c）使用者文件

這裡說明目前版本怎麼安裝、怎麼輸入、怎麼切換模型，以及資料與模型的授權。內容只涵蓋這個 branch 已存在的行為；沒有量測數字的地方代表尚未驗證，不是品質保證。

## 目前狀態

| 項目 | 狀態 | 依據 |
|---|---|---|
| 注音 golden | PASS，45 個案例 | 本機 `go run ./cmd/0c-golden`，2026-11-18 |
| 單一 Mixed profile | 程式與 Linux 協定測試存在 | `cmd/0c-server`、`platforms/windows/tsf` |
| 逐片段候選、長詞單一候選 | 引擎與 Windows TSF 測試存在 | `core/session`、`platforms/windows/tsf`、`p2-mixed` |
| 混合語言 benchmark | 可重現的 baseline，不是上線門檻 | `testdata/mixed/README.md` |
| 日文羅馬字與混打 | 核心與測試存在；日文轉換品質仍在評測 | `core/ja`、`core/session` |
| Windows Phase 2 實機驗收 | **NOT RUN** | `platforms/windows/test/vm/LIVE-RUN.md` |
| 日文模型包 | 單一模型包（extra reranker + public 標點）已 pin HF revision；尚未接入推論 | `models/pairing.lock.json`、`PACKAGING.md` |
| Windows Python-free 套件 | 打包檢查與單元測試存在；VM 安裝 NOT RUN | `PACKAGING.md`、`scripts/test_windows_package.py` |

注音的首選正確率、延遲與模型大小有量測時才寫數字，來源在根目錄 [README](../README.md) 與 Phase 2 報告。日文目前沒有可與 Mozc 比較的品質數字；VM 驗收沒有執行，不能把未跑的项目寫成通過。

## 文件

| 文件 | 內容 |
|---|---|
| [安裝、升級、回復與移除](install.md) | Windows、macOS、Linux 的安裝位置、升級、rollback、uninstall 與資料保留 |
| [輸入與候選](typing.md) | 單一 Mixed profile、注音／羅馬字／英文混打、逐片段候選、長詞合併、日文輸入 |
| [模型與模型包](model-sets.md) | 注音模型、日文單一模型包、常駐設定與回報方式 |
| [授權頁](licensing.md) | 程式、公開核心資料與模型、CC BY-SA 實驗組、UniDic、IPADic、Windows runtime |

平台外殼的建置細節與按鍵差異在各自的 README：

- [Windows TSF](../platforms/windows/README.md)
- [macOS InputMethodKit](../platforms/macos/README.md)
- [Linux Fcitx5](../platforms/linux-fcitx5/README.md)
- [Linux IBus](../platforms/linux-ibus/README.md)
- [Linux 系統安裝](../platforms/linux-system/README.md)
- [設定程式](../platforms/settings/README.md)

## 回報問題

回報時附上版本、平台、套件來源（官網、系統套件或原始碼建置）與逐鍵重現步驟。隱私與資料處理見官網 [隱私與資料](https://0c.biggo.com/privacy/)；授權全文與逐來源聲明見 [legal/](../legal/README.md)。
