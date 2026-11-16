package eval

import (
	"encoding/json"
	"os"
	"testing"
)

const (
	jaRulesPath   = "../../testdata/ja/rules.json"
	jaBaseline    = "../../testdata/ja/baseline.json"
	jaT03Fixture  = "../../scripts/spike/ja-zenz/fixtures/anthy_sample200.json"
	jaDataDir     = "../../data/ja"
	anthyCorpusID = "anthy-corpus1"
)

func loadJAFixture(t *testing.T) (*JARules, string, []JACorpusEntry) {
	t.Helper()
	rules, hash, err := LoadJARules(jaRulesPath)
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := LoadJACorpusSet(rules, jaRulesPath)
	if err != nil {
		t.Fatal(err)
	}
	return rules, hash, corpora
}

func TestJACorpusProvenance(t *testing.T) {
	rules, _, corpora := loadJAFixture(t)
	if err := CheckJACorpusProvenance(corpora, rules, jaT03Fixture); err != nil {
		t.Fatal(err)
	}

	var anthy *JACorpusEntry
	seen := map[string]bool{}
	for i := range corpora {
		seen[corpora[i].Corpus.Name] = true
		if corpora[i].Corpus.Name == anthyCorpusID {
			anthy = &corpora[i]
		}
	}
	if anthy == nil {
		t.Fatal("anthy-corpus1 不在題庫內")
	}
	for _, name := range []string{"ajimee-200", "modern-500", "hard-cases"} {
		if !seen[name] {
			t.Errorf("缺少題庫 %s", name)
		}
	}
	if anthy.Corpus.Count != 200 || len(anthy.Corpus.Items) != 200 {
		t.Fatalf("Anthy 題數 = %d/%d，want 200", anthy.Corpus.Count, len(anthy.Corpus.Items))
	}
	boundary := 0
	for _, item := range anthy.Corpus.Items {
		if len(item.Segments) > 0 {
			boundary++
		}
	}
	if boundary != 199 {
		t.Fatalf("Anthy 邊界題數 = %d，want 199", boundary)
	}
	if got, want := anthy.Corpus.CorpusFiles["corpus.1.txt"], "da187509c065ad294626d4241b08265aad2b898148cb857b8cbf7c4f1e2ad9db"; got != want {
		t.Fatalf("Anthy corpus.1 來源雜湊 = %s，want %s", got, want)
	}
	for _, c := range corpora {
		if c.Corpus.Name == anthyCorpusID && c.Corpus.Source != "Anthy 9100h calctrans/corpus.1.txt" {
			t.Errorf("Anthy 來源 = %q", c.Corpus.Source)
		}
		if c.Corpus.Name == "modern-500" && len(c.Corpus.Items) != 500 {
			t.Errorf("modern-500 題數 = %d，want 500", len(c.Corpus.Items))
		}
	}
}

func TestJAFixtureReusesT03Corpus1(t *testing.T) {
	raw, err := os.ReadFile(jaT03Fixture)
	if err != nil {
		t.Fatal(err)
	}
	var t03 struct {
		CorpusFiles map[string]string `json:"corpus_files"`
		Items       []struct {
			Reading string `json:"reading"`
			Surface string `json:"surface"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &t03); err != nil {
		t.Fatal(err)
	}
	if len(t03.CorpusFiles) != 1 || t03.CorpusFiles["corpus.1.txt"] == "" {
		t.Fatalf("T03 fixture 不是 corpus.1-only：%#v", t03.CorpusFiles)
	}

	_, _, corpora := loadJAFixture(t)
	for _, c := range corpora {
		if c.Corpus.Name != anthyCorpusID {
			continue
		}
		if got := c.Corpus.Count; got != len(t03.Items) {
			t.Fatalf("Anthy 題數 = %d，T03 = %d", got, len(t03.Items))
		}
		want := map[string]bool{}
		for _, item := range t03.Items {
			want[item.Reading+"\x00"+item.Surface] = true
		}
		for _, item := range c.Corpus.Items {
			if !want[item.Reading+"\x00"+item.Surface] {
				t.Errorf("題目 %q 不在 T03 corpus.1 fixture 內", item.Surface)
			}
		}
	}
}

func TestJARunDeterministic(t *testing.T) {
	rules, hash, corpora := loadJAFixture(t)
	dec, engine, err := LoadJADecoder(jaDataDir)
	if err != nil {
		t.Fatal(err)
	}
	opts := JAOptions{TopN: rules.TopN, TimeoutMS: rules.TimeoutMS}
	first := RunJA(dec, engine, rules, hash, corpora, opts)
	second := RunJA(dec, engine, rules, hash, corpora, opts)
	a, err := DeterminismSignature(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DeterminismSignature(second)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("同一份資料連跑兩次，非延遲結果不同")
	}
}

func TestJARegressionGate(t *testing.T) {
	rules, hash, corpora := loadJAFixture(t)
	dec, engine, err := LoadJADecoder(jaDataDir)
	if err != nil {
		t.Fatal(err)
	}
	report := RunJA(dec, engine, rules, hash, corpora, JAOptions{TopN: rules.TopN, TimeoutMS: rules.TimeoutMS})
	base, err := LoadJABaseline(jaBaseline)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range JARegression(report, base, hash, rules.Gate) {
		t.Error(problem)
	}
}
