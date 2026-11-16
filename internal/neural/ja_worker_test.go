package neural

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestJaProcess 是 Go 測試子行程，模擬 T14 worker 的協定與失敗模式。
// 它不載入模型；真模型 smoke test 走 JA_TEST_REAL。
func TestJaProcess(t *testing.T) {
	mode := os.Getenv("JA_TEST_CHILD")
	if mode == "" {
		return
	}
	model := os.Getenv("JA_TEST_MODEL_SHA")
	if model == "" {
		model = "test-model"
	}
	switch mode {
	case "silent_exit":
		os.Exit(1)
	case "crash_once":
		if _, err := os.Stat(os.Getenv("JA_TEST_STATE")); err != nil {
			_ = os.WriteFile(os.Getenv("JA_TEST_STATE"), []byte("crashed"), 0o600)
			os.Exit(3)
		}
	case "startup_runtime_missing":
		fmt.Println(`{"ready":false,"error":"runtime_missing","detail":"llama-server"}`)
		os.Exit(1)
	case "startup_bad_contract":
		fmt.Printf(`{"ready":true,"schema":"other","model_sha256":%q}`+"\n", model)
		os.Exit(0)
	case "crash_before_ready":
		os.Exit(3)
	}
	fmt.Printf(`{"ready":true,"schema":%q,"backend":"llama.cpp","backend_commit":"test","context_policy":%q,`+
		`"model_sha256":%q,"limits":{"max_reading_units":64,"max_candidates":8,"max_left_context_units":128}}`+"\n",
		JaSchema, JaContextPolicy, model)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req jaWireRequest
		_ = json.Unmarshal(scanner.Bytes(), &req)
		switch mode {
		case "timeout":
			time.Sleep(time.Second)
		case "crash":
			os.Exit(3)
		case "oversize":
			fmt.Println(strings.Repeat("x", frameLimit+1))
			continue
		}
		reply := jaWireReply{ID: req.ID, Revision: req.Revision, Schema: JaSchema, Model: model}
		if mode == "fallback_timeout" {
			reply.Fallback = "timeout"
			b, _ := json.Marshal(reply)
			fmt.Println(string(b))
			continue
		}
		if mode == "stale" {
			reply.Revision = 0
		}
		if mode == "wrong_model" {
			reply.Model = "other"
		}
		if mode == "wrong_id" {
			reply.ID = 0
		}
		if mode == "bad_score" {
			reply.Candidates = []jaWireVerdict{{Score: 1, LogProb: 1}}
		}
		prefix := ""
		if req.Draft != nil {
			prefix = req.Draft.Surface
		} else if len(req.Candidates) > 0 {
			prefix = req.Candidates[0].Surface
		}
		reply.Prefix = prefix
		reply.FixedClauses = 1
		reply.FixedReadingUnits = 2
		if req.Draft != nil {
			reply.Draft = &jaWireVerdict{Surface: req.Draft.Surface, LogProb: -1, Score: -1, Prefix: prefix, Passed: true}
		}
		for _, candidate := range req.Candidates {
			ok := strings.HasPrefix(candidate.Surface, prefix)
			reply.Candidates = append(reply.Candidates, jaWireVerdict{
				Surface: candidate.Surface, LogProb: -float64(len(candidate.Surface)),
				Score: -float64(len(candidate.Surface)), Prefix: prefix,
				Passed: ok && candidate.Surface == prefix, PrefixOK: ok,
			})
		}
		if mode == "bad_score" {
			reply.Candidates = make([]jaWireVerdict, len(req.Candidates))
		}
		b, _ := json.Marshal(reply)
		fmt.Println(string(b))
	}
	os.Exit(0)
}

func childJa(t *testing.T, mode string, timeout time.Duration) *JaRuntime {
	t.Helper()
	c := jaDefaults(JaConfig{Timeout: timeout, RetryCooldown: time.Hour})
	m := testJaManifest()
	r := newJaRuntime(c, m, func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestJaProcess$")
		cmd.Env = append(os.Environ(), "JA_TEST_CHILD="+mode)
		return cmd
	})
	r.launch()
	t.Cleanup(r.Close)
	return r
}

func testJaManifest() JaManifest {
	m := JaManifest{Schema: JaSchema, Backend: JaBackend, BackendCommit: "test", ContextPolicy: JaContextPolicy}
	m.Model.SHA256 = "test-model"
	m.Limits.MaxReadingUnits = 64
	m.Limits.MaxCandidates = 8
	m.Limits.MaxLeftContextUnits = 128
	return m
}

func waitJa(t *testing.T, r *JaRuntime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.WaitReady(ctx); err != nil {
		t.Fatalf("worker not ready: %v", err)
	}
}

func jaRequest() JaRequest {
	return JaRequest{
		Revision: 7, LeftContext: "私の名前は",
		Reading: []string{"き", "ょ", "う", "は", "い", "い", "て", "ん", "き"},
		Draft: &JaCandidate{Surface: "今日はいい天気", Reading: []string{"き", "ょ", "う", "は", "い", "い", "て", "ん", "き"},
			Clauses: []JaClause{{Reading: []string{"き", "ょ", "う"}, Surface: "今日"}, {Reading: []string{"は"}, Surface: "は"}}},
		Candidates: []JaCandidate{
			{Surface: "今日はいい天気", Reading: []string{"き", "ょ", "う", "は", "い", "い", "て", "ん", "き"}},
			{Surface: "今日はいい天気です", Reading: []string{"き", "ょ", "う", "は", "い", "い", "て", "ん", "き"}},
		},
	}
}

func TestJaVerifyProtocol(t *testing.T) {
	r := childJa(t, "success", 100*time.Millisecond)
	waitJa(t, r)
	res, err := r.Verify(jaRequest())
	if err != nil {
		t.Fatal(err)
	}
	if res.Prefix != "今日はいい天気" || len(res.Scores) != 2 || len(res.Passed) != 2 || len(res.Verdicts) != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !res.Passed[0] || res.Passed[1] || !res.PrefixOK[0] || !res.PrefixOK[1] {
		t.Fatalf("verdicts: %+v", res.Verdicts)
	}
	if res.Draft == nil || !res.Draft.Passed || res.FixedClauses != 1 || res.FixedReadingUnits != 2 {
		t.Fatalf("draft: %+v", res.Draft)
	}
	if s := r.Stats(); s.Verified != 1 || s.Calls != 1 || s.Timeouts != 0 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestJaDeadlineAndFailure(t *testing.T) {
	for _, mode := range []string{"timeout", "fallback_timeout", "crash", "stale", "wrong_model", "wrong_id", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			r := childJa(t, mode, 40*time.Millisecond)
			waitJa(t, r)
			start := time.Now()
			_, err := r.Verify(jaRequest())
			if err == nil {
				t.Fatal("invalid worker accepted")
			}
			if (mode == "timeout" || mode == "fallback_timeout") && !errors.Is(err, ErrTimeout) {
				t.Fatalf("expected timeout: %v", err)
			}
			if time.Since(start) > 500*time.Millisecond {
				t.Fatal("unbounded worker wait")
			}
			if mode == "stale" || mode == "wrong_model" || mode == "wrong_id" || mode == "crash" || mode == "oversize" || mode == "fallback_timeout" {
				if _, err = r.Verify(jaRequest()); !errors.Is(err, ErrUnavailable) {
					t.Fatalf("late worker reused: %v", err)
				}
			}
		})
	}
}

func TestJaStartupErrors(t *testing.T) {
	for mode, want := range map[string]string{
		"silent_exit":             JaReasonStartFailed,
		"startup_runtime_missing": JaReasonRuntimeMissing,
		"startup_bad_contract":    JaReasonStartFailed,
		"crash_before_ready":      JaReasonStartFailed,
	} {
		t.Run(mode, func(t *testing.T) {
			r := childJa(t, mode, 20*time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := r.WaitReady(ctx)
			if err == nil {
				t.Fatal("broken worker reported ready")
			}
			if got := describeJaModelError(err); got != want {
				t.Fatalf("reason %q (%v), want %q", got, err, want)
			}
			if _, err = r.Verify(jaRequest()); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("broken worker accepted request: %v", err)
			}
			if r.Ready() {
				t.Fatal("broken worker is ready")
			}
		})
	}
}

func TestJaCancel(t *testing.T) {
	r := childJa(t, "timeout", 100*time.Millisecond)
	waitJa(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if _, err := r.VerifyContext(ctx, jaRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("cancel waited for worker")
	}
	if _, err := r.Verify(jaRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cancelled worker reused: %v", err)
	}
}

func TestJaRestartAfterCrash(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	c := jaDefaults(JaConfig{Timeout: 100 * time.Millisecond, RetryCooldown: 5 * time.Millisecond})
	m := testJaManifest()
	r := newJaRuntime(c, m, func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestJaProcess$")
		cmd.Env = append(os.Environ(), "JA_TEST_CHILD=crash_once", "JA_TEST_STATE="+state)
		return cmd
	})
	r.launch()
	t.Cleanup(r.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.WaitReady(ctx); err == nil {
		t.Fatal("first start unexpectedly succeeded")
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := r.Verify(jaRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("restart not triggered: %v", err)
	}
	waitJa(t, r)
	if res, err := r.Verify(jaRequest()); err != nil || len(res.Verdicts) != 2 {
		t.Fatalf("restarted worker: %v %+v", err, res)
	}
}

func TestJaRequestValidation(t *testing.T) {
	m := testJaManifest()
	bad := jaRequest()
	bad.Candidates[0].Reading = []string{"き"}
	if err := validateJaRequest(bad, m); !errors.Is(err, ErrJaUnsupported) {
		t.Fatalf("reading mismatch: %v", err)
	}
	bad = jaRequest()
	bad.Reading = []string{"き", "ょ", "う", "は", "い", "い", "て", "ん", "き", "で", "す", "ね", "が", "あ", "る"}
	if err := validateJaRequest(bad, m); !errors.Is(err, ErrJaUnsupported) {
		t.Fatalf("reading limit: %v", err)
	}
	bad = jaRequest()
	bad.LeftContext = strings.Repeat("あ", 129)
	if err := validateJaRequest(bad, m); !errors.Is(err, ErrJaUnsupported) {
		t.Fatalf("context limit: %v", err)
	}
	bad = jaRequest()
	bad.Candidates = nil
	if err := validateJaRequest(bad, m); !errors.Is(err, ErrJaUnsupported) {
		t.Fatalf("no candidates: %v", err)
	}
	if err := validateJaRequest(jaRequest(), m); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}

func writeJaPackage(t *testing.T, manifest string, model []byte) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if model != nil {
		if err := os.WriteFile(filepath.Join(dir, "model.gguf"), model, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256(model)
	return dir, hex.EncodeToString(sum[:])
}

func jaManifestJSON(sha, backend, commit string, size int) string {
	return fmt.Sprintf(`{"schema":%q,"backend":%q,"backend_commit":%q,"context_policy":%q,`+
		`"model":{"file":"model.gguf","sha256":%q,"bytes":%d},`+
		`"limits":{"max_reading_units":64,"max_candidates":8,"max_left_context_units":128},"context_size":512}`,
		JaSchema, backend, commit, JaContextPolicy, sha, size)
}

func TestJaModelPairing(t *testing.T) {
	model := []byte("mock model")
	base, sum := writeJaPackage(t, jaManifestJSON("", JaBackend, "commit", len(model)), model)
	good := jaManifestJSON(sum, JaBackend, "commit", len(model))
	if err := os.WriteFile(filepath.Join(base, "manifest.json"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := jaDefaults(JaConfig{Package: base, ModelSHA256: sum, BackendCommit: "commit"})
	if _, err := loadJaManifest(cfg); err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}
	for name, mutate := range map[string]func(*JaConfig, string){
		"hash mismatch": func(c *JaConfig, dir string) { c.ModelSHA256 = strings.Repeat("0", 64) },
		"missing model": func(c *JaConfig, dir string) {
			if err := os.Remove(filepath.Join(dir, "model.gguf")); err != nil {
				t.Fatal(err)
			}
		},
		"backend mismatch": func(c *JaConfig, dir string) {
			_ = os.WriteFile(filepath.Join(dir, "manifest.json"),
				[]byte(jaManifestJSON(sum, "onnx", "commit", len(model))), 0o600)
		},
		"commit mismatch": func(c *JaConfig, dir string) { c.BackendCommit = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := writeJaPackage(t, good, model)
			c := cfg
			c.Package = dir
			mutate(&c, dir)
			_, err := loadJaManifest(c)
			if err == nil {
				t.Fatal("invalid package accepted")
			}
			var modelErr *JaModelError
			if !errors.As(err, &modelErr) {
				t.Fatalf("not a JaModelError: %v", err)
			}
		})
	}
	missing := filepath.Join(t.TempDir(), "missing")
	_, err := loadJaManifest(JaConfig{Package: missing, Timeout: 100 * time.Millisecond, Threads: 4})
	var modelErr *JaModelError
	if !errors.As(err, &modelErr) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing manifest: %v", err)
	}
}

// T30 可以只換 Script 成 mock_runtime.py，協定與逾時行為完全相同。
func TestJaMockPythonRuntime(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	script, err := filepath.Abs("../../platforms/neural/ja/tests/mock_runtime.py")
	if err != nil || script == "" {
		t.Skip("mock runtime not found")
	}
	pkg, err := filepath.Abs("../../platforms/neural/ja/tests/mock-package")
	if err != nil {
		t.Skip("mock package not found")
	}
	r, err := OpenJa(JaConfig{Python: python, Script: script, Package: pkg, Server: "mock",
		Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	waitJa(t, r)
	res, err := r.Verify(jaRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Verdicts) != 2 || !res.Passed[0] || res.Passed[1] {
		t.Fatalf("mock verdicts: %+v", res.Verdicts)
	}
}

func waitJaRestart(r *JaRuntime) {
	deadline := time.Now().Add(10 * time.Second)
	for !r.Ready() && time.Now().Before(deadline) {
		_, _ = r.Verify(jaRequest())
		time.Sleep(20 * time.Millisecond)
	}
}

// 真模型 smoke：JA_TEST_REAL=1、JA_TEST_PACKAGE、JA_TEST_LLAMA_SERVER。
// 只驗證端到端與分數方向，不當延遲認證。
func TestJaRealModelSmoke(t *testing.T) {
	if os.Getenv("JA_TEST_REAL") != "1" {
		t.Skip("set JA_TEST_REAL=1 with a paired zenz GGUF to run")
	}
	python := os.Getenv("JA_TEST_PYTHON")
	if python == "" {
		python = "python3"
	}
	script := os.Getenv("JA_TEST_SCRIPT")
	if script == "" {
		script = "../../platforms/neural/ja/ja_runtime.py"
	}
	r, err := OpenJa(JaConfig{
		Python: python, Script: script, Package: os.Getenv("JA_TEST_PACKAGE"),
		Server: os.Getenv("JA_TEST_LLAMA_SERVER"), ModelSHA256: JaZenzModelSHA256,
		BackendCommit: JaLlamaCPPCommit, Threads: 6, Timeout: 100 * time.Millisecond, StartTimeout: 90 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := r.WaitReady(ctx); err != nil {
		t.Fatalf("real model not ready: %v", err)
	}
	res, err := r.Verify(jaRequest())
	if err != nil {
		// 主機忙碌時第一次可能超過 100 ms；功能 smoke 允許重啟後重試，
		// 但延遲分佈只計成功的樣本。
		for attempt := 0; attempt < 3 && err != nil; attempt++ {
			waitJaRestart(r)
			res, err = r.Verify(jaRequest())
		}
	}
	if err != nil {
		t.Fatalf("real verify: %v", err)
	}
	t.Logf("prefix=%q draft=%+v verdicts=%+v elapsed=%s", res.Prefix, res.Draft, res.Verdicts, res.Elapsed)
	if len(res.Verdicts) != 2 || res.Draft == nil || !res.Draft.Passed {
		t.Fatalf("unexpected real verdicts: %+v", res.Verdicts)
	}
	if res.Scores[0] < res.Scores[1] {
		t.Fatalf("preferred candidate scored lower: %v", res.Scores)
	}
	if n, _ := strconv.Atoi(os.Getenv("JA_TEST_SMOKE_N")); n > 1 {
		times := []float64{float64(res.Elapsed.Microseconds()) / 1000}
		timeouts := 0
		for i := 1; i < n; i++ {
			got, err := r.Verify(jaRequest())
			if err != nil {
				timeouts++
				// 逾時會結束 worker；觸發背景重啟並等就緒，下一次才是獨立
				// 樣本，不會把重啟期間的 ErrUnavailable 重複算成逾時。
				waitJaRestart(r)
				continue
			}
			times = append(times, float64(got.Elapsed.Microseconds())/1000)
		}
		sort.Float64s(times)
		pct := func(p float64) float64 {
			if len(times) == 0 {
				return 0
			}
			pos := float64(len(times)-1) * p / 100
			lo := int(pos)
			hi := lo + 1
			if hi >= len(times) {
				hi = len(times) - 1
			}
			return times[lo] + (times[hi]-times[lo])*(pos-float64(lo))
		}
		t.Logf("preliminary verify n=%d timeouts=%d p50=%.2fms p95=%.2fms max=%.2fms",
			len(times), timeouts, pct(50), pct(95), times[len(times)-1])
	}
}
