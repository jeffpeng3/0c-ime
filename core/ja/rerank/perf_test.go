package rerank

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
)

// TestRerankOverheadWithoutModel 量的是退回路徑的 Go 端開銷，不是模型延遲。
// 真正的 P50/P95 要在沒有其他 agent 或 CI 的安靜時段量。
func TestRerankOverheadWithoutModel(t *testing.T) {
	dec := ambiguousDecoder(t)
	seq := seqUnits("あめあめあめあめ")
	drafts, err := dec.NBest(seq, langContext(), 8)
	if err != nil {
		t.Fatal(err)
	}
	r := New(dec, VerifierFunc(func(context.Context, Request) (Result, error) {
		return Result{}, fmt.Errorf("model unavailable")
	}), Config{})
	const runs = 200
	var total time.Duration
	for i := 0; i < runs; i++ {
		start := time.Now()
		_ = r.Rerank(seq, drafts, langContext())
		total += time.Since(start)
	}
	avg := total / runs
	t.Logf("退回路徑平均 %v，總計 %v", avg, total)
	if avg > 5*time.Millisecond {
		t.Fatalf("退回路徑平均 %v，超過 5 ms", avg)
	}
}

func TestRerankAllocations(t *testing.T) {
	dec := ambiguousDecoder(t)
	seq := seqUnits("あめあめあめあめ")
	drafts, err := dec.NBest(seq, langContext(), 8)
	if err != nil {
		t.Fatal(err)
	}
	r := New(dec, VerifierFunc(func(context.Context, Request) (Result, error) {
		return Result{}, fmt.Errorf("model unavailable")
	}), Config{})
	allocs := testing.AllocsPerRun(100, func() {
		_ = r.Rerank(seq, drafts, langContext())
	})
	t.Logf("退回路徑每次配置 %.0f 次", allocs)
}

func BenchmarkRerankFallback(b *testing.B) {
	dec := benchDecoder(b)
	seq := seqUnits("あめあめあめあめ")
	drafts, err := dec.NBest(seq, langContext(), 8)
	if err != nil {
		b.Fatal(err)
	}
	r := New(dec, VerifierFunc(func(context.Context, Request) (Result, error) {
		return Result{}, fmt.Errorf("model unavailable")
	}), Config{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Rerank(seq, drafts, langContext())
	}
}

func BenchmarkRerankResearch(b *testing.B) {
	dec := benchDecoder(b)
	seq := seqUnits("あめあめあめあめ")
	drafts, err := dec.NBest(seq, langContext(), 8)
	if err != nil {
		b.Fatal(err)
	}
	v := &scriptedVerifier{target: "雨雨飴飴", prefix: "雨雨"}
	r := New(dec, v, Config{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Rerank(seq, drafts, langContext())
	}
}

func benchDecoder(b *testing.B) *decode.Decoder {
	b.Helper()
	return ambiguousDecoder(b)
}
