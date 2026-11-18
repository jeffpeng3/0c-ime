package eval

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/score"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
	"github.com/0c-biggo/0c-ime/internal/datadir"
)

const (
	fuzzyCorpusPath   = "../../testdata/fuzzy/fuzzy-input.json"
	fuzzyManifestPath = "../../testdata/fuzzy/fuzzy-input.manifest.json"
)

func TestFuzzyCorpusFrozen(t *testing.T) {
	built, err := BuildFuzzyCorpus(zhGoldenCorpusPath, jaLexCorpusPath, DefaultFuzzyBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(fuzzyCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeJSON(t, built); !bytes.Equal(got, want) {
		t.Fatalf("testdata/fuzzy/fuzzy-input.json is stale; rerun `go run ./cmd/0c-eval-fuzzy build`")
	}
	if built.Count != 919 {
		t.Fatalf("count = %d, want 919", built.Count)
	}
}

func TestFuzzyCorpusDeterministic(t *testing.T) {
	opts := DefaultFuzzyBuildOptions()
	a, err := BuildFuzzyCorpus(zhGoldenCorpusPath, jaLexCorpusPath, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildFuzzyCorpus(zhGoldenCorpusPath, jaLexCorpusPath, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodeJSON(t, a), encodeJSON(t, b)) {
		t.Fatal("two builds from the same sources differ")
	}
}

func TestFuzzyCorpusManifestPinned(t *testing.T) {
	corpus, hash, err := LoadFuzzyCorpus(fuzzyCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadFuzzyManifest(fuzzyManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if problems := FuzzyCorpusProblems(corpus, hash, manifest); len(problems) > 0 {
		t.Fatalf("manifest problems: %v", problems)
	}
	if manifest.Models.Used {
		t.Fatal("manifest must not claim model use")
	}
	if manifest.Kinds[FuzzyWrongKey] == 0 || manifest.Kinds[FuzzyMissingKey] == 0 || manifest.Kinds[FuzzyDoubledKey] == 0 || manifest.Kinds[FuzzyNearSound] == 0 {
		t.Fatalf("manifest kinds = %v", manifest.Kinds)
	}
	if manifest.Languages[FuzzyLangZh] == 0 || manifest.Languages[FuzzyLangJa] == 0 {
		t.Fatalf("manifest languages = %v", manifest.Languages)
	}
}

func TestFuzzyCorpusMutations(t *testing.T) {
	corpus, _, err := LoadFuzzyCorpus(fuzzyCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, item := range corpus.Items {
		kinds[item.Language+"/"+item.Kind]++
		if item.Input == item.OriginalInput {
			if item.Kind == FuzzyDoubledKey && item.Language == FuzzyLangZh {
				// 靜態注音排列把同一個鍵重按視為同一個符號；這是真實的
				// 冪等行為，不是題庫沒擾動。
			} else {
				t.Errorf("%s: %s did not change the input", item.ID, item.Kind)
			}
		}
		if item.Kind == FuzzyDoubledKey && item.Mutation.From != item.Mutation.To {
			t.Errorf("%s: doubled-key mutation = %+v", item.ID, item.Mutation)
		}
		if item.Kind == FuzzyNearSound {
			if item.Language != FuzzyLangZh || item.NoisyReading == item.Reading {
				t.Errorf("%s: near-sound case is not a zhuyin reading change: %+v", item.ID, item)
			}
			if len(zhuyin.ParseReading(item.NoisyReading)) != len(zhuyin.ParseReading(item.Reading)) {
				t.Errorf("%s: near-sound changed the syllable count", item.ID)
			}
		}
		if item.Language == FuzzyLangJa {
			units, pending, _ := composeRomaji(item.Input)
			if units != item.NoisyReading || pending != item.Pending {
				t.Errorf("%s: romaji replay = %q/%q, corpus = %q/%q", item.ID, units, pending, item.NoisyReading, item.Pending)
			}
		}
	}
	for _, language := range FuzzyLanguages() {
		for _, kind := range []string{FuzzyWrongKey, FuzzyMissingKey, FuzzyDoubledKey} {
			if kinds[language+"/"+kind] == 0 {
				t.Errorf("no %s/%s cases", language, kind)
			}
		}
	}
	if kinds[FuzzyLangZh+"/"+FuzzyNearSound] == 0 {
		t.Error("no zhuyin near-sound cases")
	}
}

func TestFuzzyRunnerKeepsFallbackInDenominator(t *testing.T) {
	corpus, hash, err := LoadFuzzyCorpus(fuzzyCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	zhOnly := filterFuzzyTestCorpus(corpus, FuzzyLangZh)
	eng, err := datadir.Open(zhGoldenDataDir)
	if err != nil {
		t.Fatal(err)
	}
	env := &FuzzyEnv{Engine: eng, Info: FuzzyEngineInfo{DataDir: zhGoldenDataDir, Models: "none", ModelsUsed: false}}
	rep, err := RunFuzzyCorpus(env, zhOnly, hash, FuzzyRunOptions{TopN: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Cases) != zhOnly.Count {
		t.Fatalf("%d cases, want %d", len(rep.Cases), zhOnly.Count)
	}
	var all *FuzzyGroupReport
	for i := range rep.Groups {
		g := &rep.Groups[i]
		if g.Language == "" && g.Kind == "" {
			all = g
		}
		if g.N != g.Raw.N || g.N != g.Fallback.N {
			t.Fatalf("%s/%s denominator mismatch: n=%d raw=%d fallback=%d", g.Language, g.Kind, g.N, g.Raw.N, g.Fallback.N)
		}
	}
	if all == nil || all.N != zhOnly.Count {
		t.Fatalf("overall group = %+v, want n=%d", all, zhOnly.Count)
	}
	if all.Degraded.Cases > all.N {
		t.Fatalf("degraded %d exceeds denominator %d", all.Degraded.Cases, all.N)
	}
	if rep.Engine.ModelsUsed || rep.Config.Models != "none" {
		t.Fatalf("report claims model use: %+v / %+v", rep.Engine, rep.Config)
	}
	if rep.Ranker.Requests != rep.Ranker.Fallback || rep.Ranker.Model != 0 || rep.Ranker.Timeout != 0 {
		t.Fatalf("ranker counters = %+v, want every request served by fallback", rep.Ranker)
	}
	for _, c := range rep.Cases {
		if c.Rank > 0 && c.FallbackOutcome != "fallback" {
			t.Fatalf("%s: rank %d with fallback outcome %q", c.ID, c.Rank, c.FallbackOutcome)
		}
		if c.Rank == 0 && c.FallbackOutcome != "none" && c.FallbackOutcome != "fallback" {
			t.Fatalf("%s: unexpected fallback outcome %q", c.ID, c.FallbackOutcome)
		}
	}
}

func TestFuzzyRunnerDeterministic(t *testing.T) {
	corpus, hash, err := LoadFuzzyCorpus(fuzzyCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	zhOnly := filterFuzzyTestCorpus(corpus, FuzzyLangZh)
	eng, err := datadir.Open(zhGoldenDataDir)
	if err != nil {
		t.Fatal(err)
	}
	env := &FuzzyEnv{Engine: eng, Info: FuzzyEngineInfo{DataDir: zhGoldenDataDir, Models: "none"}}
	first, err := RunFuzzyCorpus(env, zhOnly, hash, FuzzyRunOptions{TopN: 8})
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunFuzzyCorpus(env, zhOnly, hash, FuzzyRunOptions{TopN: 8})
	if err != nil {
		t.Fatal(err)
	}
	a, err := FuzzyDeterminismSignature(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := FuzzyDeterminismSignature(second)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("two runs over the same corpus differ")
	}
}

func TestFuzzyJAPathWithStubDecoder(t *testing.T) {
	corpus, hash, err := LoadFuzzyCorpus(fuzzyCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	jaOnly := filterFuzzyTestCorpus(corpus, FuzzyLangJa)
	eng, err := datadir.Open(zhGoldenDataDir)
	if err != nil {
		t.Fatal(err)
	}
	dec := stubFuzzyJA{byReading: map[string]string{}}
	for _, item := range jaOnly.Items {
		if item.NoisyReading != "" {
			dec.byReading[item.NoisyReading] = item.Surface
		}
	}
	env := &FuzzyEnv{Engine: eng, Decoder: dec, Info: FuzzyEngineInfo{DataDir: zhGoldenDataDir, Models: "none"}}
	rep, err := RunFuzzyCorpus(env, jaOnly, hash, FuzzyRunOptions{TopN: 8, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Cases) != 20 {
		t.Fatalf("%d cases, want 20", len(rep.Cases))
	}
	if rep.Cases[0].Rank != 1 || !rep.Cases[0].FallbackIntended {
		t.Fatalf("stub decoder should reach the reference: %+v", rep.Cases[0])
	}
}

type stubFuzzyJA struct {
	byReading map[string]string
}

func (s stubFuzzyJA) NBest(seq reading.Sequence, _ lang.Context, n int) ([]lang.Result, error) {
	key := ""
	for _, u := range seq {
		key += string(u)
	}
	surface, ok := s.byReading[key]
	if !ok {
		return nil, nil
	}
	out := []lang.Result{{
		Clauses: []lang.Clause{{Reading: seq, Surface: surface}},
		Score:   1,
	}}
	if n > 1 {
		out = append(out, lang.Result{Clauses: []lang.Clause{{Reading: seq, Surface: "候補"}}, Score: 0})
	}
	return out, nil
}

func TestFuzzyFallbackUnavailableScorer(t *testing.T) {
	ranker := score.NewRanker(score.RankerConfig{Models: []score.Scorer{fuzzyUnavailableScorer{}}})
	rank, outcome, _, err := fuzzyFallbackRank(FuzzyLangZh, "甲", []string{"乙", "甲"}, nil, []float64{0, 1}, ranker)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != "fallback" || rank != 2 {
		t.Fatalf("outcome=%q rank=%d, want fallback/2", outcome, rank)
	}
	counters := ranker.Counters()
	if counters.Requests != 1 || counters.Fallback != 1 || counters.Degraded() != 1 {
		t.Fatalf("counters = %+v, want the degraded request in the denominator", counters)
	}
	rank, outcome, _, err = fuzzyFallbackRank(FuzzyLangZh, "甲", nil, nil, nil, ranker)
	if err != nil || rank != 0 || outcome != "none" {
		t.Fatalf("empty candidates = %d/%q/%v, want 0/none/nil", rank, outcome, err)
	}
}

type fuzzyUnavailableScorer struct{}

func (fuzzyUnavailableScorer) Languages() []lang.ID { return nil }

func (fuzzyUnavailableScorer) Score(context.Context, score.Request) ([]float64, error) {
	return nil, score.ErrUnavailable
}

func filterFuzzyTestCorpus(c *FuzzyCorpus, language string) *FuzzyCorpus {
	out := *c
	out.Items = nil
	for _, item := range c.Items {
		if item.Language == language {
			out.Items = append(out.Items, item)
		}
	}
	out.Count = len(out.Items)
	return &out
}
