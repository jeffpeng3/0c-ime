package jaeval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCorpusFixture(t *testing.T) {
	c, sha, err := LoadCorpus("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "jaeval-test-fixture" || len(c.Cases) != 3 || c.Count != 3 {
		t.Fatalf("unexpected corpus: %+v", c)
	}
	if len(sha) != 64 {
		t.Fatalf("corpus sha256 = %q", sha)
	}
	if c.Cases[1].Input != "ㄐㄧㄣㄊㄧㄢkyou" {
		t.Fatalf("case order changed: %+v", c.Cases)
	}
}

func TestCorpusValidateRejectsBadInput(t *testing.T) {
	base := Corpus{
		Schema:  CorpusSchema,
		Name:    "fixture",
		License: "CC0-1.0",
		Source:  "test",
		Count:   1,
		Cases: []Case{{
			ID:       "a",
			Category: "ja-word",
			Input:    "kyou",
			Expect:   "今日",
		}},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid corpus rejected: %v", err)
	}
	tests := []struct {
		name string
		edit func(*Corpus)
	}{
		{"schema", func(c *Corpus) { c.Schema = 2 }},
		{"count", func(c *Corpus) { c.Count = 2 }},
		{"duplicate", func(c *Corpus) {
			c.Cases = append(c.Cases, c.Cases[0])
			c.Count = 2
		}},
		{"category", func(c *Corpus) { c.Cases[0].Category = "unknown" }},
		{"empty input", func(c *Corpus) { c.Cases[0].Input = "  " }},
		{"empty expect", func(c *Corpus) { c.Cases[0].Expect = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			c.Cases = append([]Case(nil), base.Cases...)
			tt.edit(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("invalid corpus accepted")
			}
		})
	}
}

func TestSnapshotCompareAndUpdate(t *testing.T) {
	results := []CaseResult{
		{ID: "a", Category: "ja-word", Input: "kyou", Expect: "今日", Output: "今日", Candidates: []string{"今日", "きょう"}, Rank: 1},
		{ID: "b", Category: "mixed-zh-ja", Input: "ㄐㄧㄣㄊㄧㄢkyou", Expect: "今天今日", Output: "今天今日", Candidates: []string{"今天今日"}, Rank: 1},
	}
	want := BuildSnapshot("fixture", "corpus-sha", results)
	if len(want.SHA256) != 64 {
		t.Fatalf("snapshot sha256 = %q", want.SHA256)
	}
	if diff := CompareSnapshots(want, want); len(diff) != 0 {
		t.Fatalf("identical snapshot differs: %v", diff)
	}

	changedResults := append([]CaseResult(nil), results...)
	changedResults[0].Output = "きょう"
	changedResults[0].Candidates = []string{"きょう", "今日"}
	changedResults[0].Rank = 0
	got := BuildSnapshot("fixture", "corpus-sha", changedResults)
	diffs := CompareSnapshots(want, got)
	if !hasDiff(diffs, "a", "output") || !hasDiff(diffs, "a", "rank") {
		t.Fatalf("changed case not reported: %v", diffs)
	}
	if hasDiff(diffs, "b", "") {
		t.Fatalf("unchanged case reported: %v", diffs)
	}

	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := WriteSnapshot(path, got); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SHA256 != got.SHA256 {
		t.Fatalf("loaded sha %s, want %s", loaded.SHA256, got.SHA256)
	}
	if diff := CompareSnapshots(got, loaded); len(diff) != 0 {
		t.Fatalf("update round-trip differs: %v", diff)
	}
	if !strings.Contains(FormatDiffs(diffs), "case a: output") {
		t.Fatalf("diff is not readable:\n%s", FormatDiffs(diffs))
	}
}

func hasDiff(diffs []Diff, id, field string) bool {
	for _, d := range diffs {
		if d.ID == id && (field == "" || d.Field == field) {
			return true
		}
	}
	return false
}

func TestLoadSnapshotRejectsBadChecksum(t *testing.T) {
	s := BuildSnapshot("fixture", "corpus-sha", []CaseResult{{
		ID: "a", Category: "ja-word", Input: "kyou", Expect: "今日", Output: "今日", Candidates: []string{"今日"}, Rank: 1,
	}})
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := WriteSnapshot(path, s); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `"sha256": "`, `"sha256": "0`, 1))
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSnapshot(path); err == nil {
		t.Fatal("snapshot with a bad checksum was accepted")
	}
}

func TestReportAggregation(t *testing.T) {
	results := []CaseResult{
		{ID: "a", Category: "ja-word", Output: "今日", Candidates: []string{"今日"}, Rank: 1},
		{ID: "b", Category: "ja-word", Output: "学校", Candidates: []string{"学校", "がっこう"}, Rank: 2},
		{ID: "c", Category: "mixed-zh-ja", Output: "今天今日", Candidates: []string{"今天今日", "x", "y"}, Rank: 1},
	}
	rep := BuildReport(results)
	if rep.Cases != 3 || rep.Top1 != 2 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.AvgCandidates != 2 {
		t.Fatalf("avg candidates = %v, want 2", rep.AvgCandidates)
	}
	if rep.LongestID != "c" || rep.LongestRunes != 4 {
		t.Fatalf("longest = %s/%d", rep.LongestID, rep.LongestRunes)
	}
	if len(rep.ByCategory) != 2 || rep.ByCategory[0].Category != "ja-word" || rep.ByCategory[1].Category != "mixed-zh-ja" {
		t.Fatalf("categories not sorted: %+v", rep.ByCategory)
	}
	if rep.ByCategory[0].Top1 != 1 || rep.ByCategory[0].AvgCandidates != 1.5 {
		t.Fatalf("ja-word category = %+v", rep.ByCategory[0])
	}
	text := FormatReport(rep)
	for _, want := range []string{"cases: 3", "2/3", "average candidates: 2.00", "longest conversion: c (4 runes)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q:\n%s", want, text)
		}
	}
}

func TestSplitZhuyinAndParts(t *testing.T) {
	syls, ok := splitZhuyin("ㄐㄧㄣㄊㄧㄢ")
	if !ok {
		t.Fatal("valid zhuyin rejected")
	}
	if len(syls) != 2 || syls[0] != "ㄐㄧㄣ" || syls[1] != "ㄊㄧㄢ" {
		t.Fatalf("syllables = %v", syls)
	}
	syls, ok = splitZhuyin("ㄨㄛˇㄉㄜ˙")
	if !ok || len(syls) != 2 || syls[0] != "ㄨㄛˇ" || syls[1] != "ㄉㄜ˙" {
		t.Fatalf("toned syllables = %v ok=%v", syls, ok)
	}
	if _, ok := splitZhuyin("ㄐㄧㄣabc"); ok {
		t.Fatal("mixed zhuyin/latin run accepted as zhuyin")
	}

	parts := splitParts("ㄐㄧㄣㄊㄧㄢkyou!")
	if len(parts) != 3 || parts[0].kind != partZhuyin || parts[1].kind != partLatin || parts[2].kind != partLiteral {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestFallbackWithoutData(t *testing.T) {
	e, info, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if info.JapaneseLoaded || info.ChineseLoaded {
		t.Fatalf("fallback engine loaded data: %+v", info)
	}
	if got := e.ConvertString("kyou"); len(got) == 0 || got[0] != "きょう" {
		t.Fatalf("kana fallback = %v", got)
	}
	if got := e.ConvertString("hello"); len(got) == 0 || got[0] != "hello" {
		t.Fatalf("english fallback = %v", got)
	}
	if got := e.ConvertString("ㄐㄧㄣㄊㄧㄢ"); len(got) == 0 || got[0] != "ㄐㄧㄣㄊㄧㄢ" {
		t.Fatalf("zhuyin fallback = %v", got)
	}
}
