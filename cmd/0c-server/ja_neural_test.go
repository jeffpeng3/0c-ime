package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/ja/rerank"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/session"
	"github.com/0c-biggo/0c-ime/internal/neural"
)

func jaWiringTestData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "lexicon"), 0o755); err != nil {
		t.Fatal(err)
	}
	words := strings.Join([]string{
		"今日\tきょう\t1.0\tnoun\tcurated\t-",
		"京\tきょう\t3.0\tnoun\tcurated\t-",
		"は\tは\t1.0\tparticle\tcurated\t-",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "lexicon", "words.tsv"), []byte(words), 0o644); err != nil {
		t.Fatal(err)
	}
	conns := "# 左詞性\t右詞性\t成本\n<s>\tnoun\t0.2\nnoun\tparticle\t0.2\nparticle\t</s>\t0.2\nnoun\t</s>\t0.2\n"
	if err := os.WriteFile(filepath.Join(dir, "connection.tsv"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newJaWiringTestSession(t *testing.T, w *jaNeuralWiring) *session.Session {
	t.Helper()
	s := session.New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	if !w.Attach(s) {
		t.Fatal("Attach(ja) = false")
	}
	if s.Language() != lang.Ja {
		t.Fatalf("language = %q", s.Language())
	}
	return s
}

func typeJaWiringKeys(s *session.Session, keys string) proto.Snapshot {
	var snap proto.Snapshot
	for _, k := range keys {
		snap = s.Handle(proto.Request{Type: proto.Key, Key: string(k)})
	}
	return snap
}

func jaCommit(t *testing.T, s *session.Session, readingKeys string) proto.Snapshot {
	t.Helper()
	typeJaWiringKeys(s, readingKeys)
	snap := s.Handle(proto.Request{Type: proto.Key, Key: proto.KeyConvert})
	if !snap.Converting {
		t.Fatalf("変換されていない: %#v", snap)
	}
	return s.Handle(proto.Request{Type: proto.Key, Key: proto.KeyEnter})
}

func jaMockPaths(t *testing.T) (python, script, pkg string) {
	t.Helper()
	var err error
	python, err = exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	script, err = filepath.Abs("../../platforms/neural/ja/tests/mock_runtime.py")
	if err != nil || script == "" {
		t.Skip("mock runtime not found")
	}
	pkg, err = filepath.Abs("../../platforms/neural/ja/tests/mock-package")
	if err != nil || pkg == "" {
		t.Skip("mock package not found")
	}
	return python, script, pkg
}

// 模型缺失時仍建立統計 profile，日文輸入照常轉換，而且不會留下 worker。
func TestJaNeuralMissingModelKeepsStatisticalConversion(t *testing.T) {
	data := jaWiringTestData(t)
	missing := filepath.Join(t.TempDir(), "models", "ja")
	w, err := newJaNeuralWiring(data, jaNeuralOptions{
		Enabled: true, Package: missing, Python: "python3", Threads: 2, Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if w.host != nil {
		t.Fatal("沒有模型時不應建立 worker")
	}
	if w.State() != jaNeuralStateMissing || w.Reason() != "" {
		t.Fatalf("state = %q reason = %q", w.State(), w.Reason())
	}
	s := newJaWiringTestSession(t, w)
	commit := jaCommit(t, s, "kyouha")
	if commit.Commit != "今日は" {
		t.Fatalf("統計轉換 = %q", commit.Commit)
	}
	if stats := w.RerankerStats(); stats.Calls != 0 {
		t.Fatalf("沒有 worker 卻呼叫了 rerank：%+v", stats)
	}
}

// 模型 manifest 存在但權重檔缺失時，狀態要說得出原因，日文仍用統計轉換。
func TestJaNeuralMissingModelFileReportsFallback(t *testing.T) {
	pkg := t.TempDir()
	manifest := `{"schema":"ja-zenz-verify-v1","backend":"llama.cpp","backend_commit":"test",` +
		`"context_policy":"confirmed_left_128","model":{"file":"model.gguf","sha256":"` + strings.Repeat("0", 64) + `","bytes":5}}`
	if err := os.WriteFile(filepath.Join(pkg, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := newJaNeuralWiring(jaWiringTestData(t), jaNeuralOptions{
		Enabled: true, Package: pkg, Python: "python3", Script: "missing.py", Server: "missing", Threads: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if w.host != nil {
		t.Fatal("模型檔缺失時不應建立 worker")
	}
	if w.State() != jaNeuralStateFallback || w.Reason() != "model_missing" {
		t.Fatalf("state = %q reason = %q", w.State(), w.Reason())
	}
	s := newJaWiringTestSession(t, w)
	if commit := jaCommit(t, s, "kyouha"); commit.Commit != "今日は" {
		t.Fatalf("統計轉換 = %q", commit.Commit)
	}
}

// mock runtime 的端到端：session 輸入 → T12 draft → T30 rerank → T14 verify，
// 再回到 session 選字。確認共用的是同一個 worker，而不是測試替身。
func TestJaNeuralMockRuntimeEndToEnd(t *testing.T) {
	python, script, pkg := jaMockPaths(t)
	data := jaWiringTestData(t)
	w, err := newJaNeuralWiring(data, jaNeuralOptions{
		Enabled: true, Package: pkg, Python: python, Script: script, Server: "mock", Threads: 2,
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if w.host == nil || w.State() != "starting" {
		t.Fatalf("host = %v state = %q", w.host, w.State())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.host.WaitReady(ctx); err != nil {
		t.Skipf("mock runtime not ready: %v", err)
	}
	if w.State() != "ready" {
		t.Fatalf("state = %q", w.State())
	}
	s := newJaWiringTestSession(t, w)
	commit := jaCommit(t, s, "kyouha")
	if commit.Commit == "" {
		t.Fatal("沒有送出変換結果")
	}
	if commit.Text() != "" || commit.Converting {
		t.Fatalf("送出後快照 = %q converting %v", commit.Text(), commit.Converting)
	}
	if stats := w.RerankerStats(); stats.Calls == 0 || stats.Verified == 0 {
		t.Fatalf("rerank stats = %+v", stats)
	}
	if stats := w.Stats(); stats.Verified == 0 {
		t.Fatalf("worker stats = %+v", stats)
	}
}

// 逾時時 T30 保留統計結果，session 不卡住；worker 結束後下一次會重新啟動。
func TestJaNeuralTimeoutKeepsStatisticalConversion(t *testing.T) {
	python, script, pkg := jaMockPaths(t)
	wrapper := filepath.Join(t.TempDir(), "slow_mock.py")
	body := fmt.Sprintf("import runpy, sys\nsys.argv += ['--delay-ms', '1000']\nrunpy.run_path(%s, run_name='__main__')\n", strconv.Quote(script))
	if err := os.WriteFile(wrapper, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	data := jaWiringTestData(t)
	w, err := newJaNeuralWiring(data, jaNeuralOptions{
		Enabled: true, Package: pkg, Python: python, Script: wrapper, Server: "mock", Threads: 2,
		Timeout: 40 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.host.WaitReady(ctx); err != nil {
		t.Skipf("mock runtime not ready: %v", err)
	}
	s := newJaWiringTestSession(t, w)
	start := time.Now()
	commit := jaCommit(t, s, "kyouha")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("逾時仍卡住 %v", elapsed)
	}
	if commit.Commit != "今日は" {
		t.Fatalf("逾時後統計結果 = %q", commit.Commit)
	}
	if stats := w.RerankerStats(); stats.Timeouts == 0 {
		t.Fatalf("rerank stats = %+v", stats)
	}
}

// 建立日文接線不會改到注音 session 的語言或 rerank 選項；注音 golden 由
// internal/golden 的既有測試守門。
func TestJaNeuralWiringLeavesZhuyinSessionAlone(t *testing.T) {
	w, err := newJaNeuralWiring(jaWiringTestData(t), jaNeuralOptions{
		Enabled: true, Package: filepath.Join(t.TempDir(), "missing"), Python: "python3", Threads: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := session.New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	if s.Language() != lang.Zh {
		t.Fatalf("注音 session 語言 = %q", s.Language())
	}
	if s.Rerank.Scorer != nil || s.Rerank.Weight != 0 {
		t.Fatalf("注音 rerank 選項被改動：%+v", s.Rerank)
	}
	ja := newJaWiringTestSession(t, w)
	if ja.Language() != lang.Ja || s.Language() != lang.Zh {
		t.Fatalf("語言互相污染：ja=%q zh=%q", ja.Language(), s.Language())
	}
}

type fakeJaVerifyRuntime struct {
	req    neural.JaRequest
	result neural.JaResult
	err    error
}

func (f *fakeJaVerifyRuntime) VerifyContext(_ context.Context, req neural.JaRequest) (neural.JaResult, error) {
	f.req = req
	return f.result, f.err
}

func jaWiringUnits(s string) reading.Sequence {
	out := make(reading.Sequence, 0, len([]rune(s)))
	for _, r := range s {
		out = append(out, reading.Unit(r))
	}
	return out
}

func jaRerankRequest() rerank.Request {
	seq := jaWiringUnits("きょうは")
	return rerank.Request{
		Revision: 7, LeftContext: "私の", Reading: seq,
		Draft: &rerank.Candidate{Surface: "今日は", Reading: seq,
			Clauses: []rerank.Clause{{Reading: jaWiringUnits("きょう"), Surface: "今日"}, {Reading: jaWiringUnits("は"), Surface: "は"}}},
		Candidates: []rerank.Candidate{
			{Surface: "今日は", Reading: seq},
			{Surface: "京は", Reading: seq},
		},
	}
}

func TestJaVerifierMapsRequestAndVerdicts(t *testing.T) {
	rt := &fakeJaVerifyRuntime{result: neural.JaResult{
		Prefix: "今日", FixedClauses: 1, FixedReadingUnits: 3,
		Draft:    &neural.JaVerdict{Surface: "今日は", LogProb: -2, Score: -0.4, Prefix: "今日", Passed: true},
		Verdicts: []neural.JaVerdict{{Surface: "今日は", Score: -0.4, LogProb: -2, Passed: true, Prefix: "今日", PrefixOK: true}, {Surface: "京は", Score: -0.9, LogProb: -3, PrefixOK: true}},
		Elapsed:  5 * time.Millisecond,
	}}
	got, err := (jaVerifier{rt: rt}).Verify(context.Background(), jaRerankRequest())
	if err != nil {
		t.Fatal(err)
	}
	if rt.req.Revision != 7 || rt.req.LeftContext != "私の" || len(rt.req.Reading) != 4 || rt.req.Reading[0] != "き" {
		t.Fatalf("request = %+v", rt.req)
	}
	if rt.req.Draft == nil || rt.req.Draft.Surface != "今日は" || len(rt.req.Draft.Clauses) != 2 ||
		rt.req.Draft.Clauses[0].Surface != "今日" || rt.req.Draft.Clauses[0].Reading[0] != "き" {
		t.Fatalf("draft = %+v", rt.req.Draft)
	}
	if len(rt.req.Candidates) != 2 || rt.req.Candidates[1].Surface != "京は" || rt.req.Candidates[1].Reading[3] != "は" {
		t.Fatalf("candidates = %+v", rt.req.Candidates)
	}
	if got.Prefix != "今日" || got.FixedClauses != 1 || got.FixedReadingUnits != 3 ||
		len(got.Scores) != 2 || got.Scores[0] != -0.4 || got.Scores[1] != -0.9 {
		t.Fatalf("result = %+v", got)
	}
	if got.Draft == nil || !got.Draft.Passed || got.Draft.Surface != "今日は" {
		t.Fatalf("draft result = %+v", got.Draft)
	}
	if !got.Passed[0] || got.Passed[1] || !got.PrefixOK[0] || !got.PrefixOK[1] {
		t.Fatalf("flags = passed %v prefix_ok %v", got.Passed, got.PrefixOK)
	}
	if got.Verdicts[1].Surface != "京は" || got.Elapsed != 5*time.Millisecond {
		t.Fatalf("verdicts = %+v elapsed %v", got.Verdicts, got.Elapsed)
	}
}

func TestJaVerifierMapsProtocolAndSentinels(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   error
		want error
	}{
		{"timeout", neural.ErrTimeout, rerank.ErrTimeout},
		{"unavailable", neural.ErrUnavailable, rerank.ErrUnavailable},
		{"unsupported", neural.ErrJaUnsupported, rerank.ErrUnsupported},
		{"deadline", context.DeadlineExceeded, rerank.ErrTimeout},
		{"cancel", context.Canceled, rerank.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (jaVerifier{rt: &fakeJaVerifyRuntime{err: tc.in}}).Verify(context.Background(), jaRerankRequest())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	_, err := (jaVerifier{rt: &fakeJaVerifyRuntime{result: neural.JaResult{Verdicts: []neural.JaVerdict{{Surface: "今日は"}}}}}).
		Verify(context.Background(), jaRerankRequest())
	if !errors.Is(err, rerank.ErrUnsupported) {
		t.Fatalf("short verdict list err = %v", err)
	}
	if _, err := (jaVerifier{}).Verify(context.Background(), jaRerankRequest()); !errors.Is(err, rerank.ErrUnavailable) {
		t.Fatalf("nil runtime err = %v", err)
	}
}
