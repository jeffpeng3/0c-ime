package decode_test

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

func testDecoder(t *testing.T) *decode.Decoder {
	t.Helper()
	f, err := os.Open("testdata/lexicon.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lex, err := lexicon.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	return &decode.Decoder{Lex: lex}
}

func units(s string) reading.Sequence {
	var seq reading.Sequence
	for _, r := range s {
		seq = append(seq, reading.Unit(r))
	}
	return seq
}

func clauseSurfaces(res lang.Result) []string {
	out := make([]string, len(res.Clauses))
	for i, c := range res.Clauses {
		out[i] = c.Surface
	}
	return out
}

type corpusCase struct {
	reading  reading.Sequence
	segments []string
	surface  string
}

func loadCorpus(t *testing.T) []corpusCase {
	t.Helper()
	data, err := os.ReadFile("testdata/anthy-corpus-sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	var cases []corpusCase
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		blocks := strings.Split(line, "| |")
		if len(blocks) != 2 {
			t.Fatalf("無法解析 corpus 行：%q", line)
		}
		readSegs := splitSegments(blocks[0])
		surfSegs := splitSegments(blocks[1])
		if len(readSegs) != len(surfSegs) {
			t.Fatalf("讀音與詞面文節數不同：%q", line)
		}
		var seq reading.Sequence
		for _, s := range readSegs {
			seq = append(seq, units(s)...)
		}
		cases = append(cases, corpusCase{
			reading:  seq,
			segments: surfSegs,
			surface:  strings.Join(surfSegs, ""),
		})
	}
	return cases
}

func splitSegments(block string) []string {
	block = strings.TrimSpace(block)
	block = strings.TrimPrefix(block, "|")
	block = strings.TrimSuffix(block, "|")
	if block == "" {
		return nil
	}
	parts := strings.Split(block, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func TestAnthyCorpusSamples(t *testing.T) {
	dec := testDecoder(t)
	cases := loadCorpus(t)
	if len(cases) < 5 {
		t.Fatalf("樣本數 %d 太少", len(cases))
	}
	for _, tc := range cases {
		res, err := dec.Convert(tc.reading, lang.Context{})
		if err != nil {
			t.Fatalf("%s: Convert: %v", tc.surface, err)
		}
		if got := decode.ResultSurface(res.Clauses); got != tc.surface {
			t.Errorf("讀音 %s 轉成 %q，want %q", reading.Key(tc.reading), got, tc.surface)
			continue
		}
		if got := clauseSurfaces(res); !reflect.DeepEqual(got, tc.segments) {
			t.Errorf("%s 文節 = %#v，want %#v", tc.surface, got, tc.segments)
		}
		for i, c := range res.Clauses {
			if len(c.Segments) == 0 {
				t.Errorf("%s 第 %d 文節沒有 segments", tc.surface, i)
			}
			if reading.Key(c.Reading) == "" {
				t.Errorf("%s 第 %d 文節沒有讀音", tc.surface, i)
			}
		}
	}
}

func TestAnthyCorpusBoundariesReproducible(t *testing.T) {
	dec := testDecoder(t)
	for _, tc := range loadCorpus(t) {
		first, err := dec.Convert(tc.reading, lang.Context{})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			next, err := dec.Convert(tc.reading, lang.Context{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(clauseSurfaces(first), clauseSurfaces(next)) {
				t.Fatalf("%s 第 %d 次文節邊界不同：%#v vs %#v",
					tc.surface, i, clauseSurfaces(first), clauseSurfaces(next))
			}
		}
	}
}

func TestVariableLengthReading(t *testing.T) {
	dec := testDecoder(t)
	res, err := dec.Convert(units("あした"), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode.ResultSurface(res.Clauses); got != "明日" {
		t.Fatalf("あした = %q, want 明日", got)
	}
	if got := reading.Key(res.Clauses[0].Reading); got != "あ し た" {
		t.Fatalf("讀音 = %q", got)
	}
	if len(res.Clauses[0].Segments) != 1 {
		t.Fatalf("Segments = %#v", res.Clauses[0].Segments)
	}
}

func TestBuilderVariableLengthLattice(t *testing.T) {
	dec := testDecoder(t)
	lat := decode.Builder{Lex: dec.Lex}.Build(units("あした"))
	nodes := lat.Nodes(0)
	var ends []int
	for _, n := range nodes {
		ends = append(ends, n.End)
		if n.End == 3 && n.Entry.Surface != "明日" {
			t.Errorf("end 3 = %q, want 明日", n.Entry.Surface)
		}
		if n.End == 1 && !n.Unknown {
			t.Errorf("單字かな fallback 標記錯誤：%#v", n)
		}
	}
	found := false
	for _, end := range ends {
		if end == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("lattice 沒有可変長讀音：%#v", nodes)
	}
	if got := lat.Nodes(3); got != nil {
		t.Fatalf("序列尾端不應有詞弧：%#v", got)
	}
	if got := lat.Nodes(-1); got != nil {
		t.Fatalf("負位置不應有詞弧：%#v", got)
	}
}

func TestNBestAndCandidates(t *testing.T) {
	dec := testDecoder(t)
	results, err := dec.NBest(units("はし"), lang.Context{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("NBest = %d 條", len(results))
	}
	want := map[string]bool{"橋": true, "箸": true, "端": true}
	for _, r := range results {
		if !want[decode.ResultSurface(r.Clauses)] {
			t.Errorf("NBest 出現非預期詞面 %#v", clauseSurfaces(r))
		}
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].Score < results[i].Score {
			t.Errorf("NBest 未依分數排序：%v < %v", results[i-1].Score, results[i].Score)
		}
	}

	cands, err := dec.Candidates(units("きょう"), lang.Span{Start: 0, Length: 3}, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) < 2 || cands[0].Surface != "今日" {
		t.Fatalf("Candidates = %#v", cands)
	}
	if reading.Key(cands[0].Reading) != "き ょ う" {
		t.Fatalf("Candidate.Reading = %q", reading.Key(cands[0].Reading))
	}
	if cands[0].Source != lang.SourceConvert {
		t.Fatalf("Source = %q", cands[0].Source)
	}

	seq := units("あしたをよむ")
	cands, err = dec.Candidates(seq, lang.Span{Start: 0, Length: 3}, lang.Context{})
	if err != nil || len(cands) == 0 || cands[0].Surface != "明日" {
		t.Fatalf("子範圍 Candidates = %#v, %v", cands, err)
	}
}

func TestCandidatesRejectBadSpan(t *testing.T) {
	dec := testDecoder(t)
	seq := units("あした")
	for _, span := range []lang.Span{
		{Start: -1, Length: 1},
		{Start: 0, Length: 0},
		{Start: 2, Length: 2},
		{Start: 4, Length: 1},
	} {
		cands, err := dec.Candidates(seq, span, lang.Context{})
		if err != nil || cands != nil {
			t.Errorf("span %#v = %#v, %v", span, cands, err)
		}
	}
}

func TestPrefixes(t *testing.T) {
	dec := testDecoder(t)
	cands := dec.Prefixes(units("あし"))
	if len(cands) != 1 || cands[0].Surface != "明日" {
		t.Fatalf("Prefixes(あし) = %#v", cands)
	}
	if cands[0].Source != lang.SourcePredict || reading.Key(cands[0].Reading) != "あ し た" {
		t.Fatalf("Prefixes 候選 = %#v", cands[0])
	}
	predicted := dec.Prefixes(units("は"))
	if len(predicted) != 4 {
		t.Fatalf("Prefixes(は) = %#v", predicted)
	}
	surfaces := map[string]bool{}
	for _, c := range predicted {
		surfaces[c.Surface] = true
	}
	for _, want := range []string{"橋", "箸", "端"} {
		if !surfaces[want] {
			t.Errorf("Prefixes(は) 缺少 %q", want)
		}
	}
	if got := dec.Prefixes(units("はし")); got != nil {
		t.Fatalf("讀音已完整時 Prefixes = %#v", got)
	}
	if got := dec.Prefixes(units("きょう")); got != nil {
		t.Fatalf("沒有更長詞時 Prefixes = %#v", got)
	}
	if got := dec.Prefixes(nil); got != nil {
		t.Fatalf("空讀音 Prefixes = %#v", got)
	}
}

func TestUnknownFallback(t *testing.T) {
	dec := &decode.Decoder{Lex: lexicon.NewMem()}
	res, err := dec.Convert(units("ぬこ"), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode.ResultSurface(res.Clauses); got != "ぬこ" {
		t.Fatalf("未知詞 = %q", got)
	}
	if got := clauseSurfaces(res); !reflect.DeepEqual(got, []string{"ぬ", "こ"}) {
		t.Fatalf("未知詞文節 = %#v", got)
	}

	empty, err := dec.Convert(nil, lang.Context{})
	if err != nil || len(empty.Clauses) != 0 {
		t.Fatalf("空讀音 = %#v, %v", empty, err)
	}
	if got, err := dec.NBest(nil, lang.Context{}, 3); err != nil || got != nil {
		t.Fatalf("空 NBest = %#v, %v", got, err)
	}
}

func TestConnectionCostChangesBestPath(t *testing.T) {
	lex := lexicon.NewMem()
	for _, e := range []lexicon.Entry{
		{Surface: "雨", Reading: units("あめ"), Cost: 1, POS: lexicon.POSNoun},
		{Surface: "飴", Reading: units("あめ"), Cost: 1, POS: lexicon.POSAdverb},
	} {
		if err := lex.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	conn := lexicon.NewMatrix()
	conn.Set(lexicon.POSNoun, lexicon.POSEOS, 0)
	conn.Set(lexicon.POSAdverb, lexicon.POSEOS, 10)
	dec := &decode.Decoder{Lex: lex, Conn: conn}
	res, err := dec.Convert(units("あめ"), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode.ResultSurface(res.Clauses); got != "雨" {
		t.Fatalf("連接成本未影響結果：%q", got)
	}
}

func TestSegmenterClauses(t *testing.T) {
	conn := lexicon.NewMatrix()
	nodes := []decode.Node{
		{Entry: lexicon.Entry{Surface: "今日", Reading: units("きょう"), Cost: 1, POS: lexicon.POSNoun}},
		{Entry: lexicon.Entry{Surface: "は", Reading: units("は"), Cost: 1, POS: lexicon.POSParticle}},
		{Entry: lexicon.Entry{Surface: "いい", Reading: units("いい"), Cost: 1, POS: lexicon.POSAdjective}},
		{Entry: lexicon.Entry{Surface: "天気", Reading: units("てんき"), Cost: 1, POS: lexicon.POSNoun}},
		{Entry: lexicon.Entry{Surface: "です", Reading: units("です"), Cost: 1, POS: lexicon.POSAuxiliary}},
	}
	var got []string
	for _, c := range decode.Clauses(nodes, conn, decode.Segmenter{}) {
		got = append(got, c.Surface)
	}
	want := []string{"今日は", "いい", "天気です"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("文節 = %#v, want %#v", got, want)
	}

	compound := []decode.Node{
		{Entry: lexicon.Entry{Surface: "日本", Reading: units("にほん"), Cost: 1, POS: lexicon.POSNoun}},
		{Entry: lexicon.Entry{Surface: "語", Reading: units("ご"), Cost: 1, POS: lexicon.POSNoun}},
	}
	conn.Set(lexicon.POSNoun, lexicon.POSNoun, 0)
	if got := decode.Clauses(compound, conn, decode.Segmenter{MergeCost: 0.5}); len(got) != 1 || got[0].Surface != "日本語" {
		t.Fatalf("複合名詞 = %#v", got)
	}
	if got := decode.Clauses(compound, conn, decode.Segmenter{}); len(got) != 2 {
		t.Fatalf("停用複合詞 = %#v", got)
	}

	prefixed := []decode.Node{
		{Entry: lexicon.Entry{Surface: "お", Reading: units("お"), Cost: 1, POS: lexicon.POSPrefix}},
		{Entry: lexicon.Entry{Surface: "茶", Reading: units("ちゃ"), Cost: 1, POS: lexicon.POSNoun}},
		{Entry: lexicon.Entry{Surface: "を", Reading: units("を"), Cost: 1, POS: lexicon.POSParticle}},
	}
	if got := decode.Clauses(prefixed, conn, decode.Segmenter{}); len(got) != 1 || got[0].Surface != "お茶を" {
		t.Fatalf("接頭詞 = %#v", got)
	}
}

type learnChopsticks struct{}

func (learnChopsticks) Cost(_ reading.Sequence, _ int, e lexicon.Entry, _ lang.Context) float64 {
	if e.Surface == "箸" {
		return -10
	}
	return 0
}

func TestLearningHook(t *testing.T) {
	dec := testDecoder(t)
	dec.Learning = learnChopsticks{}
	res, err := dec.Convert(units("はし"), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode.ResultSurface(res.Clauses); got != "箸" {
		t.Fatalf("學習鉤子未生效：%q", got)
	}
	var _ decode.Learning = decode.NoLearning{}
	var _ decode.Verifier = decode.NoVerifier{}
}

type verifyHash struct{}

func (verifyHash) Score(_ reading.Sequence, res lang.Result, _ lang.Context) float64 {
	if decode.ResultSurface(res.Clauses) == "端" {
		return 100
	}
	return 0
}

func TestVerifierHook(t *testing.T) {
	dec := testDecoder(t)
	dec.Verifier = verifyHash{}
	res, err := dec.Convert(units("はし"), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode.ResultSurface(res.Clauses); got != "端" {
		t.Fatalf("驗證鉤子未生效：%q", got)
	}
}

func Test32KanaP95(t *testing.T) {
	dec := testDecoder(t)
	seq := units(strings.Repeat("あじをしめるみせに", 3) + "ねこねこね")
	if len(seq) != 32 {
		t.Fatalf("測試讀音長度 = %d, want 32", len(seq))
	}
	const runs = 200
	durations := make([]time.Duration, 0, runs)
	for i := 0; i < runs; i++ {
		start := time.Now()
		if _, err := dec.Convert(seq, lang.Context{}); err != nil {
			t.Fatal(err)
		}
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[len(durations)*95/100]
	t.Logf("32 かな P95 = %v", p95)
	if p95 > 30*time.Millisecond {
		t.Fatalf("32 かな P95 = %v，超過 30 ms", p95)
	}
}
