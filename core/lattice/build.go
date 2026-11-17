package lattice

import (
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

// ErrEmptyInput 表示沒有文字、片段或候選可以建立 lattice。
var ErrEmptyInput = errors.New("lattice: 沒有輸入")

// Input 是建立 lattice 的原始輸入與分段結果。
//
// Text 是原始文字，Span 以 rune 為單位；Segments 是腳本分段器的輸出。
// Text 可以留空，此時輸入長度由 Segments 的結尾決定；BuildFrom 需要 Text
// 才能把原始詞面交給候選來源（例如英文與專有名詞）。
type Input struct {
	Text     string            `json:"text,omitempty"`
	Segments []segment.Segment `json:"segments,omitempty"`
}

// End 回傳輸入的 rune 長度。
//
// 取 Text 長度與所有片段結尾的最大值，讓文字與分段暫時不一致時仍能建立
// 完整 lattice；超過的候選會在 Build 時被拒絕。
func (in Input) End() int {
	end := 0
	if in.Text != "" {
		end = utf8.RuneCountInString(in.Text)
	}
	for _, seg := range in.Segments {
		if seg.Span.End > end {
			end = seg.Span.End
		}
	}
	return end
}

// Query 是 lattice 對候選來源的查詢，對應一段連續輸入範圍。
//
// Query 與 lexicon.Query 的欄位語意相同，但 core/lattice 不能 import
// core/lexicon（lexicon 已經 import lattice，反向會形成循環）。整合層可用
// CandidateSourceFunc 把 lexicon.CandidateSource 轉接進來：
//
//	src := lattice.CandidateSourceFunc(func(q lattice.Query) []lattice.Candidate {
//		return lex.Candidates(lexicon.Query{
//			Span: q.Span, Script: q.Script, Reading: q.Reading,
//			Language: q.Language, Variant: q.Variant, Limit: q.Limit,
//		})
//	})
//
// 來源可以回傳多語言候選；Query.Language 只是分段器已知的提示，留空不代表
// 只能查詢單一語言。
type Query struct {
	// Span 是查詢涵蓋的原始輸入範圍。
	Span segment.Span `json:"span"`
	// Text 是這個範圍的原始文字。沒有原始文字時是空字串。
	Text string `json:"text,omitempty"`
	// Script 是範圍內主要書寫系統；混合時是 segment.ScriptMixed。
	Script segment.Script `json:"script,omitempty"`
	// Reading 是範圍的讀音。拉丁字母的讀音可以是空序列。
	Reading reading.Sequence `json:"reading,omitempty"`
	// Language 是分段器已知的語言提示；未判定時為零值。
	Language lang.LanguageTag `json:"language"`
	// Variant 是已知的輸出變體提示；未判定時是空字串。
	Variant lang.OutputVariant `json:"variant,omitempty"`
	// Limit 是候選數量上限，0 表示不限制。
	Limit int `json:"limit,omitempty"`
}

// CandidateSource 依 Query 回傳候選。回傳空切片或 nil 表示這個範圍沒有候選。
type CandidateSource interface {
	Candidates(Query) []Candidate
}

// CandidateSourceFunc 讓一般函式直接當 CandidateSource 使用。
type CandidateSourceFunc func(Query) []Candidate

// Candidates 實作 CandidateSource。
func (f CandidateSourceFunc) Candidates(q Query) []Candidate { return f(q) }

// Build 用既有的分段與候選建立 lattice。
//
// 候選可以跨越多個基礎片段：只要候選的 Span 合法，Viterbi 就能用一條邊
// 覆蓋整個範圍，輸出時自然合併成一個片段。Build 不查詢任何來源，適合已經
// 由前端準備好候選的情況。
//
// 無效、零長度或超出輸入範圍的候選會讓 Build 回傳錯誤，不會被默默丟棄。
// 相同 Candidate.Key 的候選只保留路徑成本較低者。
func Build(in Input, candidates []Candidate, cfg Config) (*Lattice, error) {
	if err := validateInput(in); err != nil {
		return nil, err
	}
	total := in.End()
	if total == 0 {
		total = candidateEnd(candidates)
	}
	if total == 0 {
		return nil, ErrEmptyInput
	}
	edges, err := normalize(candidates, total, cfg)
	if err != nil {
		return nil, err
	}
	return &Lattice{in: in, end: total, edges: edges, cfg: cfg}, nil
}

// BuildFrom 查詢來源並建立 lattice，支援長詞跨片段自動合併。
//
// 對每個基礎片段，BuildFrom 會依序查詢「一個片段、兩個片段、……最多
// Config.MaxMergeSegments 個片段」的連續範圍。來源若對合併範圍回傳候選，
// 這些候選就成為跨越多個片段的邊；Viterbi 選中時會自動把整段輸出成一個
// 片段，不需要另外執行合併步驟。
//
// 來源回傳的候選 Span 必須等於查詢 Span；不對應查詢範圍的候選會被忽略，
// 避免來源回傳過長或過短的詞條破壞覆蓋。Query.Text 讓英文詞典與專有名詞
// 可以依原文查詢。
func BuildFrom(in Input, src CandidateSource, cfg Config) (*Lattice, error) {
	if src == nil {
		return nil, errors.New("lattice: 候選來源是 nil")
	}
	if err := validateInput(in); err != nil {
		return nil, err
	}
	if len(in.Segments) == 0 {
		return nil, errors.New("lattice: BuildFrom 需要基礎片段")
	}
	if in.End() == 0 {
		return nil, ErrEmptyInput
	}

	runes := []rune(in.Text)
	limit := cfg.MaxMergeSegments
	if limit <= 0 || limit > len(in.Segments) {
		limit = len(in.Segments)
	}

	var candidates []Candidate
	for i := range in.Segments {
		start := in.Segments[i].Span.Start
		var seq reading.Sequence
		for j := i; j < len(in.Segments) && j-i < limit; j++ {
			if j > i && in.Segments[j-1].Span.End != in.Segments[j].Span.Start {
				break
			}
			seq = append(seq, in.Segments[j].Reading...)
			span := segment.Span{Start: start, End: in.Segments[j].Span.End}
			q := Query{
				Span:     span,
				Text:     textAt(runes, span),
				Script:   rangeScript(in.Segments[i : j+1]),
				Reading:  append(reading.Sequence(nil), seq...),
				Language: rangeLanguage(in.Segments[i : j+1]),
				Variant:  rangeVariant(in.Segments[i : j+1]),
				Limit:    cfg.Limit,
			}
			for _, c := range src.Candidates(q) {
				if c.Span != span {
					continue
				}
				candidates = append(candidates, c)
			}
		}
	}
	return Build(in, candidates, cfg)
}

func validateInput(in Input) error {
	prevEnd := 0
	for i, seg := range in.Segments {
		if !seg.Valid() {
			return fmt.Errorf("lattice: 片段 %d 無效: %#v", i, seg.Span)
		}
		if seg.Span.Len() == 0 {
			return fmt.Errorf("lattice: 片段 %d 是零長度", i)
		}
		if i > 0 && seg.Span.Start < prevEnd {
			return fmt.Errorf("lattice: 片段 %d 與前一個片段重疊", i)
		}
		prevEnd = seg.Span.End
	}
	return nil
}

func candidateEnd(candidates []Candidate) int {
	end := 0
	for _, c := range candidates {
		if c.Span.End > end {
			end = c.Span.End
		}
	}
	return end
}

func normalize(candidates []Candidate, total int, cfg Config) ([]Candidate, error) {
	out := make([]Candidate, 0, len(candidates))
	seen := make(map[string]int, len(candidates))
	for i, c := range candidates {
		if !c.Valid() {
			return nil, fmt.Errorf("lattice: 候選 %d 無效: %q", i, c.Key())
		}
		if c.Span.Len() <= 0 {
			return nil, fmt.Errorf("lattice: 候選 %d 是零長度: %#v", i, c.Span)
		}
		if c.Span.Start < 0 || c.Span.End > total {
			return nil, fmt.Errorf("lattice: 候選 %d 的 span 超出輸入: %#v", i, c.Span)
		}
		key := c.Key()
		if idx, ok := seen[key]; ok {
			if cfg.CandidateCost(c) < cfg.CandidateCost(out[idx]) {
				out[idx] = c
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return edgeLess(out[i], out[j]) })
	return out, nil
}

func edgeLess(a, b Candidate) bool {
	if a.Span.Start != b.Span.Start {
		return a.Span.Start < b.Span.Start
	}
	if a.Span.End != b.Span.End {
		return a.Span.End < b.Span.End
	}
	return a.Key() < b.Key()
}

func textAt(runes []rune, span segment.Span) string {
	if len(runes) == 0 {
		return ""
	}
	start, end := span.Start, span.End
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return ""
	}
	return string(runes[start:end])
}

func rangeScript(segs []segment.Segment) segment.Script {
	script := segs[0].Script
	for _, seg := range segs[1:] {
		if seg.Script != script {
			return segment.ScriptMixed
		}
	}
	return script
}

func rangeLanguage(segs []segment.Segment) lang.LanguageTag {
	tag := segs[0].Language
	for _, seg := range segs[1:] {
		if seg.Language.Language != tag.Language {
			return lang.LanguageTag{}
		}
	}
	return tag
}

func rangeVariant(segs []segment.Segment) lang.OutputVariant {
	variant := segs[0].Variant
	for _, seg := range segs[1:] {
		if seg.Variant != variant {
			return ""
		}
	}
	return variant
}
