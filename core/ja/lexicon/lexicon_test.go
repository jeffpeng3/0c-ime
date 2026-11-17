package lexicon_test

import (
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
)

func mustLoad(t *testing.T, src string) *lexicon.Mem {
	t.Helper()
	m, err := lexicon.Load(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m
}

func TestLoadSplitsKanaIntoUnits(t *testing.T) {
	m := mustLoad(t, "# 詞面\t讀音\t成本\t詞性\n明日\tあした\t1.5\tnoun\n味\tあ じ\t2\tnoun\n")
	if got := reading.Key(m.Lookup("明日")[0].Reading); got != "あ し た" {
		t.Fatalf("明日 讀音 = %q", got)
	}
	e := m.Lookup("味")[0]
	if got := reading.Key(e.Reading); got != "あ じ" {
		t.Fatalf("味 讀音 = %q", got)
	}
	if e.Cost != 2 || e.POS != lexicon.POSNoun {
		t.Fatalf("味 = %#v", e)
	}
}

func TestLoadRejectsBadRows(t *testing.T) {
	for _, src := range []string{
		"明日\n",
		"明日\t\n",
		"明日\tあした\t-32.768\tnoun\n",
		"明日\tあした\t32.768\tnoun\n",
		"明日\tあした\tNaN\tnoun\n",
		"明日\tあした\tabc\tnoun\n",
		"明日\tあした\t1\tunknown\n",
	} {
		if _, err := lexicon.Load(strings.NewReader(src)); err == nil {
			t.Errorf("Load(%q) 應失敗", src)
		}
	}
}

func TestLoadAcceptsSignedCost(t *testing.T) {
	m := mustLoad(t, "負\tふ\t-8.313\tnoun\n零\tれい\t0\tnoun\n正\tせい\t2.5\tnoun\n")
	if got := m.Lookup("負")[0].Cost; got != -8.313 {
		t.Fatalf("負成本 = %v", got)
	}
	if got := m.Lookup("零")[0].Cost; got != 0 {
		t.Fatalf("零成本 = %v", got)
	}
	if got := m.Lookup("正")[0].Cost; got != 2.5 {
		t.Fatalf("正成本 = %v", got)
	}
}

func TestTrieQueries(t *testing.T) {
	m := lexicon.NewMem()
	for _, e := range []lexicon.Entry{
		{Surface: "明日", Reading: reading.Sequence{"あ", "し", "た"}, Cost: 1, POS: lexicon.POSNoun},
		{Surface: "味", Reading: reading.Sequence{"あ", "じ"}, Cost: 1, POS: lexicon.POSNoun},
		{Surface: "アヒル", Reading: reading.Sequence{"あ", "ひ", "る"}, Cost: 1, POS: lexicon.POSNoun},
	} {
		if err := m.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.Exact(reading.Sequence{"あ", "じ"}); len(got) != 1 || got[0].Surface != "味" {
		t.Fatalf("Exact = %#v", got)
	}
	if got := m.Exact(reading.Sequence{"あ"}); got != nil {
		t.Fatalf("Exact(前綴) = %#v", got)
	}
	pre := m.Prefixed(reading.Sequence{"あ"})
	if len(pre) != 3 {
		t.Fatalf("Prefixed = %#v", pre)
	}
	if !m.HasPrefix(reading.Sequence{"あ", "し"}) {
		t.Fatal("HasPrefix あし 應為 true")
	}
	if m.HasPrefix(reading.Sequence{"あ", "め"}) {
		t.Fatal("HasPrefix あめ 應為 false")
	}
	if m.LongestWord() != 3 {
		t.Fatalf("LongestWord = %d", m.LongestWord())
	}

	seq := reading.Sequence{"あ", "し", "た"}
	ends := map[int]string{}
	m.Matches(seq, 0, func(end int, e lexicon.Entry) { ends[end] = e.Surface })
	if ends[3] != "明日" || len(ends) != 1 {
		t.Fatalf("Matches(あした) = %#v", ends)
	}
	ends = map[int]string{}
	m.Matches(seq, 1, func(end int, e lexicon.Entry) { ends[end] = e.Surface })
	if len(ends) != 0 {
		t.Fatalf("Matches(した) = %#v", ends)
	}
	ends = map[int]string{}
	m.Matches(reading.Sequence{"あ", "じ"}, 0, func(end int, e lexicon.Entry) { ends[end] = e.Surface })
	if ends[2] != "味" {
		t.Fatalf("Matches(あじ) = %#v", ends)
	}
}

func TestAddValidates(t *testing.T) {
	m := lexicon.NewMem()
	for _, e := range []lexicon.Entry{
		{Surface: "", Reading: reading.Sequence{"あ"}, POS: lexicon.POSNoun},
		{Surface: "あ", Reading: nil, POS: lexicon.POSNoun},
		{Surface: "あ", Reading: reading.Sequence{" "}, POS: lexicon.POSNoun},
		{Surface: "あ", Reading: reading.Sequence{"あ"}, POS: lexicon.POSVerb, Cost: lexicon.MinWordCost - 0.001},
		{Surface: "あ", Reading: reading.Sequence{"あ"}, POS: lexicon.POSVerb, Cost: lexicon.MaxWordCost + 0.001},
		{Surface: "あ", Reading: reading.Sequence{"あ"}, POS: "bad"},
	} {
		if err := m.Add(e); err == nil {
			t.Errorf("Add(%#v) 應失敗", e)
		}
	}
	if err := m.Add(lexicon.Entry{Surface: "負", Reading: reading.Sequence{"ふ"}, POS: lexicon.POSNoun, Cost: -1}); err != nil {
		t.Fatalf("負成本應被接受：%v", err)
	}
}

func TestLoadConnections(t *testing.T) {
	m, err := lexicon.LoadConnections(strings.NewReader(
		"# left\tright\tcost\n<noun>\t<noun>\t0.5\n<s>\tnoun\t0\nnoun\t</s>\t0.25\n"))
	if err == nil {
		t.Fatal("不存在的詞性應失敗")
	}
	_ = m

	m, err = lexicon.LoadConnections(strings.NewReader(
		"# left\tright\tcost\nnoun\tnoun\t0.5\n<s>\tnoun\t0\nnoun\t</s>\t0.25\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Cost(lexicon.POSNoun, lexicon.POSNoun); got != 0.5 {
		t.Fatalf("noun->noun = %v", got)
	}
	if got := m.Cost(lexicon.POSBOS, lexicon.POSNoun); got != 0 {
		t.Fatalf("BOS->noun = %v", got)
	}
	if got := m.Cost(lexicon.POSVerb, lexicon.POSNoun); got != lexicon.DefaultConnectionCost {
		t.Fatalf("未指定 = %v", got)
	}
}
