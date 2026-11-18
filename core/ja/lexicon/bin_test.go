package lexicon_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

// testPack 用 jaimport 產生一份含所有詞性與連接成本的小型 lexicon.bin。
func testPack(t *testing.T) (*jaimport.Lexicon, []byte) {
	t.Helper()
	entries := []jaimport.Entry{
		{Surface: "明日", Reading: reading.Sequence{"あ", "し", "た"}, Cost: 1.5, POS: jaimport.POSNoun},
		{Surface: "味", Reading: reading.Sequence{"あ", "じ"}, Cost: 2, POS: jaimport.POSNoun},
		{Surface: "亜", Reading: reading.Sequence{"あ"}, Cost: -0.5, POS: jaimport.POSPrefix},
		{Surface: "会う", Reading: reading.Sequence{"あ", "う"}, Cost: 1.25, POS: jaimport.POSVerb},
		{Surface: "愛", Reading: reading.Sequence{"あ", "い"}, Cost: 1.1, POS: jaimport.POSNoun},
		{Surface: "藍", Reading: reading.Sequence{"あ", "い"}, Cost: 1.3, POS: jaimport.POSNoun},
		{Surface: "あ", Reading: reading.Sequence{"あ"}, Cost: 3, POS: jaimport.POSParticle},
		{Surface: "ア", Reading: reading.Sequence{"あ"}, Cost: 4, POS: jaimport.POSOther},
	}
	// 15 種詞性各一筆，確保二進位的 posList 順序與 core/ja/lexicon 相同。
	pos := []jaimport.POS{
		jaimport.POSOther, jaimport.POSNoun, jaimport.POSVerb, jaimport.POSAdjective,
		jaimport.POSAdjectivalNoun, jaimport.POSAdverb, jaimport.POSParticle,
		jaimport.POSAuxiliary, jaimport.POSConjunction, jaimport.POSPrefix,
		jaimport.POSSuffix, jaimport.POSDeterminer, jaimport.POSInterjection,
		jaimport.POSNumber, jaimport.POSSymbol,
	}
	for i, p := range pos {
		entries = append(entries, jaimport.Entry{
			Surface: "語" + strconv.Itoa(i),
			Reading: reading.Sequence{reading.Unit(string(rune('ぁ' + i)))},
			Cost:    float64(i) / 10,
			POS:     p,
		})
	}
	conns := []jaimport.Connection{
		{Left: jaimport.POSBOS, Right: jaimport.POSNoun, Cost: 0.2},
		{Left: jaimport.POSNoun, Right: jaimport.POSParticle, Cost: 0.3},
		{Left: jaimport.POSParticle, Right: jaimport.POSEOS, Cost: 0.4},
		{Left: jaimport.POSVerb, Right: jaimport.POSSuffix, Cost: 0.5},
	}
	lex, err := jaimport.NewLexicon(entries, conns)
	if err != nil {
		t.Fatalf("NewLexicon: %v", err)
	}
	return lex, lex.Encode()
}

func loadBoth(t *testing.T) (*lexicon.Mem, *lexicon.Bin) {
	t.Helper()
	pack, data := testPack(t)
	mem, err := lexicon.Load(bytes.NewReader(jaimport.TSV(pack.Entries())))
	if err != nil {
		t.Fatalf("Load(TSV): %v", err)
	}
	bin, err := lexicon.LoadBin(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("LoadBin: %v", err)
	}
	return mem, bin
}

func entryKey(e lexicon.Entry) string {
	return e.Surface + "|" + reading.Key(e.Reading) + "|" +
		strconv.FormatFloat(e.Cost, 'g', -1, 64) + "|" + string(e.POS)
}

func entryKeys(es []lexicon.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = entryKey(e)
	}
	return out
}

func TestBinMatchesMem(t *testing.T) {
	mem, bin := loadBoth(t)
	if mem.LongestWord() != bin.LongestWord() {
		t.Fatalf("LongestWord = %d, want %d", bin.LongestWord(), mem.LongestWord())
	}

	surfaces := []string{}
	readings := []reading.Sequence{}
	seenReading := map[string]bool{}
	mem.Walk(func(e lexicon.Entry) {
		surfaces = append(surfaces, e.Surface)
		if k := reading.Key(e.Reading); !seenReading[k] {
			seenReading[k] = true
			readings = append(readings, e.Reading)
		}
	})

	for _, s := range surfaces {
		if got, want := entryKeys(bin.Lookup(s)), entryKeys(mem.Lookup(s)); !reflect.DeepEqual(got, want) {
			t.Fatalf("Lookup(%q) = %#v, want %#v", s, got, want)
		}
		if !bin.Has(s) || !mem.Has(s) {
			t.Fatalf("Has(%q) = %v, want true", s, bin.Has(s))
		}
	}
	if bin.Has("存在しない") || mem.Has("存在しない") {
		t.Fatal("Has(不存在) 應為 false")
	}
	if got := bin.Lookup("存在しない"); got != nil {
		t.Fatalf("Lookup(不存在) = %#v", got)
	}

	for _, seq := range readings {
		if got, want := entryKeys(bin.Exact(seq)), entryKeys(mem.Exact(seq)); !reflect.DeepEqual(got, want) {
			t.Fatalf("Exact(%q) = %#v, want %#v", reading.Key(seq), got, want)
		}
		for n := 0; n <= len(seq); n++ {
			prefix := seq[:n]
			if got, want := entryKeys(bin.Prefixed(prefix)), entryKeys(mem.Prefixed(prefix)); !reflect.DeepEqual(got, want) {
				t.Fatalf("Prefixed(%q) = %#v, want %#v", reading.Key(prefix), got, want)
			}
			if bin.HasPrefix(prefix) != mem.HasPrefix(prefix) {
				t.Fatalf("HasPrefix(%q) = %v, want %v", reading.Key(prefix), bin.HasPrefix(prefix), mem.HasPrefix(prefix))
			}
		}
		for start := 0; start < len(seq); start++ {
			var got, want []string
			bin.Matches(seq, start, func(end int, e lexicon.Entry) {
				got = append(got, strconv.Itoa(end)+":"+entryKey(e))
			})
			mem.Matches(seq, start, func(end int, e lexicon.Entry) {
				want = append(want, strconv.Itoa(end)+":"+entryKey(e))
			})
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Matches(%q, %d) = %#v, want %#v", reading.Key(seq), start, got, want)
			}
		}
	}

	var walked []string
	bin.Walk(func(e lexicon.Entry) { walked = append(walked, entryKey(e)) })
	var walkedMem []string
	mem.Walk(func(e lexicon.Entry) { walkedMem = append(walkedMem, entryKey(e)) })
	if !reflect.DeepEqual(walked, walkedMem) {
		t.Fatalf("Walk = %#v, want %#v", walked, walkedMem)
	}
}

func TestBinMatrix(t *testing.T) {
	mem, bin := loadBoth(t)
	if bin.Matrix() == nil {
		t.Fatal("Matrix() = nil")
	}
	// 讀 TSV 版本沒有矩陣，改用 jaimport 的連接成本逐對比較。
	pack, _ := testPack(t)
	want := pack.Matrix()
	pos := []lexicon.POS{
		lexicon.POSOther, lexicon.POSNoun, lexicon.POSVerb, lexicon.POSAdjective,
		lexicon.POSAdjectivalNoun, lexicon.POSAdverb, lexicon.POSParticle,
		lexicon.POSAuxiliary, lexicon.POSConjunction, lexicon.POSPrefix,
		lexicon.POSSuffix, lexicon.POSDeterminer, lexicon.POSInterjection,
		lexicon.POSNumber, lexicon.POSSymbol, lexicon.POSBOS, lexicon.POSEOS,
	}
	for _, l := range pos {
		for _, r := range pos {
			if got, w := bin.Matrix().Cost(l, r), want.Cost(jaimport.POS(l), jaimport.POS(r)); got != w {
				t.Fatalf("Cost(%s, %s) = %v, want %v", l, r, got, w)
			}
		}
	}
	_ = mem
}

func TestLoadBinFromFile(t *testing.T) {
	_, data := testPack(t)
	path := filepath.Join(t.TempDir(), "lexicon.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := lexicon.LoadBinSize(f, st.Size())
	if err != nil {
		t.Fatalf("LoadBinSize: %v", err)
	}
	if got := bin.Lookup("明日"); len(got) != 1 || got[0].Surface != "明日" {
		t.Fatalf("Lookup(明日) = %#v", got)
	}
}

func TestLoadBinRejectsCorruption(t *testing.T) {
	_, data := testPack(t)
	cases := map[string][]byte{
		"空檔":      nil,
		"錯 magic": append([]byte("0CJLEXXX"), data[len("0CJLEX01"):]...),
		"截半":      append([]byte(nil), data[:len(data)/2]...),
		"少一位元":    append([]byte(nil), data[:len(data)-1]...),
		"尾端多餘":    append(append([]byte(nil), data...), 0),
		"詞條數過大":   append([]byte("0CJLEX01"), 1, 1, 0xff, 0xff, 0xff, 0xff, 0x0f),
		"零詞條":     append([]byte("0CJLEX01"), 1, 0),
	}
	for name, b := range cases {
		if _, err := lexicon.LoadBin(bytes.NewReader(b)); err == nil {
			t.Errorf("%s：LoadBin 應失敗", name)
		}
	}
}

func TestBinRandomizedMatchesMem(t *testing.T) {
	// 固定的讀音字母表，包含小寫かな、長音符與重複前綴，讓 trie 有分岔與同音詞。
	alphabet := []string{"あ", "い", "う", "え", "お", "か", "き", "く", "け", "こ",
		"さ", "し", "す", "せ", "そ", "ゃ", "ゅ", "ょ", "ー"}
	pos := []jaimport.POS{
		jaimport.POSNoun, jaimport.POSVerb, jaimport.POSAdjective,
		jaimport.POSParticle, jaimport.POSAuxiliary, jaimport.POSAdverb,
	}
	var entries []jaimport.Entry
	x := uint64(0x9e3779b97f4a7c15)
	next := func(n int) int {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		return int(x % uint64(n))
	}
	for i := 0; i < 400; i++ {
		n := 1 + next(6)
		seq := make(reading.Sequence, n)
		for j := range seq {
			seq[j] = reading.Unit(alphabet[next(len(alphabet))])
		}
		entries = append(entries, jaimport.Entry{
			Surface: "語" + strconv.Itoa(i%37),
			Reading: seq,
			Cost:    float64(next(10000)-5000) / 1000,
			POS:     pos[next(len(pos))],
		})
	}
	lex, err := jaimport.NewLexicon(entries, nil)
	if err != nil {
		t.Fatalf("NewLexicon: %v", err)
	}
	mem, err := lexicon.Load(bytes.NewReader(jaimport.TSV(lex.Entries())))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	bin, err := lexicon.LoadBin(bytes.NewReader(lex.Encode()))
	if err != nil {
		t.Fatalf("LoadBin: %v", err)
	}
	if mem.LongestWord() != bin.LongestWord() {
		t.Fatalf("LongestWord = %d, want %d", bin.LongestWord(), mem.LongestWord())
	}

	seenSurface := map[string]bool{}
	seenReading := map[string]bool{}
	for _, e := range lex.Entries() {
		if !seenSurface[e.Surface] {
			seenSurface[e.Surface] = true
			if got, want := entryKeys(bin.Lookup(e.Surface)), entryKeys(mem.Lookup(e.Surface)); !reflect.DeepEqual(got, want) {
				t.Fatalf("Lookup(%q) differs", e.Surface)
			}
		}
		k := reading.Key(e.Reading)
		if !seenReading[k] {
			seenReading[k] = true
			if got, want := entryKeys(bin.Exact(e.Reading)), entryKeys(mem.Exact(e.Reading)); !reflect.DeepEqual(got, want) {
				t.Fatalf("Exact(%q) differs", k)
			}
			for i := 0; i <= len(e.Reading); i++ {
				p := e.Reading[:i]
				if got, want := entryKeys(bin.Prefixed(p)), entryKeys(mem.Prefixed(p)); !reflect.DeepEqual(got, want) {
					t.Fatalf("Prefixed(%q) differs", reading.Key(p))
				}
			}
			for start := 0; start < len(e.Reading); start++ {
				var got, want []string
				bin.Matches(e.Reading, start, func(end int, x lexicon.Entry) {
					got = append(got, strconv.Itoa(end)+":"+entryKey(x))
				})
				mem.Matches(e.Reading, start, func(end int, x lexicon.Entry) {
					want = append(want, strconv.Itoa(end)+":"+entryKey(x))
				})
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("Matches(%q, %d) differs", k, start)
				}
			}
		}
	}
}

func TestBinEmptyQueries(t *testing.T) {
	_, bin := loadBoth(t)
	if got := bin.Exact(reading.Sequence{"ん"}); got != nil {
		t.Fatalf("Exact(ん) = %#v", got)
	}
	if got := bin.Prefixed(reading.Sequence{"ん"}); got != nil {
		t.Fatalf("Prefixed(ん) = %#v", got)
	}
	if bin.HasPrefix(reading.Sequence{"ん"}) {
		t.Fatal("HasPrefix(ん) 應為 false")
	}
	var called bool
	bin.Matches(reading.Sequence{"ん"}, 0, func(int, lexicon.Entry) { called = true })
	if called {
		t.Fatal("Matches 不應回呼")
	}
	bin.Matches(reading.Sequence{"あ"}, -1, func(int, lexicon.Entry) { called = true })
	bin.Matches(reading.Sequence{"あ"}, 2, func(int, lexicon.Entry) { called = true })
	if called {
		t.Fatal("越界的 start 不應回呼")
	}
}
