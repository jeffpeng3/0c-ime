# 混合語言 fixtures（P2-03）

這裡凍結 Phase 2 混合輸入的期望結果：同一串輸入要切成哪些帶語言標籤的片段、
每段用哪個輸出變體、最後組出什麼文字。`zh/` 的注音 golden 不變，仍然驅動正式
引擎；這裡只宣告期望值，等 P2-10（分段）、P2-12（lattice／跨片段長詞）、
P2-13（session 片段）與 P2-14（輸出組裝）完成後，同一份資料直接當驗收題庫。

## 檔案

- `cases.json`：混合案例。`internal/golden` 的 `LoadMixed` 會驗證整份檔案。
- 固定資料：無。混合案例只用契約型別，不需要詞庫或語言模型。

## 執行

```sh
go test ./internal/golden/ -run 'TestMixed'
```

`LoadMixed` 在載入時就檢查：

- `span` 以 rune 為單位，`segments` 依序從 0 接到 `input` 結尾，沒有漏字或重疊。
- 每個片段的 `script` 與 span 內的輸入字元相符；`language`、`variant`、`reading` 合法。
- `variant` 等於該語言的預設輸出變體（`zh-Hant`、`ja`、`en`）。
- `segments` 的 `surface` 依序串起來等於 `output`。
- `also` 候選與選中的候選不同，但沿用同一個 span 與 script。
- `personal` 清單裡的詞一定有對應的 `personal` 片段，反之亦然。
- 有 `pieces` 的案例，至少一個 piece 邊界落在某個合併片段的中間，證明是跨片段長詞。

`cases.json` 用 `DisallowUnknownFields` 解碼，所以不能偷偷加入單一
`activeLanguage`／`mode` 這類欄位。

## 欄位

| 欄位 | 意義 |
|---|---|
| `primary` | 主要語言，只是排序偏好，不是模式開關 |
| `input` | 使用者依序送出的整串字元；span 的座標系是 rune |
| `pieces` | 長詞合併前的輸入片段（使用者分幾段打完） |
| `segments` | 期望的輸出片段：span、script、reading、surface、language、variant、source |
| `also` | 同片段沒有被選中、但必須留在候選清單裡的其他語言候選 |
| `personal` | 已在個人詞彙的詞面 |
| `output` | 片段詞面依序串起的最終文字 |

## 案例

- 情境 A（`scenario-a-primary-ja`）：主要語言日文。`watashiwa`、注音
  `ㄉㄚˋㄐㄧㄅㄚ`、`desu`、英文尾段在同一串輸入完成；日文片段用 `ja`，
  中文片段用 `zh-Hant`，英文保留原文，不自動把「私わ」改成「私は」。
- 情境 B（`scenario-b-primary-zh`）：同一串輸入，主要語言中文。注音優先中文，
  拉丁片段仍同時有 `ja` 與 `en` 候選，輸出與情境 A 相同，證明主要語言不是
  硬性 gate。
- 情境 C（`scenario-c-proper-nouns`）：`kubectl`、`pCloud`、`GitHub` 直接保留
  原文，不拆成注音或かな；`GitHub` 在個人詞彙，來源是 `personal`，同時保留
  `literal` 候選。
- 長詞跨片段：`麻婆豆腐`（兩段注音）、`東京大学`（兩段かな）、`中華民國`
  （三段注音）。合併片段吸收 piece 邊界，讀音仍保留原本的多個單位。

## 不在這裡測

分段規則、Viterbi 權重、候選排序與實際字形轉換由 P2-10～P2-14 實作；這裡只把
期望的片段、語言標籤與輸出變體鎖住。注音行為仍以 `../zh/expected/` 為 baseline。
