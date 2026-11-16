package rerank

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

func seqUnits(s string) reading.Sequence {
	out := make(reading.Sequence, 0, len([]rune(s)))
	for _, r := range s {
		out = append(out, reading.Unit(string(r)))
	}
	return out
}

func ambiguousDecoder(t testing.TB) *decode.Decoder {
	t.Helper()
	lex := lexicon.NewMem()
	for i, e := range []struct {
		surface string
		cost    float64
	}{
		{"雨", 1.0}, {"飴", 1.1}, {"天", 1.2},
	} {
		if err := lex.Add(lexicon.Entry{
			Surface: e.surface, Reading: seqUnits("あめ"), Cost: e.cost,
			POS: lexicon.POSNoun,
		}); err != nil {
			t.Fatal(err)
		}
		_ = i
	}
	return &decode.Decoder{Lex: lex}
}

func langContext() lang.Context { return lang.Context{} }

func surfaces(results []lang.Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = decode.ResultSurface(r.Clauses)
	}
	return out
}

func TestDisabledReturnsDrafts(t *testing.T) {
	dec := ambiguousDecoder(t)
	want, err := dec.NBest(seqUnits("あめあめ"), lang.Context{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	r := New(dec, VerifierFunc(func(context.Context, Request) (Result, error) {
		calls++
		return Result{}, nil
	}), Config{Disabled: true})
	dec.Reranker = r
	got, err := dec.NBest(seqUnits("あめあめ"), lang.Context{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("停用後仍呼叫驗證器 %d 次", calls)
	}
	if !reflect.DeepEqual(surfaces(got), surfaces(want)) {
		t.Fatalf("停用後結果 = %#v，want %#v", surfaces(got), surfaces(want))
	}

	r.SetEnabled(true)
	if _, err := dec.NBest(seqUnits("あめあめ"), lang.Context{}, 4); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("重新啟用後呼叫 %d 次，want 1", calls)
	}
	r.SetEnabled(false)
	if _, err := dec.NBest(seqUnits("あめあめ"), lang.Context{}, 4); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("再次停用後呼叫 %d 次，want 1", calls)
	}
}

type scriptedVerifier struct {
	target string
	prefix string
	calls  int
	err    error
	delay  time.Duration
}

func (v *scriptedVerifier) Verify(ctx context.Context, req Request) (Result, error) {
	v.calls++
	if v.delay > 0 {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(v.delay):
		}
	}
	if v.err != nil {
		return Result{}, v.err
	}
	res := Result{Prefix: v.prefix}
	for _, c := range req.Candidates {
		score := 0.0
		passed := false
		prefix := v.prefix
		if c.Surface == v.target {
			score = 10
			passed = true
			prefix = c.Surface
		}
		res.Verdicts = append(res.Verdicts, Verdict{
			Surface: c.Surface, Score: score, LogProb: score,
			Prefix: prefix, Passed: passed, PrefixOK: true,
		})
		res.Scores = append(res.Scores, score)
		res.Passed = append(res.Passed, passed)
		res.PrefixOK = append(res.PrefixOK, true)
	}
	return res, nil
}

func TestConstrainedResearchFindsCandidateOutsideNBest(t *testing.T) {
	dec := ambiguousDecoder(t)
	target := "雨雨飴飴"
	base, err := dec.NBest(seqUnits("あめあめあめあめ"), lang.Context{}, 8)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range surfaces(base) {
		if s == target {
			t.Fatalf("測試前提失敗：%s 已在 top 8", target)
		}
	}
	v := &scriptedVerifier{target: target, prefix: "雨雨"}
	r := New(dec, v, Config{})
	dec.Reranker = r
	got, err := dec.NBest(seqUnits("あめあめあめあめ"), lang.Context{}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || decode.ResultSurface(got[0].Clauses) != target {
		t.Fatalf("限制式重搜結果 = %#v，want 第一名 %s", surfaces(got), target)
	}
	if v.calls != 2 {
		t.Fatalf("verify 次數 = %d，want 2（draft + 重搜）", v.calls)
	}
	if !resultMatches(got[0], seqUnits("あめあめあめあめ")) {
		t.Fatal("重搜結果的讀音不忠實")
	}
	for _, res := range got {
		if !resultMatches(res, seqUnits("あめあめあめあめ")) {
			t.Fatalf("候選讀音不忠實：%s", decode.ResultSurface(res.Clauses))
		}
	}
}

func TestFallbackPreservesStatisticalOrder(t *testing.T) {
	dec := ambiguousDecoder(t)
	want, err := dec.NBest(seqUnits("あめあめ"), lang.Context{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"unavailable", errors.New("worker not ready")},
		{"timeout", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ambiguousDecoder(t)
			v := &scriptedVerifier{err: tc.err}
			d.Reranker = New(d, v, Config{})
			got, err := d.NBest(seqUnits("あめあめ"), lang.Context{}, 4)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(surfaces(got), surfaces(want)) {
				t.Fatalf("退回結果 = %#v，want %#v", surfaces(got), surfaces(want))
			}
		})
	}
}

func TestBudgetTimeoutFallsBack(t *testing.T) {
	dec := ambiguousDecoder(t)
	want, err := dec.NBest(seqUnits("あめあめ"), lang.Context{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	v := &scriptedVerifier{delay: time.Second}
	r := New(dec, v, Config{
		Timeout:           15 * time.Millisecond,
		PerRequestTimeout: 5 * time.Millisecond,
	})
	dec.Reranker = r
	start := time.Now()
	got, err := dec.NBest(seqUnits("あめあめ"), lang.Context{}, 4)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("逾時沒有在預算內結束：%v", elapsed)
	}
	if !reflect.DeepEqual(surfaces(got), surfaces(want)) {
		t.Fatalf("逾時結果 = %#v，want %#v", surfaces(got), surfaces(want))
	}
	if stats := r.Stats(); stats.Timeouts == 0 {
		t.Fatalf("逾時統計 = %+v", stats)
	}
}

func TestZeroConfigEnablesDefaults(t *testing.T) {
	dec := ambiguousDecoder(t)
	v := &scriptedVerifier{}
	r := New(dec, v, Config{})
	if !r.Enabled() {
		t.Fatal("零值 Config 應啟用")
	}
	cfg := r.Config()
	if cfg.Timeout != 100*time.Millisecond || cfg.MaxRounds != 2 || cfg.MaxCandidates != 8 {
		t.Fatalf("預設值 = %+v", cfg)
	}
}

func TestClampPrefixUsesWholeSegments(t *testing.T) {
	res := lang.Result{Clauses: []lang.Clause{
		{Surface: "今日", Reading: seqUnits("きょう"), Segments: []lang.CandidateSegment{
			{Surface: "今日", Reading: seqUnits("きょう")},
		}},
		{Surface: "は", Reading: seqUnits("は"), Segments: []lang.CandidateSegment{
			{Surface: "は", Reading: seqUnits("は")},
		}},
	}}
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"今日", "今日"},
		{"今日は", "今日は"},
		{"今日はいい", "今日は"},
		{"今", ""},
		{"別", ""},
	} {
		if got := clampPrefix(tc.in, res); got != tc.want {
			t.Errorf("clampPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadingFidelityRejectsMismatch(t *testing.T) {
	dec := ambiguousDecoder(t)
	r := New(dec, &scriptedVerifier{}, Config{})
	bad := lang.Result{Clauses: []lang.Clause{{Surface: "雨", Reading: seqUnits("あめ")}}}
	if _, _, ok := r.collect(seqUnits("あめあめ"), []lang.Result{bad}); ok {
		t.Fatal("讀音長度不符仍被接受")
	}
}
