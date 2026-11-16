package lang_test

import (
	"testing"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// dummy 是只存在測試檔的實作，證明契約的介面可以組合出一個語言 profile。
type dummyProfile struct{}

var (
	_ lang.Profile    = dummyProfile{}
	_ lang.Composer   = (*dummyComposer)(nil)
	_ lang.Converter  = dummyConverter{}
	_ lang.KeyPolicy  = dummyPolicy{}
	_ lang.Punctuator = dummyPunctuator{}
)

func (dummyProfile) ID() lang.ID { return lang.Ja }

func (dummyProfile) InputMethods() []lang.InputMethod {
	return []lang.InputMethod{{ID: "romaji", Name: "ローマ字"}}
}

func (dummyProfile) Limits() lang.Limits { return lang.DefaultLimits() }

func (dummyProfile) KeyPolicy() lang.KeyPolicy { return dummyPolicy{} }

func (dummyProfile) NewComposer(lang.InputMethod) lang.Composer { return &dummyComposer{} }

func (dummyProfile) NewConverter() lang.Converter { return dummyConverter{} }

// 正式實作可以回傳 nil，表示這個語言沒有專用標點器。
func (dummyProfile) NewPunctuator() lang.Punctuator { return nil }

// dummyComposer 把 "k" 當成未完成的輸入，"a" 把 "k" 完成成 "か"；其餘單一字元直接成為單位。
type dummyComposer struct {
	units   reading.Sequence
	pending string
}

func (c *dummyComposer) Insert(key string) (lang.ComposeResult, bool) {
	switch {
	case key == "k":
		c.pending += "k"
	case key == "a" && c.pending == "k":
		c.pending = ""
		c.units = append(c.units, "か")
		return lang.ComposeResult{Completed: reading.Sequence{"か"}}, true
	case utf8.RuneCountInString(key) == 1 && key != "":
		c.units = append(c.units, reading.Unit(key))
		return lang.ComposeResult{Completed: reading.Sequence{reading.Unit(key)}}, true
	default:
		return lang.ComposeResult{Pending: c.pending}, false
	}
	return lang.ComposeResult{Pending: c.pending}, true
}

func (c *dummyComposer) Backspace() bool {
	if c.pending != "" {
		c.pending = c.pending[:len(c.pending)-1]
		return true
	}
	if len(c.units) == 0 {
		return false
	}
	c.units = c.units[:len(c.units)-1]
	return true
}

func (c *dummyComposer) Reset() { *c = dummyComposer{} }

func (c *dummyComposer) Units() reading.Sequence {
	return append(reading.Sequence(nil), c.units...)
}

func (c *dummyComposer) Pending() string { return c.pending }

func (c *dummyComposer) Segments() []lang.DisplaySegment {
	segs := make([]lang.DisplaySegment, 0, len(c.units)+1)
	for _, u := range c.units {
		segs = append(segs, lang.DisplaySegment{Reading: reading.Sequence{u}, Text: string(u)})
	}
	if c.pending != "" {
		segs = append(segs, lang.DisplaySegment{
			Reading: reading.Sequence{reading.Unit(c.pending)},
			Text:    c.pending,
			Pending: true,
		})
	}
	return segs
}

type dummyConverter struct{}

func (dummyConverter) Convert(seq reading.Sequence, _ lang.Context) (lang.Result, error) {
	return lang.Result{
		Clauses: []lang.Clause{{
			Reading:  seq,
			Surface:  "今日は",
			Segments: []lang.CandidateSegment{{Reading: seq, Surface: "今日は"}},
			Score:    1,
		}},
		Score: 1,
	}, nil
}

func (dummyConverter) Candidates(seq reading.Sequence, span lang.Span, _ lang.Context) ([]lang.Candidate, error) {
	if span.Start < 0 || span.Length <= 0 || span.End() > len(seq) {
		return nil, nil
	}
	spanReading := append(reading.Sequence(nil), seq[span.Start:span.End()]...)
	return []lang.Candidate{{
		Surface:    "今日",
		Reading:    spanReading,
		Clauses:    []lang.Clause{{Reading: spanReading, Surface: "今日"}},
		Score:      1,
		Source:     lang.SourceConvert,
		Annotation: "test",
	}}, nil
}

func (dummyConverter) Prefixes(reading.Sequence) []lang.Candidate { return nil }

type dummyPolicy struct{}

func (dummyPolicy) Handle(key string, state lang.KeyState) (lang.Action, bool) {
	switch key {
	case "Enter":
		if state.Composing {
			return lang.ActionCommit, true
		}
	case "Escape":
		if state.Composing {
			return lang.ActionCancel, true
		}
	case "Convert":
		if state.Composing {
			return lang.ActionConvert, true
		}
	case "Shift+Tab":
		if state.ClauseCount > 0 {
			return lang.ActionPrevClause, true
		}
	}
	if utf8.RuneCountInString(key) == 1 {
		return lang.ActionInsert, true
	}
	return lang.ActionNone, false
}

type dummyPunctuator struct{}

func (dummyPunctuator) Insert(text string) string { return "、" + text }

func TestDummyProfileComposes(t *testing.T) {
	p := dummyProfile{}
	if p.ID() != lang.Ja {
		t.Fatalf("ID() = %q", p.ID())
	}
	if got := p.InputMethods(); len(got) != 1 || got[0].ID != "romaji" {
		t.Fatalf("InputMethods() = %#v", got)
	}
	if p.NewPunctuator() != nil {
		t.Fatal("沒有標點器時應回傳 nil")
	}
	c := p.NewComposer(p.InputMethods()[0])
	if res, ok := c.Insert("k"); !ok || res.Pending != "k" || len(res.Completed) != 0 {
		t.Fatalf("Insert(k) = %#v, %v", res, ok)
	}
	res, ok := c.Insert("a")
	if !ok || reading.Key(res.Completed) != "か" || res.Pending != "" {
		t.Fatalf("Insert(a) = %#v, %v", res, ok)
	}
	if got := reading.Key(c.Units()); got != "か" {
		t.Fatalf("Units() = %q", got)
	}
	c.Insert("k")
	segs := c.Segments()
	if len(segs) != 2 || segs[0].Text != "か" || !segs[1].Pending || segs[1].Text != "k" {
		t.Fatalf("Segments() = %#v", segs)
	}
	if !c.Backspace() || c.Pending() != "" {
		t.Fatalf("Backspace() 後 Pending = %q", c.Pending())
	}
	if !c.Backspace() || len(c.Units()) != 0 {
		t.Fatalf("Backspace() 後 Units = %#v", c.Units())
	}
	if c.Backspace() {
		t.Fatal("空 composer 的 Backspace 應回傳 false")
	}
	c.Insert("き")
	c.Reset()
	if c.Pending() != "" || len(c.Units()) != 0 {
		t.Fatalf("Reset() 後仍有內容：%#v", c.Segments())
	}
}

func TestDummyProfileConverts(t *testing.T) {
	conv := dummyProfile{}.NewConverter()
	seq := reading.Sequence{"き", "ょ", "う", "は"}
	ctx := lang.Context{Left: "今日", Right: ""}
	res, err := conv.Convert(seq, ctx)
	if err != nil || len(res.Clauses) != 1 || res.Clauses[0].Surface != "今日は" {
		t.Fatalf("Convert() = %#v, %v", res, err)
	}
	cands, err := conv.Candidates(seq, lang.Span{Start: 0, Length: 2}, ctx)
	if err != nil || len(cands) != 1 {
		t.Fatalf("Candidates() = %#v, %v", cands, err)
	}
	if reading.Key(cands[0].Reading) != "き ょ" || cands[0].Source != lang.SourceConvert {
		t.Fatalf("Candidate = %#v", cands[0])
	}
	if _, err := conv.Candidates(seq, lang.Span{Start: 3, Length: 2}, ctx); err != nil {
		t.Fatalf("超出範圍的 span 應回空，得到錯誤 %v", err)
	}
	if got := conv.Prefixes(seq); got != nil {
		t.Fatalf("Prefixes() = %#v, want nil", got)
	}
}

func TestDummyProfileKeyPolicy(t *testing.T) {
	p := dummyPolicy{}
	if act, ok := p.Handle("Enter", lang.KeyState{Composing: true}); act != lang.ActionCommit {
		t.Fatalf("Enter: %v %v", act, ok)
	}
	if act, ok := p.Handle("Enter", lang.KeyState{}); act != lang.ActionNone {
		t.Fatalf("空狀態的 Enter: %v %v", act, ok)
	}
	if act, ok := p.Handle("Escape", lang.KeyState{Composing: true}); act != lang.ActionCancel {
		t.Fatalf("Escape: %v %v", act, ok)
	}
	if act, ok := p.Handle("Convert", lang.KeyState{Composing: true}); act != lang.ActionConvert {
		t.Fatalf("Convert: %v %v", act, ok)
	}
	if act, ok := p.Handle("Shift+Tab", lang.KeyState{ClauseCount: 2, ClauseIndex: 1}); act != lang.ActionPrevClause {
		t.Fatalf("Shift+Tab: %v %v", act, ok)
	}
	if act, ok := p.Handle("あ", lang.KeyState{}); act != lang.ActionInsert {
		t.Fatalf("かな: %v %v", act, ok)
	}
}

func TestLimitsAndSpan(t *testing.T) {
	lim := lang.DefaultLimits()
	if lim.MaxReadingUnits != 64 || lim.MaxWordUnits != 32 || lim.MaxCandidates != 16 {
		t.Fatalf("DefaultLimits() = %#v", lim)
	}
	if got := (lang.Span{Start: 3, Length: 2}).End(); got != 5 {
		t.Fatalf("End() = %d, want 5", got)
	}
}
