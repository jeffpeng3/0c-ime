// Package rewrite 產生日文変換的補助候選：かな表記、數字表記、記号與顔文字。
//
// 這個套件不改轉換演算法，也不重新排序 T12 的主要 N-best。外殼先取得
// core/ja/decode 的主要候選，再把 Rewriter.Candidates 產生的候選接到後面
// （Append 會把補助候選的分數壓在主要候選之下，並依序保留）。
//
// かな候選以讀音為來源，數字候選以詞面的阿拉伯數字為來源，記号與顔文字
// 以讀音查 data/ja/symbols 的 TSV 辭典。數字與符號的行為對照與刻意差異
// 記在 MOZC.md。
package rewrite
