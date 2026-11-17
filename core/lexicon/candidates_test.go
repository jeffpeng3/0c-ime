package lexicon_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

func zhuyinQuery() lexicon.Query {
	return lexicon.Query{
		Span:     segment.Span{Start: 0, End: 2},
		Script:   segment.ScriptZhuyin,
		Reading:  reading.Sequence{"ㄓㄨˋ", "ㄧㄣ"},
		Language: lang.Tag(lang.Zh),
		Variant:  lang.VariantZhHant,
		Limit:    8,
	}
}

func TestQueryValid(t *testing.T) {
	if q := zhuyinQuery(); !q.Valid() {
		t.Fatalf("合法查詢被判為無效: %#v", q)
	}
	bad := map[string]lexicon.Query{
		"顛倒的 span": {Span: segment.Span{Start: 2, End: 1}},
		"負的 limit": {Span: segment.Span{Start: 0, End: 1}, Limit: -1},
		"非法讀音":     {Span: segment.Span{Start: 0, End: 1}, Reading: reading.Sequence{"x"}},
		"非法變體":     {Span: segment.Span{Start: 0, End: 1}, Variant: "zh-Hant-TW"},
		"空白語言":     {Span: segment.Span{Start: 0, End: 1}, Language: lang.Tag(" ")},
		"可信度超出範圍":  {Span: segment.Span{Start: 0, End: 1}, Language: lang.LanguageTag{Language: lang.Zh, Confidence: -0.1}},
	}
	for name, q := range bad {
		if q.Valid() {
			t.Errorf("%s：應為無效查詢: %#v", name, q)
		}
	}
}

func TestQueryKey(t *testing.T) {
	got := zhuyinQuery().Key()
	want := "[0,2)|zhuyin|ㄓㄨˋ ㄧㄣ|zh|zh-Hant"
	if got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
	// Limit 與可信度不改變查詢身分。
	q := zhuyinQuery()
	q.Limit = 0
	q.Language = lang.Tag(lang.Zh).WithConfidence(0.9)
	if q.Key() != got {
		t.Errorf("Limit／可信度不應影響 Key(): %q != %q", q.Key(), got)
	}
}

func TestQueryJSONRoundTrip(t *testing.T) {
	q := zhuyinQuery()
	q.Language = q.Language.WithConfidence(0.8)
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var got lexicon.Query
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, q) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, q)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"span", "script", "reading", "language", "variant", "limit"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON 缺少欄位 %q: %s", key, b)
		}
	}
}

func TestNewCandidateFillsFields(t *testing.T) {
	q := zhuyinQuery()
	e := lexicon.Entry{Surface: "注音", Reading: q.Reading, PRW: 0.5}
	cost := lattice.Cost{Dictionary: 1.5, Fuzzy: 0.25, Model: 0.75}

	c := q.Candidate(e, lattice.SourceDictionary, cost)
	if !c.Valid() {
		t.Fatalf("候選無效: %#v", c)
	}
	if c.Span != q.Span || c.Script != q.Script || c.ReadingKey() != reading.Key(q.Reading) {
		t.Errorf("input/reading 欄位錯誤: %#v", c)
	}
	if c.Surface != "注音" {
		t.Errorf("Surface = %q", c.Surface)
	}
	if c.Language != q.Language || c.Variant != q.Variant {
		t.Errorf("language/variant = %#v/%q", c.Language, c.Variant)
	}
	if c.Cost != cost || c.Source != lattice.SourceDictionary {
		t.Errorf("cost/source = %#v/%q", c.Cost, c.Source)
	}
}

func TestNewCandidateInfersLanguageAndVariant(t *testing.T) {
	ja := lexicon.Query{
		Span:    segment.Span{Start: 0, End: 2},
		Script:  segment.ScriptKana,
		Reading: reading.Sequence{"き", "ょ"},
	}
	c := ja.Candidate(lexicon.Entry{Surface: "今日", Reading: ja.Reading}, lattice.SourceDictionary, lattice.Cost{})
	if c.Language.Language != lang.Ja || c.Variant != lang.VariantJaShinjitai {
		t.Errorf("日文推斷錯誤: %#v", c)
	}

	zh := lexicon.Query{
		Span:    segment.Span{Start: 0, End: 1},
		Reading: reading.Sequence{"ㄓㄨˋ"},
	}
	if c := zh.Candidate(lexicon.Entry{Surface: "注", Reading: zh.Reading}, lattice.SourceDictionary, lattice.Cost{}); c.Language.Language != lang.Zh || c.Variant != lang.VariantZhHant {
		t.Errorf("中文推斷錯誤: %#v", c)
	}

	unknown := lexicon.Query{Span: segment.Span{Start: 0, End: 1}}
	c = unknown.Candidate(lexicon.Entry{Surface: "hello"}, lattice.SourceLiteral, lattice.Cost{})
	if !c.Language.IsZero() || c.Variant != "" {
		t.Errorf("無法判定時不應硬套語言: %#v", c)
	}
}

func TestNewCandidateHonorsExplicitVariant(t *testing.T) {
	q := zhuyinQuery()
	q.Variant = lang.VariantZhHans
	c := q.Candidate(lexicon.Entry{Surface: "注音", Reading: q.Reading}, lattice.SourcePersonal, lattice.Cost{})
	if c.Variant != lang.VariantZhHans {
		t.Errorf("Variant = %q", c.Variant)
	}
}

func TestCandidateSourceInterface(t *testing.T) {
	var src lexicon.CandidateSource = fakeSource{}
	q := zhuyinQuery()
	got := src.Candidates(q)
	if len(got) != 1 {
		t.Fatalf("Candidates() = %#v", got)
	}
	want := q.Candidate(lexicon.Entry{Surface: "注音", Reading: q.Reading}, lattice.SourceFuzzy, lattice.Cost{Fuzzy: 0.5})
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("candidate mismatch:\n got %#v\nwant %#v", got[0], want)
	}
}

type fakeSource struct{}

func (fakeSource) Candidates(q lexicon.Query) []lattice.Candidate {
	return []lattice.Candidate{
		q.Candidate(lexicon.Entry{Surface: "注音", Reading: q.Reading}, lattice.SourceFuzzy, lattice.Cost{Fuzzy: 0.5}),
	}
}
