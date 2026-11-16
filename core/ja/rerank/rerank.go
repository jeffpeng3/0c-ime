package rerank

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Reranker 實作 decode.Reranker。同一個值可以同時服務多個轉換請求；
// 每次 Rerank 都只使用區域狀態。
type Reranker struct {
	dec      *decode.Decoder
	verifier Verifier
	cfg      Config

	disabled atomic.Bool
	revision atomic.Int64

	mu    sync.Mutex
	stats Stats
}

// New 建立 reranker。dec 必須是已載入詞典的統計轉換器；verifier 為 nil
// 或 cfg.Disabled 時，Rerank 直接回傳統計結果。
func New(dec *decode.Decoder, verifier Verifier, cfg Config) *Reranker {
	r := &Reranker{dec: dec, verifier: verifier, cfg: cfg.normalized()}
	r.disabled.Store(cfg.Disabled)
	return r
}

// Enabled 回報神經層目前是否啟用。
func (r *Reranker) Enabled() bool {
	return r != nil && !r.disabled.Load() && r.verifier != nil && r.dec != nil
}

// SetEnabled 是執行期開關。關閉後下一次轉換直接使用統計結果，不呼叫驗證器。
func (r *Reranker) SetEnabled(enabled bool) {
	if r != nil {
		r.disabled.Store(!enabled)
	}
}

// Config 回傳補齊預設值後的設定。
func (r *Reranker) Config() Config {
	if r == nil {
		return Config{}
	}
	return r.cfg
}

// Stats 回傳累計統計。
func (r *Reranker) Stats() Stats {
	if r == nil {
		return Stats{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

func (r *Reranker) note(fn func(*Stats)) {
	r.mu.Lock()
	fn(&r.stats)
	r.mu.Unlock()
}

// entry 是一條候選在本次 rerank 的狀態。
type entry struct {
	res      lang.Result
	surface  string
	neural   float64
	stats    float64
	combined float64
	scored   bool
	passed   bool
	prefix   string
}

// Rerank 執行 draft、verify、限制式重搜。任何錯誤或逾時都回傳 drafts。
func (r *Reranker) Rerank(seq reading.Sequence, drafts []lang.Result, ctx lang.Context) []lang.Result {
	if !r.Enabled() || len(seq) == 0 || len(seq) > r.cfg.MaxReadingUnits || len(drafts) == 0 {
		return drafts
	}
	started := time.Now()
	r.note(func(s *Stats) { s.Calls++ })

	base := r.desiredDrafts(seq, ctx, drafts)
	entries, statSurface, ok := r.collect(seq, base)
	if !ok {
		return drafts
	}

	rounds, verified := 0, 0
	fallback := ""
	passed := false
	// 只把實際的驗證器呼叫時間算進預算。lattice 建構與限制式重搜等本地
	// 工作不影響要不要再跑一輪，否則同一輸入在慢機器或高負載下會少跑
	// 一輪，產生不同的重搜結果。
	var spent time.Duration

	for rounds < r.cfg.MaxRounds {
		remaining := r.cfg.Timeout - spent
		if remaining <= 0 {
			fallback = "budget"
			r.note(func(s *Stats) { s.BudgetStops++ })
			break
		}
		req, requestCandidates := r.request(seq, ctx, entries, statSurface)
		if len(requestCandidates) == 0 {
			break
		}
		wait := r.cfg.PerRequestTimeout
		if wait > remaining {
			wait = remaining
		}
		callCtx, cancel := context.WithTimeout(context.Background(), wait)
		start := time.Now()
		result, err := r.verifier.Verify(callCtx, req)
		spent += time.Since(start)
		cancel()
		rounds++
		if err != nil {
			fallback = fallbackReason(err)
			switch fallback {
			case "timeout":
				r.note(func(s *Stats) { s.Timeouts++ })
			case "unavailable":
				r.note(func(s *Stats) { s.Unavailable++ })
			default:
				r.note(func(s *Stats) { s.Errors++ })
			}
			break
		}
		if len(result.Verdicts) != len(requestCandidates) {
			fallback = "protocol"
			r.note(func(s *Stats) { s.Errors++ })
			break
		}
		verified++
		r.note(func(s *Stats) {
			s.Verified++
			s.Rounds++
			s.TotalUS += time.Since(start).Microseconds()
		})
		for i, verdict := range result.Verdicts {
			prefix := verdict.Prefix
			if prefix == "" {
				prefix = result.Prefix
			}
			requestCandidates[i].neural = verdict.Score
			requestCandidates[i].scored = true
			requestCandidates[i].passed = verdict.Passed && verdict.Score >= r.cfg.PassThreshold
			requestCandidates[i].prefix = prefix
		}
		r.fuse(entries, statSurface)
		best := r.best(entries)
		if best == nil {
			break
		}
		if best.passed {
			passed = true
			break
		}
		prefix := clampPrefix(best.prefix, best.res)
		if prefix == best.surface && prefix != "" {
			break
		}
		var added int
		entries, added = r.research(seq, ctx, entries, prefix)
		if added == 0 {
			break
		}
	}
	r.fuse(entries, statSurface)
	best := r.best(entries)
	if best == nil {
		return drafts
	}
	out := r.order(entries, best)
	changed := len(out) > 0 && decode.ResultSurface(out[0].Clauses) != decode.ResultSurface(drafts[0].Clauses)
	if changed && best.surface != statSurface {
		// MinGain 是相對統計第一名的融合分數門檻。
		if draft := findBySurface(entries, statSurface); draft != nil && draft.scored &&
			best.combined-draft.combined < r.cfg.MinGain {
			out = r.order(entries, draft)
			best = draft
			changed = false
		}
	}
	o := Observation{
		ReadingUnits: len(seq),
		Drafts:       len(drafts),
		Candidates:   len(entries),
		Rounds:       rounds,
		Verified:     verified,
		Elapsed:      time.Since(started),
		Fallback:     fallback,
		Passed:       passed,
		Selected:     changed,
		Changed:      changed,
	}
	if r.cfg.Observer != nil {
		r.cfg.Observer.Observe(o)
	}
	if len(out) == 0 {
		return drafts
	}
	return out
}

// desiredDrafts 取得最多 MaxCandidates 條統計候選。傳入的 drafts 可能只有
// Convert 要求的條數；用一個沒有 reranker 的淺層副本補足，避免遞迴。
func (r *Reranker) desiredDrafts(seq reading.Sequence, ctx lang.Context, drafts []lang.Result) []lang.Result {
	if len(drafts) >= r.cfg.MaxCandidates {
		return drafts
	}
	shallow := *r.dec
	shallow.Reranker = nil
	more, err := shallow.NBest(seq, ctx, r.cfg.MaxCandidates)
	if err != nil || len(more) <= len(drafts) {
		return drafts
	}
	return more
}

// collect 把統計結果轉成 entry，並檢查讀音忠實度。回傳統計第一名表記。
func (r *Reranker) collect(seq reading.Sequence, results []lang.Result) ([]*entry, string, bool) {
	entries := make([]*entry, 0, len(results))
	seen := map[string]bool{}
	for _, res := range results {
		if !resultMatches(res, seq) {
			continue
		}
		surface := decode.ResultSurface(res.Clauses)
		if surface == "" || seen[surface] {
			continue
		}
		seen[surface] = true
		entries = append(entries, &entry{res: res, surface: surface, stats: res.Score})
	}
	if len(entries) == 0 {
		return nil, "", false
	}
	return entries, entries[0].surface, true
}

func resultMatches(res lang.Result, seq reading.Sequence) bool {
	var got reading.Sequence
	for _, clause := range res.Clauses {
		if clause.Surface == "" {
			return false
		}
		got = append(got, clause.Reading...)
	}
	return reading.Key(got) == reading.Key(seq)
}

// request 建立下一次 verify。Draft 固定是統計第一名，候選依目前融合順序
// 取前 MaxCandidates 條。
func (r *Reranker) request(seq reading.Sequence, ctx lang.Context, entries []*entry, statSurface string) (Request, []*entry) {
	left := []rune(ctx.Left)
	if len(left) > r.cfg.MaxLeftContextUnits {
		left = left[len(left)-r.cfg.MaxLeftContextUnits:]
	}
	req := Request{
		Revision:    int(r.revision.Add(1)),
		LeftContext: string(left),
		Reading:     append(reading.Sequence(nil), seq...),
	}
	ordered := append([]*entry(nil), entries...)
	// 第一輪全部未評分，依統計分數排序。重搜之後新候選尚未評分，
	// 優先送進去，否則它們永遠排不進 MaxCandidates。
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].scored != ordered[j].scored {
			return !ordered[i].scored
		}
		if ordered[i].scored {
			if ordered[i].combined != ordered[j].combined {
				return ordered[i].combined > ordered[j].combined
			}
			return ordered[i].surface < ordered[j].surface
		}
		if ordered[i].stats != ordered[j].stats {
			return ordered[i].stats > ordered[j].stats
		}
		return ordered[i].surface < ordered[j].surface
	})
	cands := make([]*entry, 0, r.cfg.MaxCandidates)
	for _, e := range ordered {
		cand, ok := candidateOf(e.res, seq)
		if !ok {
			continue
		}
		if e.surface == statSurface {
			c := cand
			req.Draft = &c
		}
		if len(cands) >= r.cfg.MaxCandidates {
			continue
		}
		req.Candidates = append(req.Candidates, cand)
		cands = append(cands, e)
	}
	if req.Draft == nil {
		return Request{}, nil
	}
	return req, cands
}

func candidateOf(res lang.Result, seq reading.Sequence) (Candidate, bool) {
	if !resultMatches(res, seq) {
		return Candidate{}, false
	}
	c := Candidate{Surface: decode.ResultSurface(res.Clauses)}
	for _, clause := range res.Clauses {
		c.Clauses = append(c.Clauses, Clause{
			Reading: append(reading.Sequence(nil), clause.Reading...),
			Surface: clause.Surface,
		})
		c.Reading = append(c.Reading, clause.Reading...)
	}
	return c, c.Surface != "" && reading.Key(c.Reading) == reading.Key(seq)
}

// fuse 重新計算融合分數。統計分數以本批候選的範圍正規化到 [0,1]。
func (r *Reranker) fuse(entries []*entry, statSurface string) {
	min, max := 0.0, 0.0
	first := true
	for _, e := range entries {
		if first || e.stats < min {
			min = e.stats
		}
		if first || e.stats > max {
			max = e.stats
		}
		first = false
	}
	span := max - min
	for _, e := range entries {
		norm := 0.5
		if span > 0 {
			norm = (e.stats - min) / span
		}
		e.combined = r.cfg.Weights.Neural*e.neural + r.cfg.Weights.Stats*norm
		if e.surface == statSurface {
			e.combined += r.cfg.Weights.Draft
		}
	}
}

// best 回傳已評分候選中融合分數最高者。同分時依表記排序，確保可重現。
func (r *Reranker) best(entries []*entry) *entry {
	var best *entry
	for _, e := range entries {
		if !e.scored {
			continue
		}
		if best == nil || e.combined > best.combined ||
			e.combined == best.combined && e.surface < best.surface {
			best = e
		}
	}
	return best
}

func findBySurface(entries []*entry, surface string) *entry {
	for _, e := range entries {
		if e.surface == surface {
			return e
		}
	}
	return nil
}

// order 把已評分候選依融合分數排序，未評分候選留在統計順序後方。
func (r *Reranker) order(entries []*entry, first *entry) []lang.Result {
	scored := make([]*entry, 0, len(entries))
	unscored := make([]*entry, 0, len(entries))
	for _, e := range entries {
		if e.scored {
			scored = append(scored, e)
		} else {
			unscored = append(unscored, e)
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i] == first {
			return true
		}
		if scored[j] == first {
			return false
		}
		if scored[i].combined != scored[j].combined {
			return scored[i].combined > scored[j].combined
		}
		return scored[i].surface < scored[j].surface
	})
	sort.SliceStable(unscored, func(i, j int) bool {
		if unscored[i].stats != unscored[j].stats {
			return unscored[i].stats > unscored[j].stats
		}
		return unscored[i].surface < unscored[j].surface
	})
	out := make([]lang.Result, 0, len(entries))
	for _, e := range append(scored, unscored...) {
		out = append(out, e.res)
	}
	return out
}

// research 用模型的高機率前綴做限制式重搜，回傳更新後的候選與新增數。
func (r *Reranker) research(seq reading.Sequence, ctx lang.Context, entries []*entry, prefix string) ([]*entry, int) {
	known := map[string]bool{}
	for _, e := range entries {
		known[e.surface] = true
	}
	results := r.search(seq, ctx, prefix, r.cfg.ResearchCandidates)
	added := 0
	for _, res := range results {
		if !resultMatches(res, seq) {
			continue
		}
		surface := decode.ResultSurface(res.Clauses)
		if surface == "" || known[surface] {
			continue
		}
		known[surface] = true
		entries = append(entries, &entry{res: res, surface: surface, stats: res.Score})
		added++
	}
	return entries, added
}

func fallbackReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), errors.Is(err, ErrTimeout):
		return "timeout"
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	case errors.Is(err, ErrUnsupported):
		return "unsupported"
	default:
		return "error"
	}
}

// clampPrefix 把模型前綴裁到候選的完整詞邊界。前綴若切在詞中間，重搜
// 無法在 lattice 上對齊，寧可少固定一個詞。
func clampPrefix(prefix string, res lang.Result) string {
	if prefix == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(prefix)
	for _, clause := range res.Clauses {
		segs := clause.Segments
		if len(segs) == 0 {
			segs = []lang.CandidateSegment{{Reading: clause.Reading, Surface: clause.Surface}}
		}
		for _, seg := range segs {
			next := b.String() + seg.Surface
			n := len([]rune(next))
			if n > len(runes) || string(runes[:n]) != next {
				return b.String()
			}
			b.WriteString(seg.Surface)
			if n == len(runes) {
				return b.String()
			}
		}
	}
	return b.String()
}
