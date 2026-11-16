package neural

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os/exec"
	"sync"
	"time"
)

// ErrJaUnsupported 表示請求不符合 T14 協定（讀音不對齊、候選過多等）。
// 呼叫端應退回統計轉換；worker 本身沒有故障。
var ErrJaUnsupported = errors.New("unsupported japanese neural request")

// JaStats 是驗證器的累計統計，不含文字。T30 與狀態摘要可據此決定是否
// 暫時停用神經層。
type JaStats struct {
	Calls       int   `json:"calls"`
	Verified    int   `json:"verified"`
	Timeouts    int   `json:"timeouts"`
	Errors      int   `json:"errors"`
	Unavailable int   `json:"unavailable"`
	TotalUS     int64 `json:"total_us"`
}

// JaRuntime 是日文驗證 worker 的宿主。OpenJa 只驗證模型配對並背景啟動，
// 不在引擎啟動路徑等待模型載入；模型就緒前的 Verify 立刻回 ErrUnavailable，
// 呼叫端照統計結果繼續，輸入不會被阻塞。
type JaRuntime struct {
	cfg      JaConfig
	manifest JaManifest
	newCmd   func() *exec.Cmd

	mu       sync.Mutex
	cmd      *exec.Cmd
	input    io.WriteCloser
	output   *bufio.Scanner
	procDone chan struct{}
	readyCh  chan struct{}
	ready    bool
	starting bool
	dead     bool
	closed   bool
	startErr error
	retryAt  time.Time
	next     uint64
	stats    JaStats
}

// OpenJa 驗證 models/ja/manifest.json 與模型雜湊後建立 runtime；worker 與
// llama-server 在背景啟動。模型缺失時回傳 *JaModelError，Reason 可直接顯示。
func OpenJa(c JaConfig) (*JaRuntime, error) {
	c = jaDefaults(c)
	if c.Python == "" || c.Script == "" || c.Package == "" || c.Server == "" {
		return nil, errors.New("japanese neural runtime configuration is incomplete")
	}
	m, err := loadJaManifest(c)
	if err != nil {
		return nil, err
	}
	args := []string{
		"--package", c.Package,
		"--llama-server", c.Server,
		"--threads", fmt.Sprint(c.Threads),
		"--timeout-ms", fmt.Sprint(c.Timeout.Milliseconds()),
		"--model-sha256", m.Model.SHA256,
		"--backend-commit", m.BackendCommit,
	}
	newCmd := func() *exec.Cmd { return pythonCommand(c.Python, c.Script, args...) }
	r := newJaRuntime(c, m, newCmd)
	r.launch()
	return r, nil
}

// newJaRuntime 由 OpenJa 與測試共用；呼叫端負責呼叫 launch。
func newJaRuntime(c JaConfig, m JaManifest, newCmd func() *exec.Cmd) *JaRuntime {
	return &JaRuntime{cfg: jaDefaults(c), manifest: m, newCmd: newCmd}
}

// Manifest 回傳已核對的套件設定，T30 用它取得候選與前文上限。
func (r *JaRuntime) Manifest() JaManifest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.manifest
}

func (r *JaRuntime) Stats() JaStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

// Ready 回報 worker 是否已完成握手。模型載入中為 false，不是錯誤。
func (r *JaRuntime) Ready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready && !r.dead && !r.closed
}

// StartupError 回傳最近一次啟動失敗的原因；就緒或尚未失敗時為 nil。
func (r *JaRuntime) StartupError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startErr
}

// WaitReady 等待第一次握手完成。模型檔很大或磁碟慢時可以依 context 取消；
// 啟動失敗會回傳 *JaStartupError 或 *JaModelError。
func (r *JaRuntime) WaitReady(ctx context.Context) error {
	for {
		r.mu.Lock()
		if r.ready && !r.dead {
			r.mu.Unlock()
			return nil
		}
		if r.closed {
			r.mu.Unlock()
			return ErrUnavailable
		}
		if r.dead {
			err := r.startErr
			r.mu.Unlock()
			if err != nil {
				return err
			}
			return ErrUnavailable
		}
		if !r.starting && r.startErr != nil {
			err := r.startErr
			r.mu.Unlock()
			return err
		}
		ch := r.readyCh
		r.mu.Unlock()
		if ch == nil {
			return ErrUnavailable
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

func jaRetryCooldown(reason string, normal time.Duration) time.Duration {
	switch reason {
	case JaReasonRuntimeMissing, JaReasonModelMissing, JaReasonHashMismatch,
		JaReasonManifestMissing, JaReasonManifestInvalid, JaReasonBackendMismatch, JaReasonContract:
		// 缺執行環境或模型配對錯誤時不要每幾秒重試；使用者補檔後下一次
		// 呼叫仍可重新 OpenJa。
		return 10 * time.Minute
	}
	return normal
}

// maybeRestart 在 worker 死掉且冷卻期過後重新啟動。Verify 不會等待它。
func (r *JaRuntime) maybeRestart() {
	r.mu.Lock()
	if r.closed || r.ready || r.starting || time.Now().Before(r.retryAt) {
		r.mu.Unlock()
		return
	}
	r.dead = false
	r.startErr = nil
	r.mu.Unlock()
	r.launch()
}

// launch 啟動 worker 並在背景完成握手。所有等待都有上限；失敗只影響神經層。
func (r *JaRuntime) launch() {
	r.mu.Lock()
	if r.closed || r.starting || r.ready {
		r.mu.Unlock()
		return
	}
	r.starting = true
	ch := make(chan struct{})
	r.readyCh = ch
	cmd := r.newCmd()
	input, err := cmd.StdinPipe()
	if err != nil {
		r.failStartLocked(ch, &JaStartupError{Reason: JaReasonStartFailed}, err)
		return
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		r.failStartLocked(ch, &JaStartupError{Reason: JaReasonStartFailed}, err)
		return
	}
	cmd.Stderr = io.Discard
	configureJaProcess(cmd)
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		reason := JaReasonStartFailed
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			reason = JaReasonRuntimeMissing
		}
		r.failStartLocked(ch, &JaStartupError{Reason: reason}, err)
		return
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), frameLimit)
	done := make(chan struct{})
	r.cmd, r.input, r.output, r.procDone = cmd, input, scanner, done
	r.mu.Unlock()

	go func() {
		_ = cmd.Wait()
		close(done)
		r.mu.Lock()
		if r.cmd == cmd {
			r.ready = false
			r.starting = false
			if !r.closed {
				r.dead = true
			}
		}
		r.mu.Unlock()
	}()

	_, err = r.handshake(scanner, r.cfg.StartTimeout)
	if err != nil {
		r.stop()
		r.mu.Lock()
		if r.cmd == cmd {
			r.starting = false
			r.ready = false
			r.dead = true
			r.startErr = err
			reason := describeJaModelError(err)
			r.retryAt = time.Now().Add(jaRetryCooldown(reason, r.cfg.RetryCooldown))
		}
		close(ch)
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	if r.closed || r.cmd != cmd {
		r.starting = false
		r.mu.Unlock()
		r.stop()
		close(ch)
		return
	}
	r.starting = false
	r.ready = true
	r.dead = false
	r.startErr = nil
	close(ch)
	r.mu.Unlock()
}

func (r *JaRuntime) failStartLocked(ch chan struct{}, startup *JaStartupError, cause error) {
	r.starting = false
	r.ready = false
	r.dead = true
	r.startErr = startup.wrap(cause)
	r.retryAt = time.Now().Add(jaRetryCooldown(startup.Reason, r.cfg.RetryCooldown))
	close(ch)
	r.mu.Unlock()
}

type jaReady struct {
	Ready         bool   `json:"ready"`
	Error         string `json:"error"`
	Detail        string `json:"detail"`
	Schema        string `json:"schema"`
	Backend       string `json:"backend"`
	BackendCommit string `json:"backend_commit"`
	ContextPolicy string `json:"context_policy"`
	Model         string `json:"model_sha256"`
	Limits        struct {
		MaxReadingUnits     int `json:"max_reading_units"`
		MaxCandidates       int `json:"max_candidates"`
		MaxLeftContextUnits int `json:"max_left_context_units"`
	} `json:"limits"`
}

// handshake 讀第一行並核對套件身分。worker 只在模型與 llama-server 都
// 就緒後才送出 ready；失敗時 error 是固定代碼。
func (r *JaRuntime) handshake(scanner *bufio.Scanner, timeout time.Duration) (jaReady, error) {
	b, err := scanWithTimeout(scanner, timeout)
	var ready jaReady
	if err == nil {
		err = json.Unmarshal(b, &ready)
	}
	if err != nil {
		reason := JaReasonStartFailed
		if errors.Is(err, ErrTimeout) {
			reason = JaReasonStartTimeout
		}
		return ready, &JaStartupError{Reason: reason}
	}
	if !ready.Ready {
		reason := ready.Error
		switch reason {
		case JaReasonRuntimeMissing, JaReasonModelMissing, JaReasonHashMismatch,
			JaReasonManifestMissing, JaReasonManifestInvalid, JaReasonBackendMismatch,
			JaReasonContract, JaReasonServerFailed, JaReasonServerTimeout:
		default:
			reason = JaReasonStartFailed
		}
		return ready, &JaStartupError{Reason: reason, Detail: ready.Detail}
	}
	if ready.Schema != JaSchema || ready.Backend != JaBackend || ready.ContextPolicy != JaContextPolicy ||
		ready.Model != r.manifest.Model.SHA256 || ready.BackendCommit != r.manifest.BackendCommit ||
		ready.Limits.MaxReadingUnits != r.manifest.Limits.MaxReadingUnits ||
		ready.Limits.MaxCandidates != r.manifest.Limits.MaxCandidates ||
		ready.Limits.MaxLeftContextUnits != r.manifest.Limits.MaxLeftContextUnits {
		return ready, &JaStartupError{Reason: JaReasonStartFailed}
	}
	return ready, nil
}

func scanWithTimeout(scanner *bufio.Scanner, timeout time.Duration) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		if !scanner.Scan() {
			err := scanner.Err()
			if err == nil {
				err = io.EOF
			}
			done <- result{err: err}
			return
		}
		done <- result{b: append([]byte(nil), scanner.Bytes()...)}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.b, r.err
	case <-timer.C:
		return nil, ErrTimeout
	}
}

// Verify 對 draft 與候選做一次限制式驗證。Timeout 以 JaConfig 為上限；
// worker 內部會先回逾時，宿主硬逾時則殺掉 worker，下一次呼叫立刻回
// ErrUnavailable，不排隊、不阻塞輸入。
func (r *JaRuntime) Verify(req JaRequest) (JaResult, error) {
	return r.VerifyContext(context.Background(), req)
}

// VerifyContext 與 Verify 相同，另外接受外殼的取消（例如使用者繼續打字）。
// 取消會結束 worker，避免晚到的回覆與下一個請求錯位。
func (r *JaRuntime) VerifyContext(ctx context.Context, req JaRequest) (JaResult, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return JaResult{}, ErrUnavailable
	}
	if !r.ready || r.dead {
		startErr := r.startErr
		r.stats.Calls++
		r.stats.Unavailable++
		r.mu.Unlock()
		r.maybeRestart()
		if startErr != nil {
			return JaResult{}, fmt.Errorf("%w: %w", ErrUnavailable, startErr)
		}
		return JaResult{}, ErrUnavailable
	}
	if err := validateJaRequest(req, r.manifest); err != nil {
		r.stats.Calls++
		r.stats.Errors++
		r.mu.Unlock()
		return JaResult{}, err
	}
	r.stats.Calls++
	r.next++
	id := r.next
	started := time.Now()
	defer func() {
		r.stats.TotalUS += time.Since(started).Microseconds()
	}()
	if err := ctx.Err(); err != nil {
		r.stats.Errors++
		r.mu.Unlock()
		return JaResult{}, err
	}
	request := jaWireRequest{
		ID: id, Revision: req.Revision, Schema: JaSchema, Model: r.manifest.Model.SHA256,
		LeftContext: req.LeftContext, Reading: req.Reading, Draft: req.Draft, Candidates: req.Candidates,
	}
	b, err := json.Marshal(request)
	if err != nil {
		r.stats.Errors++
		r.mu.Unlock()
		return JaResult{}, err
	}
	if len(b)+1 > frameLimit {
		r.stats.Errors++
		r.mu.Unlock()
		return JaResult{}, errors.New("japanese neural request frame too large")
	}
	b, err = r.exchangeLocked(ctx, append(b, '\n'), r.cfg.Timeout)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			r.stats.Timeouts++
		} else {
			r.stats.Errors++
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				r.stopLocked()
			}
		}
		r.mu.Unlock()
		return JaResult{}, err
	}
	var reply jaWireReply
	if err = json.Unmarshal(b, &reply); err != nil || reply.ID != id || reply.Revision != req.Revision ||
		reply.Schema != JaSchema || reply.Model != r.manifest.Model.SHA256 {
		r.stats.Errors++
		r.stopLocked()
		r.mu.Unlock()
		return JaResult{}, errors.New("japanese neural reply identity mismatch")
	}
	if reply.Fallback != "" {
		if reply.Fallback == "timeout" {
			r.stats.Timeouts++
			// llama-server 可能還在跑逾時的請求；整個 worker 結束，下一次
			// 從乾淨的 server 重啟，避免晚到的結果污染 slot。
			r.stopLocked()
			r.mu.Unlock()
			return JaResult{}, ErrTimeout
		}
		r.stats.Errors++
		r.mu.Unlock()
		return JaResult{}, fmt.Errorf("%w: %s", ErrJaUnsupported, reply.Detail)
	}
	result, err := jaResult(reply, req)
	if err != nil {
		r.stats.Errors++
		r.stopLocked()
		r.mu.Unlock()
		return JaResult{}, err
	}
	valid := true
	for _, v := range result.Verdicts {
		if math.IsNaN(v.Score) || math.IsInf(v.Score, 0) || math.IsNaN(v.LogProb) || math.IsInf(v.LogProb, 0) {
			valid = false
			break
		}
	}
	if !valid {
		r.stats.Errors++
		r.stopLocked()
		r.mu.Unlock()
		return JaResult{}, errors.New("invalid japanese neural score")
	}
	result.Elapsed = time.Since(started)
	r.stats.Verified++
	r.mu.Unlock()
	return result, nil
}

// exchangeLocked 只允許一個 I/O goroutine；呼叫端必須持有 mu。
func (r *JaRuntime) exchangeLocked(ctx context.Context, request []byte, timeout time.Duration) ([]byte, error) {
	if r.input == nil || r.output == nil {
		return nil, ErrUnavailable
	}
	input, output := r.input, r.output
	type result struct {
		b   []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		if _, err := input.Write(request); err != nil {
			done <- result{err: err}
			return
		}
		if !output.Scan() {
			err := output.Err()
			if err == nil {
				err = io.EOF
			}
			done <- result{err: err}
			return
		}
		done <- result{b: append([]byte(nil), output.Bytes()...)}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-done:
		return res.b, res.err
	case <-ctx.Done():
		r.stopLocked()
		return nil, ctx.Err()
	case <-timer.C:
		// 硬逾時：worker 可能卡在 llama-server，直接結束，下一次重啟。
		r.stopLocked()
		return nil, ErrTimeout
	}
}

// stop 殺掉 worker；Unix 上 worker 自成 process group，llama-server 一起結束。
func (r *JaRuntime) stop() {
	r.mu.Lock()
	r.stopLocked()
	r.mu.Unlock()
}

func (r *JaRuntime) stopLocked() {
	if r.cmd != nil && !r.dead {
		r.dead = true
		r.ready = false
		if r.input != nil {
			r.input.Close()
		}
		if r.cmd.Process != nil {
			terminateJaProcess(r.cmd.Process)
		}
	}
}

// Close 停止 worker 並等待行程結束（最多兩秒；模型載入中的 worker 會一併結束）。
func (r *JaRuntime) Close() {
	r.mu.Lock()
	r.closed = true
	r.stopLocked()
	done := r.procDone
	r.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}
