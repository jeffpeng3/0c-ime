package personal

import (
	"errors"
	"math"
	"sort"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// 學習加成的預設值。
//
// Boost 是每次出現的加成，取 log2 讓第二次以後遞減；MaxBoost 是單一文節的上限。
// 上限刻意不大：統計成本差距超過 MaxBoost 的候選，學習不該翻盤，
// 這樣才不會因為選過一次罕見詞，就把明顯正確的候選擠到後面。
const (
	DefaultBoost    = 0.75
	DefaultMaxBoost = 2.0
	DefaultNBest    = 8
)

// Learner 把個人學習套到轉換結果上，並負責接收學習事件。
//
// Learner 可被多個 goroutine 同時使用；Store 本身有鎖。
// Store 為 nil 時不學習也不加成，方便外殼在沒有學習資料時照常運作。
type Learner struct {
	Store *Store
	// Boost 是每次學習的加成；0 使用 DefaultBoost。
	Boost float64
	// MaxBoost 是單一文節的加成上限；0 使用 DefaultMaxBoost。
	MaxBoost float64
	// NBest 是 Convert 重排時保留的候選數；0 使用 DefaultNBest。
	NBest int
}

// NewLearner 建立使用 st 的學習器；st 可以是 nil（不學習，也不加成）。
func NewLearner(st *Store) *Learner {
	return &Learner{Store: st}
}

var _ jadecode.Learning = (*Learner)(nil)

func (l *Learner) boost() float64 {
	if l == nil {
		return 0
	}
	if l.Boost > 0 {
		return l.Boost
	}
	return DefaultBoost
}

func (l *Learner) maxBoost() float64 {
	if l == nil {
		return 0
	}
	if l.MaxBoost > 0 {
		return l.MaxBoost
	}
	return DefaultMaxBoost
}

func (l *Learner) nbest() int {
	if l == nil || l.NBest <= 0 {
		return DefaultNBest
	}
	return l.NBest
}

// bonus 回傳單一學習條目的加成。次數越多加成越高，但有上限。
func (l *Learner) bonus(e Entry) float64 {
	if e.Count <= 0 {
		return 0
	}
	b := l.boost() * math.Log2(1+float64(e.Count))
	return math.Min(b, l.maxBoost())
}

// clauseReading 回傳文節的讀音；舊資料可能只有 segments，兩者都接受。
func clauseReading(c lang.Clause) reading.Sequence {
	if len(c.Reading) > 0 {
		return c.Reading
	}
	var seq reading.Sequence
	for _, seg := range c.Segments {
		seq = append(seq, seg.Reading...)
	}
	return seq
}

func segmentSurfaces(c lang.Clause) []string {
	if len(c.Segments) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.Segments))
	for _, seg := range c.Segments {
		if seg.Surface != "" {
			out = append(out, seg.Surface)
		}
	}
	return out
}

func segmentReadings(c lang.Clause) []reading.Sequence {
	if len(c.Segments) == 0 {
		return nil
	}
	out := make([]reading.Sequence, 0, len(c.Segments))
	for _, seg := range c.Segments {
		if len(seg.Reading) == 0 {
			return nil
		}
		out = append(out, seg.Reading)
	}
	return out
}

// clauseBonus 回傳某個文節在指定前文下的加成；只看同讀音、同前文、同詞面。
func (l *Learner) clauseBonus(c lang.Clause, left string) float64 {
	if l == nil || l.Store == nil || c.Surface == "" {
		return 0
	}
	e, ok := l.Store.Lookup(clauseReading(c), left)
	if !ok || e.Surface != c.Surface {
		return 0
	}
	return l.bonus(e)
}

// Score 回傳整串文節的學習加成。
//
// 第一個文節用 ctx 的左文；之後每個文節用前一個文節的詞面，和學習時記下的
// 左文節一致。換了前文就不會套用不相關的學習。
func (l *Learner) Score(clauses []lang.Clause, left string) float64 {
	if l == nil || l.Store == nil {
		return 0
	}
	total := 0.0
	for _, c := range clauses {
		total += l.clauseBonus(c, left)
		if c.Surface != "" {
			left = c.Surface
		}
	}
	return total
}

// LearnClauses 把一次提交的文節記成一次學習交易，回傳記下的筆數。
// 第一個文節的前文是 left，其餘用前一個文節的詞面。時間由宿主另行提供時用 LearnClausesAt。
func (l *Learner) LearnClauses(clauses []lang.Clause, left, source string) int {
	return l.LearnClausesAt(clauses, left, source, 0)
}

// LearnClausesAt 與 LearnClauses 相同，但把宿主提供的 Unix 秒記進 last_used。
func (l *Learner) LearnClausesAt(clauses []lang.Clause, left, source string, at int64) int {
	if l == nil || l.Store == nil {
		return 0
	}
	sels := make([]Selection, 0, len(clauses))
	for _, c := range clauses {
		rd := clauseReading(c)
		if len(rd) == 0 || c.Surface == "" {
			continue
		}
		sels = append(sels, Selection{
			Reading:         rd,
			Surface:         c.Surface,
			Segments:        segmentSurfaces(c),
			SegmentReadings: segmentReadings(c),
			Left:            left,
			Source:          source,
			At:              at,
		})
		left = c.Surface
	}
	return l.Store.RecordAll(sels)
}

// LearnSelection 記下使用者明確選字。
func (l *Learner) LearnSelection(clauses []lang.Clause, left string) int {
	return l.LearnClauses(clauses, left, SourceSelection)
}

// LearnClauseChange 記下使用者改文節後的結果。
func (l *Learner) LearnClauseChange(clauses []lang.Clause, left string) int {
	return l.LearnClauses(clauses, left, SourceClause)
}

// LearnAccepted 記下 Tab 接受的候選。
func (l *Learner) LearnAccepted(clauses []lang.Clause, left string) int {
	return l.LearnClauses(clauses, left, SourceAccept)
}

// LearnCandidate 依候選內容學習；候選沒有文節時用整筆候選當一個文節。
func (l *Learner) LearnCandidate(c lang.Candidate, left, source string) int {
	clauses := c.Clauses
	if len(clauses) == 0 {
		if c.Surface == "" || len(c.Reading) == 0 {
			return 0
		}
		clauses = []lang.Clause{{Reading: c.Reading, Surface: c.Surface}}
	}
	return l.LearnClauses(clauses, left, source)
}

// Cost 實作 decode.Learning：符合學習的詞條回傳負成本，讓 Viterbi 優先選它。
//
// 只有句首（start == 0）能確定左文就是 ctx.Left；句中位置的真實左文是前一個詞，
// 不是 ctx.Left，硬套會把不相關的學習用到別的位置，所以一律回 0。
// 整句重排由 Converter 依文節處理。
func (l *Learner) Cost(seq reading.Sequence, start int, e lexicon.Entry, ctx lang.Context) float64 {
	if l == nil || l.Store == nil || start != 0 {
		return 0
	}
	if len(e.Reading) == 0 || start+len(e.Reading) > len(seq) {
		return 0
	}
	for i, u := range e.Reading {
		if seq[start+i] != u {
			return 0
		}
	}
	entry, ok := l.Store.Lookup(e.Reading, ctx.Left)
	if !ok || entry.Surface != e.Surface {
		return 0
	}
	return -l.bonus(entry)
}

// Converter 是接上個人學習的日文轉換器，包裝 T12 的統計解碼器。
//
// 只調整排序與分數比較，不改變讀音對應、不丟掉候選。來源不是轉換的候選
// （記号、かな表記等）不加成，維持明確候選類型原本的相對順序。
type Converter struct {
	Decoder *jadecode.Decoder
	Learner *Learner
}

// NewConverter 建立學習轉換器。
func NewConverter(dec *jadecode.Decoder, l *Learner) *Converter {
	return &Converter{Decoder: dec, Learner: l}
}

var _ lang.Converter = (*Converter)(nil)

// Convert 回傳重排後的最佳轉換結果。沒有學習資料時直接交給解碼器，行為不變。
func (c *Converter) Convert(seq reading.Sequence, ctx lang.Context) (lang.Result, error) {
	if c == nil || c.Decoder == nil {
		return lang.Result{}, errors.New("日文轉換器沒有解碼器")
	}
	if len(seq) == 0 {
		return lang.Result{}, nil
	}
	if c.Learner == nil || c.Learner.Store.Len() == 0 {
		return c.Decoder.Convert(seq, ctx)
	}
	results, err := c.Decoder.NBest(seq, ctx, c.Learner.nbest())
	if err != nil {
		return lang.Result{}, err
	}
	if len(results) == 0 {
		return c.Decoder.Convert(seq, ctx)
	}
	best := results[0]
	bestAdj := best.Score + c.Learner.Score(best.Clauses, ctx.Left)
	for _, r := range results[1:] {
		adj := r.Score + c.Learner.Score(r.Clauses, ctx.Left)
		if adj > bestAdj || adj == bestAdj && betterResult(r, best) {
			best, bestAdj = r, adj
		}
	}
	return best, nil
}

// betterResult 是同分時的穩定順序：原始分數高者先，再依詞面。
func betterResult(a, b lang.Result) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return jadecode.ResultSurface(a.Clauses) < jadecode.ResultSurface(b.Clauses)
}

// Candidates 回傳重排後的候選。學習加成只作用在同讀音、同前文的轉換候選。
func (c *Converter) Candidates(seq reading.Sequence, span lang.Span, ctx lang.Context) ([]lang.Candidate, error) {
	if c == nil || c.Decoder == nil {
		return nil, errors.New("日文轉換器沒有解碼器")
	}
	cands, err := c.Decoder.Candidates(seq, span, ctx)
	if err != nil || len(cands) < 2 {
		return cands, err
	}
	if c.Learner == nil || c.Learner.Store.Len() == 0 {
		return cands, nil
	}
	type ranked struct {
		cand  lang.Candidate
		score float64
	}
	rs := make([]ranked, len(cands))
	for i, cand := range cands {
		rs[i].cand = cand
		rs[i].score = cand.Score
		if !boostable(cand) {
			continue
		}
		clauses := cand.Clauses
		if len(clauses) == 0 && cand.Surface != "" && len(cand.Reading) > 0 {
			clauses = []lang.Clause{{Reading: cand.Reading, Surface: cand.Surface}}
		}
		rs[i].score += c.Learner.Score(clauses, ctx.Left)
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].score != rs[j].score {
			return rs[i].score > rs[j].score
		}
		if rs[i].cand.Score != rs[j].cand.Score {
			return rs[i].cand.Score > rs[j].cand.Score
		}
		return rs[i].cand.Surface < rs[j].cand.Surface
	})
	for i := range rs {
		cands[i] = rs[i].cand
	}
	return cands, nil
}

// boostable 回報候選是否可由學習重排。明確的候選類型（記号等）不加成、
// 也不因學習被換到別的類型前面。
func boostable(c lang.Candidate) bool {
	switch c.Source {
	case "", lang.SourceConvert, lang.SourcePersonal:
		return true
	}
	return false
}

// Prefixes 直接交給解碼器；預測補全不因學習改變。
func (c *Converter) Prefixes(seq reading.Sequence) []lang.Candidate {
	if c == nil || c.Decoder == nil {
		return nil
	}
	return c.Decoder.Prefixes(seq)
}
