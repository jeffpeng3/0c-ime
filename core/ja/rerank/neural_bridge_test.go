package rerank

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/internal/neural"
)

// t14Verifier 是測試用的 T14 轉接層。core 不能 import internal/neural，
// 正式外殼要把同樣的轉換放在非 core 的 wiring 套件。
type t14Verifier struct {
	rt *neural.JaRuntime
}

func (v t14Verifier) Verify(ctx context.Context, req Request) (Result, error) {
	nreq := neural.JaRequest{
		Revision:    req.Revision,
		LeftContext: req.LeftContext,
	}
	for _, u := range req.Reading {
		nreq.Reading = append(nreq.Reading, string(u))
	}
	if req.Draft != nil {
		c := t14Candidate(*req.Draft)
		nreq.Draft = &c
	}
	for _, c := range req.Candidates {
		nreq.Candidates = append(nreq.Candidates, t14Candidate(c))
	}
	nres, err := v.rt.VerifyContext(ctx, nreq)
	if err != nil {
		return Result{}, mapT14Error(err)
	}
	out := Result{
		Prefix:            nres.Prefix,
		FixedClauses:      nres.FixedClauses,
		FixedReadingUnits: nres.FixedReadingUnits,
		Scores:            nres.Scores,
		Passed:            nres.Passed,
		PrefixOK:          nres.PrefixOK,
		Elapsed:           nres.Elapsed,
	}
	if nres.Draft != nil {
		x := t14Verdict(*nres.Draft)
		out.Draft = &x
	}
	for _, v := range nres.Verdicts {
		out.Verdicts = append(out.Verdicts, t14Verdict(v))
	}
	return out, nil
}

func t14Candidate(c Candidate) neural.JaCandidate {
	out := neural.JaCandidate{Surface: c.Surface}
	for _, u := range c.Reading {
		out.Reading = append(out.Reading, string(u))
	}
	for _, clause := range c.Clauses {
		nc := neural.JaClause{Surface: clause.Surface}
		for _, u := range clause.Reading {
			nc.Reading = append(nc.Reading, string(u))
		}
		out.Clauses = append(out.Clauses, nc)
	}
	return out
}

func t14Verdict(v neural.JaVerdict) Verdict {
	return Verdict{
		Surface: v.Surface, LogProb: v.LogProb, Score: v.Score,
		Prefix: v.Prefix, Passed: v.Passed, PrefixOK: v.PrefixOK,
	}
}

func mapT14Error(err error) error {
	switch {
	case errors.Is(err, neural.ErrTimeout):
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	case errors.Is(err, neural.ErrUnavailable):
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	case errors.Is(err, neural.ErrJaUnsupported):
		return fmt.Errorf("%w: %v", ErrUnsupported, err)
	default:
		return err
	}
}

func TestT14ErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		in   error
		want error
	}{
		{neural.ErrTimeout, ErrTimeout},
		{neural.ErrUnavailable, ErrUnavailable},
		{neural.ErrJaUnsupported, ErrUnsupported},
	} {
		if err := mapT14Error(tc.in); !errors.Is(err, tc.want) {
			t.Errorf("mapT14Error(%v) = %v, want %v", tc.in, err, tc.want)
		}
	}
	if got := fallbackReason(mapT14Error(neural.ErrTimeout)); got != "timeout" {
		t.Fatalf("timeout fallbackReason = %q", got)
	}
	if got := fallbackReason(mapT14Error(neural.ErrUnavailable)); got != "unavailable" {
		t.Fatalf("unavailable fallbackReason = %q", got)
	}
}

// TestRerankWithT14MockRuntime 用 T14 的 mock worker 跑完整 draft、verify、
// 重搜流程。沒有 python3 時略過。
func TestRerankWithT14MockRuntime(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	script, err := filepath.Abs("../../../platforms/neural/ja/tests/mock_runtime.py")
	if err != nil {
		t.Skip("mock runtime not found")
	}
	pkg, err := filepath.Abs("../../../platforms/neural/ja/tests/mock-package")
	if err != nil {
		t.Skip("mock package not found")
	}
	rt, err := neural.OpenJa(neural.JaConfig{
		Python: python, Script: script, Package: pkg, Server: "mock",
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rt.WaitReady(ctx); err != nil {
		t.Skipf("mock runtime not ready: %v", err)
	}

	dec := ambiguousDecoder(t)
	r := New(dec, t14Verifier{rt: rt}, Config{})
	dec.Reranker = r
	seq := seqUnits("あめあめ")
	got, err := dec.NBest(seq, lang.Context{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("沒有結果")
	}
	for _, res := range got {
		if !resultMatches(res, seq) {
			t.Fatalf("讀音不忠實：%s", decodeSurface(res))
		}
	}
	if stats := r.Stats(); stats.Calls == 0 || stats.Verified == 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func decodeSurface(res lang.Result) string {
	var s string
	for _, c := range res.Clauses {
		s += c.Surface
	}
	return s
}
