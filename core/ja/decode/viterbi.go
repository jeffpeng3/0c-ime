package decode

import (
	"sort"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Decoder 是日文かな漢字変換的統計核心：かな lattice 上的 Viterbi，輸出文節。
//
// Decoder 不保存輸入狀態，也不接 session；同一個實作可以同時服務多個轉換請求。
type Decoder struct {
	Lex       lexicon.Source
	Conn      lexicon.Connection
	Segmenter Segmenter

	// MaxWordUnits 是單一詞條可佔用的讀音單位數；0 使用 lang.DefaultLimits。
	MaxWordUnits int
	// UnknownCost 是未知詞 fallback 的單一かな成本；0 使用 DefaultUnknownCost。
	UnknownCost float64
	// MaxNBest 是 Convert 保留給驗證器重排的候選數；0 使用 8。
	MaxNBest int
	// MaxCandidates 是 Candidates 一次最多列出的候選數；0 使用 lang.DefaultLimits。
	MaxCandidates int

	// Learning、Verifier 與 Reranker 是 T21、T30 的掛鉤；nil 表示不做任何調整。
	Learning Learning
	Verifier Verifier
	Reranker Reranker
}

// hypothesis 是在某個（位置, 詞性）狀態上的一條部分路徑。
type hypothesis struct {
	cost float64
	prev *hypothesis
	node Node
	key  string // 詞序列的穩定鍵，只用於同分時的排序
}

type posHypotheses struct {
	pos  lexicon.POS
	hyps []*hypothesis
}

type stateSet struct {
	order []lexicon.POS
	m     map[lexicon.POS]*posHypotheses
}

func newStateSet() *stateSet {
	return &stateSet{m: map[lexicon.POS]*posHypotheses{}}
}

func (s *stateSet) get(pos lexicon.POS) []*hypothesis {
	if st, ok := s.m[pos]; ok {
		return st.hyps
	}
	return nil
}

func (s *stateSet) add(pos lexicon.POS, h *hypothesis, k int) {
	st, ok := s.m[pos]
	if !ok {
		st = &posHypotheses{pos: pos}
		s.m[pos] = st
		s.order = append(s.order, pos)
	}
	st.hyps = insertHypothesis(st.hyps, h, k)
}

// insertHypothesis 把 h 插入依成本排序的清單，只保留前 k 名。
// 同分時依詞序列鍵排序，確保結果可重現。
func insertHypothesis(list []*hypothesis, h *hypothesis, k int) []*hypothesis {
	i := 0
	for i < len(list) && !hypothesisLess(h, list[i]) {
		i++
	}
	if i >= k {
		return list
	}
	list = append(list, nil)
	copy(list[i+1:], list[i:])
	list[i] = h
	if len(list) > k {
		list = list[:k]
	}
	return list
}

func hypothesisLess(a, b *hypothesis) bool {
	if a.cost != b.cost {
		return a.cost < b.cost
	}
	return a.key < b.key
}

// DesiredNBest 回傳 Convert 要保留的候選數。
func (d *Decoder) desiredNBest() int {
	if d.MaxNBest > 0 {
		return d.MaxNBest
	}
	return 8
}

func (d *Decoder) desiredCandidates() int {
	if d.MaxCandidates > 0 {
		return d.MaxCandidates
	}
	return lang.DefaultLimits().MaxCandidates
}

// searchResult 是一條完整路徑與含句尾連接成本的總成本。
type searchResult struct {
	h    *hypothesis
	cost float64
}

// search 回傳依統計成本排序的前 k 條完整路徑。
func (d *Decoder) search(seq reading.Sequence, ctx lang.Context, k int) []searchResult {
	if k <= 0 {
		k = 1
	}
	if len(seq) == 0 {
		return nil
	}
	lat := Builder{
		Lex:          d.Lex,
		MaxWordUnits: d.MaxWordUnits,
		UnknownCost:  d.UnknownCost,
		Learning:     d.Learning,
		Context:      ctx,
	}.Build(seq)
	states := make([]*stateSet, len(seq)+1)
	for i := range states {
		states[i] = newStateSet()
	}
	states[0].add(lexicon.POSBOS, &hypothesis{}, k)
	for start := 0; start < len(seq); start++ {
		set := states[start]
		if len(set.order) == 0 {
			continue
		}
		for _, node := range lat[start] {
			for _, pos := range set.order {
				conn := connectionCost(d.Conn, pos, node.Entry.POS)
				for _, h := range set.get(pos) {
					dst := states[node.End]
					dst.add(node.Entry.POS, &hypothesis{
						cost: h.cost + node.Entry.Cost + conn,
						prev: h,
						node: node,
						key:  h.key + "\x00" + node.Entry.Key(),
					}, k)
				}
			}
		}
	}

	var finals []searchResult
	last := states[len(seq)]
	for _, pos := range last.order {
		conn := connectionCost(d.Conn, pos, lexicon.POSEOS)
		for _, h := range last.get(pos) {
			finals = append(finals, searchResult{h: h, cost: h.cost + conn})
		}
	}
	sort.SliceStable(finals, func(i, j int) bool {
		if finals[i].cost != finals[j].cost {
			return finals[i].cost < finals[j].cost
		}
		return finals[i].h.key < finals[j].h.key
	})
	if len(finals) > k {
		finals = finals[:k]
	}
	return finals
}

// pathNodes 把假設還原成詞序列。
func pathNodes(h *hypothesis) []Node {
	var rev []Node
	for n := h; n != nil; n = n.prev {
		if n.prev == nil && n.node.Entry.Surface == "" {
			break
		}
		rev = append(rev, n.node)
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func (d *Decoder) result(r searchResult, seq reading.Sequence) lang.Result {
	clauses := Clauses(pathNodes(r.h), d.Conn, d.Segmenter)
	return lang.Result{Clauses: clauses, Score: -r.cost}
}

// sortResults 依分數由高到低排序，同分時依詞面排序。
func sortResults(results []lang.Result) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return ResultSurface(results[i].Clauses) < ResultSurface(results[j].Clauses)
	})
}
