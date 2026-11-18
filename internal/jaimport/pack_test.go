package jaimport

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
)

func mustEntry(t *testing.T, surface, kana string, cost float64, pos POS) Entry {
	t.Helper()
	r, err := NormalizeReading(kana)
	if err != nil {
		t.Fatal(err)
	}
	return MakeEntry(surface, r, cost, pos, []string{"curated"}, ConjNone)
}

func testLexicon(t *testing.T) *Lexicon {
	t.Helper()
	lex, err := NewLexicon([]Entry{
		mustEntry(t, "明日", "あした", 1, POSNoun),
		mustEntry(t, "味", "あじ", 1, POSNoun),
		mustEntry(t, "アヒル", "あひる", 1, POSNoun),
		mustEntry(t, "亜", "あ", 2, POSNoun),
		mustEntry(t, "予定", "よてい", 1.5, POSNoun),
		mustEntry(t, "食べる", "たべる", 1, POSVerb),
	}, []Connection{
		{Left: POSBOS, Right: POSNoun, Cost: 0.1},
		{Left: POSNoun, Right: POSEOS, Cost: 0.2},
		{Left: POSBOS, Right: POSVerb, Cost: 0.15},
		{Left: POSVerb, Right: POSEOS, Cost: 0.25},
	})
	if err != nil {
		t.Fatal(err)
	}
	return lex
}

func TestLexiconRoundTripAndQueries(t *testing.T) {
	lex := testLexicon(t)
	data := lex.Encode()
	if len(data) == 0 {
		t.Fatal("Encode 空")
	}
	got, err := DecodeLexicon(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 6 {
		t.Fatalf("Len = %d", got.Len())
	}
	if got.LongestWord() != 3 {
		t.Fatalf("LongestWord = %d", got.LongestWord())
	}
	if !bytes.Equal(lex.Encode(), data) {
		t.Fatal("同一份詞典編碼兩次不同")
	}
	if !bytes.Equal(got.Encode(), data) {
		t.Fatal("解碼後重新編碼不同")
	}
	if e := got.Exact(reading.Sequence{"あ", "じ"}); len(e) != 1 || e[0].Surface != "味" {
		t.Fatalf("Exact = %#v", e)
	}
	if e := got.Exact(reading.Sequence{"あ"}); len(e) != 1 || e[0].Surface != "亜" {
		t.Fatalf("Exact(あ) = %#v", e)
	}
	if got.HasPrefix(reading.Sequence{"あ", "し"}) != true {
		t.Fatal("HasPrefix(あし) 應為 true")
	}
	if got.HasPrefix(reading.Sequence{"あ", "め"}) {
		t.Fatal("HasPrefix(あめ) 應為 false")
	}
	pre := got.Prefixed(reading.Sequence{"あ"})
	if len(pre) != 4 {
		t.Fatalf("Prefixed(あ) = %d 筆：%#v", len(pre), pre)
	}
	if got.Lookup("明日")[0].Surface != "明日" || !got.Has("明日") || got.Has("なし") {
		t.Fatal("詞面索引錯誤")
	}
	ends := map[int]string{}
	got.Matches(reading.Sequence{"あ", "し", "た"}, 0, func(end int, e Entry) { ends[end] = e.Surface })
	if ends[3] != "明日" || ends[1] != "亜" || len(ends) != 2 {
		t.Fatalf("Matches = %#v", ends)
	}
	if c := got.Matrix().Cost(POSBOS, POSNoun); c != 0.1 {
		t.Fatalf("BOS→noun = %v", c)
	}
	if c := got.Matrix().Cost(POSAdverb, POSNoun); c != DefaultConnectionCost {
		t.Fatalf("未指定 = %v", c)
	}
}

func TestLexiconDeterministicFromShuffledInput(t *testing.T) {
	a := testLexicon(t)
	lex, err := NewLexicon([]Entry{
		mustEntry(t, "食べる", "たべる", 1, POSVerb),
		mustEntry(t, "予定", "よてい", 1.5, POSNoun),
		mustEntry(t, "亜", "あ", 2, POSNoun),
		mustEntry(t, "アヒル", "あひる", 1, POSNoun),
		mustEntry(t, "味", "あじ", 1, POSNoun),
		mustEntry(t, "明日", "あした", 1, POSNoun),
	}, []Connection{
		{Left: POSVerb, Right: POSEOS, Cost: 0.25},
		{Left: POSNoun, Right: POSEOS, Cost: 0.2},
		{Left: POSBOS, Right: POSVerb, Cost: 0.15},
		{Left: POSBOS, Right: POSNoun, Cost: 0.1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Encode(), lex.Encode()) {
		t.Fatal("輸入順序不同造成不同位元組")
	}
}

func TestDecodeLexiconRejectsCorruption(t *testing.T) {
	data := testLexicon(t).Encode()
	for name, b := range map[string][]byte{
		"empty":     {},
		"magic":     append([]byte("XXXXXXXX"), data[8:]...),
		"truncated": data[:len(data)-1],
		"trailing":  append(append([]byte(nil), data...), 0),
	} {
		if _, err := DecodeLexicon(b); err == nil {
			t.Errorf("%s 應解碼失敗", name)
		}
	}
	if _, err := DecodeLexicon(nil); err == nil {
		t.Fatal("nil 應失敗")
	}
}

func TestLMRoundTrip(t *testing.T) {
	lm := NewLM(map[string]float64{"明日": 1.5, "味": 2, "これから": -0.49}, map[[2]string]float64{{"明日", "味"}: 0.7})
	if lm.Len() != 3 || lm.BigramLen() != 1 {
		t.Fatalf("LM = %#v", lm)
	}
	data := lm.Encode()
	if !bytes.Equal(data, lm.Encode()) {
		t.Fatal("LM 編碼不穩定")
	}
	got, err := DecodeLM(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Unigram["明日"] != 1.5 || got.Unigram["これから"] != -0.49 || got.Bigram[[2]string{"明日", "味"}] != 0.7 {
		t.Fatalf("解碼 = %#v", got)
	}
	if _, err := DecodeLM(append(data, 0)); err == nil {
		t.Fatal("尾端多餘位元組應失敗")
	}
	if _, err := DecodeLM([]byte("XXXXXXXX")); err == nil {
		t.Fatal("magic 錯誤應失敗")
	}
}

func TestPackSizeAndMemory(t *testing.T) {
	const n = 20000
	kana := []rune("あいうえおかきくけこさしすせそたちつてとなにぬねのはひふへほまみむめもやゆよらりるれろわをん")
	entries := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		surface := fmt.Sprintf("語%d", i)
		r := reading.Sequence{
			reading.Unit(string(kana[i%len(kana)])),
			reading.Unit(string(kana[(i/len(kana))%len(kana)])),
			reading.Unit(string(kana[(i/(len(kana)*len(kana)))%len(kana)])),
		}
		entries = append(entries, MakeEntry(surface, r, 1+float64(i%50)/10, POSNoun, []string{"curated"}, ConjNone))
	}
	lex, err := NewLexicon(entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded := lex.Encode()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	decoded, err := DecodeLexicon(encoded)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if decoded.Len() != n {
		t.Fatalf("解碼詞條 = %d", decoded.Len())
	}
	heap := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if heap < 0 {
		heap = 0
	}
	t.Logf("詞典量測：%d 筆，encoded=%d bytes（%.2f bytes/筆），解碼後 heap≈%d bytes，估計常駐=%d bytes",
		n, len(encoded), float64(len(encoded))/n, heap, decoded.RuntimeBytesEstimate())
	if len(encoded) > n*80 {
		t.Errorf("編碼過大：%d bytes / %d 筆", len(encoded), n)
	}
}

func BenchmarkDecodeLexicon(b *testing.B) {
	const n = 20000
	kana := []rune("あいうえおかきくけこさしすせそたちつてとなにぬねのはひふへほまみむめもやゆよらりるれろわをん")
	entries := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		r := reading.Sequence{
			reading.Unit(string(kana[i%len(kana)])),
			reading.Unit(string(kana[(i/len(kana))%len(kana)])),
		}
		entries = append(entries, MakeEntry(fmt.Sprintf("語%d", i), r, 1, POSNoun, nil, ConjNone))
	}
	lex, err := NewLexicon(entries, nil)
	if err != nil {
		b.Fatal(err)
	}
	data := lex.Encode()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeLexicon(data); err != nil {
			b.Fatal(err)
		}
	}
}
