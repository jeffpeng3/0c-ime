package rerank_test

import (
	"context"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/rerank"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/eval"
	"github.com/0c-biggo/0c-ime/internal/neural"
)

const (
	jaRules    = "../../../testdata/ja/rules.json"
	jaBaseline = "../../../testdata/ja/baseline.json"
	jaData     = "../../../data/ja"
)

func loadJA(t *testing.T) (*eval.JARules, string, []eval.JACorpusEntry, *decode.Decoder, eval.JAEngineInfo) {
	t.Helper()
	rules, hash, err := eval.LoadJARules(jaRules)
	if err != nil {
		t.Fatal(err)
	}
	corpora, err := eval.LoadJACorpusSet(rules, jaRules)
	if err != nil {
		t.Fatal(err)
	}
	dec, engine, err := eval.LoadJADecoder(jaData)
	if err != nil {
		t.Fatal(err)
	}
	return rules, hash, corpora, dec, engine
}

func runJA(t *testing.T, dec *decode.Decoder, engine eval.JAEngineInfo, rules *eval.JARules, hash string, corpora []eval.JACorpusEntry) eval.JAReport {
	t.Helper()
	return eval.RunJA(dec, engine, rules, hash, corpora, eval.JAOptions{TopN: rules.TopN})
}

// oracleVerifier 只用在 T33 ceiling 測試：它用題庫正解當成理想驗證器，
// 證明限制式重搜能把手上的資料變成更好的第一名。這不是模型品質數字。
type oracleVerifier struct {
	accept map[string][]string
}

func (v *oracleVerifier) Verify(_ context.Context, req rerank.Request) (rerank.Result, error) {
	accepted := v.accept[reading.Key(req.Reading)]
	out := rerank.Result{}
	for _, c := range req.Candidates {
		prefix := ""
		ok := false
		for _, a := range accepted {
			if c.Surface == a {
				ok = true
			}
			if p := commonRunes(a, c.Surface); len([]rune(p)) > len([]rune(prefix)) {
				prefix = p
			}
		}
		score := float64(len([]rune(prefix)))
		if ok {
			score += 5
		}
		out.Verdicts = append(out.Verdicts, rerank.Verdict{
			Surface: c.Surface, Score: score, LogProb: score * float64(len([]rune(c.Surface))),
			Prefix: prefix, Passed: ok, PrefixOK: true,
		})
	}
	return out, nil
}

func commonRunes(a, b string) string {
	ar, br := []rune(a), []rune(b)
	n := len(ar)
	if len(br) < n {
		n = len(br)
	}
	i := 0
	for i < n && ar[i] == br[i] {
		i++
	}
	return string(ar[:i])
}

func TestT33GateWithoutModel(t *testing.T) {
	rules, hash, corpora, dec, engine := loadJA(t)
	report := runJA(t, dec, engine, rules, hash, corpora)
	base, err := eval.LoadJABaseline(jaBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if problems := eval.JARegression(report, base, hash, rules.Gate); len(problems) > 0 {
		t.Fatalf("沒有神經層時 gate 失敗：%v", problems)
	}
}

type unavailableVerifier struct{ calls int }

func (v *unavailableVerifier) Verify(context.Context, rerank.Request) (rerank.Result, error) {
	v.calls++
	return rerank.Result{}, neural.ErrUnavailable
}

func TestT33UnavailableModelMatchesStatisticalBaseline(t *testing.T) {
	rules, hash, corpora, dec, engine := loadJA(t)
	base := runJA(t, dec, engine, rules, hash, corpora)
	verifier := &unavailableVerifier{}
	dec.Reranker = rerank.New(dec, verifier, rerank.Config{})
	got := runJA(t, dec, engine, rules, hash, corpora)
	if verifier.calls == 0 {
		t.Fatal("測試前提失敗：驗證器沒有被呼叫")
	}
	first, err := eval.DeterminismSignature(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := eval.DeterminismSignature(got)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("模型不可用時改變了統計結果")
	}
}

func TestT33ConstrainedResearchCeiling(t *testing.T) {
	rules, hash, corpora, dec, engine := loadJA(t)
	base := runJA(t, dec, engine, rules, hash, corpora)
	accept := map[string][]string{}
	for _, entry := range corpora {
		for _, item := range entry.Corpus.Items {
			key := reading.Key(readingOf(item.Reading))
			accept[key] = append([]string{item.Surface}, item.Accept...)
		}
	}
	dec.Reranker = rerank.New(dec, &oracleVerifier{accept: accept}, rerank.Config{})
	got := runJA(t, dec, engine, rules, hash, corpora)

	baseAnthy := findCorpus(base.Corpora, "anthy-corpus1")
	gotAnthy := findCorpus(got.Corpora, "anthy-corpus1")
	if baseAnthy == nil || gotAnthy == nil {
		t.Fatal("anthy-corpus1 不在 T33 題庫")
	}
	if gotAnthy.Metrics.Top1Rate <= baseAnthy.Metrics.Top1Rate {
		t.Fatalf("oracle 前綴沒有改善 top1：%.4f -> %.4f",
			baseAnthy.Metrics.Top1Rate, gotAnthy.Metrics.Top1Rate)
	}
	if gotAnthy.Metrics.Top1 != gotAnthy.Metrics.Top5 {
		t.Fatalf("top1/top5 不一致：%d/%d", gotAnthy.Metrics.Top1, gotAnthy.Metrics.Top5)
	}
	t.Logf("anthy-corpus1 top1 %.4f -> %.4f（oracle 前綴 ceiling）",
		baseAnthy.Metrics.Top1Rate, gotAnthy.Metrics.Top1Rate)

	again := runJA(t, dec, engine, rules, hash, corpora)
	a, err := eval.DeterminismSignature(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := eval.DeterminismSignature(again)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("重搜結果不可重現")
	}
}

func findCorpus(reports []eval.JACorpusReport, name string) *eval.JACorpusReport {
	for i := range reports {
		if reports[i].Name == name {
			return &reports[i]
		}
	}
	return nil
}

func readingOf(s string) reading.Sequence {
	out := make(reading.Sequence, 0, len([]rune(s)))
	for _, r := range s {
		out = append(out, reading.Unit(string(r)))
	}
	return out
}
