package jadiclex

import (
	"os"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
)

const referencePackEnv = "OC_JADIC_REFERENCE_PACK"

func referenceSource(t *testing.T) *Source {
	t.Helper()
	path := os.Getenv(referencePackEnv)
	if path == "" {
		t.Skipf("未設定 %s，跳過真實 pack 測試", referencePackEnv)
	}
	src, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func seq(s string) reading.Sequence {
	out := make(reading.Sequence, 0, len(s))
	for _, r := range s {
		out = append(out, reading.Unit(r))
	}
	return out
}

// 參考 pack 的 あした 讀音要能查到 明日，且詞條帶讀音與 signed 成本。
func TestReferencePackReadingIndex(t *testing.T) {
	src := referenceSource(t)
	if src.Entries() != 1147021 {
		t.Fatalf("詞條數 = %d，want 1147021", src.Entries())
	}
	ashita := seq("あした")
	var matches []lexicon.Entry
	src.Matches(ashita, 0, func(end int, e lexicon.Entry) {
		if end != len(ashita) {
			return
		}
		if reading.Key(e.Reading) != reading.Key(ashita) {
			t.Fatalf("%q 的讀音 = %q，want あした", e.Surface, reading.Key(e.Reading))
		}
		matches = append(matches, e)
	})
	if !hasSurface(matches, "明日") {
		t.Fatalf("あした 的候選缺少 明日：%#v", surfaces(matches))
	}
	for _, e := range matches {
		if e.Surface == "明日" {
			if e.POS != lexicon.POSNoun {
				t.Fatalf("明日 的詞性 = %q，want noun", e.POS)
			}
			if e.Cost < lexicon.MinWordCost || e.Cost > lexicon.MaxWordCost {
				t.Fatalf("明日 的成本 = %v 超出容許範圍", e.Cost)
			}
		}
	}
	// 同一份 pack 的兩次查詢順序必須相同。
	var again []string
	src.Matches(ashita, 0, func(end int, e lexicon.Entry) {
		if end == len(ashita) {
			again = append(again, e.Surface)
		}
	})
	if got, want := surfaces(matches), again; len(got) != len(want) || !equalStrings(got, want) {
		t.Fatalf("查詢順序不穩定：%#v vs %#v", got, want)
	}
	if !src.HasPrefix(seq("あし")) || len(src.Prefixed(seq("あし"))) == 0 {
		t.Fatal("あし 應有較長的詞條前綴")
	}
	if len(src.Exact(ashita)) == 0 {
		t.Fatal("Exact(あした) 不應為空")
	}
}

func hasSurface(entries []lexicon.Entry, surface string) bool {
	for _, e := range entries {
		if e.Surface == surface {
			return true
		}
	}
	return false
}

func surfaces(entries []lexicon.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Surface
	}
	return out
}

func equalStrings(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
