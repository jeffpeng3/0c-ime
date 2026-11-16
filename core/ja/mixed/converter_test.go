package mixed_test

import (
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/mixed"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

type fakeConverter struct {
	calls        []reading.Sequence
	candidateSeq reading.Sequence
	candidate    lang.Span
	prefixCalls  []reading.Sequence
	prefix       []lang.Candidate
}

func (f *fakeConverter) Convert(seq reading.Sequence, _ lang.Context) (lang.Result, error) {
	f.calls = append(f.calls, append(reading.Sequence(nil), seq...))
	return lang.Result{Clauses: []lang.Clause{{
		Reading:  append(reading.Sequence(nil), seq...),
		Surface:  unitsText(seq),
		Segments: []lang.CandidateSegment{{Reading: append(reading.Sequence(nil), seq...), Surface: unitsText(seq)}},
		Score:    1,
	}}, Score: 1}, nil
}

func (f *fakeConverter) Candidates(seq reading.Sequence, span lang.Span, _ lang.Context) ([]lang.Candidate, error) {
	f.candidateSeq = append(reading.Sequence(nil), seq...)
	f.candidate = span
	return []lang.Candidate{{
		Surface: "今日",
		Reading: append(reading.Sequence(nil), seq[span.Start:span.End()]...),
		Source:  lang.SourceConvert,
	}}, nil
}

func (f *fakeConverter) Prefixes(seq reading.Sequence) []lang.Candidate {
	f.prefixCalls = append(f.prefixCalls, append(reading.Sequence(nil), seq...))
	return f.prefix
}

func TestConverterKeepsLiteralRunsOutOfJapaneseConversion(t *testing.T) {
	fake := &fakeConverter{}
	c := mixed.NewConverter(fake, mixed.Config{})
	seq := append(units("きょうは"), units("API")...)
	seq = append(seq, units("です")...)

	result, err := c.Convert(seq, lang.Context{Left: "前"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("底層收到 %d 段，want 2：%q", len(fake.calls), fake.calls)
	}
	for _, call := range fake.calls {
		if strings.ContainsFunc(unitsText(call), func(r rune) bool { return r <= 0x7f }) {
			t.Fatalf("ASCII 被送進日文変換：%q", unitsText(call))
		}
	}
	if len(result.Clauses) != 3 {
		t.Fatalf("文節數 = %d, want 3：%#v", len(result.Clauses), result.Clauses)
	}
	if result.Clauses[1].Surface != "API" {
		t.Fatalf("中間文節 = %q, want API", result.Clauses[1].Surface)
	}
	if got := resultSurface(result.Clauses); got != "きょうはAPIです" {
		t.Fatalf("整句 = %q", got)
	}
}

func TestConverterURLIsNeverConverted(t *testing.T) {
	fake := &fakeConverter{}
	c := mixed.NewConverter(fake, mixed.Config{})
	const url = "https://example.com/a?b=1#x"
	seq := append(units("URLは"), units(url)...)
	seq = append(seq, units("です")...)
	result, err := c.Convert(seq, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("底層呼叫 = %d, want 2", len(fake.calls))
	}
	got := resultSurface(result.Clauses)
	if !strings.Contains(got, url) {
		t.Fatalf("網址被改動：%q", got)
	}
	if got != "URLは"+url+"です" {
		t.Fatalf("整句 = %q", got)
	}
}

func TestConverterSpacing(t *testing.T) {
	seq := append(units("今日は"), units("hello")...)
	seq = append(seq, units("世界")...)

	none := mixed.NewConverter(&fakeConverter{}, mixed.Config{Spacing: mixed.SpacingNone})
	res, err := none.Convert(seq, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultSurface(res.Clauses); got != "今日はhello世界" {
		t.Fatalf("預設空白 = %q，日文預設不應補空白", got)
	}

	always := mixed.NewConverter(&fakeConverter{}, mixed.Config{Spacing: mixed.SpacingAlways})
	res, err = always.Convert(seq, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultSurface(res.Clauses); got != "今日は hello 世界" {
		t.Fatalf("設定加空白 = %q", got)
	}
}

func TestConverterCandidateSpanMapping(t *testing.T) {
	fake := &fakeConverter{}
	c := mixed.NewConverter(fake, mixed.Config{})
	seq := append(units("きょう"), units("hel")...)

	literal, err := c.Candidates(seq, lang.Span{Start: 3, Length: 3}, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 0 {
		t.Fatal("英文候選不應呼叫底層 converter")
	}
	if len(literal) < 2 || literal[0].Surface != "hel" || literal[0].Source != mixed.SourceLiteral {
		t.Fatalf("英文候選 = %#v", literal)
	}
	found := false
	for _, cand := range literal {
		if cand.Surface == "hello" && cand.Source == mixed.SourceEnglish {
			found = true
		}
	}
	if !found {
		t.Fatalf("英文候選缺少 hello：%#v", literal)
	}

	japanese, err := c.Candidates(seq, lang.Span{Start: 0, Length: 3}, lang.Context{Left: "前"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.candidate != (lang.Span{Start: 0, Length: 3}) {
		t.Fatalf("底層 span = %#v", fake.candidate)
	}
	if unitsText(fake.candidateSeq) != "きょう" {
		t.Fatalf("底層 seq = %q", unitsText(fake.candidateSeq))
	}
	if len(japanese) != 1 || japanese[0].Surface != "今日" {
		t.Fatalf("日文候選 = %#v", japanese)
	}
}

func TestConverterURLCandidatesOnlyLiteral(t *testing.T) {
	c := mixed.NewConverter(&fakeConverter{}, mixed.Config{})
	const url = "https://example.com/x"
	cands, err := c.Candidates(units(url), lang.Span{Start: 0, Length: len([]rune(url))}, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Surface != url {
		t.Fatalf("網址候選 = %#v", cands)
	}
}

func TestConverterWidthCandidates(t *testing.T) {
	c := mixed.NewConverter(&fakeConverter{}, mixed.Config{})
	cands, err := c.Candidates(units("API"), lang.Span{Start: 0, Length: 3}, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cand := range cands {
		if cand.Surface == "ＡＰＩ" && cand.Annotation == mixed.AnnotationWidth {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少全角英數候選：%#v", cands)
	}
}

func TestConverterPrefixes(t *testing.T) {
	fake := &fakeConverter{prefix: []lang.Candidate{{Surface: "今日", Source: lang.SourcePredict}}}
	c := mixed.NewConverter(fake, mixed.Config{})
	if got := c.Prefixes(units("きょう")); len(got) != 1 || got[0].Surface != "今日" {
		t.Fatalf("日文 prefix = %#v", got)
	}
	if len(fake.prefixCalls) != 1 || unitsText(fake.prefixCalls[0]) != "きょう" {
		t.Fatalf("底層 prefix seq = %#v", fake.prefixCalls)
	}
	if got := c.Prefixes(units("hel")); len(got) == 0 || got[0].Surface != "hello" {
		t.Fatalf("英文 prefix = %#v", got)
	}
}

func TestConverterEmpty(t *testing.T) {
	c := mixed.NewConverter(&fakeConverter{}, mixed.Config{})
	res, err := c.Convert(nil, lang.Context{})
	if err != nil || len(res.Clauses) != 0 {
		t.Fatalf("空序列 = %#v, %v", res, err)
	}
}

func TestConverterNoBaseFallsBackToReading(t *testing.T) {
	c := mixed.NewConverter(nil, mixed.Config{})
	res, err := c.Convert(append(units("きょう"), units("API")...), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultSurface(res.Clauses); got != "きょうAPI" {
		t.Fatalf("沒有底層 converter 時 = %q", got)
	}
}

func units(text string) reading.Sequence {
	out := make(reading.Sequence, 0, len(text))
	for _, r := range text {
		out = append(out, reading.Unit(string(r)))
	}
	return out
}

func unitsText(seq reading.Sequence) string {
	var b strings.Builder
	for _, u := range seq {
		b.WriteString(string(u))
	}
	return b.String()
}

func resultSurface(clauses []lang.Clause) string {
	var b strings.Builder
	for _, c := range clauses {
		b.WriteString(c.Surface)
	}
	return b.String()
}
