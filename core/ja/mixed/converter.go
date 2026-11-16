package mixed

import (
	"strings"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Converter 包住日文 converter，把讀音序列切成日文段與照字面段。
//
// 只有日文段會交給底層 converter；英文、網址、email、識別字與全角英數都
// 直接變成一個文節，因此不會被かな漢字変換，也不會擾動前後日文的文節。
type Converter struct {
	base    lang.Converter
	english *English
	cfg     Config
}

var _ lang.Converter = (*Converter)(nil)

// NewConverter 建立混打轉換器。base 為 nil 時，日文段會原樣以讀音顯示，
// 方便測試或沒有詞典的環境。
func NewConverter(base lang.Converter, cfg Config) *Converter {
	cfg = cfg.normalized()
	return &Converter{base: base, english: cfg.Vocabulary, cfg: cfg}
}

// Convert 回傳整段讀音的轉換結果。
func (c *Converter) Convert(seq reading.Sequence, ctx lang.Context) (lang.Result, error) {
	if c == nil || len(seq) == 0 {
		return lang.Result{}, nil
	}
	parts := splitParts(seq)
	clauses := make([]lang.Clause, 0, len(parts))
	left := ctx.Left
	score := 0.0
	for _, p := range parts {
		sub := seq[p.start : p.start+p.length]
		if p.literal {
			text := sequenceText(sub)
			clauses = append(clauses, literalClause(sub, text))
			left += text
			continue
		}
		if c.base == nil {
			text := sequenceText(sub)
			clauses = append(clauses, literalClause(sub, text))
			left += text
			continue
		}
		res, err := c.base.Convert(sub, lang.Context{Left: left, Right: ctx.Right})
		if err != nil {
			return lang.Result{}, err
		}
		if len(res.Clauses) == 0 {
			text := sequenceText(sub)
			clauses = append(clauses, literalClause(sub, text))
			left += text
			continue
		}
		clauses = append(clauses, res.Clauses...)
		score += res.Score
		left += resultSurface(res.Clauses)
	}
	clauses = c.applySpacing(clauses)
	return lang.Result{Clauses: clauses, Score: score}, nil
}

// Candidates 回傳指定範圍的候選。範圍在照字面段時只列原樣與英文補完；
// 範圍在日文段時把區域範圍交給底層 converter。
func (c *Converter) Candidates(seq reading.Sequence, span lang.Span, ctx lang.Context) ([]lang.Candidate, error) {
	if c == nil || span.Start < 0 || span.Length <= 0 || span.End() > len(seq) {
		return nil, nil
	}
	for _, p := range splitParts(seq) {
		if span.Start < p.start || span.End() > p.start+p.length {
			continue
		}
		if p.literal {
			return c.literalCandidates(seq[span.Start:span.End()]), nil
		}
		if c.base == nil {
			text := sequenceText(seq[span.Start:span.End()])
			return []lang.Candidate{{
				Surface: text,
				Reading: append(reading.Sequence(nil), seq[span.Start:span.End()]...),
				Source:  SourceLiteral,
				Score:   1,
			}}, nil
		}
		local := lang.Span{Start: span.Start - p.start, Length: span.Length}
		left := ctx.Left + sequenceText(seq[:p.start])
		return c.base.Candidates(seq[p.start:p.start+p.length], local, lang.Context{Left: left, Right: ctx.Right})
	}
	return nil, nil
}

// Prefixes 回傳可作為預測的詞面前綴。結尾是英數段時使用英文詞典；結尾是
// 日文段時交給底層 converter。
func (c *Converter) Prefixes(seq reading.Sequence) []lang.Candidate {
	if c == nil || len(seq) == 0 {
		return nil
	}
	parts := splitParts(seq)
	last := parts[len(parts)-1]
	text := sequenceText(seq[last.start : last.start+last.length])
	if last.literal {
		if !asciiLetters(text) {
			return nil
		}
		return c.english.Candidates(text, c.cfg.MaxCandidates)
	}
	if c.base == nil {
		return nil
	}
	return c.base.Prefixes(seq[last.start : last.start+last.length])
}

func (c *Converter) literalCandidates(sub reading.Sequence) []lang.Candidate {
	text := sequenceText(sub)
	limit := c.cfg.MaxCandidates
	out := []lang.Candidate{{
		Surface: text,
		Reading: append(reading.Sequence(nil), sub...),
		Score:   1,
		Source:  SourceLiteral,
	}}
	seen := map[string]bool{text: true}
	add := func(surface, source, annotation string, score float64) {
		if surface == "" || seen[surface] || len(out) >= limit {
			return
		}
		seen[surface] = true
		out = append(out, lang.Candidate{
			Surface:    surface,
			Reading:    append(reading.Sequence(nil), sub...),
			Score:      score,
			Source:     source,
			Annotation: annotation,
		})
	}
	kind := Classify(text, c.english)
	if kind == KindEnglish || kind == KindLatin {
		for _, cand := range c.english.Candidates(text, limit-len(out)) {
			add(cand.Surface, cand.Source, cand.Annotation, cand.Score/1e6)
		}
	}
	if kind != KindURL && kind != KindEmail && hasLatinAlnum(text) {
		if variant, ok := WidthVariant(text); ok {
			add(variant, SourceLiteral, AnnotationWidth, 0.5)
		}
	}
	return out
}

func (c *Converter) applySpacing(clauses []lang.Clause) []lang.Clause {
	if c.cfg.Spacing != SpacingAlways {
		return clauses
	}
	for i := 1; i < len(clauses); i++ {
		left, ok := lastRune(clauses[i-1].Surface)
		if !ok {
			continue
		}
		right, ok := firstRune(clauses[i].Surface)
		if !ok {
			continue
		}
		if NeedsSpaceBetween(left, right) {
			clauses[i].Surface = " " + clauses[i].Surface
		}
	}
	return clauses
}

type part struct {
	start, length int
	literal       bool
}

func splitParts(seq reading.Sequence) []part {
	var parts []part
	for i := 0; i < len(seq); {
		literal := isLiteralUnit(seq[i])
		j := i + 1
		for j < len(seq) && isLiteralUnit(seq[j]) == literal {
			j++
		}
		parts = append(parts, part{start: i, length: j - i, literal: literal})
		i = j
	}
	return parts
}

func isLiteralUnit(u reading.Unit) bool {
	s := string(u)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r <= unicode.MaxASCII || IsFullWidthAlnum(r) {
			continue
		}
		return false
	}
	return true
}

func literalClause(units reading.Sequence, text string) lang.Clause {
	return lang.Clause{
		Reading:  append(reading.Sequence(nil), units...),
		Surface:  text,
		Segments: []lang.CandidateSegment{{Reading: append(reading.Sequence(nil), units...), Surface: text}},
	}
}

func resultSurface(clauses []lang.Clause) string {
	var b strings.Builder
	for _, c := range clauses {
		b.WriteString(c.Surface)
	}
	return b.String()
}

func firstRune(s string) (rune, bool) {
	for _, r := range s {
		return r, true
	}
	return 0, false
}

func lastRune(s string) (rune, bool) {
	var last rune
	ok := false
	for _, r := range s {
		last, ok = r, true
	}
	return last, ok
}
