// Package decode 是日文かな漢字変換的統計核心。
//
// 轉換流程：依讀音建立かな lattice，用 Viterbi 選出最低成本的詞序列，
// 再依詞性與連接成本合併成文節（lang.Clause）。另外提供 N-best 候選、
// 前綴預測，以及個人學習與神經驗證的掛鉤。
//
// 這個套件只依賴 core/reading、core/lang 與 core/ja/lexicon，不碰 session、
// 檔案或網路；外殼自行決定何時呼叫與如何顯示。
package decode
