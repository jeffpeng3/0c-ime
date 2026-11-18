package decode

import (
	"sort"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	jalm "github.com/0c-biggo/0c-ime/core/ja/lm"
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
	// PreferHiragana 對平假名讀音套用詞面書寫系統偏好（見 Builder）。
	PreferHiragana bool
	// LM 是選填的詞語言模型。nil 時完全不改變原本的詞成本＋連接成本計分。
	LM *jalm.Model
	// LMWeight 是 LM 成本（unigram＋bigram）的乘數。0 表示即使載入模型也不使用；
	// 載入器應在模型可用時填入已校準的預設值。
	LMWeight float64
}

// DefaultLMWeight 是載入 lm.bin 後的預設權重。現有可散布模型只有 15 個
// bigram，且 unigram 幾乎重複詞典的詞成本；modern-500 的 top-1 在 0～4
// 的每個權重都是 0，hard-cases 的 +1 題來自 unigram 的整體尺度而非 bigram
// 順序。因此校準結果是 0：模型可以載入與查詢，但預設不改變排序。等有真實
// bigram 覆蓋的模型時，再以同一組 tuning corpus 重新選值。
const DefaultLMWeight = 0.0

// hypothesis 是在某個（位置, 詞性）狀態上的一條部分路徑。
type hypothesis struct {
	cost float64
	prev *hypothesis
	node Node
	key  string // 詞序列的穩定鍵，只用於同分時的排序
}

// stateKey 是 Viterbi 的狀態。沒有 LM 時 ctx 永遠是空字串，行為與只依詞性
// 分組完全相同；有 LM 時，只有會成為 bigram 左詞的前一個詞面才需要拆狀態。
type stateKey struct {
	pos lexicon.POS
	ctx string
}

type posHypotheses struct {
	key  stateKey
	hyps []*hypothesis
}

type stateSet struct {
	order []stateKey
	m     map[stateKey]*posHypotheses
}

func newStateSet() *stateSet {
	return &stateSet{m: map[stateKey]*posHypotheses{}}
}

func (s *stateSet) get(key stateKey) []*hypothesis {
	if st, ok := s.m[key]; ok {
		return st.hyps
	}
	return nil
}

func (s *stateSet) add(key stateKey, h *hypothesis, k int) {
	st, ok := s.m[key]
	if !ok {
		st = &posHypotheses{key: key}
		s.m[key] = st
		s.order = append(s.order, key)
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

// lmCost 回傳 left 之後接 cur 的加權 LM 成本。模型不存在或權重為 0 時回傳 0，
// 讓沒有 lm.bin 的執行環境保持原本的計分與排序。
func (d *Decoder) lmCost(left, cur string) float64 {
	if d.LM == nil || d.LMWeight == 0 {
		return 0
	}
	return d.LMWeight * d.LM.Cost(left, cur)
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
		Lex:            d.Lex,
		MaxWordUnits:   d.MaxWordUnits,
		UnknownCost:    d.UnknownCost,
		Learning:       d.Learning,
		Context:        ctx,
		PreferHiragana: d.PreferHiragana,
	}.Build(seq)
	states := make([]*stateSet, len(seq)+1)
	for i := range states {
		states[i] = newStateSet()
	}
	states[0].add(stateKey{pos: lexicon.POSBOS}, &hypothesis{}, k)
	for start := 0; start < len(seq); start++ {
		set := states[start]
		if len(set.order) == 0 {
			continue
		}
		for _, node := range lat[start] {
			for _, key := range set.order {
				conn := connectionCost(d.Conn, key.pos, node.Entry.POS)
				for _, h := range set.get(key) {
					lm := d.lmCost(h.node.Entry.Surface, node.Entry.Surface)
					next := stateKey{pos: node.Entry.POS}
					if d.LM != nil && d.LMWeight != 0 && d.LM.HasLeft(node.Entry.Surface) {
						next.ctx = node.Entry.Surface
					}
					dst := states[node.End]
					dst.add(next, &hypothesis{
						cost: h.cost + node.Entry.Cost + conn + lm,
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
	for _, key := range last.order {
		conn := connectionCost(d.Conn, key.pos, lexicon.POSEOS)
		for _, h := range last.get(key) {
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
