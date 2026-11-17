# UniDic 著作者表示（unidic-cwj-202512）

本檔是 public core 詞典包必須附上的著作者表示。NINJAL 沒有在 `copying/` 提供獨立的 `AUTHORS` 檔；`unidic-cwj-202512.zip` 內也只有 `license/BSD`、`license/COPYING`、`license/GPL`、`license/LGPL`，沒有 `AUTHORS`。因此本檔由 artifact 自帶的 `README.md` 與 `license/COPYING` 逐字整理，內容不是新增的授權條件。

## 1. Artifact 自帶的著作權與實作聲明

以下四行逐字取自 `unidic-cwj-202512.zip` 的 `README.md`（全文見 `README-unidic-cwj-202512.md`）：

```text
## COPYRIGHT

- 国立国語研究所（言語資源開発センター/「語彙資源」プロジェクト/「BCCWJ2」プロジェクト）

## IMPLEMENTOR

- 小木曽智信 togiso@ninjal.ac.jp
```

## 2. Artifact 授權檔的著作權人

`license/COPYING`（全文見 `COPYING-unidic-cwj-202512.txt`）記載：

```text
This work are copyrighted free software by
National Institute for Japanese Language and Linguistics
```

`license/BSD`（全文見 `BSD-unidic-cwj-202512.txt`）的著作權行是：

```text
Copyright (c) 2023 National Institute for Japanese Language and Linguistics
```

## 3. 網站版 BSD 的著作權人

`https://clrd.ninjal.ac.jp/unidic/copying/BSD`（全文見 `BSD.txt`）的著作權行是：

```text
Copyright (c) 2011-2021, The UniDic Consortium
All rights reserved.
```

NINJAL FAQ 的 BSD 署名示例要求明示「AUTHORS に記載された著作者」，歷史版本的 `AUTHORS` 內容為 `The UniDic Consortium`。散布時兩個著作權人字樣都要保留，因為網站條款與 artifact 條款的著作權行不同。

## 4. 散布時的固定做法

1. 詞典包同時附 `BSD.txt` 與 `BSD-unidic-cwj-202512.txt`，以及本檔。
2. NOTICE 寫明使用 `unidic-cwj-202512`（現代書き言葉UniDic ver.2025.12）。
3. 不改寫著作權人字樣，不合併成單一 `BSD-3-Clause` 標籤。
4. 若日後換用其他 artifact，且該 zip 內含 `AUTHORS`，改為逐字附上該檔，並在本檔記錄差異。
