package rewrite

import (
	"strings"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// SourceRewrite 是かな與數字表記候選的來源標記。外殼不得假設只有
// core/lang 內建的來源值。
const SourceRewrite = "rewrite"

// DefaultMaxCandidates 是單次 Candidates 呼叫最多回傳的補助候選數。
const DefaultMaxCandidates = 40

// かな候選的註解。
const (
	AnnotationHiragana       = "ひらがな"
	AnnotationKatakana       = "カタカナ"
	AnnotationHalfwidthKana  = "半角カタカナ"
	AnnotationFullwidthLatin = "全角英数"
	AnnotationHalfwidthLatin = "半角英数"
)

// Rewriter 產生日文変換的補助候選。
//
// 零值可用，但不含記号辭典；通常用 New 或 NewWithSymbols 建立。
type Rewriter struct {
	// Symbols 是記号與顔文字辭典；nil 表示不產生這類候選。
	Symbols *Symbols
	// MaxCandidates 是單次輸出的上限；0 表示 DefaultMaxCandidates。
	MaxCandidates int
}

// New 建立不含記号辭典的 rewriter。
func New() *Rewriter {
	return &Rewriter{MaxCandidates: DefaultMaxCandidates}
}

// NewWithSymbols 建立含記号與顔文字辭典的 rewriter。
func NewWithSymbols(symbols *Symbols) *Rewriter {
	return &Rewriter{Symbols: symbols, MaxCandidates: DefaultMaxCandidates}
}

type langCandidate struct {
	surface    string
	annotation string
}

// Candidates 依詞面與讀音產生補助候選。
//
// かな候選來自讀音，全角／半角英數與數字候選來自詞面，記号與顔文字
// 來自 Symbols 的読み查詢。回傳值不含主要轉換候選；外殼用 Append 把它們
// 接在主要候選之後。相同詞面只保留第一筆。
func (r *Rewriter) Candidates(surface string, seq reading.Sequence) []lang.Candidate {
	var out []lang.Candidate
	seen := make(map[string]struct{})
	add := func(text, annotation, source string) {
		if text == "" || text == surface {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		out = append(out, lang.Candidate{
			Surface:    text,
			Reading:    append(reading.Sequence(nil), seq...),
			Source:     source,
			Annotation: annotation,
		})
	}

	kana := readingText(seq)
	if hasKana(kana) {
		add(ToHiragana(kana), AnnotationHiragana, SourceRewrite)
		add(ToKatakana(kana), AnnotationKatakana, SourceRewrite)
		add(ToHalfwidthKana(kana), AnnotationHalfwidthKana, SourceRewrite)
	}

	if hasASCIIAlnum(surface) {
		add(ToFullwidthLatin(surface), AnnotationFullwidthLatin, SourceRewrite)
	}
	if hasFullwidthAlnum(surface) {
		add(ToHalfwidthLatin(surface), AnnotationHalfwidthLatin, SourceRewrite)
	}

	for _, c := range numberCandidates(surface) {
		add(c.surface, c.annotation, SourceRewrite)
	}

	for _, sym := range r.Symbols.Lookup(kana) {
		add(sym.Surface, sym.Annotation, lang.SourceSymbol)
	}

	limit := r.MaxCandidates
	if limit <= 0 {
		limit = DefaultMaxCandidates
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Append 把補助候選接在主要候選之後，主要候選的順序與分數不變。
//
// 補助候選若與主要候選同詞面會略過；其分數會被設在主要候選最低分之下，
// 這樣即使外殼依分數重排，主要 N-best 仍然在前。回傳新 slice，不修改輸入。
func Append(primary, extra []lang.Candidate) []lang.Candidate {
	out := make([]lang.Candidate, 0, len(primary)+len(extra))
	seen := make(map[string]struct{}, len(primary)+len(extra))
	floor := 0.0
	havePrimary := false
	for _, c := range primary {
		out = append(out, c)
		if c.Surface != "" {
			seen[c.Surface] = struct{}{}
		}
		if !havePrimary || c.Score < floor {
			floor = c.Score
			havePrimary = true
		}
	}

	added := 0
	for _, c := range extra {
		if c.Surface == "" {
			continue
		}
		if _, ok := seen[c.Surface]; ok {
			continue
		}
		seen[c.Surface] = struct{}{}
		added++
		if havePrimary {
			c.Score = floor - float64(added)
		} else {
			c.Score = -float64(added)
		}
		out = append(out, c)
	}
	return out
}

func readingText(seq reading.Sequence) string {
	var b strings.Builder
	for _, u := range seq {
		b.WriteString(string(u))
	}
	return b.String()
}

func hasKana(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'ぁ' && r <= 'ゖ',
			r >= 'ァ' && r <= 'ヺ',
			r >= 'ｦ' && r <= 'ﾟ',
			r == 'ー', r == '゛', r == '゜', r == 'ゝ', r == 'ゞ', r == 'ヽ', r == 'ヾ':
			return true
		}
	}
	return false
}

func hasASCIIAlnum(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

func hasFullwidthAlnum(s string) bool {
	for _, r := range s {
		if r >= '０' && r <= '９' || r >= 'Ａ' && r <= 'Ｚ' || r >= 'ａ' && r <= 'ｚ' {
			return true
		}
	}
	return false
}
