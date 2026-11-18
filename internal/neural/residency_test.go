package neural

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/punct"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/internal/neural/native"
)

// A build without the native backend must report native_unavailable and fall
// back to statistics; there is no interpreter fallback.
func TestNativePunctuationNeedsNativeBackend(t *testing.T) {
	if native.Available() {
		t.Skip("this build contains the native ONNX backend")
	}
	_, err := LoadModel(context.Background(), ModelSpec{Name: "punct", Language: "zh",
		Role: RolePunct, Backend: BackendNative, Package: t.TempDir()}, &Prepared{})
	if !errors.Is(err, native.ErrNativeUnavailable) {
		t.Fatalf("LoadModel err = %v, want native_unavailable", err)
	}
	var modelErr *ModelError
	if !errors.As(err, &modelErr) || modelErr.Reason != ReasonNativeUnavailable {
		t.Fatalf("model error %+v", err)
	}
}

// syntheticONNX 產生極小的 protobuf 外框：ir_version、producer、graph、opset_import。
// 不是可推論的模型，只用來測檔案核對與 corrupt 分類；真正的 ONNX 由
// TestPlaceholderModelProbe 以 OC_PLACEHOLDER_MODELS 驗證。
func syntheticONNX(seed byte) []byte {
	graph := []byte{0x0a, 0x01, seed} // GraphProto.name
	opset := []byte{0x08, 0x0d}       // OperatorSetIdProto.id = 13
	out := []byte{0x08, 0x08, 0x12, 0x04, 't', 'e', 's', 't'}
	out = append(out, 0x3a, byte(len(graph)))
	out = append(out, graph...)
	out = append(out, 0x42, byte(len(opset)))
	out = append(out, opset...)
	return out
}

type syntheticPackage struct {
	dir  string
	spec ModelSpec
}

func writeSyntheticPackage(t *testing.T, root, name string, role Role, schema, dictionary string, seed byte) syntheticPackage {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	model := syntheticONNX(seed)
	if err := os.WriteFile(filepath.Join(dir, "model.onnx"), model, 0o644); err != nil {
		t.Fatal(err)
	}
	vocab := []byte(`{"characters":[],"bases":[]}`)
	if err := os.WriteFile(filepath.Join(dir, "vocab.json"), vocab, 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"model.onnx": digest(model), "vocab.json": digest(vocab)}
	manifest := map[string]any{"schema": schema, "files": files, "dictionary_sha256": dictionary}
	if schema != PunctSchema {
		manifest["context_policy"] = "confirmed_left_64"
		manifest["weight"] = 0.5
		manifest["margin"] = 0.1
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mustJSON(t, manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return syntheticPackage{dir: dir, spec: ModelSpec{Name: name, Language: "zh", Role: role, Package: dir,
		DictionarySHA256: dictionary, Threads: 1, Timeout: 20 * time.Millisecond, Preload: true, ValidateONNX: true}}
}

func digest(b []byte) string {
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type fakeRuntime struct {
	name    string
	delay   time.Duration
	block   <-chan struct{}
	started chan struct{}
	once    sync.Once

	mu      sync.Mutex
	inFL    int
	closed  atomic.Int32
	order   *closeOrder
	scored  atomic.Int32
	maxSeen *atomic.Int64
}

func (f *fakeRuntime) Score(r rerank.Request) ([]float64, error) {
	return f.ScoreContext(context.Background(), r)
}

func (f *fakeRuntime) ScoreContext(ctx context.Context, _ rerank.Request) ([]float64, error) {
	f.mu.Lock()
	f.inFL++
	if f.maxSeen != nil {
		for {
			old := f.maxSeen.Load()
			if int64(f.inFL) <= old || f.maxSeen.CompareAndSwap(old, int64(f.inFL)) {
				break
			}
		}
	}
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.inFL--; f.mu.Unlock() }()
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.scored.Add(1)
	return []float64{1}, nil
}

func (f *fakeRuntime) Punctuate(string) ([]punct.Edit, error) {
	return f.PunctuateContext(context.Background(), "")
}

func (f *fakeRuntime) PunctuateContext(ctx context.Context, _ string) ([]punct.Edit, error) {
	if _, err := f.ScoreContext(ctx, rerank.Request{}); err != nil {
		return nil, err
	}
	return []punct.Edit{{Offset: 1, Text: "，"}}, nil
}

func (f *fakeRuntime) loaded() *Loaded {
	return &Loaded{Scorer: f, Punctuator: f, Options: rerank.Options{Weight: 0.5}, ResidentBytes: 128,
		Close: func() error {
			f.closed.Add(1)
			if f.order != nil {
				f.order.add(f.name)
			}
			return nil
		},
		State: func() string { return "ready" }, Reason: func() string { return "" },
		Stats: func() Stats { return Stats{Scored: int(f.scored.Load())} }}
}

type closeOrder struct {
	mu    sync.Mutex
	names []string
}

func (o *closeOrder) add(name string) {
	o.mu.Lock()
	o.names = append(o.names, name)
	o.mu.Unlock()
}

func (o *closeOrder) list() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.names...)
}

type fakeLoader struct {
	mu    sync.Mutex
	calls []string
	build func(ModelSpec) (*fakeRuntime, error)
	delay time.Duration
}

func (l *fakeLoader) load(ctx context.Context, spec ModelSpec, _ *Prepared) (*Loaded, error) {
	l.mu.Lock()
	l.calls = append(l.calls, spec.Name)
	l.mu.Unlock()
	if l.delay > 0 {
		select {
		case <-time.After(l.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if l.build != nil {
		rt, err := l.build(spec)
		if err != nil {
			return nil, err
		}
		return rt.loaded(), nil
	}
	return (&fakeRuntime{name: spec.Name}).loaded(), nil
}

func (l *fakeLoader) called() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func testSpecs() []ModelSpec {
	return []ModelSpec{
		{Name: "zh-select", Language: "zh", Role: RoleSelect},
		{Name: "zh-punct", Language: "zh", Role: RolePunct},
		{Name: "ja-rerank", Language: "ja", Role: RoleRerank},
	}
}

func TestParseMode(t *testing.T) {
	for _, value := range []string{"auto", "all", "primary", "none", " AUTO "} {
		if _, err := ParseMode(value); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
	}
	if _, err := ParseMode("sometimes"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestParseBackendRemovedPython(t *testing.T) {
	for _, value := range []string{"native", " NATIVE "} {
		if _, err := ParseBackend(value); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
	}
	if _, err := ParseBackend("python"); err == nil {
		t.Fatal("python backend accepted after removal")
	}
	if _, err := ParseBackend("rust"); err == nil {
		t.Fatal("unknown backend accepted")
	}
}

func TestLoadModelNativeUnavailable(t *testing.T) {
	if NativeAvailable() {
		t.Skip("native backend is built")
	}
	p := writeSyntheticPackage(t, t.TempDir(), "native-select", RoleSelect, rerank.SequenceSchemaV1, strings.Repeat("d", 64), 7)
	p.spec.Backend = BackendNative
	if _, err := LoadModel(context.Background(), p.spec, nil); err == nil {
		t.Fatal("native backend unexpectedly available")
	} else if !errors.Is(err, native.ErrNativeUnavailable) {
		t.Fatalf("err = %v, want ErrNativeUnavailable", err)
	}
}

// P2-43: native load failures carry stable reasons and the manager falls back
// to the statistical engine.
func TestNativeFailuresCarryStableReasons(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: bad shape", native.ErrUnsupportedModel), ReasonUnsupportedModel},
		{fmt.Errorf("%w: no dll", native.ErrRuntimeMissing), ReasonRuntimeMissing},
		{fmt.Errorf("%w: no tag", native.ErrNativeUnavailable), ReasonNativeUnavailable},
		{fmt.Errorf("%w: hash", native.ErrPackageHash), ReasonHashMismatch},
	} {
		var modelErr *ModelError
		if !errors.As(wrapNativeError(c.err), &modelErr) || modelErr.Reason != c.want {
			t.Errorf("wrapNativeError(%v) = %v, want reason %s", c.err, wrapNativeError(c.err), c.want)
		}
	}
}

func TestManagerModeSelection(t *testing.T) {
	t.Run("all", func(t *testing.T) {
		l := &fakeLoader{}
		m, err := OpenManager(ManagerConfig{Mode: ModeAll, Loader: l.load}, testSpecs()...)
		if err != nil {
			t.Fatal(err)
		}
		defer m.Close()
		waitFor(t, "all models", func() bool { return m.Residency().Loaded == 3 })
		for _, name := range []string{"zh-select", "zh-punct", "ja-rerank"} {
			if state := m.State(name); state != "ready" {
				t.Fatalf("%s state %s", name, state)
			}
		}
		if h := m.Handle("zh-select"); h == nil || h.Options().Scorer == nil || h.Options().Weight != 0.5 || h.Options().Async.Scorer == nil {
			t.Fatalf("scorer handle options: %+v", h)
		}
		if h := m.Handle("zh-punct"); h == nil || h.Options().Scorer != nil || h.Options().Async.Scorer != nil {
			t.Fatalf("punctuator handle must not expose a scorer: %+v", h)
		}
		if got := len(l.called()); got != 3 {
			t.Fatalf("loader calls %d", got)
		}
	})
	t.Run("primary", func(t *testing.T) {
		l := &fakeLoader{}
		m, err := OpenManager(ManagerConfig{Mode: ModePrimary, PrimaryLanguage: "zh", Loader: l.load}, testSpecs()...)
		if err != nil {
			t.Fatal(err)
		}
		defer m.Close()
		waitFor(t, "primary models", func() bool { return m.Residency().Loaded == 2 })
		if _, err := m.ScoreContext(context.Background(), "ja-rerank", rerank.Request{}); !errors.Is(err, ErrDisabled) {
			t.Fatalf("secondary language: %v", err)
		}
		if state := m.State("ja-rerank"); state != stateDisabled {
			t.Fatalf("ja state %s", state)
		}
	})
	t.Run("none", func(t *testing.T) {
		l := &fakeLoader{}
		m, err := OpenManager(ManagerConfig{Mode: ModeNone, Loader: l.load}, testSpecs()...)
		if err != nil {
			t.Fatal(err)
		}
		defer m.Close()
		if _, err := m.ScoreContext(context.Background(), "zh-select", rerank.Request{}); !errors.Is(err, ErrDisabled) {
			t.Fatalf("none mode: %v", err)
		}
		if len(l.called()) != 0 || m.Residency().Loaded != 0 {
			t.Fatalf("none mode loaded models: %v", l.called())
		}
	})
}

func TestManagerLazyLoadDoesNotBlock(t *testing.T) {
	l := &fakeLoader{delay: 150 * time.Millisecond, build: func(spec ModelSpec) (*fakeRuntime, error) {
		return &fakeRuntime{name: spec.Name}, nil
	}}
	started := time.Now()
	m, err := OpenManager(ManagerConfig{Mode: ModeAuto, PrimaryLanguage: "ja", Loader: l.load},
		ModelSpec{Name: "zh-select", Language: "zh", Role: RoleSelect})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	openElapsed := time.Since(started)
	if openElapsed > 50*time.Millisecond {
		t.Fatalf("open blocked: %v", openElapsed)
	}
	if state := m.State("zh-select"); state != stateIdle {
		t.Fatalf("secondary model loaded eagerly: %s", state)
	}
	started = time.Now()
	if _, err := m.ScoreContext(context.Background(), "zh-select", rerank.Request{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("first request must return unavailable without waiting: %v", err)
	}
	firstElapsed := time.Since(started)
	if firstElapsed > 20*time.Millisecond {
		t.Fatalf("first request blocked: %v", firstElapsed)
	}
	loadStarted := time.Now()
	waitFor(t, "lazy load", func() bool { return m.Residency().Loaded == 1 })
	t.Logf("synthetic cold start: open=%v first_request=%v load_ready=%v", openElapsed, firstElapsed, time.Since(loadStarted))
	if scores, err := m.ScoreContext(context.Background(), "zh-select", rerank.Request{}); err != nil || len(scores) != 1 {
		t.Fatalf("after lazy load: %v %v", scores, err)
	}
}

func TestManagerPackageFallbacks(t *testing.T) {
	dict := fmt.Sprintf("%064x", 1)
	root := t.TempDir()
	valid := writeSyntheticPackage(t, root, "valid", RoleSelect, "zuin-sequence-runtime-v1", dict, 1)

	cases := []struct {
		name   string
		spec   ModelSpec
		reason string
	}{
		{"missing", ModelSpec{Name: "missing", Language: "zh", Role: RoleSelect, Package: filepath.Join(root, "absent"),
			DictionarySHA256: dict, Threads: 1, Timeout: 20 * time.Millisecond, Preload: true}, ReasonMissing},
		{"hash", ModelSpec{Name: "hash", Language: "zh", Role: RoleSelect, Package: valid.dir,
			DictionarySHA256: dict, Threads: 1, Timeout: 20 * time.Millisecond, Preload: true}, ReasonHashMismatch},
		{"version", ModelSpec{Name: "version", Language: "zh", Role: RoleSelect, Package: valid.dir,
			DictionarySHA256: dict, Threads: 1, Timeout: 20 * time.Millisecond, Preload: true}, ReasonVersionMismatch},
	}
	if err := os.WriteFile(filepath.Join(valid.dir, "model.onnx"), []byte("not onnx"), 0o644); err != nil {
		t.Fatal(err)
	}
	corruptDir := filepath.Join(root, "corrupt")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "manifest.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		name   string
		spec   ModelSpec
		reason string
	}{"corrupt", ModelSpec{Name: "corrupt", Language: "zh", Role: RoleSelect, Package: corruptDir,
		DictionarySHA256: dict, Threads: 1, Timeout: 20 * time.Millisecond, Preload: true}, ReasonCorrupt})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := c.spec
			if c.name == "version" {
				p := writeSyntheticPackage(t, root, "version", RoleSelect, "zuin-sequence-runtime-v9", dict, 2)
				spec.Package = p.dir
			}
			m, err := OpenManager(ManagerConfig{Mode: ModeAuto, Loader: (&fakeLoader{}).load}, spec)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if m.StartupOK(spec.Name) {
				t.Fatal("broken package reported startup ok")
			}
			if state := m.State(spec.Name); state != stateFallback {
				t.Fatalf("state %s", state)
			}
			if reason := m.Reason(spec.Name); reason != c.reason {
				t.Fatalf("reason %s, want %s", reason, c.reason)
			}
			if _, err := m.ScoreContext(context.Background(), spec.Name, rerank.Request{}); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("score must degrade: %v", err)
			}
			if r := m.Residency(); r.Failed != 1 || r.Loaded != 0 {
				t.Fatalf("residency %+v", r)
			}
		})
	}
}

func TestManagerConcurrentRequestsShareExecutor(t *testing.T) {
	var max atomic.Int64
	l := &fakeLoader{build: func(spec ModelSpec) (*fakeRuntime, error) {
		return &fakeRuntime{name: spec.Name, delay: 3 * time.Millisecond, maxSeen: &max}, nil
	}}
	m, err := OpenManager(ManagerConfig{Mode: ModeAll, MaxConcurrent: 2, Loader: l.load},
		ModelSpec{Name: "a", Language: "zh", Role: RoleSelect},
		ModelSpec{Name: "b", Language: "zh", Role: RoleSelect})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitFor(t, "both models", func() bool { return m.Residency().Loaded == 2 })
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		name := "a"
		if i%2 == 1 {
			name = "b"
		}
		go func() {
			defer wg.Done()
			if scores, err := m.ScoreContext(context.Background(), name, rerank.Request{}); err != nil || len(scores) != 1 {
				errs <- fmt.Errorf("%s: %v %v", name, scores, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := max.Load(); got > 2 {
		t.Fatalf("shared executor exceeded capacity: %d", got)
	}
	waitFor(t, "executor drain", func() bool { return m.Residency().InFlight == 0 })
}

func TestManagerShutdownDuringInflightWork(t *testing.T) {
	release := make(chan struct{})
	var rt *fakeRuntime
	l := &fakeLoader{build: func(spec ModelSpec) (*fakeRuntime, error) {
		rt = &fakeRuntime{name: spec.Name, block: release, started: make(chan struct{})}
		return rt, nil
	}}
	m, err := OpenManager(ManagerConfig{Mode: ModeAll, Loader: l.load},
		ModelSpec{Name: "a", Language: "zh", Role: RoleSelect})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "model", func() bool { return m.Residency().Loaded == 1 })
	done := make(chan error, 1)
	go func() {
		_, err := m.ScoreContext(context.Background(), "a", rerank.Request{})
		done <- err
	}()
	select {
	case <-rt.started:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight request never started")
	}
	started := time.Now()
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish during in-flight work")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("close took %v", elapsed)
	}
	if err := <-done; err == nil {
		t.Fatal("in-flight request succeeded after shutdown")
	}
	if rt.closed.Load() != 1 {
		t.Fatalf("backend closed %d times", rt.closed.Load())
	}
	if _, err := m.ScoreContext(context.Background(), "a", rerank.Request{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("after close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if rt.closed.Load() != 1 {
		t.Fatalf("second Close closed backend again")
	}
}

func TestManagerCloseOrderAndResidency(t *testing.T) {
	order := &closeOrder{}
	l := &fakeLoader{build: func(spec ModelSpec) (*fakeRuntime, error) {
		return &fakeRuntime{name: spec.Name, order: order}, nil
	}}
	m, err := OpenManager(ManagerConfig{Mode: ModeAll, Loader: l.load},
		ModelSpec{Name: "b", Language: "zh", Role: RoleSelect},
		ModelSpec{Name: "a", Language: "zh", Role: RolePunct})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "models", func() bool { return m.Residency().Loaded == 2 })
	if r := m.Residency(); r.ResidentBytes != 256 || r.PeakBytes < 256 {
		t.Fatalf("residency %+v", r)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if got := order.list(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("close order %v", got)
	}
	if r := m.Residency(); r.ResidentBytes != 0 || r.Loaded != 0 {
		t.Fatalf("after close %+v", r)
	}
}

func TestPrepareAndValidateSyntheticONNX(t *testing.T) {
	dict := fmt.Sprintf("%064x", 2)
	root := t.TempDir()
	p := writeSyntheticPackage(t, root, "select", RoleSelect, "zuin-sequence-runtime-v1", dict, 7)
	prepared, err := prepareModel(p.spec)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Bytes == 0 || prepared.Options.Weight != 0.5 || prepared.Manifest.Files["model.onnx"] == "" {
		t.Fatalf("%+v", prepared)
	}
	if err := validateONNXFile(filepath.Join(p.dir, "model.onnx")); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.onnx")
	if err := os.WriteFile(bad, syntheticONNX(1)[:6], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateONNXFile(bad); !errors.Is(err, errONNXCorrupt) {
		t.Fatalf("truncated ONNX: %v", err)
	}
	if err := os.WriteFile(bad, []byte("not a protobuf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateONNXFile(bad); err == nil {
		t.Fatal("garbage accepted as ONNX")
	}
}

func TestPlaceholderFlagFromManifest(t *testing.T) {
	dict := fmt.Sprintf("%064x", 3)
	root := t.TempDir()
	p := writeSyntheticPackage(t, root, "placeholder", RoleSelect, "zuin-sequence-runtime-v1", dict, 9)
	b, err := os.ReadFile(filepath.Join(p.dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["placeholder"] = true
	m["quality"] = "untrained"
	if err := os.WriteFile(filepath.Join(p.dir, "manifest.json"), mustJSON(t, m), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareModel(p.spec)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Placeholder || prepared.Quality != "untrained" {
		t.Fatalf("placeholder not surfaced: %+v", prepared)
	}
	mgr, err := OpenManager(ManagerConfig{Mode: ModeAll, Loader: func(ctx context.Context, spec ModelSpec, prepared *Prepared) (*Loaded, error) {
		loaded := (&fakeRuntime{name: spec.Name}).loaded()
		loaded.Placeholder, loaded.Quality = prepared.Placeholder, prepared.Quality
		return loaded, nil
	}}, p.spec)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	waitFor(t, "placeholder", func() bool { return mgr.Residency().Loaded == 1 })
	if !mgr.Placeholder("placeholder") || !mgr.Residency().Models[0].Placeholder {
		t.Fatal("manager did not surface placeholder")
	}
}

func TestArtifactManifestValidation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "reranker-public-core")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	model := syntheticONNX(11)
	path := filepath.Join(dir, "reranker-public-core.placeholder.int8.onnx")
	if err := os.WriteFile(path, model, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema": "0c.model.manifest/1", "kind": "reranker", "artifact_id": "reranker-public-core",
		"variant": "public-core", "placeholder": true, "quality": "untrained", "production_eligible": false,
		"files": map[string]any{"int8": map[string]any{"path": "reranker-public-core/reranker-public-core.placeholder.int8.onnx",
			"bytes": len(model), "sha256": digest(model), "quantization": "dynamic-quint8"}},
		"io": map[string]any{"outputs": []any{map[string]any{"name": "scores"}}},
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, mustJSON(t, manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact, err := ValidateArtifact(manifestPath, root, "int8")
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Manifest.Placeholder || artifact.Manifest.Quality != "untrained" || artifact.Bytes != int64(len(model)) {
		t.Fatalf("%+v", artifact)
	}
	if err := os.WriteFile(path, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateArtifact(manifestPath, root, "int8"); !errors.Is(err, errPackageHashMismatch) {
		t.Fatalf("hash mismatch not detected: %v", err)
	}
}
