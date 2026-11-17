package lattice_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

func newCandidate(span segment.Span, surface string, id lang.ID, variant lang.OutputVariant, dict float64) lattice.Candidate {
	return lattice.Candidate{
		Segment: segment.Segment{
			Span:     span,
			Script:   segment.ScriptLatin,
			Surface:  surface,
			Language: lang.Tag(id),
			Variant:  variant,
		},
		Cost:   lattice.Cost{Dictionary: dict},
		Source: lattice.SourceDictionary,
	}
}

func wordCandidate(span segment.Span, surface string, script segment.Script, seq reading.Sequence, id lang.ID, variant lang.OutputVariant, dict float64, src lattice.Source) lattice.Candidate {
	return lattice.Candidate{
		Segment: segment.Segment{
			Span:     span,
			Script:   script,
			Reading:  seq,
			Surface:  surface,
			Language: lang.Tag(id),
			Variant:  variant,
		},
		Cost:   lattice.Cost{Dictionary: dict},
		Source: src,
	}
}

func zhuyinSeq(s string) reading.Sequence {
	fields := strings.Fields(s)
	out := make(reading.Sequence, len(fields))
	for i, f := range fields {
		out[i] = reading.Unit(f)
	}
	return out
}

func kanaSeq(s string) reading.Sequence {
	var out reading.Sequence
	for _, r := range s {
		out = append(out, reading.Unit(r))
	}
	return out
}

func TestNBestOrdersPathsAndIsDeterministic(t *testing.T) {
	in := lattice.Input{Text: "ab"}
	cands := []lattice.Candidate{
		newCandidate(segment.Span{Start: 0, End: 2}, "AB", lang.Zh, lang.VariantZhHant, 1.5),
		newCandidate(segment.Span{Start: 0, End: 1}, "a1", lang.Zh, lang.VariantZhHant, 1),
		newCandidate(segment.Span{Start: 1, End: 2}, "b1", lang.Zh, lang.VariantZhHant, 1),
		newCandidate(segment.Span{Start: 0, End: 1}, "a2", lang.Zh, lang.VariantZhHant, 3),
		newCandidate(segment.Span{Start: 1, End: 2}, "b2", lang.Zh, lang.VariantZhHant, 3),
	}
	lat, err := lattice.Build(in, cands, lattice.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(lat.Edges()); got != 5 {
		t.Fatalf("Edges() = %d, want 5", got)
	}

	paths := lat.NBest(3)
	if len(paths) != 3 {
		t.Fatalf("NBest(3) 回傳 %d 條，want 3", len(paths))
	}
	if paths[0].Surface() != "AB" || paths[1].Surface() != "a1b1" {
		t.Fatalf("前兩條路徑錯誤: %q, %q", paths[0].Surface(), paths[1].Surface())
	}
	if paths[0].Cost != 1.5 || paths[1].Cost != 2 {
		t.Errorf("成本錯誤: %v, %v", paths[0].Cost, paths[1].Cost)
	}
	if paths[2].Cost != 4 {
		t.Errorf("第三條成本 = %v, want 4", paths[2].Cost)
	}

	all := lat.NBest(10)
	if len(all) != 5 {
		t.Fatalf("NBest(10) 回傳 %d 條，want 5", len(all))
	}
	if !reflect.DeepEqual(paths, lat.NBest(3)) {
		t.Error("同一 lattice 的 NBest 結果不穩定")
	}
	if one := lat.NBest(0); len(one) != 1 || one[0].Surface() != "AB" {
		t.Errorf("NBest(0) 應等同 NBest(1): %#v", one)
	}
	best, ok := lat.Best()
	if !ok || best.Surface() != "AB" {
		t.Errorf("Best() = %#v, %v", best, ok)
	}
}

func TestSwitchCostProducesDistinctLanguages(t *testing.T) {
	in := lattice.Input{Text: "ab"}
	var cands []lattice.Candidate
	for _, span := range []segment.Span{{Start: 0, End: 1}, {Start: 1, End: 2}} {
		cands = append(cands,
			newCandidate(span, "ja", lang.Ja, lang.VariantJa, 1),
			newCandidate(span, "zh", lang.Zh, lang.VariantZhHant, 1),
		)
	}
	cfg := lattice.DefaultConfig()
	cfg.SwitchCost = 0.5
	cfg.Primary = lang.Und
	lat, err := lattice.Build(in, cands, cfg)
	if err != nil {
		t.Fatal(err)
	}
	paths := lat.NBest(4)
	if len(paths) != 4 {
		t.Fatalf("回傳 %d 條，want 4", len(paths))
	}
	for i, p := range paths {
		want := 2.0
		if i >= 2 {
			want = 2.5
		}
		if p.Cost != want {
			t.Errorf("paths[%d].Cost = %v, want %v（%s）", i, p.Cost, want, p.Surface())
		}
	}
}

func TestCandidateCostAndTransitionCost(t *testing.T) {
	cfg := lattice.DefaultConfig()
	cfg.Primary = lang.Ja
	cfg.UnknownPenalty = 0.25

	ja := newCandidate(segment.Span{Start: 0, End: 1}, "日", lang.Ja, lang.VariantJa, 1)
	zh := newCandidate(segment.Span{Start: 0, End: 1}, "中", lang.Zh, lang.VariantZhHant, 1)
	und := newCandidate(segment.Span{Start: 0, End: 1}, "?", lang.Und, "", 1)
	if got := cfg.CandidateCost(ja); got != 0.95 {
		t.Errorf("主要語言候選成本 = %v, want 0.95", got)
	}
	if got := cfg.CandidateCost(zh); got != 1 {
		t.Errorf("其他語言候選成本 = %v, want 1", got)
	}
	if got := cfg.CandidateCost(und); got != 1.25 {
		t.Errorf("未知語言候選成本 = %v, want 1.25", got)
	}

	jaTag, zhTag := lang.Tag(lang.Ja), lang.Tag(lang.Zh)
	cases := []struct {
		name     string
		from, to lang.LanguageTag
		want     float64
	}{
		{"同一語言", jaTag, jaTag, 0},
		{"未標語言", lang.LanguageTag{}, jaTag, 0},
		{"主要語言在左", jaTag, zhTag, 0.1},
		{"主要語言在右", zhTag, jaTag, 0.1},
		{"其他語言互切", zhTag, lang.Tag(lang.En), 0.5},
	}
	for _, tc := range cases {
		if got := cfg.TransitionCost(tc.from, tc.to); got != tc.want {
			t.Errorf("%s: TransitionCost = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBuildFromMergesLongWordAcrossSegments(t *testing.T) {
	in := lattice.Input{
		Text: "台北",
		Segments: []segment.Segment{
			{Span: segment.Span{Start: 0, End: 1}, Script: segment.ScriptZhuyin, Reading: zhuyinSeq("ㄊㄞˊ"), Language: lang.Tag(lang.Zh), Variant: lang.VariantZhHant},
			{Span: segment.Span{Start: 1, End: 2}, Script: segment.ScriptZhuyin, Reading: zhuyinSeq("ㄅㄟˇ"), Language: lang.Tag(lang.Zh), Variant: lang.VariantZhHant},
		},
	}
	queries := map[[2]int]int{}
	texts := map[[2]int]string{}
	src := lattice.CandidateSourceFunc(func(q lattice.Query) []lattice.Candidate {
		key := [2]int{q.Span.Start, q.Span.End}
		queries[key]++
		texts[key] = q.Text
		switch q.Span {
		case (segment.Span{Start: 0, End: 1}):
			return []lattice.Candidate{
				wordCandidate(q.Span, "台", segment.ScriptZhuyin, zhuyinSeq("ㄊㄞˊ"), lang.Zh, lang.VariantZhHant, 0.9, lattice.SourceDictionary),
			}
		case (segment.Span{Start: 1, End: 2}):
			return []lattice.Candidate{
				wordCandidate(q.Span, "北", segment.ScriptZhuyin, zhuyinSeq("ㄅㄟˇ"), lang.Zh, lang.VariantZhHant, 0.9, lattice.SourceDictionary),
			}
		case (segment.Span{Start: 0, End: 2}):
			return []lattice.Candidate{
				wordCandidate(q.Span, "台北", segment.ScriptZhuyin, q.Reading, lang.Zh, lang.VariantZhHant, 0.5, lattice.SourceDictionary),
			}
		}
		return nil
	})

	lat, err := lattice.BuildFrom(in, src, lattice.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if queries[[2]int{0, 2}] == 0 {
		t.Fatal("沒有查詢跨片段的長詞範圍")
	}
	if got := texts[[2]int{0, 2}]; got != "台北" {
		t.Errorf("長詞查詢 Text = %q, want %q", got, "台北")
	}
	best, ok := lat.Best()
	if !ok {
		t.Fatal("找不到路徑")
	}
	if best.Surface() != "台北" {
		t.Fatalf("Surface = %q, want 台北", best.Surface())
	}
	if len(best.Edges) != 1 {
		t.Fatalf("長詞沒有自動合併：%d 條邊 %#v", len(best.Edges), best.Segments())
	}
	if best.Edges[0].Span != (segment.Span{Start: 0, End: 2}) {
		t.Errorf("合併後 span = %#v", best.Edges[0].Span)
	}
	if got := best.Edges[0].ReadingKey(); got != reading.Key(zhuyinSeq("ㄊㄞˊ ㄅㄟˇ")) {
		t.Errorf("合併後讀音 = %q", got)
	}
	if segs := best.Segments(); len(segs) != 1 || segs[0].Surface != "台北" {
		t.Errorf("Segments() 沒有合併: %#v", segs)
	}

	// 限制只能查單一片段時，長詞邊不會出現，路徑自然退回兩個片段。
	cfg := lattice.DefaultConfig()
	cfg.MaxMergeSegments = 1
	split, err := lattice.BuildFrom(in, src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	splitBest, ok := split.Best()
	if !ok || splitBest.Surface() != "台北" {
		t.Fatalf("split path = %#v, %v", splitBest, ok)
	}
	if len(splitBest.Edges) != 2 {
		t.Errorf("MaxMergeSegments=1 仍合併: %#v", splitBest.Edges)
	}
}

func TestBuildRejectsInvalidInput(t *testing.T) {
	cfg := lattice.DefaultConfig()
	text := lattice.Input{Text: "ab"}

	if _, err := lattice.Build(lattice.Input{}, nil, cfg); !errors.Is(err, lattice.ErrEmptyInput) {
		t.Errorf("空輸入錯誤 = %v, want ErrEmptyInput", err)
	}

	bad := map[string]lattice.Candidate{
		"顛倒 span": newCandidate(segment.Span{Start: 1, End: 0}, "x", lang.Zh, lang.VariantZhHant, 1),
		"零長度":     newCandidate(segment.Span{Start: 1, End: 1}, "x", lang.Zh, lang.VariantZhHant, 1),
		"超出輸入":    newCandidate(segment.Span{Start: 0, End: 3}, "x", lang.Zh, lang.VariantZhHant, 1),
	}
	for name, c := range bad {
		if _, err := lattice.Build(text, []lattice.Candidate{c}, cfg); err == nil {
			t.Errorf("%s：Build 沒有回傳錯誤", name)
		}
	}

	src := lattice.CandidateSourceFunc(func(lattice.Query) []lattice.Candidate { return nil })
	if _, err := lattice.BuildFrom(lattice.Input{Text: "a"}, src, cfg); err == nil {
		t.Error("BuildFrom 在沒有基礎片段時應回傳錯誤")
	}
	if _, err := lattice.BuildFrom(lattice.Input{Text: "a"}, nil, cfg); err == nil {
		t.Error("BuildFrom 在來源是 nil 時應回傳錯誤")
	}
	overlap := lattice.Input{
		Text: "abc",
		Segments: []segment.Segment{
			{Span: segment.Span{Start: 0, End: 2}},
			{Span: segment.Span{Start: 1, End: 3}},
		},
	}
	if _, err := lattice.BuildFrom(overlap, src, cfg); err == nil {
		t.Error("重疊片段應回傳錯誤")
	}
}

func TestDuplicatesKeepLowestCostAndBadSpansAreIgnored(t *testing.T) {
	in := lattice.Input{Text: "a"}
	a := newCandidate(segment.Span{Start: 0, End: 1}, "a", lang.Zh, lang.VariantZhHant, 2)
	b := newCandidate(segment.Span{Start: 0, End: 1}, "a", lang.Zh, lang.VariantZhHant, 1)
	if a.Key() != b.Key() {
		t.Fatalf("測試候選的 Key 不同: %q != %q", a.Key(), b.Key())
	}
	lat, err := lattice.Build(in, []lattice.Candidate{a, b}, lattice.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	edges := lat.Edges()
	if len(edges) != 1 {
		t.Fatalf("除重後有 %d 條邊，want 1", len(edges))
	}
	if edges[0].Dictionary != 1 {
		t.Errorf("保留的候選成本 = %v, want 1", edges[0].Dictionary)
	}

	// 來源回傳不對應查詢範圍的候選時，BuildFrom 會忽略它。
	src := lattice.CandidateSourceFunc(func(q lattice.Query) []lattice.Candidate {
		return []lattice.Candidate{
			newCandidate(segment.Span{Start: q.Span.Start, End: q.Span.End + 1}, "too long", lang.Zh, lang.VariantZhHant, 1),
		}
	})
	seg := lattice.Input{
		Text:     "a",
		Segments: []segment.Segment{{Span: segment.Span{Start: 0, End: 1}}},
	}
	empty, err := lattice.BuildFrom(seg, src, lattice.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := empty.Best(); ok {
		t.Error("範圍不符的候選不應形成路徑")
	}
}

func TestLongWordWinsTieAgainstSplit(t *testing.T) {
	in := lattice.Input{
		Text: "台北",
		Segments: []segment.Segment{
			{Span: segment.Span{Start: 0, End: 1}, Script: segment.ScriptZhuyin, Reading: zhuyinSeq("ㄊㄞˊ")},
			{Span: segment.Span{Start: 1, End: 2}, Script: segment.ScriptZhuyin, Reading: zhuyinSeq("ㄅㄟˇ")},
		},
	}
	src := lattice.CandidateSourceFunc(func(q lattice.Query) []lattice.Candidate {
		surface := map[segment.Span]string{
			{Start: 0, End: 1}: "台",
			{Start: 1, End: 2}: "北",
			{Start: 0, End: 2}: "台北",
		}[q.Span]
		if surface == "" {
			return nil
		}
		return []lattice.Candidate{
			wordCandidate(q.Span, surface, segment.ScriptZhuyin, q.Reading, lang.Zh, lang.VariantZhHant, 1, lattice.SourceDictionary),
		}
	})
	lat, err := lattice.BuildFrom(in, src, lattice.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	best, ok := lat.Best()
	if !ok {
		t.Fatal("找不到路徑")
	}
	if len(best.Edges) != 1 || best.Edges[0].Surface != "台北" {
		t.Errorf("同成本時沒有優先合併長詞: %#v", best.Segments())
	}
}

// lexiconCandidateSource 用 P2-11 的 lexicon.Query.Candidate 組候選，模擬整合層。
type lexiconCandidateSource struct {
	entries map[string][]lexicon.Entry
}

func (s lexiconCandidateSource) Candidates(q lexicon.Query) []lattice.Candidate {
	var out []lattice.Candidate
	for _, e := range s.entries[reading.Key(q.Reading)] {
		out = append(out, q.Candidate(e, lattice.SourceDictionary, lattice.Cost{Dictionary: 1}))
	}
	return out
}

func TestLexiconCandidateSourceIntegration(t *testing.T) {
	entries := map[string][]lexicon.Entry{
		reading.Key(zhuyinSeq("ㄊㄞˊ")):     {{Surface: "台", Reading: zhuyinSeq("ㄊㄞˊ")}},
		reading.Key(zhuyinSeq("ㄅㄟˇ")):     {{Surface: "北", Reading: zhuyinSeq("ㄅㄟˇ")}},
		reading.Key(zhuyinSeq("ㄊㄞˊ ㄅㄟˇ")): {{Surface: "台北", Reading: zhuyinSeq("ㄊㄞˊ ㄅㄟˇ")}},
	}
	source := lexiconCandidateSource{entries: entries}
	src := lattice.CandidateSourceFunc(func(q lattice.Query) []lattice.Candidate {
		return source.Candidates(lexicon.Query{
			Span:     q.Span,
			Script:   q.Script,
			Reading:  q.Reading,
			Language: q.Language,
			Variant:  q.Variant,
			Limit:    q.Limit,
		})
	})

	in := lattice.Input{
		Text: "台北",
		Segments: []segment.Segment{
			{Span: segment.Span{Start: 0, End: 1}, Script: segment.ScriptZhuyin, Reading: zhuyinSeq("ㄊㄞˊ"), Language: lang.Tag(lang.Zh), Variant: lang.VariantZhHant},
			{Span: segment.Span{Start: 1, End: 2}, Script: segment.ScriptZhuyin, Reading: zhuyinSeq("ㄅㄟˇ"), Language: lang.Tag(lang.Zh), Variant: lang.VariantZhHant},
		},
	}
	lat, err := lattice.BuildFrom(in, src, lattice.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	best, ok := lat.Best()
	if !ok || best.Surface() != "台北" || len(best.Edges) != 1 {
		t.Fatalf("lexicon 長詞沒有合併: %#v, %v", best.Segments(), ok)
	}
	if best.Edges[0].Language.Language != lang.Zh || best.Edges[0].Variant != lang.VariantZhHant {
		t.Errorf("候選語言或變體錯誤: %#v", best.Edges[0].Segment)
	}
}
