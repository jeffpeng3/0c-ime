package rerank

import (
	"sort"
	"strings"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// hyp 是限制式重搜的一條部分路徑。matched 是已符合的前綴字數。
type hyp struct {
	cost    float64
	prev    *hyp
	node    decode.Node
	key     string
	matched int
}

// searchState 是某個（位置, 詞性, 已符合前綴長度）的最佳假設。
type stateKey struct {
	pos     lexicon.POS
	matched int
}

type searchState struct {
	pos     lexicon.POS
	matched int
	hyps    []*hyp
}

type searchSet struct {
	order []stateKey
	m     map[stateKey]*searchState
}

func newSearchSet() *searchSet {
	return &searchSet{m: map[stateKey]*searchState{}}
}

func (s *searchSet) add(pos lexicon.POS, matched int, h *hyp, k int) {
	key := stateKey{pos: pos, matched: matched}
	st, ok := s.m[key]
	if !ok {
		st = &searchState{pos: pos, matched: matched}
		s.m[key] = st
		s.order = append(s.order, key)
	}
	st.hyps = insertHyp(st.hyps, h, k)
}

func insertHyp(list []*hyp, h *hyp, k int) []*hyp {
	i := 0
	for i < len(list) && !hypLess(h, list[i]) {
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

func hypLess(a, b *hyp) bool {
	if a.cost != b.cost {
		return a.cost < b.cost
	}
	return a.key < b.key
}

// search 在統計 lattice 上重搜，要求輸出以 prefix 開頭。prefix 必須是
// 完整詞的邊界（由 clampPrefix 保證）；lattice 允許同一段讀音有不同切法，
// 只要串起來的表記仍等於 prefix。
func (r *Reranker) search(seq reading.Sequence, ctx lang.Context, prefix string, k int) []lang.Result {
	if k <= 0 || len(seq) == 0 {
		return nil
	}
	lat := decode.Builder{
		Lex:          r.dec.Lex,
		MaxWordUnits: r.dec.MaxWordUnits,
		UnknownCost:  r.dec.UnknownCost,
		Learning:     r.dec.Learning,
		Context:      ctx,
	}.Build(seq)
	if len(lat) != len(seq) {
		return nil
	}
	runes := []rune(prefix)
	states := make([]*searchSet, len(seq)+1)
	for i := range states {
		states[i] = newSearchSet()
	}
	states[0].add(lexicon.POSBOS, 0, &hyp{}, k)
	for start := 0; start < len(seq); start++ {
		set := states[start]
		if len(set.order) == 0 {
			continue
		}
		for _, node := range lat[start] {
			surface := []rune(node.Entry.Surface)
			for _, key := range set.order {
				st := set.m[key]
				nextMatched, ok := matchPrefix(runes, st.matched, surface)
				if !ok {
					continue
				}
				conn := connectionCost(r.dec.Conn, key.pos, node.Entry.POS)
				for _, h := range st.hyps {
					states[node.End].add(node.Entry.POS, nextMatched, &hyp{
						cost:    h.cost + node.Entry.Cost + conn,
						prev:    h,
						node:    node,
						key:     h.key + "\x00" + node.Entry.Key(),
						matched: nextMatched,
					}, k)
				}
			}
		}
	}
	var finals []searchResult
	last := states[len(seq)]
	full := len(runes)
	for _, key := range last.order {
		if key.matched != full {
			continue
		}
		st := last.m[key]
		conn := connectionCost(r.dec.Conn, key.pos, lexicon.POSEOS)
		for _, h := range st.hyps {
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
	out := make([]lang.Result, 0, len(finals))
	seen := map[string]bool{}
	for _, final := range finals {
		nodes := searchPath(final.h)
		if len(nodes) == 0 {
			continue
		}
		clauses := decode.Clauses(nodes, r.dec.Conn, r.dec.Segmenter)
		surface := decode.ResultSurface(clauses)
		if surface == "" || seen[surface] || !strings.HasPrefix(surface, prefix) {
			continue
		}
		seen[surface] = true
		out = append(out, lang.Result{Clauses: clauses, Score: -final.cost})
	}
	return out
}

// matchPrefix 回報這個詞面能否接在已符合 matched 字的前綴之後。詞面不得
// 跨過前綴尾端，確保固定的是完整詞。
func matchPrefix(prefix []rune, matched int, surface []rune) (int, bool) {
	if matched >= len(prefix) {
		return matched, true
	}
	remaining := prefix[matched:]
	if len(surface) > len(remaining) {
		return 0, false
	}
	for i, r := range surface {
		if remaining[i] != r {
			return 0, false
		}
	}
	return matched + len(surface), true
}

type searchResult struct {
	h    *hyp
	cost float64
}

func searchPath(h *hyp) []decode.Node {
	var rev []decode.Node
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

func connectionCost(conn lexicon.Connection, left, right lexicon.POS) float64 {
	if conn == nil {
		return lexicon.DefaultConnectionCost
	}
	return conn.Cost(left, right)
}
