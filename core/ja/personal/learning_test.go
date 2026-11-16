package personal

import (
	"testing"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	jalex "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

func testEntry(surface string, cost float64, rs ...string) jalex.Entry {
	return jalex.Entry{Surface: surface, Reading: units(rs...), Cost: cost, POS: jalex.POSNoun}
}

func testDecoder(t *testing.T, entries ...jalex.Entry) *jadecode.Decoder {
	t.Helper()
	lex := jalex.NewMem()
	for _, e := range entries {
		if err := lex.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	return &jadecode.Decoder{Lex: lex}
}

func convertSurface(t *testing.T, c *Converter, seq reading.Sequence, left string) string {
	t.Helper()
	res, err := c.Convert(seq, lang.Context{Left: left})
	if err != nil {
		t.Fatal(err)
	}
	return jadecode.ResultSurface(res.Clauses)
}

func TestLearnedSelectionRanksFirst(t *testing.T) {
	dec := testDecoder(t, testEntry("今日", 1, "き", "ょ", "う"), testEntry("京", 1.5, "き", "ょ", "う"))
	st := NewStore()
	c := NewConverter(dec, NewLearner(st))
	seq := units("き", "ょ", "う")

	if got := convertSurface(t, c, seq, ""); got != "今日" {
		t.Fatalf("學習前 = %q, want 今日", got)
	}
	clause := []lang.Clause{{Reading: seq, Surface: "京"}}
	if n := c.Learner.LearnSelection(clause, ""); n != 1 {
		t.Fatalf("學習筆數 = %d", n)
	}
	if got := convertSurface(t, c, seq, ""); got != "京" {
		t.Fatalf("學習後 = %q, want 京", got)
	}
}

func TestDifferentLeftContextDoesNotReuseLearning(t *testing.T) {
	dec := testDecoder(t, testEntry("今日", 1, "き", "ょ", "う"), testEntry("京", 1.5, "き", "ょ", "う"))
	c := NewConverter(dec, NewLearner(NewStore()))
	seq := units("き", "ょ", "う")

	c.Learner.LearnSelection([]lang.Clause{{Reading: seq, Surface: "京"}}, "今日は")
	if got := convertSurface(t, c, seq, "今日は"); got != "京" {
		t.Fatalf("同前文 = %q, want 京", got)
	}
	if got := convertSurface(t, c, seq, "空気が"); got != "今日" {
		t.Fatalf("換前文不該沿用學習：%q, want 今日", got)
	}
	if got := convertSurface(t, c, seq, ""); got != "今日" {
		t.Fatalf("句首不該沿用句中學習：%q, want 今日", got)
	}
}

func TestLearningDoesNotOverrideClearStatisticalWinner(t *testing.T) {
	dec := testDecoder(t, testEntry("今日", 1, "き", "ょ", "う"), testEntry("京", 10, "き", "ょ", "う"))
	st := NewStore()
	c := NewConverter(dec, NewLearner(st))
	seq := units("き", "ょ", "う")
	for i := 0; i < 8; i++ {
		c.Learner.LearnSelection([]lang.Clause{{Reading: seq, Surface: "京"}}, "")
	}
	if got := convertSurface(t, c, seq, ""); got != "今日" {
		t.Fatalf("差距明顯時學習不該翻盤：%q, want 今日", got)
	}
	if e, _ := st.Lookup(seq, ""); e.Count != 8 {
		t.Fatalf("次數 = %d", e.Count)
	}
}

func TestCandidatesPutLearnedFirst(t *testing.T) {
	dec := testDecoder(t, testEntry("今日", 1, "き", "ょ", "う"), testEntry("京", 1.5, "き", "ょ", "う"))
	c := NewConverter(dec, NewLearner(NewStore()))
	seq := units("き", "ょ", "う")
	span := lang.Span{Start: 0, Length: 3}

	cands, err := c.Candidates(seq, span, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) < 2 || cands[0].Surface != "今日" {
		t.Fatalf("學習前候選 = %+v", cands)
	}
	c.Learner.LearnAccepted([]lang.Clause{{Reading: seq, Surface: "京"}}, "")
	cands, err = c.Candidates(seq, span, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) < 2 || cands[0].Surface != "京" {
		t.Fatalf("學習後候選 = %+v", cands)
	}
}

func TestLearningSourcesAreRecorded(t *testing.T) {
	seq := units("き", "ょ", "う")
	clause := []lang.Clause{{Reading: seq, Surface: "今日"}}
	for _, tc := range []struct {
		name   string
		learn  func(*Learner, []lang.Clause, string) int
		source string
	}{
		{"selection", (*Learner).LearnSelection, SourceSelection},
		{"clause", (*Learner).LearnClauseChange, SourceClause},
		{"accept", (*Learner).LearnAccepted, SourceAccept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := NewStore()
			if n := tc.learn(NewLearner(st), clause, "前"); n != 1 {
				t.Fatalf("學習筆數 = %d", n)
			}
			e, ok := st.Lookup(seq, "前")
			if !ok || e.Source != tc.source {
				t.Fatalf("條目 = %+v, %v", e, ok)
			}
		})
	}
}

func TestLearnClausesChainsLeftContextAndSegments(t *testing.T) {
	st := NewStore()
	l := NewLearner(st)
	clauses := []lang.Clause{
		{
			Reading: units("く", "う", "き"),
			Surface: "空気",
			Segments: []lang.CandidateSegment{
				{Reading: units("く", "う", "き"), Surface: "空気"},
			},
		},
		{
			Reading: units("が"),
			Surface: "が",
			Segments: []lang.CandidateSegment{
				{Reading: units("が"), Surface: "が"},
			},
		},
	}
	if n := l.LearnSelection(clauses, "今日"); n != 2 {
		t.Fatalf("學習筆數 = %d", n)
	}
	first, ok := st.Lookup(units("く", "う", "き"), "今日")
	if !ok || first.Surface != "空気" || len(first.Reading) != 1 || first.Reading[0] != "くうき" {
		t.Fatalf("第一個文節 = %+v, %v", first, ok)
	}
	second, ok := st.Lookup(units("が"), "空気")
	if !ok || second.Surface != "が" || len(second.Segments) != 1 || second.Segments[0] != "が" {
		t.Fatalf("第二個文節 = %+v, %v", second, ok)
	}
	if got := l.Score(clauses, "今日"); got <= 0 {
		t.Fatalf("同前文的整句加成 = %v，應大於 0", got)
	}
	if got := l.Score(clauses, "空気が"); got <= 0 {
		t.Fatalf("後續文節的前文仍是空気，加成 = %v", got)
	}
}

func TestCostOnlyAtSentenceStartWithMatchingContext(t *testing.T) {
	seq := units("き", "ょ", "う")
	st := NewStore()
	l := NewLearner(st)
	l.LearnSelection([]lang.Clause{{Reading: seq, Surface: "京"}}, "空気が")
	e := testEntry("京", 1.5, "き", "ょ", "う")

	if got := l.Cost(seq, 0, e, lang.Context{Left: "空気が"}); got >= 0 {
		t.Fatalf("同前文成本 = %v，應為負值", got)
	}
	if got := l.Cost(seq, 0, e, lang.Context{Left: "今日は"}); got != 0 {
		t.Fatalf("不同前文成本 = %v", got)
	}
	if got := l.Cost(seq, 1, e, lang.Context{Left: "空気が"}); got != 0 {
		t.Fatalf("句中不得套用句首前文：%v", got)
	}
	if got := l.Cost(seq, 0, testEntry("今日", 1, "き", "ょ", "う"), lang.Context{Left: "空気が"}); got != 0 {
		t.Fatalf("不同詞面成本 = %v", got)
	}
}

func TestLearnClausesAtRecordsTime(t *testing.T) {
	st := NewStore()
	l := NewLearner(st)
	clause := []lang.Clause{{Reading: units("あ"), Surface: "亜"}}
	if n := l.LearnClausesAt(clause, "", SourceAccept, 1760000123); n != 1 {
		t.Fatalf("學習筆數 = %d", n)
	}
	if e, ok := st.Lookup(units("あ"), ""); !ok || e.LastUsed != 1760000123 {
		t.Fatalf("last_used = %+v, %v", e, ok)
	}
}

func TestLearnCandidateWithoutClauses(t *testing.T) {
	st := NewStore()
	l := NewLearner(st)
	cand := lang.Candidate{Surface: "京", Reading: units("き", "ょ", "う"), Score: 1, Source: lang.SourceConvert}
	if n := l.LearnCandidate(cand, "今日", SourceClause); n != 1 {
		t.Fatalf("學習筆數 = %d", n)
	}
	e, ok := st.Lookup(units("き", "ょ", "う"), "今日")
	if !ok || e.Surface != "京" || e.Source != SourceClause {
		t.Fatalf("條目 = %+v, %v", e, ok)
	}
}

func TestNilStoreLearnerIsInert(t *testing.T) {
	l := NewLearner(nil)
	if n := l.LearnSelection([]lang.Clause{{Reading: units("あ"), Surface: "亜"}}, ""); n != 0 {
		t.Fatalf("學習筆數 = %d", n)
	}
	if got := l.Score([]lang.Clause{{Reading: units("あ"), Surface: "亜"}}, ""); got != 0 {
		t.Fatalf("加成 = %v", got)
	}
	if got := l.Cost(units("あ"), 0, testEntry("亜", 1, "あ"), lang.Context{}); got != 0 {
		t.Fatalf("成本 = %v", got)
	}
}

func TestConverterPrefixesDelegate(t *testing.T) {
	dec := testDecoder(t,
		testEntry("今日", 1, "き", "ょ", "う"),
		testEntry("今日は", 1, "き", "ょ", "う", "は"),
	)
	c := NewConverter(dec, NewLearner(NewStore()))
	got := c.Prefixes(units("き", "ょ"))
	if len(got) == 0 {
		t.Fatal("預測前綴不該因學習包裝而消失")
	}
}
