package decode

import (
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// DefaultUnknownCost 是未知詞 fallback 的單一かな成本。
// 夠高讓已知詞優先，但仍可轉換詞典沒有收錄的かな。
const DefaultUnknownCost = 8.0

// Node 是 lattice 上的一條弧：從 Start 走到 End 的一個詞。
type Node struct {
	Start, End int
	Entry      lexicon.Entry
	// Unknown 表示這是未知詞 fallback，不是詞典詞條。
	Unknown bool
}

// Lattice 是依起點分組的詞弧。Lattice[start] 列出所有從 start 開始的詞。
type Lattice [][]Node

// Nodes 回傳從 pos 開始的所有詞弧。
func (l Lattice) Nodes(pos int) []Node {
	if pos < 0 || pos >= len(l) {
		return nil
	}
	return l[pos]
}

// Builder 依讀音序列建立かな lattice。
//
// 每個起點都會查詢詞典 trie 的所有前綴，所以可変長讀音自然被展開；
// 另外固定加入一個單字かな的未知詞 fallback。
type Builder struct {
	Lex          lexicon.Source
	MaxWordUnits int
	UnknownCost  float64
	Learning     Learning
	Context      lang.Context
	// PreferHiragana 讓平假名讀音優先保留平假名／漢字詞面，不讓成本接近的
	// 片假名詞面或拉丁字母雜訊勝出。零值表示完全照成本排序。
	PreferHiragana bool
}

// Build 建立 lattice。seq 的每個單位都是一個かな字元。
func (b Builder) Build(seq reading.Sequence) Lattice {
	if len(seq) == 0 {
		return nil
	}
	maxUnits := b.MaxWordUnits
	if maxUnits <= 0 {
		maxUnits = lang.DefaultLimits().MaxWordUnits
	}
	unknownCost := b.UnknownCost
	if unknownCost <= 0 {
		unknownCost = DefaultUnknownCost
	}
	lat := make(Lattice, len(seq))
	for start := range seq {
		var nodes []Node
		if b.Lex != nil {
			b.Lex.Matches(seq, start, func(end int, e lexicon.Entry) {
				if end-start > maxUnits {
					return
				}
				if b.Learning != nil {
					e.Cost += b.Learning.Cost(seq, start, e, b.Context)
				}
				nodes = append(nodes, Node{Start: start, End: end, Entry: e})
			})
		}
		if b.PreferHiragana {
			nodes = applyScriptPolicy(nodes, seq, start)
		}
		nodes = append(nodes, Node{
			Start: start,
			End:   start + 1,
			Entry: lexicon.Entry{
				Surface: string(seq[start]),
				Reading: reading.Sequence{seq[start]},
				Cost:    unknownCost,
				POS:     lexicon.POSOther,
			},
			Unknown: true,
		})
		lat[start] = nodes
	}
	return lat
}
