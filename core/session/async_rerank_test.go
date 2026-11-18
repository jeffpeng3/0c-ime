package session

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// asyncFake 是可控的候選背景評分器。called 收到請求後才由測試放行，
// 用來確認按鍵路徑真的沒有等模型。
type asyncFake struct {
	called  chan rerank.Request
	release chan struct{}
	score   func(rerank.Request) ([]float64, error)
}

func (f *asyncFake) ScoreContext(ctx context.Context, r rerank.Request) ([]float64, error) {
	f.called <- r
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.score == nil {
		return make([]float64, len(r.Candidates)), nil
	}
	return f.score(r)
}

func asyncCandidateSession(scorer rerank.ContextScorer, timeout time.Duration, disabled bool) *Session {
	lex, sys := lexicon.NewMem(), lm.NewMem()
	for i, e := range []struct {
		w string
		p float64
	}{
		{"景", 1e-3}, {"井", 3e-4}, {"警", 2e-4}, {"頸", 1e-4},
	} {
		lex.Add(lexicon.Entry{Surface: e.w, Reading: zhuyin.ReadingUnits(zhuyin.ParseReading("ㄐㄧㄥˇ")), PRW: float64(1 + i)})
		sys.SetUni(e.w, e.p)
	}
	s := New(personal.NewModel(lex, sys))
	s.Opt.PrefixAware = false
	s.documentEnabled = true
	s.neuralLeft = "今天"
	s.DisableModelScoring = disabled
	s.Rerank = rerank.Options{
		Weight: 1, Margin: 0, ContextRunes: rerank.MaxContextRunes,
		TextWeight: 1, TextMargin: 0,
		Async: rerank.AsyncOptions{Scorer: scorer, Timeout: timeout, TopK: 3},
	}
	return s
}

func asyncCandidateTexts(snap proto.Snapshot) []string {
	if snap.Candidates == nil {
		return nil
	}
	out := make([]string, len(snap.Candidates.Items))
	for i, item := range snap.Candidates.Items {
		out[i] = item.Text
	}
	return out
}

func waitRequest(t *testing.T, f *asyncFake) rerank.Request {
	t.Helper()
	select {
	case r := <-f.called:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("背景評分沒有收到請求")
		return rerank.Request{}
	}
}

// waitPoll 等到 Poll 套用結果或背景評分被丟棄。
func waitPoll(t *testing.T, s *Session) (proto.Snapshot, bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if snap, ok := s.Poll(); ok {
			return snap, true
		}
		if s.asyncPending == nil {
			return proto.Snapshot{}, false
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待背景評分逾時")
	return proto.Snapshot{}, false
}

func openAsyncCandidates(t *testing.T, s *Session) proto.Snapshot {
	t.Helper()
	typeSyl(s, "ㄐㄧㄥˇ")
	return s.Handle(proto.Request{Type: proto.Candidates})
}

func TestAsyncCandidatesApplyWhenCurrent(t *testing.T) {
	release := make(chan struct{})
	f := &asyncFake{
		called:  make(chan rerank.Request, 1),
		release: release,
		score: func(r rerank.Request) ([]float64, error) {
			scores := make([]float64, len(r.Candidates))
			scores[len(scores)-1] = 100 // 統計第三名在最前
			return scores, nil
		},
	}
	s := asyncCandidateSession(f, time.Second, false)
	first := openAsyncCandidates(t, s)
	before := asyncCandidateTexts(first)
	if len(before) < 4 {
		t.Fatalf("測試前提：至少 4 個候選，實得 %v", before)
	}
	req := waitRequest(t, f)
	if len(req.Candidates) != 3 {
		t.Fatalf("gating 只送 3 個，實得 %d", len(req.Candidates))
	}
	if got := asyncCandidateTexts(s.snapshot()); !reflect.DeepEqual(got, before) {
		t.Fatalf("模型回來前順序被改動：%v → %v", before, got)
	}
	if _, ok := s.Poll(); ok {
		t.Fatal("模型尚未回來卻回報更新")
	}
	close(release)
	snap, ok := waitPoll(t, s)
	if !ok {
		t.Fatal("目前的背景評分沒有套用")
	}
	got := asyncCandidateTexts(snap)
	if len(got) != len(before) {
		t.Fatalf("候選數改變：%v → %v", before, got)
	}
	want := []string{before[2], before[0], before[1]}
	if !reflect.DeepEqual(got[:3], want) || !reflect.DeepEqual(got[3:], before[3:]) {
		t.Fatalf("重排 %v，want 前三 %v、其餘 %v", got, want, before[3:])
	}
}

func TestAsyncStaleResultDiscarded(t *testing.T) {
	release := make(chan struct{})
	f := &asyncFake{
		called:  make(chan rerank.Request, 1),
		release: release,
		score: func(r rerank.Request) ([]float64, error) {
			scores := make([]float64, len(r.Candidates))
			scores[len(scores)-1] = 100
			return scores, nil
		},
	}
	s := asyncCandidateSession(f, time.Second, false)
	first := openAsyncCandidates(t, s)
	before := asyncCandidateTexts(first)
	waitRequest(t, f)
	// 換版本：候選視窗仍在，但這次請求已不是產生背景評分的那一次。
	s.Handle(proto.Request{Type: proto.Display, Columns: 5})
	close(release)
	if _, ok := waitPoll(t, s); ok {
		t.Fatal("過期結果被套用")
	}
	if got := asyncCandidateTexts(s.snapshot()); !reflect.DeepEqual(got, before) {
		t.Fatalf("過期結果改動順序：%v → %v", before, got)
	}
}

func TestAsyncTimeoutKeepsOrder(t *testing.T) {
	f := &asyncFake{called: make(chan rerank.Request, 1)}
	f.release = nil // 等 ctx 取消
	s := asyncCandidateSession(f, 10*time.Millisecond, false)
	first := openAsyncCandidates(t, s)
	before := asyncCandidateTexts(first)
	waitRequest(t, f)
	if snap, ok := waitPoll(t, s); ok {
		t.Fatalf("逾時仍回報更新：%v", asyncCandidateTexts(snap))
	}
	if got := asyncCandidateTexts(s.snapshot()); !reflect.DeepEqual(got, before) {
		t.Fatalf("逾時改動順序：%v → %v", before, got)
	}
}

func TestAsyncErrorKeepsOrder(t *testing.T) {
	f := &asyncFake{
		called: make(chan rerank.Request, 1),
		score: func(rerank.Request) ([]float64, error) {
			return nil, errors.New("model unavailable")
		},
	}
	s := asyncCandidateSession(f, time.Second, false)
	first := openAsyncCandidates(t, s)
	before := asyncCandidateTexts(first)
	waitRequest(t, f)
	if _, ok := waitPoll(t, s); ok {
		t.Fatal("錯誤結果被套用")
	}
	if got := asyncCandidateTexts(s.snapshot()); !reflect.DeepEqual(got, before) {
		t.Fatalf("錯誤改動順序：%v → %v", before, got)
	}
}

func TestAsyncModelDisabledMatchesStatisticalOrder(t *testing.T) {
	f := &asyncFake{called: make(chan rerank.Request, 1)}
	disabled := asyncCandidateSession(f, time.Second, true)
	got := asyncCandidateTexts(openAsyncCandidates(t, disabled))
	select {
	case <-f.called:
		t.Fatal("停用模型仍送出背景評分")
	default:
	}
	statistical := asyncCandidateSession(nil, time.Second, false)
	want := asyncCandidateTexts(openAsyncCandidates(t, statistical))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("停用模型順序 %v，統計順序 %v", got, want)
	}
}

func TestAsyncCancelledOnSelectionAndClose(t *testing.T) {
	for _, tc := range []struct {
		name string
		stop func(*Session)
	}{
		{"select", func(s *Session) { s.Handle(proto.Request{Type: proto.Select, Index: 0}) }},
		{"close", func(s *Session) { s.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			f := &asyncFake{called: make(chan rerank.Request, 1), release: release}
			s := asyncCandidateSession(f, time.Second, false)
			openAsyncCandidates(t, s)
			waitRequest(t, f)
			tc.stop(s)
			close(release)
			if _, ok := waitPoll(t, s); ok {
				t.Fatal("取消後仍套用結果")
			}
		})
	}
}

// 確認 gating 用的候選是統計前 3 名，不是把整份清單送去模型。
func TestAsyncGatingUsesStatisticalTopThree(t *testing.T) {
	release := make(chan struct{})
	f := &asyncFake{called: make(chan rerank.Request, 1), release: release}
	s := asyncCandidateSession(f, time.Second, false)
	first := openAsyncCandidates(t, s)
	before := asyncCandidateTexts(first)
	req := waitRequest(t, f)
	for i, row := range req.Candidates {
		if row.Text != before[i] {
			t.Fatalf("第 %d 名送出 %q，統計順序是 %q", i, row.Text, before[i])
		}
	}
	close(release)
	// 全 0 分不超過 margin，因此不重排；Poll 沒有更新是預期行為。
	if _, ok := waitPoll(t, s); ok {
		t.Fatal("全 0 分不應改變順序")
	}
	if got := asyncCandidateTexts(s.snapshot()); !reflect.DeepEqual(got, before) {
		t.Fatalf("全 0 分改動順序：%v → %v", before, got)
	}
}
