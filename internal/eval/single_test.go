package eval

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/datadir"
)

const (
	zhGoldenCorpusPath   = "../../testdata/eval/zhuyin-golden.json"
	zhGoldenManifestPath = "../../testdata/eval/zhuyin-golden.manifest.json"
	zhGoldenDataDir      = "../../testdata/golden/zh/data"
	zhGoldenDir          = "../../testdata/golden/zh"
	jaLexCorpusPath      = "../../testdata/eval/ja-lexicon-ambiguous.json"
	jaLexManifestPath    = "../../testdata/eval/ja-lexicon-ambiguous.manifest.json"
	jaLexBaselinePath    = "../../testdata/eval/baseline.json"
	referencePackEnv     = "OC_JADIC_REFERENCE_PACK"
)

func encodeJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestRankMetrics(t *testing.T) {
	var m RankMetrics
	for _, rank := range []int{1, 2, 0, 4} {
		m.Add(rank)
	}
	m.Finalize()
	if m.N != 4 || m.Top1 != 1 || m.Top1Rate != 0.25 || m.Misses != 1 {
		t.Fatalf("metrics = %+v", m)
	}
	want := (1 + 0.5 + 0 + 0.25) / 4
	if m.MRR != want {
		t.Fatalf("mrr = %v, want %v", m.MRR, want)
	}
	// Finalize 不是累加；重複呼叫結果不變。
	m.Finalize()
	if m.MRR != want {
		t.Fatalf("second Finalize changed mrr to %v", m.MRR)
	}
	if got := Ranked([]string{"甲", "乙"}, "乙"); got != 2 {
		t.Fatalf("rank = %d, want 2", got)
	}
	if got := Ranked([]string{"甲"}, "乙"); got != 0 {
		t.Fatalf("missing rank = %d, want 0", got)
	}
}

// 提交進 repo 的注音題庫必須與 golden 重新產生的結果逐位元相同。
func TestZhGoldenCorpusIsFrozen(t *testing.T) {
	built, err := BuildZhGoldenCorpus(zhGoldenDir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(zhGoldenCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeJSON(t, built); !bytes.Equal(got, want) {
		t.Fatalf("testdata/eval/zhuyin-golden.json is stale; rerun `go run ./cmd/0c-eval-single build-zh`")
	}
	if built.Count != 17 {
		t.Fatalf("count = %d, want 17", built.Count)
	}
	if got, want := built.SourcePin.CasesSHA256, "4c698a4112fd1e9dc98318e4753ae86258d4e0619f1743157bca96fb4105a6c0"; got != want {
		t.Fatalf("cases hash = %s, want %s", got, want)
	}
}

func TestZhGoldenCorpusManifest(t *testing.T) {
	corpus, hash, err := LoadZhGoldenCorpus(zhGoldenCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadCorpusManifest(zhGoldenManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Corpus != corpus.Name || manifest.SHA256 != hash || manifest.Count != corpus.Count {
		t.Fatalf("manifest %+v does not pin corpus %s (%d)", manifest, hash, corpus.Count)
	}
	if manifest.CasesSHA256 != corpus.SourcePin.CasesSHA256 || manifest.ExpectedSHA256 != corpus.SourcePin.ExpectedSHA256 {
		t.Fatalf("manifest source pin does not match corpus")
	}
}

// 注音題庫在固定資料上必須全部 top-1、整句正確；否則 baseline 的
// 「不能退步」會從一個已經退步的起點開始。
func TestRunZhGoldenBaseline(t *testing.T) {
	corpus, hash, err := LoadZhGoldenCorpus(zhGoldenCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := datadir.Open(zhGoldenDataDir)
	if err != nil {
		t.Fatal(err)
	}
	info := ZhEngineInfo{DataDir: zhGoldenDataDir}
	info.LexiconSHA256, err = shaFile(filepath.Join(zhGoldenDataDir, "lexicon.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	info.LMSHA256, err = shaFile(filepath.Join(zhGoldenDataDir, "lm.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rep := RunZhGolden(eng, corpus, hash, info)
	if rep.Metrics.Segments.N != 92 || rep.Metrics.Sentences != 17 {
		t.Fatalf("segments/sentences = %d/%d, want 92/17", rep.Metrics.Segments.N, rep.Metrics.Sentences)
	}
	if rep.Metrics.Segments.Top1Rate != 1 || rep.Metrics.Segments.MRR != 1 || rep.Metrics.SentenceExactRate != 1 {
		t.Fatalf("zhuyin baseline regressed: %+v", rep.Metrics)
	}
	base, err := LoadSingleBaseline(jaLexBaselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if problems := ZhGoldenProblems(rep, base.Zhuyin); len(problems) > 0 {
		t.Fatal(problems)
	}
}

func TestZhGoldenGateRejectsDrop(t *testing.T) {
	base := &ZhGoldenBaseline{
		CorpusSHA256:      "abc",
		Engine:            ZhEngineInfo{LexiconSHA256: "lex", LMSHA256: "lm"},
		SegmentTop1Rate:   1,
		SegmentMRR:        1,
		Sentences:         17,
		SentenceExactRate: 1,
		Gate:              SingleGate{Gated: true},
	}
	rep := ZhGoldenReport{
		CorpusSHA256: "abc",
		Engine:       ZhEngineInfo{LexiconSHA256: "lex", LMSHA256: "lm"},
		Metrics: ZhGoldenMetrics{
			Segments:          RankMetrics{N: 92, Top1: 91, Top1Rate: 91.0 / 92, MRR: 0.99},
			Sentences:         17,
			SentencesExact:    16,
			SentenceExactRate: 16.0 / 17,
		},
	}
	problems := ZhGoldenProblems(rep, base)
	if len(problems) != 3 {
		t.Fatalf("problems = %v, want three regressions", problems)
	}
}

func TestJALexCorpusPinned(t *testing.T) {
	corpus, hash, err := LoadJALexCorpus(jaLexCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadCorpusManifest(jaLexManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Corpus != corpus.Name || manifest.SHA256 != hash || manifest.Count != corpus.Count || manifest.Holdout != corpus.HoldoutSize {
		t.Fatalf("manifest does not pin corpus")
	}
	if corpus.Count != 1200 || corpus.HoldoutSize != 400 || corpus.Seed != 60 {
		t.Fatalf("unexpected corpus shape: count=%d holdout=%d seed=%d", corpus.Count, corpus.HoldoutSize, corpus.Seed)
	}
	if corpus.Pack.SHA256 != "8e57a18deb64d0fdc8147b53b5e234ecda412fa49feb0e8096addc4349582840" {
		t.Fatalf("pack sha256 = %s", corpus.Pack.SHA256)
	}
	holdout := 0
	for _, item := range corpus.Items {
		if item.Holdout {
			holdout++
		}
		best := item.Candidates[0]
		if best.Surface != item.Reference {
			t.Fatalf("%s reference is not the cheapest candidate", item.ID)
		}
		for _, cand := range item.Candidates {
			if cand.Cost < best.Cost || cand.Cost == best.Cost && cand.Surface < best.Surface {
				t.Fatalf("%s candidate order is not dictionary order", item.ID)
			}
			if !isKanjiSurface(cand.Surface) {
				t.Fatalf("%s candidate %q is not Han", item.ID, cand.Surface)
			}
		}
	}
	if holdout != corpus.HoldoutSize {
		t.Fatalf("holdout = %d, want %d", holdout, corpus.HoldoutSize)
	}
}

// 同一份 pack、同一組參數必須產生同一份題庫；設定參考 pack 才執行。
func TestJALexBuildDeterministic(t *testing.T) {
	pack := os.Getenv(referencePackEnv)
	if pack == "" {
		t.Skipf("未設定 %s，跳過真實 pack 建置", referencePackEnv)
	}
	committed, committedHash, err := LoadJALexCorpus(jaLexCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	packManifest := filepath.Join(filepath.Dir(pack), "manifest.json")
	opts := DefaultJALexBuildOptions()
	first, err := BuildJALexCorpus(pack, packManifest, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildJALexCorpus(pack, packManifest, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodeJSON(t, first), encodeJSON(t, second)) {
		t.Fatal("two builds from the same pack differ")
	}
	if got := shaBytes(encodeJSON(t, first)); got != committedHash {
		t.Fatalf("rebuilt corpus sha256 %s does not match committed %s; rerun build-ja deliberately", got, committedHash)
	}
	if first.Pack.SHA256 != committed.Pack.SHA256 || first.Count != committed.Count {
		t.Fatal("rebuilt corpus does not match the committed source pin")
	}
}

// 日文 runner 在同一份 pack 上連跑兩次，非時間欄位必須完全相同。
func TestJALexRunDeterministic(t *testing.T) {
	pack := os.Getenv(referencePackEnv)
	if pack == "" {
		t.Skipf("未設定 %s，跳過真實 pack 執行", referencePackEnv)
	}
	corpus, hash, err := LoadJALexCorpus(jaLexCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	dec, packSHA, _, err := LoadJALexDecoder(pack, "../../data/ja/connection.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if packSHA != corpus.Pack.SHA256 {
		t.Fatalf("pack sha256 %s does not match corpus %s", packSHA, corpus.Pack.SHA256)
	}
	first, err := RunJALex(dec, corpus, hash, 8)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunJALex(dec, corpus, hash, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodeJSON(t, first), encodeJSON(t, second)) {
		t.Fatal("two runs over the same pack differ")
	}
}

// 日文兩組排序的計分與回退路徑，用固定替身轉換器驗證；不需要 pack。
func TestRunJALexArms(t *testing.T) {
	corpus := &JALexCorpus{
		Schema: JALexSchema, Name: "test", License: "test", Source: "test",
		Seed: 1, SampleSize: 2, HoldoutSize: 1, CandidateCap: 4,
		ReferenceRule: "test",
		Pack:          JALexPack{Path: "test.pack", SHA256: "0123456789012345678901234567890123456789012345678901234567890123"},
		Count:         2,
		Items: []JALexItem{
			{ID: "a", Reading: "あ", Reference: "亜", Holdout: true, Candidates: []JALexCandidate{{Surface: "亜", Cost: 1}, {Surface: "唖", Cost: 2}}},
			{ID: "b", Reading: "い", Reference: "伊", Candidates: []JALexCandidate{{Surface: "伊", Cost: 1}}},
		},
	}
	conv := fakeJALexConverter{results: map[string][]lang.Result{
		"あ": {jaResult("唖", "あ", 2), jaResult("亜", "あ", 1)},
		"い": {jaResult("伊", "い", 1)},
	}}
	rep, err := RunJALex(conv, corpus, "corpus", 8)
	if err != nil {
		t.Fatal(err)
	}
	all, ok := splitByName(rep, "all")
	if !ok {
		t.Fatal("missing all split")
	}
	if all.N != 2 || all.Dictionary.Top1Rate != 1 || all.Dictionary.MRR != 1 {
		t.Fatalf("dictionary arm = %+v", all.Dictionary)
	}
	if all.Ranker.Top1Rate != 0.5 {
		t.Fatalf("ranker top1 = %v, want 0.5", all.Ranker.Top1Rate)
	}
	if want := (0.5 + 1) / 2; all.Ranker.MRR != want {
		t.Fatalf("ranker mrr = %v, want %v", all.Ranker.MRR, want)
	}
	if all.RankerOutcomes["fallback"] != 2 {
		t.Fatalf("ranker outcomes = %v, want two fallback requests", all.RankerOutcomes)
	}
	holdout, _ := splitByName(rep, "holdout")
	if holdout.N != 1 || holdout.Ranker.Top1Rate != 0 || holdout.Ranker.MRR != 0.5 {
		t.Fatalf("holdout = %+v", holdout)
	}
}

type fakeJALexConverter struct {
	results map[string][]lang.Result
}

func (f fakeJALexConverter) NBest(seq reading.Sequence, _ lang.Context, n int) ([]lang.Result, error) {
	out := f.results[reading.Key(seq)]
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func jaResult(surface, readingText string, score float64) lang.Result {
	seq := make(reading.Sequence, 0, len(readingText))
	for _, r := range readingText {
		seq = append(seq, reading.Unit(r))
	}
	return lang.Result{
		Clauses: []lang.Clause{{Reading: seq, Surface: surface}},
		Score:   score,
	}
}

func TestJALexGatePolicy(t *testing.T) {
	rep := JALexReport{
		CorpusSHA256: "corpus",
		PackSHA256:   "pack",
		Splits: []JALexSplitReport{{
			Split:      "holdout",
			N:          400,
			Dictionary: JALexArmMetrics{RankMetrics: RankMetrics{N: 400, Top1: 300, Top1Rate: 0.75, MRR: 0.8}},
			Ranker:     JALexArmMetrics{RankMetrics: RankMetrics{N: 400, Top1: 200, Top1Rate: 0.5, MRR: 0.6}},
		}},
	}
	ungated := &JALexBaseline{
		CorpusSHA256: "corpus", PackSHA256: "pack", Split: "holdout", N: 400,
		DictionaryTop1Rate: 1, DictionaryMRR: 1, RankerTop1Rate: 0.66, RankerMRR: 0.76,
		Gate: SingleGate{Gated: false},
	}
	if problems := JALexProblems(rep, ungated); len(problems) != 0 {
		t.Fatalf("ungated baseline should not fail on quality: %v", problems)
	}
	if delta := JALexDelta(rep, ungated); delta == "" {
		t.Fatal("delta is empty")
	}
	badHash := *ungated
	badHash.PackSHA256 = "other"
	if problems := JALexProblems(rep, &badHash); len(problems) != 1 {
		t.Fatalf("hash mismatch must fail closed: %v", problems)
	}
	skipped := rep
	skipped.Splits = append([]JALexSplitReport(nil), rep.Splits...)
	skipped.Splits[0].RankerSkipped = true
	if problems := JALexProblems(skipped, ungated); len(problems) != 1 {
		t.Fatalf("skipped ranker must fail closed: %v", problems)
	}
	gated := *ungated
	gated.Gate.Gated = true
	if problems := JALexProblems(rep, &gated); len(problems) != 4 {
		t.Fatalf("problems = %v, want four quality drops", problems)
	}
}
