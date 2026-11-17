package score

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
)

// slowScorer 故意不理會 context，模擬卡住的模型；逾時必須由 Ranker 自己保證。
type slowScorer struct {
	wait  time.Duration
	calls atomic.Int64
}

func (s *slowScorer) Languages() []lang.ID { return []lang.ID{lang.Zh, lang.Ja, lang.En} }

func (s *slowScorer) Score(context.Context, Request) ([]float64, error) {
	s.calls.Add(1)
	time.Sleep(s.wait)
	return []float64{100, 0, 0, 0}, nil
}

// spyScorer 記下是否被呼叫與收到的請求，隱私測試用它確認文字沒有離開邊界。
type spyScorer struct {
	calls int
	last  Request
}

func (s *spyScorer) Languages() []lang.ID { return []lang.ID{lang.Zh, lang.Ja, lang.En} }

func (s *spyScorer) Score(_ context.Context, req Request) ([]float64, error) {
	s.calls++
	s.last = req
	return []float64{2, 0, 0, 0}, nil
}

// controlScorer 依序回傳不同結果，讓同一個 Ranker 的計數器走過四種來源。
type controlScorer struct {
	reply func(context.Context) ([]float64, error)
	calls atomic.Int64
}

func (s *controlScorer) Languages() []lang.ID { return []lang.ID{lang.Zh, lang.Ja, lang.En} }

func (s *controlScorer) Score(ctx context.Context, _ Request) ([]float64, error) {
	s.calls.Add(1)
	return s.reply(ctx)
}

func failSafeRequest() RankRequest {
	return RankRequest{
		Privacy:          PublicPrivacy(),
		Context:          "前文",
		PreviousLanguage: lang.Tag(lang.Ja),
		PrimaryLanguage:  lang.Ja,
		Candidates:       scenarioZhuyin(),
	}
}

func statisticalRanking(t *testing.T, req RankRequest) []Ranked {
	t.Helper()
	req.Privacy = PublicPrivacy()
	return mustRank(t, NewRanker(RankerConfig{}), req)
}

func assertAllCandidates(t *testing.T, req RankRequest, got []Ranked) {
	t.Helper()
	if len(got) != len(req.Candidates) {
		t.Fatalf("ranked %d candidates, want %d", len(got), len(req.Candidates))
	}
	seen := make([]bool, len(req.Candidates))
	for _, item := range got {
		if item.Index < 0 || item.Index >= len(req.Candidates) {
			t.Fatalf("index %d out of range", item.Index)
		}
		if seen[item.Index] {
			t.Fatalf("index %d appeared twice", item.Index)
		}
		seen[item.Index] = true
		if item.Reject {
			t.Errorf("fallback rejected candidate %d", item.Index)
		}
	}
}

// TestRankerTimeoutFallsBackToStatistics：模型卡住時不得拖過時間預算，排序
// 必須與完全沒有模型相同，且所有候選都還在。
func TestRankerTimeoutFallsBackToStatistics(t *testing.T) {
	req := failSafeRequest()
	want := statisticalRanking(t, req)
	slow := &slowScorer{wait: 2 * time.Second}
	ranker := NewRanker(RankerConfig{Models: []Scorer{slow}, Timeout: 10 * time.Millisecond})

	started := time.Now()
	got, err := ranker.RankResult(context.Background(), req)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("Rank blocked for %v, want near the 10ms budget", elapsed)
	}
	if got.Outcome != OutcomeTimeout {
		t.Fatalf("outcome = %v, want timeout", got.Outcome)
	}
	if slow.calls.Load() != 1 {
		t.Fatalf("scorer calls = %d, want 1", slow.calls.Load())
	}
	if !reflect.DeepEqual(got.Items, want) {
		t.Fatalf("timeout ranking = %#v, want statistical %#v", got.Items, want)
	}
	assertAllCandidates(t, req, got.Items)

	counters := ranker.Counters()
	if counters != (Counters{Requests: 1, Timeout: 1}) {
		t.Fatalf("counters = %+v, want one timeout in denominator", counters)
	}
	if counters.Covered() != counters.Requests {
		t.Fatalf("covered = %d, requests = %d", counters.Covered(), counters.Requests)
	}
}

// TestRankerInvalidScoresFallBack：缺模型、不可用、錯誤、長度不符與非有限
// 分數都走同一條統計回退，候選順序與完全沒有模型時相同。
func TestRankerInvalidScoresFallBack(t *testing.T) {
	req := failSafeRequest()
	want := statisticalRanking(t, req)
	cases := []struct {
		name  string
		model Scorer
	}{
		{name: "missing model", model: nil},
		{name: "unavailable", model: stubScorer{langs: []lang.ID{lang.Zh, lang.Ja, lang.En}, err: ErrUnavailable}},
		{name: "worker error", model: stubScorer{langs: []lang.ID{lang.Zh}, err: errors.New("worker down")}},
		{name: "short scores", model: stubScorer{langs: []lang.ID{lang.Zh}, raw: []float64{1}}},
		{name: "long scores", model: stubScorer{langs: []lang.ID{lang.Zh}, raw: []float64{1, 2, 3, 4, 5}}},
		{name: "nan score", model: stubScorer{langs: []lang.ID{lang.Zh}, raw: []float64{1, math.NaN(), 2, 3}}},
		{name: "inf score", model: stubScorer{langs: []lang.ID{lang.Zh}, raw: []float64{math.Inf(1), 2, 3, 4}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ranker := NewRanker(RankerConfig{Models: []Scorer{tc.model}})
			got, err := ranker.RankResult(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if got.Outcome != OutcomeFallback {
				t.Fatalf("outcome = %v, want fallback", got.Outcome)
			}
			if !reflect.DeepEqual(got.Items, want) {
				t.Fatalf("fallback ranking = %#v, want statistical %#v", got.Items, want)
			}
			assertAllCandidates(t, req, got.Items)
			counters := ranker.Counters()
			if counters.Requests != 1 || counters.Fallback != 1 || counters.Covered() != counters.Requests {
				t.Fatalf("counters = %+v, want one fallback in denominator", counters)
			}
		})
	}
}

// TestRankerPrivacyNeverCallsModel：密碼、安全輸入、私人與未宣告的請求都
// 不得呼叫模型，也不得把前文帶出去；統計順序與候選數量不變。
func TestRankerPrivacyNeverCallsModel(t *testing.T) {
	req := failSafeRequest()
	req.Context = "帳號 secret@example.com"
	want := statisticalRanking(t, req)
	for _, privacy := range []Privacy{
		PrivacyUnspecified,
		PrivacyPrivate,
		PrivacyPassword,
		PrivacySecure,
	} {
		t.Run(privacy.String(), func(t *testing.T) {
			spy := &spyScorer{}
			ranker := NewRanker(RankerConfig{Models: []Scorer{spy}})
			private := req
			private.Privacy = privacy
			got, err := ranker.RankResult(context.Background(), private)
			if err != nil {
				t.Fatal(err)
			}
			if spy.calls != 0 {
				t.Fatalf("model called %d times for %s privacy", spy.calls, privacy)
			}
			if got.Outcome != OutcomePrivacy {
				t.Fatalf("outcome = %v, want privacy", got.Outcome)
			}
			if !reflect.DeepEqual(got.Items, want) {
				t.Fatalf("private ranking = %#v, want statistical %#v", got.Items, want)
			}
			assertAllCandidates(t, req, got.Items)
			counters := ranker.Counters()
			if counters != (Counters{Requests: 1, Privacy: 1}) {
				t.Fatalf("counters = %+v, want one privacy skip in denominator", counters)
			}
		})
	}

	t.Run("public reaches model", func(t *testing.T) {
		spy := &spyScorer{}
		ranker := NewRanker(RankerConfig{Models: []Scorer{spy}})
		if _, err := ranker.RankResult(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		if spy.calls != 1 {
			t.Fatalf("public request called model %d times, want 1", spy.calls)
		}
	})
}

// TestRankerCountersKeepDegradedRequests：四種來源共用同一個分母，回退與
// 逾時不會被藏起來。
func TestRankerCountersKeepDegradedRequests(t *testing.T) {
	req := failSafeRequest()
	control := &controlScorer{}
	ranker := NewRanker(RankerConfig{Models: []Scorer{control}, Timeout: 10 * time.Millisecond})

	control.reply = func(context.Context) ([]float64, error) {
		return []float64{2, 0, 0, 0}, nil
	}
	if got, err := ranker.RankResult(context.Background(), req); err != nil || got.Outcome != OutcomeModel {
		t.Fatalf("model outcome = %v, err = %v", got.Outcome, err)
	}

	control.reply = func(context.Context) ([]float64, error) {
		return nil, ErrUnavailable
	}
	if got, err := ranker.RankResult(context.Background(), req); err != nil || got.Outcome != OutcomeFallback {
		t.Fatalf("fallback outcome = %v, err = %v", got.Outcome, err)
	}

	control.reply = func(context.Context) ([]float64, error) {
		time.Sleep(2 * time.Second)
		return []float64{2, 0, 0, 0}, nil
	}
	if got, err := ranker.RankResult(context.Background(), req); err != nil || got.Outcome != OutcomeTimeout {
		t.Fatalf("timeout outcome = %v, err = %v", got.Outcome, err)
	}

	private := req
	private.Privacy = PrivacyPassword
	if got, err := ranker.RankResult(context.Background(), private); err != nil || got.Outcome != OutcomePrivacy {
		t.Fatalf("privacy outcome = %v, err = %v", got.Outcome, err)
	}

	counters := ranker.Counters()
	want := Counters{Requests: 4, Model: 1, Fallback: 1, Timeout: 1, Privacy: 1}
	if counters != want {
		t.Fatalf("counters = %+v, want %+v", counters, want)
	}
	if counters.Degraded() != 3 {
		t.Fatalf("degraded = %d, want 3", counters.Degraded())
	}
	if counters.Covered() != counters.Requests {
		t.Fatalf("covered = %d, requests = %d", counters.Covered(), counters.Requests)
	}
}

// TestZhAdapterRefusesPrivateRequest 是 score 邊界的第二層隱私保護：即使
// 呼叫端繞過 Ranker 直接使用 adapter，私人文字也不會送進模型。
func TestZhAdapterRefusesPrivateRequest(t *testing.T) {
	scorer := &recordingZhScorer{}
	adapter := NewZhAdapter(scorer)
	req := Request{
		Privacy:    PrivacyPassword,
		Context:    "secret@example.com",
		Candidates: []lattice.Candidate{zhCandidate("密碼", "ㄇㄧˋ ㄇㄚˇ", lang.Zh), zhCandidate("覓馬", "ㄇㄧˋ ㄇㄚˇ", lang.Zh)},
	}
	if _, err := adapter.Score(context.Background(), req); !errors.Is(err, ErrPrivate) {
		t.Fatalf("err = %v, want ErrPrivate", err)
	}
	if len(scorer.calls) != 0 {
		t.Fatalf("model called %d times for private request", len(scorer.calls))
	}
}

// TestRankerCanceledParentContextStillErrors：呼叫端取消不是模型逾時，必須
// 維持原本的錯誤語意，不能計入逾時。
func TestRankerCanceledParentContextStillErrors(t *testing.T) {
	req := failSafeRequest()
	ranker := NewRanker(RankerConfig{Models: []Scorer{&slowScorer{wait: time.Second}}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ranker.RankResult(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if counters := ranker.Counters(); counters.Requests != 0 {
		t.Fatalf("canceled request counted: %+v", counters)
	}
}
