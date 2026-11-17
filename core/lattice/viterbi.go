package lattice

import (
	"sort"
	"strings"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/segment"
)

// Weights 是候選成本換算成路徑成本的權重。
//
// Dictionary 與 Fuzzy 越低越好，Model 越高越好；因此路徑成本是
// Dictionary*Dictionary + Fuzzy*Fuzzy - Model*Model。權重 0 表示忽略該項。
type Weights struct {
	Dictionary float64 `json:"dictionary"`
	Fuzzy      float64 `json:"fuzzy"`
	Model      float64 `json:"model"`
}

// DefaultWeights 是第一版的權重：三項等權。
func DefaultWeights() Weights { return Weights{Dictionary: 1, Fuzzy: 1, Model: 1} }

// Config 是多語言 Viterbi 的設定。
//
// Primary 只是先驗，不是硬性模式：它讓主要語言的邊少付一點成本，也讓涉及
// 主要語言的切換便宜一些，但不會排除任何語言。
type Config struct {
	// Primary 是使用者設定的主要語言；空值表示沒有偏好。
	Primary lang.ID `json:"primary,omitempty"`
	// SwitchCost 是兩個不同語言之間的切換成本。
	SwitchCost float64 `json:"switch_cost"`
	// PrimarySwitchCost 是切換其中一側為主要語言時的成本。
	// 通常小於 SwitchCost，讓主要語言的路徑更容易延續。
	PrimarySwitchCost float64 `json:"primary_switch_cost"`
	// PrimaryPrior 是主要語言每條邊的先驗折扣，從路徑成本中扣除。
	PrimaryPrior float64 `json:"primary_prior"`
	// UnknownPenalty 是沒有語言標籤的邊的額外成本。
	UnknownPenalty float64 `json:"unknown_penalty"`
	// Weights 是候選成本項的權重。
	Weights Weights `json:"weights"`
	// MaxMergeSegments 是 BuildFrom 查詢長詞時最多合併幾個基礎片段。
	// 0 或負數表示不限制，1 表示只查單一片段。
	MaxMergeSegments int `json:"max_merge_segments,omitempty"`
	// Limit 是每次來源查詢的候選上限，0 表示不限制。
	Limit int `json:"limit,omitempty"`
}

// DefaultConfig 是第一版的 Viterbi 設定。
//
// 預設沒有主要語言；切換成本 0.5，涉及主要語言時 0.1，主要語言先驗 0.05。
func DefaultConfig() Config {
	return Config{
		SwitchCost:        0.5,
		PrimarySwitchCost: 0.1,
		PrimaryPrior:      0.05,
		Weights:           DefaultWeights(),
	}
}

// CandidateCost 把單一候選的成本換算成路徑成本。
//
// 成本越低越好；模型分數越高，扣掉的就越多。沒有語言標籤的候選加上
// UnknownPenalty；主要語言候選扣掉 PrimaryPrior。這裡不包含切換成本。
func (c Config) CandidateCost(cand Candidate) float64 {
	w := c.Weights
	cost := w.Dictionary*cand.Dictionary + w.Fuzzy*cand.Fuzzy - w.Model*cand.Model
	switch {
	case cand.Language.IsZero():
		cost += c.UnknownPenalty
	case c.Primary != lang.Und && cand.Language.Language == c.Primary:
		cost -= c.PrimaryPrior
	}
	return cost
}

// TransitionCost 回傳從 from 接到 to 的語言切換成本。
//
// 同一語言、任一邊沒有語言標籤時都是 0。不同語言時，若主要語言是其中
// 一側就使用 PrimarySwitchCost，否則使用 SwitchCost。
func (c Config) TransitionCost(from, to lang.LanguageTag) float64 {
	if from.IsZero() || to.IsZero() || from.Language == to.Language {
		return 0
	}
	if c.Primary != lang.Und && (from.Language == c.Primary || to.Language == c.Primary) {
		return c.PrimarySwitchCost
	}
	return c.SwitchCost
}

// Lattice 是可搜尋的多語言 lattice。
//
// 邊一律帶語言標籤；路徑可以跨語言。Lattice 不保存任何「目前語言」狀態，
// 同一份 lattice 可以用不同 Config.Primary 重複搜尋而不改變內容。
type Lattice struct {
	in    Input
	end   int
	edges []Candidate
	cfg   Config
}

// End 回傳輸入的 rune 長度。
func (l *Lattice) End() int {
	if l == nil {
		return 0
	}
	return l.end
}

// Config 回傳建立 lattice 時使用的設定。
func (l *Lattice) Config() Config {
	if l == nil {
		return Config{}
	}
	return l.cfg
}

// Input 回傳建立 lattice 時的原始輸入副本。
func (l *Lattice) Input() Input {
	if l == nil {
		return Input{}
	}
	in := l.in
	in.Segments = append([]segment.Segment(nil), l.in.Segments...)
	return in
}

// Edges 回傳所有邊的副本，依 span 與穩定鍵排序。
func (l *Lattice) Edges() []Candidate {
	if l == nil {
		return nil
	}
	return append([]Candidate(nil), l.edges...)
}

// Path 是一條候選路徑。
//
// Edges 已依輸入順序排列，且每條邊的 Segment 就是輸出片段；長詞候選會以
// 單一條邊覆蓋多個基礎片段，因此不需要再合併。
type Path struct {
	Edges []Candidate `json:"edges"`
	// Cost 是整條路徑的 Viterbi 成本，越低越好。
	Cost float64 `json:"cost"`
}

// Segments 回傳路徑的片段副本。
func (p Path) Segments() []segment.Segment {
	out := make([]segment.Segment, len(p.Edges))
	for i := range p.Edges {
		out[i] = p.Edges[i].Segment
	}
	return out
}

// Surface 把路徑的詞面依序串接，不插入額外分隔。
func (p Path) Surface() string {
	var b strings.Builder
	for _, e := range p.Edges {
		b.WriteString(e.Surface)
	}
	return b.String()
}

// Key 回傳路徑的穩定鍵，供除重與決定性排序使用。
func (p Path) Key() string {
	if len(p.Edges) == 0 {
		return ""
	}
	parts := make([]string, len(p.Edges))
	for i, e := range p.Edges {
		parts[i] = e.Key()
	}
	return strings.Join(parts, "\x1f")
}

// Best 回傳成本最低的路徑。
func (l *Lattice) Best() (Path, bool) {
	paths := l.NBest(1)
	if len(paths) == 0 {
		return Path{}, false
	}
	return paths[0], true
}

// NBest 回傳最多 n 條不同的路徑，依成本由低到高排列。
//
// 這是 k-best Viterbi：DP 狀態是（輸入位置、最後一條邊的語言），因為切換
// 成本讓「到同一個位置但最後語言不同」的路徑不能互相比較。n <= 0 視為 1。
// 相同成本時先偏好邊數較少的路徑（長詞合併優先），再以 Path.Key 排序，
// 確保結果可重現。無法完整覆蓋輸入時回傳 nil。
func (l *Lattice) NBest(n int) []Path {
	if n <= 0 {
		n = 1
	}
	if l == nil || len(l.edges) == 0 || l.end <= 0 {
		return nil
	}

	byStart := make(map[int][]Candidate)
	positions := map[int]struct{}{0: {}, l.end: {}}
	for _, edge := range l.edges {
		byStart[edge.Span.Start] = append(byStart[edge.Span.Start], edge)
		positions[edge.Span.Start] = struct{}{}
		positions[edge.Span.End] = struct{}{}
	}
	ordered := make([]int, 0, len(positions))
	for pos := range positions {
		ordered = append(ordered, pos)
	}
	sort.Ints(ordered)

	// dp[pos][lastLanguage] 是到 pos 為止、最後一條邊語言為 lastLanguage 的
	// 前 n 條路徑。位置 0 的初始狀態沒有語言。
	dp := map[int]map[lang.ID][]Path{
		0: {lang.Und: {{}}},
	}
	for _, pos := range ordered {
		states := dp[pos]
		if len(states) == 0 {
			continue
		}
		for _, edge := range byStart[pos] {
			if edge.Span.End <= pos || edge.Span.End > l.end {
				continue
			}
			dest := dp[edge.Span.End]
			if dest == nil {
				dest = make(map[lang.ID][]Path)
				dp[edge.Span.End] = dest
			}
			edgeCost := l.cfg.CandidateCost(edge)
			lastLang := edge.Language.Language
			for from, paths := range states {
				step := l.cfg.TransitionCost(lang.LanguageTag{Language: from}, edge.Language) + edgeCost
				for _, p := range paths {
					next := Path{
						Edges: append(append([]Candidate(nil), p.Edges...), edge),
						Cost:  p.Cost + step,
					}
					dest[lastLang] = insertTop(dest[lastLang], next, n)
				}
			}
		}
	}

	var final []Path
	for _, paths := range dp[l.end] {
		final = append(final, paths...)
	}
	sort.SliceStable(final, func(i, j int) bool { return pathLess(final[i], final[j]) })
	if len(final) > n {
		final = final[:n]
	}
	if len(final) == 0 {
		return nil
	}
	return final
}

func insertTop(list []Path, p Path, n int) []Path {
	key := p.Key()
	for i := range list {
		if list[i].Key() != key {
			continue
		}
		if p.Cost < list[i].Cost {
			list[i] = p
			sort.SliceStable(list, func(a, b int) bool { return pathLess(list[a], list[b]) })
		}
		return list
	}
	list = append(list, p)
	sort.SliceStable(list, func(i, j int) bool { return pathLess(list[i], list[j]) })
	if len(list) > n {
		list = list[:n]
	}
	return list
}

func pathLess(a, b Path) bool {
	if a.Cost != b.Cost {
		return a.Cost < b.Cost
	}
	if len(a.Edges) != len(b.Edges) {
		return len(a.Edges) < len(b.Edges)
	}
	return a.Key() < b.Key()
}
