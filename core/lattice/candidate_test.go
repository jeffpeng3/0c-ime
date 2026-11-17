package lattice_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

func testCandidate() lattice.Candidate {
	return lattice.Candidate{
		Segment: segment.Segment{
			Span:     segment.Span{Start: 0, End: 2},
			Script:   segment.ScriptZhuyin,
			Reading:  reading.Sequence{"ㄓㄨˋ", "ㄧㄣ"},
			Surface:  "注音",
			Language: lang.Tag(lang.Zh).WithConfidence(0.8),
			Variant:  lang.VariantZhHant,
		},
		Cost: lattice.Cost{
			Dictionary: 1.5,
			Fuzzy:      0.25,
			Model:      0.75,
		},
		Source: lattice.SourceDictionary,
	}
}

func TestCandidatePromotedFields(t *testing.T) {
	c := testCandidate()
	if !c.Valid() {
		t.Fatal("合法候選被判為無效")
	}
	if c.Span != (segment.Span{Start: 0, End: 2}) {
		t.Errorf("Span = %#v", c.Span)
	}
	if c.Surface != "注音" || c.Language.Language != lang.Zh || c.Variant != lang.VariantZhHant {
		t.Errorf("promoted fields 錯誤: %#v", c)
	}
	if c.Dictionary != 1.5 || c.Fuzzy != 0.25 || c.Model != 0.75 {
		t.Errorf("cost 錯誤: %#v", c.Cost)
	}
	if c.Source != lattice.SourceDictionary {
		t.Errorf("Source = %q", c.Source)
	}
}

func TestCandidateKey(t *testing.T) {
	got := testCandidate().Key()
	want := "[0,2)|ㄓㄨˋ ㄧㄣ|注音|zh|zh-Hant|dictionary"
	if got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
	if (lattice.Candidate{Segment: segment.Segment{Span: segment.Span{Start: 2, End: 1}}}).Valid() {
		t.Error("顛倒的 span 應為無效")
	}
}

func TestCandidateJSONIsFlat(t *testing.T) {
	c := testCandidate()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"span", "script", "reading", "surface", "language", "variant",
		"dictionary_cost", "fuzzy_cost", "model_score", "source",
	} {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON 缺少平坦欄位 %q: %s", key, b)
		}
	}
	for _, key := range []string{"Segment", "Cost"} {
		if _, ok := raw[key]; ok {
			t.Errorf("JSON 不應出現巢狀欄位 %q: %s", key, b)
		}
	}

	var got lattice.Candidate
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, c)
	}
}
