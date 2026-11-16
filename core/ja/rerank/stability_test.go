package rerank

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
)

func result(clauses ...string) lang.Result {
	var res lang.Result
	for _, s := range clauses {
		res.Clauses = append(res.Clauses, lang.Clause{
			Surface:  s,
			Reading:  seqUnits("かな"),
			Segments: []lang.CandidateSegment{{Surface: s, Reading: seqUnits("かな")}},
		})
	}
	return res
}

func TestTrackerStablePrefixGrowsWithInput(t *testing.T) {
	tr := NewTracker()
	a := tr.Observe(seqUnits("きょう"), result("今日"))
	if a.StableClauses != 0 || a.StableSurface != "" {
		t.Fatalf("第一筆沒有穩定前綴：%+v", a)
	}
	b := tr.Observe(seqUnits("きょうは"), result("今日", "は"))
	if b.StableSurface != "今日" || b.StableClauses != 1 || !b.Extended {
		t.Fatalf("延伸後穩定前綴 = %+v", b)
	}
	c := tr.Observe(seqUnits("きょうはいい"), result("今日", "は", "いい"))
	if c.StableSurface != "今日は" || c.StableClauses != 2 || !c.Extended {
		t.Fatalf("第二次延伸 = %+v", c)
	}
	if !c.Changed {
		t.Fatal("詞面改變時 Changed 應為 true")
	}
}

func TestTrackerResetsWhenReadingChanges(t *testing.T) {
	tr := NewTracker()
	tr.Observe(seqUnits("きょうは"), result("今日", "は"))
	got := tr.Observe(seqUnits("きよう"), result("きよう"))
	if got.StableSurface != "" || got.StableClauses != 0 || got.Extended {
		t.Fatalf("讀音改動後 = %+v", got)
	}
	tr.Reset()
	got = tr.Observe(seqUnits("きょう"), result("今日"))
	if got.Extended || got.StableSurface != "" {
		t.Fatalf("Reset 後 = %+v", got)
	}
}

func TestTrackerClauseChangeCutsStablePrefix(t *testing.T) {
	tr := NewTracker()
	tr.Observe(seqUnits("はしをわたる"), result("橋", "を", "渡る"))
	got := tr.Observe(seqUnits("はしをわたるひと"), result("箸", "を", "渡る", "人"))
	if got.StableSurface != "" || got.StableClauses != 0 {
		t.Fatalf("第一文節改變後 = %+v", got)
	}
}

func TestReadingPrefixIsUnitAware(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"", "は し", true},
		{"は", "は し", true},
		{"は", "はし", false},
		{"は し", "は し", true},
		{"は し", "は し た", true},
		{"し", "は し", false},
	} {
		if got := readingPrefix(tc.a, tc.b); got != tc.want {
			t.Errorf("readingPrefix(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
