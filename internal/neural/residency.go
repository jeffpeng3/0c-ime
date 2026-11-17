package neural

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0c-biggo/0c-ime/core/punct"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/internal/neural/native"
)

// Backend selects how a model package is executed. python is the default;
// native is opt-in until parity has been proven on the target model.
type Backend string

const (
	BackendPython Backend = "python"
	BackendNative Backend = "native"
)

// ParseBackend parses the -neural-backend value.
func ParseBackend(value string) (Backend, error) {
	switch backend := Backend(strings.ToLower(strings.TrimSpace(value))); backend {
	case BackendPython, BackendNative:
		return backend, nil
	}
	return "", fmt.Errorf("unknown neural backend %q", value)
}

// NativeAvailable reports whether this binary contains the native ONNX
// backend. Default CGO-disabled builds return false.
func NativeAvailable() bool { return native.Available() }

// Mode 是模型常駐策略（P2-30 / FR-9）。
//
//	auto    主要語言在背景載入；其他語言第一次用到時才在背景載入（預設）
//	all     所有模型在背景載入
//	primary 只允許主要語言，其他語言不載入
//	none    不載入任何模型，一律使用統計轉換
type Mode string

const (
	ModeAuto    Mode = "auto"
	ModeAll     Mode = "all"
	ModePrimary Mode = "primary"
	ModeNone    Mode = "none"
)

// ParseMode 解析設定值；未知的模式回傳錯誤，由呼叫端決定要回退或拒絕啟動。
func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case ModeAuto, ModeAll, ModePrimary, ModeNone:
		return mode, nil
	}
	return "", fmt.Errorf("unknown model mode %q", value)
}

func (m Mode) valid() bool {
	switch m {
	case ModeAuto, ModeAll, ModePrimary, ModeNone:
		return true
	}
	return false
}

// allows 回報某個語言在模式下是否允許載入。
func (m Mode) allows(language, primary string) bool {
	switch m {
	case ModeNone:
		return false
	case ModePrimary:
		return language == primary
	default:
		return true
	}
}

// eager 回報 OpenManager 是否要立刻在背景排入載入。auto 只先載入主要語言，
// 其他語言等第一次請求；all 全部排入；primary 只排入主要語言。
func (m Mode) eager(language, primary string) bool {
	switch m {
	case ModeAll:
		return true
	case ModeAuto, ModePrimary:
		return language == primary
	}
	return false
}

// Role 是模型在輸入法裡的用途。目前正式路徑是中文 sequence 選字與補標點；
// 日文 reranker 與 P2-25P 的 0c.model.manifest/1 bundle 由 P2-31 接上，
// 這個管理器只負責載入、常駐、失敗回退與共用執行器，不假設 tensor 契約。
type Role string

const (
	RoleSelect  Role = "select"
	RolePunct   Role = "punct"
	RoleRerank  Role = "rerank"
	RoleJAPunct Role = "ja-punct"
)

func (r Role) valid() bool {
	switch r {
	case RoleSelect, RolePunct, RoleRerank, RoleJAPunct:
		return true
	}
	return false
}

// ModelSpec 描述一個可管理的模型。Name 必須唯一；Language 用 zh、ja 等語言 ID。
// Preload 會在 OpenManager 同步核對 manifest 與雜湊（只讀 metadata，不載入模型），
// 讓 scorer 的權重、margin 在建立 session 前就可用；模型本身仍在背景啟動。
type ModelSpec struct {
	Name             string
	Language         string
	Role             Role
	Backend          Backend
	Python           string
	Script           string
	Package          string
	DictionarySHA256 string
	Threads          int
	Timeout          time.Duration
	Preload          bool
	ValidateONNX     bool
	// ORTLibrary overrides the ONNX Runtime shared library used by the native
	// backend. Empty uses the platform default (onnxruntime.so/.dll).
	ORTLibrary string
}

// Prepared 是同步核對過的套件 metadata。Bytes 只計算 manifest 列出的模型與字表檔案。
type Prepared struct {
	Manifest  Manifest
	ModelSHA  string
	PunctPath string
	Bytes     int64
	Options   rerank.Options
	// Placeholder 與 Quality 來自 manifest；placeholder 模型不得被當成已訓練模型。
	Placeholder bool
	Quality     string
}

// Loaded 是背景載入完成的後端。Loader 必須在 ctx 取消時盡快返回；Manager 會在
// Close 時先取消 context，再等待所有載入與執行中的請求。
type Loaded struct {
	Scorer        rerank.Scorer
	Punctuator    punct.Punctuator
	Options       rerank.Options
	ResidentBytes int64
	Placeholder   bool
	Quality       string
	Close         func() error
	State         func() string
	Reason        func() string
	Stats         func() Stats
	Restarts      func() int
}

// Loader 建立後端。prepared 可能為 nil（延遲載入時才核對）。
type Loader func(ctx context.Context, spec ModelSpec, prepared *Prepared) (*Loaded, error)

// ManagerConfig 是模型常駐管理器的設定。
type ManagerConfig struct {
	Mode            Mode
	PrimaryLanguage string
	MaxConcurrent   int
	Loader          Loader
	Log             func(string, ...any)
	// RetryAfter 是載入失敗後再嘗試的冷卻；0 使用 30 秒。
	RetryAfter time.Duration
	Now        func() time.Time
}

// ModelStatus 是單一模型的可觀測狀態；不含任何輸入文字。
type ModelStatus struct {
	Name          string `json:"name"`
	Language      string `json:"language"`
	Role          Role   `json:"role"`
	State         string `json:"state"`
	Reason        string `json:"reason,omitempty"`
	ResidentBytes int64  `json:"resident_bytes"`
	Loaded        bool   `json:"loaded"`
	Placeholder   bool   `json:"placeholder"`
	Quality       string `json:"quality,omitempty"`
}

// Residency 是管理器當下的常駐與執行統計，供 P2-33 量測記憶體與壓力測試。
type Residency struct {
	Mode            Mode          `json:"mode"`
	PrimaryLanguage string        `json:"primary_language"`
	Models          []ModelStatus `json:"models"`
	ResidentBytes   int64         `json:"resident_bytes"`
	PeakBytes       int64         `json:"peak_bytes"`
	Loaded          int           `json:"loaded"`
	Loading         int           `json:"loading"`
	Failed          int           `json:"failed"`
	Disabled        int           `json:"disabled"`
	InFlight        int64         `json:"in_flight"`
	Queued          int64         `json:"queued"`
	Concurrency     int           `json:"concurrency"`
}

const (
	stateDisabled = "disabled"
	stateIdle     = "idle"
	stateLoading  = "loading"
	stateFallback = "fallback"
	stateClosed   = "closed"
)

var (
	ErrClosed   = errors.New("neural model manager closed")
	ErrDisabled = errors.New("neural model disabled by mode")
)

// ModelError 把載入失敗分類成固定原因代碼，狀態摘要不含檔案內容或輸入文字。
type ModelError struct {
	Reason string
	Err    error
}

func (e *ModelError) Error() string {
	if e.Err == nil {
		return "neural model " + e.Reason
	}
	return "neural model " + e.Reason + ": " + e.Err.Error()
}
func (e *ModelError) Unwrap() error { return e.Err }

type modelEntry struct {
	spec        ModelSpec
	prepared    *Prepared
	loaded      *Loaded
	state       string
	reason      string
	err         error
	loading     bool
	disabled    bool
	lastAttempt time.Time
	lastUsed    time.Time
	bytes       int64
}

// Manager 是 ONNX Runtime 多模型常駐管理器。所有模型共用一個有界的執行器，
// 一次最多 MaxConcurrent 個推論同時使用 CPU；載入與第一次推論都不在按鍵路徑上等待。
type Manager struct {
	mu        sync.Mutex
	cfg       ManagerConfig
	entries   map[string]*modelEntry
	order     []string
	exec      *sharedExecutor
	ctx       context.Context
	cancel    context.CancelFunc
	loadWG    sync.WaitGroup
	runWG     sync.WaitGroup
	closing   bool
	closed    bool
	closeDone chan struct{}
	resident  int64
	peak      int64
}

// OpenManager 建立管理器並立即返回；preload 的 manifest 核對是唯一的同步 I/O，
// 模型載入一律在背景。未知模式或重複名稱是設定錯誤。
func OpenManager(cfg ManagerConfig, specs ...ModelSpec) (*Manager, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModeAuto
	}
	if !cfg.Mode.valid() {
		return nil, fmt.Errorf("unknown model mode %q", cfg.Mode)
	}
	if cfg.PrimaryLanguage == "" {
		cfg.PrimaryLanguage = "zh"
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = DefaultThreads()
	}
	if cfg.RetryAfter == 0 {
		cfg.RetryAfter = 30 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Loader == nil {
		cfg.Loader = LoadModel
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{cfg: cfg, entries: map[string]*modelEntry{}, exec: newSharedExecutor(cfg.MaxConcurrent),
		ctx: ctx, cancel: cancel, closeDone: make(chan struct{})}
	for _, spec := range specs {
		if spec.Name == "" {
			spec.Name = spec.Language + ":" + string(spec.Role)
		}
		if spec.Language == "" {
			spec.Language = "zh"
		}
		if !spec.Role.valid() {
			cancel()
			return nil, fmt.Errorf("invalid model role %q", spec.Role)
		}
		if _, ok := m.entries[spec.Name]; ok {
			cancel()
			return nil, fmt.Errorf("duplicate model name %q", spec.Name)
		}
		e := &modelEntry{spec: spec, state: stateIdle}
		if !cfg.Mode.allows(spec.Language, cfg.PrimaryLanguage) {
			e.disabled, e.state = true, stateDisabled
		}
		m.entries[spec.Name] = e
		m.order = append(m.order, spec.Name)
	}
	sort.Strings(m.order)

	m.mu.Lock()
	for _, name := range m.order {
		e := m.entries[name]
		if e.disabled {
			continue
		}
		if e.spec.Preload {
			prepared, err := prepareModel(e.spec)
			if err != nil {
				m.failLocked(e, err)
				continue
			}
			e.prepared, e.bytes = prepared, prepared.Bytes
		}
		if cfg.Mode.eager(e.spec.Language, cfg.PrimaryLanguage) {
			m.startLoadLocked(e)
		}
	}
	m.mu.Unlock()
	return m, nil
}

// LoadModel 是正式載入器：sequence 套件走 Supervisor，補標點走 PunctWorker。
func LoadModel(ctx context.Context, spec ModelSpec, prepared *Prepared) (*Loaded, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared == nil {
		p, err := prepareModel(spec)
		if err != nil {
			return nil, err
		}
		prepared = p
	}
	switch spec.Role {
	case RoleSelect, RoleRerank:
		if spec.Backend == BackendNative {
			return loadNative(spec, prepared)
		}
		w, err := openPreparedSupervisedAsync(Config{Python: spec.Python, Script: spec.Script, Package: spec.Package,
			DictionarySHA256: spec.DictionarySHA256, Threads: spec.Threads, Timeout: spec.Timeout}, prepared.Manifest)
		if err != nil {
			return nil, err
		}
		return &Loaded{Scorer: w, Options: prepared.Options, ResidentBytes: prepared.Bytes,
			Placeholder: prepared.Placeholder, Quality: prepared.Quality,
			Close: func() error { w.Close(); return nil }, State: w.State, Reason: w.Reason, Stats: w.Stats, Restarts: w.Restarts}, nil
	case RolePunct, RoleJAPunct:
		command := func() *exec.Cmd {
			return pythonCommand(spec.Python, spec.Script, "--package", spec.Package, "--threads", fmt.Sprint(spec.Threads))
		}
		w, err := openPunctPrepared(PunctConfig{Python: spec.Python, Script: spec.Script, Package: spec.Package,
			Threads: spec.Threads, Timeout: spec.Timeout}, prepared.PunctPath, command)
		if err != nil {
			return nil, err
		}
		return &Loaded{Punctuator: w, ResidentBytes: prepared.Bytes,
			Placeholder: prepared.Placeholder, Quality: prepared.Quality,
			Close: func() error { w.Close(); return nil }, State: w.State, Reason: w.Reason, Stats: w.Stats}, nil
	}
	return nil, &ModelError{Reason: ReasonFailed, Err: fmt.Errorf("unsupported role %q", spec.Role)}
}

// loadNative uses the same manifest and vocab.json as the Python worker; only
// the executor changes. Native is only implemented for sequence/reranker
// roles; punctuation stays on the Python path for now.
func loadNative(spec ModelSpec, prepared *Prepared) (*Loaded, error) {
	b, err := native.Open(native.Config{Package: spec.Package, DictionarySHA256: spec.DictionarySHA256,
		Threads: spec.Threads, Timeout: spec.Timeout, LibraryPath: spec.ORTLibrary})
	if err != nil {
		return nil, wrapNativeError(err)
	}
	return &Loaded{Scorer: b, Options: prepared.Options, ResidentBytes: prepared.Bytes,
		Placeholder: prepared.Placeholder, Quality: prepared.Quality,
		Close: b.Close, State: b.State, Reason: b.Reason,
		Stats: func() Stats {
			st := b.Stats()
			return Stats{Calls: st.Calls, Scored: st.Scored, Timeouts: st.Timeouts, Errors: st.Errors,
				Unavailable: st.Unavailable, TotalUS: st.TotalUS}
		},
		Restarts: func() int { return 0 }}, nil
}

func wrapNativeError(err error) error {
	reason := ReasonFailed
	switch {
	case errors.Is(err, os.ErrNotExist):
		reason = ReasonMissing
	case errors.Is(err, native.ErrPackageHash):
		reason = ReasonHashMismatch
	case errors.Is(err, native.ErrPackageContract):
		reason = ReasonVersionMismatch
	case errors.Is(err, native.ErrPackageCorrupt):
		reason = ReasonCorrupt
	case errors.Is(err, native.ErrRuntimeMissing):
		reason = ReasonRuntimeMissing
	}
	return &ModelError{Reason: reason, Err: err}
}

func prepareModel(spec ModelSpec) (*Prepared, error) {
	switch spec.Role {
	case RoleSelect, RoleRerank:
		c := Config{Python: spec.Python, Script: spec.Script, Package: spec.Package, DictionarySHA256: spec.DictionarySHA256,
			Threads: spec.Threads, Timeout: spec.Timeout}
		m, err := loadManifest(c)
		if err != nil {
			return nil, wrapPrepareError(err)
		}
		if spec.ValidateONNX {
			if err := validateONNXFile(filepath.Join(spec.Package, "model.onnx")); err != nil {
				return nil, wrapPrepareError(err)
			}
		}
		return &Prepared{Manifest: m, ModelSHA: m.Files["model.onnx"], Bytes: packageBytes(spec.Package, m.Files),
			Options: optionsFor(m), Placeholder: m.Placeholder, Quality: m.Quality}, nil
	case RolePunct, RoleJAPunct:
		c := PunctConfig{Python: spec.Python, Script: spec.Script, Package: spec.Package, Threads: spec.Threads, Timeout: spec.Timeout}
		model, bytes, placeholder, quality, err := loadPunctManifest(c)
		if err != nil {
			return nil, wrapPrepareError(err)
		}
		if spec.ValidateONNX {
			if err := validateONNXFile(filepath.Join(spec.Package, "model.onnx")); err != nil {
				return nil, wrapPrepareError(err)
			}
		}
		return &Prepared{PunctPath: model, Bytes: bytes, Placeholder: placeholder, Quality: quality}, nil
	}
	return nil, &ModelError{Reason: ReasonFailed, Err: fmt.Errorf("unsupported role %q", spec.Role)}
}

func wrapPrepareError(err error) error {
	var modelErr *ModelError
	if errors.As(err, &modelErr) {
		return err
	}
	return &ModelError{Reason: modelReason(err), Err: err}
}

// modelReason 把套件錯誤分類成固定代碼。分類只看 sentinel，不解析訊息內容。
func modelReason(err error) string {
	var modelErr *ModelError
	if errors.As(err, &modelErr) && modelErr.Reason != "" {
		return modelErr.Reason
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ReasonMissing
	case errors.Is(err, errPackageHashMismatch), errors.Is(err, errPunctPackageHash):
		return ReasonHashMismatch
	case errors.Is(err, errPackageContract), errors.Is(err, errPunctPackageContract):
		return ReasonVersionMismatch
	case errors.Is(err, errCorruptPackage), errors.Is(err, errONNXCorrupt):
		return ReasonCorrupt
	}
	var startup *StartupError
	if errors.As(err, &startup) {
		return startup.Reason
	}
	return ReasonFailed
}

func packageBytes(pkg string, files map[string]string) int64 {
	var total int64
	for name := range files {
		if info, err := os.Stat(filepath.Join(pkg, name)); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}

func (m *Manager) failLocked(e *modelEntry, err error) {
	e.state, e.err = stateFallback, err
	e.lastAttempt = m.cfg.Now()
	e.reason = modelReason(err)
	if m.cfg.Log != nil {
		m.cfg.Log("模型 %s 無法使用，保留統計轉換：%v", e.spec.Name, err)
	}
}

func (m *Manager) startLoadLocked(e *modelEntry) {
	if e.disabled || e.loading || e.loaded != nil || m.closing || m.closed {
		return
	}
	now := m.cfg.Now()
	if !e.lastAttempt.IsZero() && now.Sub(e.lastAttempt) < m.cfg.RetryAfter {
		return
	}
	e.loading, e.state, e.lastAttempt = true, stateLoading, now
	prepared := e.prepared
	m.loadWG.Add(1)
	go func() {
		defer m.loadWG.Done()
		loaded, err := m.cfg.Loader(m.ctx, e.spec, prepared)
		m.finishLoad(e, loaded, err, prepared)
	}()
}

func (m *Manager) finishLoad(e *modelEntry, loaded *Loaded, err error, prepared *Prepared) {
	m.mu.Lock()
	e.loading = false
	if err != nil {
		m.failLocked(e, err)
		m.mu.Unlock()
		return
	}
	if prepared != nil && e.prepared == nil {
		e.prepared = prepared
		if prepared.Bytes > 0 {
			e.bytes = prepared.Bytes
		}
	}
	if m.closing || m.closed {
		e.state = stateClosed
		m.mu.Unlock()
		if loaded != nil && loaded.Close != nil {
			_ = loaded.Close()
		}
		return
	}
	if loaded == nil {
		m.failLocked(e, &ModelError{Reason: ReasonFailed, Err: errors.New("loader returned no model")})
		m.mu.Unlock()
		return
	}
	e.loaded = loaded
	e.state = "ready"
	if e.bytes == 0 {
		e.bytes = loaded.ResidentBytes
	}
	if e.bytes > 0 {
		m.resident += e.bytes
		if m.resident > m.peak {
			m.peak = m.resident
		}
	}
	if loaded.Placeholder && m.cfg.Log != nil {
		m.cfg.Log("模型 %s 是 PLACEHOLDER（quality=%s）：不得當成已訓練模型或進入正式安裝包", e.spec.Name, loaded.Quality)
	}
	m.mu.Unlock()
}

// handleEntry 取得 entry；呼叫端需持有 m.mu。
func (m *Manager) handleEntry(name string) *modelEntry { return m.entries[name] }

// State 回報模型狀態。載入中的 worker 由後端自己回報 loading、ready、fallback、
// runtime_missing；模型尚未排入載入時是 idle，被模式排除時是 disabled。
func (m *Manager) State(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.closing {
		return stateClosed
	}
	e := m.handleEntry(name)
	if e == nil {
		return "unknown"
	}
	if e.loaded != nil && e.loaded.State != nil {
		if state := e.loaded.State(); state != "" {
			return state
		}
	}
	return e.state
}

// Reason 回傳最近一次載入或 worker 啟動失敗的固定原因代碼。
func (m *Manager) Reason(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.handleEntry(name)
	if e == nil {
		return ""
	}
	if e.loaded != nil && e.loaded.Reason != nil {
		if reason := e.loaded.Reason(); reason != "" {
			return reason
		}
	}
	return e.reason
}

// Stats 回傳單一模型的推論統計；沒有後端時為零值。
func (m *Manager) Stats(name string) Stats {
	m.mu.Lock()
	e := m.handleEntry(name)
	loaded := (*Loaded)(nil)
	if e != nil {
		loaded = e.loaded
	}
	m.mu.Unlock()
	if loaded != nil && loaded.Stats != nil {
		return loaded.Stats()
	}
	return Stats{}
}

// Restarts 回傳後端重啟次數；補標點與 sequence 後端都可能是 0。
func (m *Manager) Restarts(name string) int {
	m.mu.Lock()
	e := m.handleEntry(name)
	loaded := (*Loaded)(nil)
	if e != nil {
		loaded = e.loaded
	}
	m.mu.Unlock()
	if loaded != nil && loaded.Restarts != nil {
		return loaded.Restarts()
	}
	return 0
}

// Options 回傳 scorer 的 rerank 選項。Preload 成功的模型在 worker 就緒前就有值。
func (m *Manager) Options(name string) rerank.Options {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.handleEntry(name)
	if e == nil {
		return rerank.Options{}
	}
	if e.loaded != nil && e.loaded.Options.Weight > 0 {
		return e.loaded.Options
	}
	if e.prepared != nil {
		return e.prepared.Options
	}
	return rerank.Options{}
}

// StartupOK 回報 preload 是否成功；被模式排除的模型回傳 false。
func (m *Manager) StartupOK(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.handleEntry(name)
	return e != nil && !e.disabled && e.err == nil
}

func (m *Manager) Placeholder(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.handleEntry(name)
	if e == nil {
		return false
	}
	if e.loaded != nil {
		return e.loaded.Placeholder
	}
	return e.prepared != nil && e.prepared.Placeholder
}

// Residency 回傳所有模型與共用執行器的即時統計。
func (m *Manager) Residency() Residency {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := Residency{Mode: m.cfg.Mode, PrimaryLanguage: m.cfg.PrimaryLanguage,
		ResidentBytes: m.resident, PeakBytes: m.peak,
		InFlight: m.exec.inFlight.Load(), Queued: m.exec.queued.Load(), Concurrency: m.cfg.MaxConcurrent}
	for _, name := range m.order {
		e := m.entries[name]
		state := e.state
		if e.loaded != nil && e.loaded.State != nil {
			if s := e.loaded.State(); s != "" {
				state = s
			}
		}
		status := ModelStatus{Name: e.spec.Name, Language: e.spec.Language, Role: e.spec.Role,
			State: state, Reason: e.reason, ResidentBytes: e.bytes, Loaded: e.loaded != nil,
			Placeholder: e.prepared != nil && e.prepared.Placeholder}
		if e.loaded != nil {
			status.Placeholder = e.loaded.Placeholder
			status.Quality = e.loaded.Quality
		} else if e.prepared != nil {
			status.Quality = e.prepared.Quality
		}
		switch {
		case e.loaded != nil:
			r.Loaded++
		case e.loading:
			r.Loading++
		case e.disabled:
			r.Disabled++
		case e.state == stateFallback:
			r.Failed++
		}
		r.Models = append(r.Models, status)
	}
	return r
}

// run 是共用的執行路徑：模型未就緒時觸發背景載入並立刻回 Unavailable，
// 不等待載入；就緒後在共用執行器上執行，Close 會取消等待中的請求。
func (m *Manager) run(ctx context.Context, name string, fn func(context.Context, *Loaded) error) error {
	m.mu.Lock()
	e := m.entries[name]
	if e == nil {
		m.mu.Unlock()
		return ErrUnavailable
	}
	if m.closed || m.closing {
		m.mu.Unlock()
		return ErrClosed
	}
	if e.disabled {
		m.mu.Unlock()
		return ErrDisabled
	}
	if e.loaded == nil {
		m.startLoadLocked(e)
		e.lastUsed = m.cfg.Now()
		m.mu.Unlock()
		return ErrUnavailable
	}
	loaded := e.loaded
	e.lastUsed = m.cfg.Now()
	m.runWG.Add(1)
	m.mu.Unlock()
	defer m.runWG.Done()

	runCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := runCtx.Err(); err != nil {
		if m.ctx.Err() != nil {
			return ErrClosed
		}
		return err
	}
	return m.exec.do(runCtx, func() error { return fn(runCtx, loaded) })
}

// ScoreContext 透過共用執行器呼叫 sequence 模型。模型未就緒時回 ErrUnavailable，
// 呼叫端保留統計轉換結果。
func (m *Manager) ScoreContext(ctx context.Context, name string, r rerank.Request) ([]float64, error) {
	var scores []float64
	err := m.run(ctx, name, func(runCtx context.Context, loaded *Loaded) error {
		if loaded.Scorer == nil {
			return ErrUnavailable
		}
		if scorer, ok := loaded.Scorer.(interface {
			ScoreContext(context.Context, rerank.Request) ([]float64, error)
		}); ok {
			s, err := scorer.ScoreContext(runCtx, r)
			scores = s
			return err
		}
		s, err := loaded.Scorer.Score(r)
		scores = s
		return err
	})
	return scores, err
}

// PunctuateContext 透過共用執行器呼叫補標點模型。
func (m *Manager) PunctuateContext(ctx context.Context, name, text string) ([]punct.Edit, error) {
	var edits []punct.Edit
	err := m.run(ctx, name, func(runCtx context.Context, loaded *Loaded) error {
		if loaded.Punctuator == nil {
			return ErrUnavailable
		}
		if punctuator, ok := loaded.Punctuator.(interface {
			PunctuateContext(context.Context, string) ([]punct.Edit, error)
		}); ok {
			e, err := punctuator.PunctuateContext(runCtx, text)
			edits = e
			return err
		}
		e, err := loaded.Punctuator.Punctuate(text)
		edits = e
		return err
	})
	return edits, err
}

// Handle 是給外殼使用的穩定介面；Options 在 preload 後即可用，Score 在模型
// 就緒前回 ErrUnavailable。同一個 Handle 可以同時被多個 session 使用。
type Handle struct {
	mgr  *Manager
	name string
}

func (m *Manager) Handle(name string) *Handle {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[name]; !ok {
		return nil
	}
	return &Handle{mgr: m, name: name}
}

func (h *Handle) Name() string { return h.name }
func (h *Handle) Options() rerank.Options {
	if h == nil {
		return rerank.Options{}
	}
	options := h.mgr.Options(h.name)
	h.mgr.mu.Lock()
	e := h.mgr.entries[h.name]
	scorer := e != nil && (e.spec.Role == RoleSelect || e.spec.Role == RoleRerank)
	h.mgr.mu.Unlock()
	if scorer {
		options.Scorer = h
	}
	return options
}
func (h *Handle) State() string {
	if h == nil {
		return stateClosed
	}
	return h.mgr.State(h.name)
}
func (h *Handle) Reason() string {
	if h == nil {
		return ""
	}
	return h.mgr.Reason(h.name)
}
func (h *Handle) Stats() Stats {
	if h == nil {
		return Stats{}
	}
	return h.mgr.Stats(h.name)
}
func (h *Handle) Restarts() int {
	if h == nil {
		return 0
	}
	return h.mgr.Restarts(h.name)
}
func (h *Handle) Placeholder() bool {
	if h == nil {
		return false
	}
	return h.mgr.Placeholder(h.name)
}
func (h *Handle) Disabled() bool {
	if h == nil {
		return false
	}
	h.mgr.mu.Lock()
	defer h.mgr.mu.Unlock()
	e := h.mgr.entries[h.name]
	return e != nil && e.disabled
}
func (h *Handle) Score(r rerank.Request) ([]float64, error) {
	return h.ScoreContext(context.Background(), r)
}
func (h *Handle) ScoreContext(ctx context.Context, r rerank.Request) ([]float64, error) {
	if h == nil {
		return nil, ErrUnavailable
	}
	return h.mgr.ScoreContext(ctx, h.name, r)
}
func (h *Handle) Punctuate(text string) ([]punct.Edit, error) {
	return h.PunctuateContext(context.Background(), text)
}
func (h *Handle) PunctuateContext(ctx context.Context, text string) ([]punct.Edit, error) {
	if h == nil {
		return nil, ErrUnavailable
	}
	return h.mgr.PunctuateContext(ctx, h.name, text)
}

// Close 先取消載入與等待中的請求，等所有執行中的推論結束，再依名稱順序關閉後端。
// 重複或同時呼叫會等第一次完成；同一個後端只會關一次。
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	if m.closing {
		done := m.closeDone
		m.mu.Unlock()
		<-done
		return nil
	}
	m.closing = true
	m.cancel()
	m.mu.Unlock()

	m.loadWG.Wait()
	m.runWG.Wait()

	var errs []error
	for _, name := range m.order {
		m.mu.Lock()
		e := m.entries[name]
		loaded, bytes := e.loaded, e.bytes
		e.loaded, e.state = nil, stateClosed
		m.mu.Unlock()
		if loaded != nil {
			if loaded.Close != nil {
				if err := loaded.Close(); err != nil {
					errs = append(errs, err)
				}
			}
			m.mu.Lock()
			m.resident -= bytes
			if m.resident < 0 {
				m.resident = 0
			}
			m.mu.Unlock()
		}
	}
	m.mu.Lock()
	m.closed, m.closing = true, false
	close(m.closeDone)
	m.mu.Unlock()
	return errors.Join(errs...)
}

type sharedExecutor struct {
	slots    chan struct{}
	inFlight atomic.Int64
	queued   atomic.Int64
}

func newSharedExecutor(n int) *sharedExecutor {
	if n < 1 {
		n = 1
	}
	return &sharedExecutor{slots: make(chan struct{}, n)}
}

func (e *sharedExecutor) do(ctx context.Context, fn func() error) error {
	e.queued.Add(1)
	select {
	case e.slots <- struct{}{}:
		e.queued.Add(-1)
	case <-ctx.Done():
		e.queued.Add(-1)
		return ctx.Err()
	}
	e.inFlight.Add(1)
	defer e.inFlight.Add(-1)
	defer func() { <-e.slots }()
	return fn()
}
