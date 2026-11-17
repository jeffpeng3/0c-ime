# UniDic 授權全文（public core）

2026-11-17 第二修訂：現代書き言葉UniDic 的 BSD-3-Clause 分支經簽核進 public core。本目錄只放授權與署名文字，不含詞典資料。`lex.csv`、`sys.dic`、`matrix.bin` 或任何衍生詞典檔都不進 Git。

## 檔案

| 檔案 | 來源 | 說明 |
|---|---|---|
| `BSD.txt` | https://clrd.ninjal.ac.jp/unidic/copying/BSD | 網站版修正 BSD 全文；著作權行 `Copyright (c) 2011-2021, The UniDic Consortium` |
| `BSD-unidic-cwj-202512.txt` | `unidic-cwj-202512.zip` 的 `license/BSD` | artifact 版修正 BSD 全文；著作權行 `Copyright (c) 2023 National Institute for Japanese Language and Linguistics` |
| `COPYING-unidic-cwj-202512.txt` | `unidic-cwj-202512.zip` 的 `license/COPYING` | 三重授權聲明（GPL／LGPL／BSD） |
| `README-unidic-cwj-202512.md` | `unidic-cwj-202512.zip` 的 `README.md` | COPYRIGHT 與 IMPLEMENTOR 的原始出處 |
| `AUTHORS.md` | 由上述三個 artifact 檔案整理 | NINJAL 沒有獨立 `AUTHORS` 檔；本檔是散布時必附的著作者表示 |

## 條款版本差異

網站 `copying/BSD` 與 artifact `license/BSD` 的授權條件文字相同，著作權行不同：

- 網站版：`Copyright (c) 2011-2021, The UniDic Consortium`
- artifact 版：`Copyright (c) 2023 National Institute for Japanese Language and Linguistics`

兩份都保留。散布時兩份都要附，不能只留一份或改寫成單一著作權人。

## SHA-256

```text
770a75de30705439084f869dbcb0bc4ebcffcb7c7124c0d74f5083170318a9bb  BSD.txt
9980b1824f0d1dac41ea93dc96780c222e5cca3c4d1b5677ac8ab923a8533c3f  BSD-unidic-cwj-202512.txt
2694852d16fccf37701a2fb85435a860bd0be9d697e8ff2e35b7d35b50129fe0  COPYING-unidic-cwj-202512.txt
4f965549e0907ba0abce29618d46b51a62e6c7107da617a7bf01ce3665c31206  README-unidic-cwj-202512.md
```

## 沒有收進來的東西

- `unidic-cwj-202512.zip` 本體與 dictionary 內容（`sys.dic`、`unk.dic`、`matrix.bin`、`char.bin`、`lex.csv` 等）。
- `copying/GPL` 與 `copying/LGPL` 全文。本專案選 BSD-3-Clause，不需要附 GPL／LGPL 全文；原始連結記在 `data/sources/corpus/public-core.tsv` 的 `pc-unidic` 列。
- CC BY-NC-SA 版本的 UniDic（https://clrd.ninjal.ac.jp/unidic/commerce_use.html 上的另一條授權）。
- 古文用（`chuko`、`chusei-*`、`jodai`、`kindai-*`、`kinsei-*`、`waka`）與方言用（`kansai`、`D-kansai`）字典。

## 使用範圍

`pc-unidic` 只核准 `unidic-cwj-202512`（現代書き言葉）與同系列、同授權的現代話し言葉（`unidic-csj-*`）。版本編號在 2023.02 起由 `2.x`／`2.2.x` 改為年月制，`2025.12` 屬同一現代語系列。古文用與方言用字典不在本專案核准範圍。
