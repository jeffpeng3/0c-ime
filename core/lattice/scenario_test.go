package lattice_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/segment"
)

type spanWriter struct {
	b strings.Builder
	n int
}

func (w *spanWriter) add(text string) segment.Span {
	start := w.n
	w.n += utf8.RuneCountInString(text)
	w.b.WriteString(text)
	return segment.Span{Start: start, End: w.n}
}

func (w *spanWriter) text() string { return w.b.String() }

func spaceCandidate(span segment.Span) lattice.Candidate {
	return lattice.Candidate{
		Segment: segment.Segment{
			Span:    span,
			Script:  segment.ScriptSymbol,
			Surface: " ",
		},
		Source: lattice.SourceSymbol,
	}
}

// mixedFixture 是情境 A/B 的同一串輸入：
// watashiwa + 注音ㄉㄚˋ ㄐㄧ ㄅㄚ + desu + is a good sentence
// 輸出應為：私わ大雞巴です is a good sentence
func mixedFixture() (lattice.Input, []lattice.Candidate) {
	var w spanWriter
	watashi := w.add("watashi")
	wa := w.add("wa")
	da := w.add("ㄉㄚˋ")
	ji := w.add("ㄐㄧ")
	ba := w.add("ㄅㄚ")
	desu := w.add("desu")
	sp1 := w.add(" ")
	is := w.add("is")
	sp2 := w.add(" ")
	a := w.add("a")
	sp3 := w.add(" ")
	good := w.add("good")
	sp4 := w.add(" ")
	sentence := w.add("sentence")

	cands := []lattice.Candidate{
		wordCandidate(watashi, "私", segment.ScriptLatin, kanaSeq("わたし"), lang.Ja, lang.VariantJa, 0.4, lattice.SourceDictionary),
		wordCandidate(wa, "わ", segment.ScriptLatin, kanaSeq("わ"), lang.Ja, lang.VariantJa, 0.5, lattice.SourceDictionary),
		wordCandidate(da, "大", segment.ScriptZhuyin, zhuyinSeq("ㄉㄚˋ"), lang.Zh, lang.VariantZhHant, 0.5, lattice.SourceDictionary),
		wordCandidate(ji, "雞", segment.ScriptZhuyin, zhuyinSeq("ㄐㄧ"), lang.Zh, lang.VariantZhHant, 0.5, lattice.SourceDictionary),
		wordCandidate(ba, "巴", segment.ScriptZhuyin, zhuyinSeq("ㄅㄚ"), lang.Zh, lang.VariantZhHant, 0.5, lattice.SourceDictionary),
		wordCandidate(desu, "です", segment.ScriptLatin, kanaSeq("です"), lang.Ja, lang.VariantJa, 0.5, lattice.SourceDictionary),
		wordCandidate(is, "is", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.3, lattice.SourceLiteral),
		wordCandidate(a, "a", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.3, lattice.SourceLiteral),
		wordCandidate(good, "good", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.3, lattice.SourceLiteral),
		wordCandidate(sentence, "sentence", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.3, lattice.SourceLiteral),
		spaceCandidate(sp1),
		spaceCandidate(sp2),
		spaceCandidate(sp3),
		spaceCandidate(sp4),
	}
	return lattice.Input{Text: w.text()}, cands
}

func TestScenarioAMixedPrimaryJapanese(t *testing.T) {
	in, cands := mixedFixture()
	cfg := lattice.DefaultConfig()
	cfg.Primary = lang.Ja

	lat, err := lattice.Build(in, cands, cfg)
	if err != nil {
		t.Fatal(err)
	}
	best, ok := lat.Best()
	if !ok {
		t.Fatal("情境 A 找不到路徑")
	}
	const want = "私わ大雞巴です is a good sentence"
	if got := best.Surface(); got != want {
		t.Fatalf("情境 A 輸出 = %q, want %q", got, want)
	}
	if len(best.Edges) != 14 {
		t.Fatalf("情境 A 路徑有 %d 條邊，want 14", len(best.Edges))
	}

	tags := make([]lang.ID, len(best.Edges))
	for i, e := range best.Edges {
		tags[i] = e.Language.Language
	}
	wantTags := []lang.ID{
		lang.Ja, lang.Ja, lang.Zh, lang.Zh, lang.Zh, lang.Ja,
		lang.Und, lang.En, lang.Und, lang.En, lang.Und, lang.En, lang.Und, lang.En,
	}
	if len(tags) != len(wantTags) {
		t.Fatalf("語言標籤數量 = %d", len(tags))
	}
	for i := range wantTags {
		if tags[i] != wantTags[i] {
			t.Errorf("edge %d 語言 = %q, want %q", i, tags[i], wantTags[i])
		}
	}

	// 每個片段保有自己的輸出變體，沒有被主要語言覆寫。
	if got := best.Edges[0].Variant; got != lang.VariantJa {
		t.Errorf("日文片段變體 = %q, want %q", got, lang.VariantJa)
	}
	if got := best.Edges[2].Variant; got != lang.VariantZhHant {
		t.Errorf("中文片段變體 = %q, want %q", got, lang.VariantZhHant)
	}
	if got := best.Edges[7].Variant; got != lang.VariantEn {
		t.Errorf("英文片段變體 = %q, want %q", got, lang.VariantEn)
	}
}

func TestScenarioBPrimaryChineseStillAllowsJapaneseAndEnglish(t *testing.T) {
	in, cands := mixedFixture()
	cfg := lattice.DefaultConfig()
	cfg.Primary = lang.Zh

	lat, err := lattice.Build(in, cands, cfg)
	if err != nil {
		t.Fatal(err)
	}
	best, ok := lat.Best()
	if !ok {
		t.Fatal("情境 B 找不到路徑")
	}
	const want = "私わ大雞巴です is a good sentence"
	if got := best.Surface(); got != want {
		t.Fatalf("情境 B 輸出 = %q, want %q", got, want)
	}

	seen := map[lang.ID]bool{}
	for _, e := range best.Edges {
		seen[e.Language.Language] = true
	}
	for _, id := range []lang.ID{lang.Ja, lang.Zh, lang.En} {
		if !seen[id] {
			t.Errorf("主要語言中文時，路徑缺少 %q 片段: %v", id, seen)
		}
	}
}

func TestPrimaryPriorChangesAmbiguousChoice(t *testing.T) {
	in := lattice.Input{Text: "ka"}
	cands := []lattice.Candidate{
		newCandidate(segment.Span{Start: 0, End: 2}, "卡", lang.Zh, lang.VariantZhHant, 1),
		newCandidate(segment.Span{Start: 0, End: 2}, "か", lang.Ja, lang.VariantJa, 1),
	}

	zh := lattice.DefaultConfig()
	zh.Primary = lang.Zh
	zhLat, err := lattice.Build(in, cands, zh)
	if err != nil {
		t.Fatal(err)
	}
	if best, _ := zhLat.Best(); best.Surface() != "卡" {
		t.Errorf("主要語言中文時選了 %q, want 卡", best.Surface())
	}

	ja := lattice.DefaultConfig()
	ja.Primary = lang.Ja
	jaLat, err := lattice.Build(in, cands, ja)
	if err != nil {
		t.Fatal(err)
	}
	if best, _ := jaLat.Best(); best.Surface() != "か" {
		t.Errorf("主要語言日文時選了 %q, want か", best.Surface())
	}
}

func TestScenarioCProperNounsStayLiteralAndPersonalWins(t *testing.T) {
	var w spanWriter
	kubectl := w.add("kubectl")
	sp1 := w.add(" ")
	pCloud := w.add("pCloud")
	sp2 := w.add(" ")
	gitHub := w.add("GitHub")

	// kubectl 有一個較貴的假切分；專有名詞應保留原文，不拆成注音或かな。
	cands := []lattice.Candidate{
		wordCandidate(kubectl, "kubectl", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.2, lattice.SourceLiteral),
		wordCandidate(kubectl, "我", segment.ScriptZhuyin, zhuyinSeq("ㄨㄛˇ"), lang.Zh, lang.VariantZhHant, 0.6, lattice.SourceDictionary),
		wordCandidate(segment.Span{Start: kubectl.Start, End: kubectl.Start + 4}, "クベ", segment.ScriptLatin, kanaSeq("くべ"), lang.Ja, lang.VariantJa, 0.3, lattice.SourceDictionary),
		wordCandidate(segment.Span{Start: kubectl.Start + 4, End: kubectl.End}, "クトル", segment.ScriptLatin, kanaSeq("くとる"), lang.Ja, lang.VariantJa, 0.3, lattice.SourceDictionary),
		wordCandidate(pCloud, "pCloud", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.2, lattice.SourceLiteral),
		wordCandidate(gitHub, "GitHub", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.2, lattice.SourceLiteral),
		spaceCandidate(sp1),
		spaceCandidate(sp2),
	}
	cfg := lattice.DefaultConfig()
	lat, err := lattice.Build(lattice.Input{Text: w.text()}, cands, cfg)
	if err != nil {
		t.Fatal(err)
	}
	best, ok := lat.Best()
	if !ok {
		t.Fatal("情境 C 找不到路徑")
	}
	if got, want := best.Surface(), "kubectl pCloud GitHub"; got != want {
		t.Fatalf("情境 C 輸出 = %q, want %q", got, want)
	}
	for _, e := range best.Edges {
		if e.Span == kubectl && e.Surface != "kubectl" {
			t.Errorf("kubectl 被拆開: %#v", e)
		}
		if e.Surface != "kubectl" && e.Surface != "pCloud" && e.Surface != "GitHub" && e.Surface != " " {
			t.Errorf("路徑出現非原文片段: %q", e.Surface)
		}
	}

	// 加入個人詞彙後，同樣的詞面優先使用 personal 來源。
	personal := wordCandidate(kubectl, "kubectl", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.05, lattice.SourcePersonal)
	dict := wordCandidate(kubectl, "kubectl", segment.ScriptLatin, nil, lang.En, lang.VariantEn, 0.8, lattice.SourceDictionary)
	only := lattice.Input{Text: "kubectl"}
	personalLat, err := lattice.Build(only, []lattice.Candidate{dict, personal}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	pBest, ok := personalLat.Best()
	if !ok {
		t.Fatal("個人詞彙 lattice 找不到路徑")
	}
	if len(pBest.Edges) != 1 || pBest.Edges[0].Source != lattice.SourcePersonal {
		t.Errorf("加入個人詞彙後沒有優先使用 personal: %#v", pBest.Edges)
	}
}
