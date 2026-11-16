package neural

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 這個檔是 T14 runtime 的 production 宿主接線。core 不能 import
// internal/neural，而 core/ja/rerank 的測試會 import 這個套件，所以這裡
// 不能 import core/ja/rerank（否則測試會形成 import cycle）。T14 → T30 的
// 型別轉換放在 cmd/0c-server；這個檔只負責共享 worker、延遲啟動、逾時與
// 乾淨關閉。

// JaNeural 的狀態；字串會出現在 health，不含任何使用者文字。
const (
	JaNeuralDisabled  = "disabled"
	JaNeuralStarting  = "starting"
	JaNeuralReady     = "ready"
	JaNeuralFallback  = "fallback"
	JaNeuralClosed    = "closed"
	defaultJaRetryGap = 10 * time.Minute
)

// JaNeuralConfig 是共享 worker 的設定。Open 通常包住 OpenJa；測試可以換成
// 只回錯誤的函式，驗證模型缺失時的退回。Disabled 為 true 或 Open 為 nil 時
// 整個神經層停用。
type JaNeuralConfig struct {
	Disabled      bool
	Open          func() (*JaRuntime, error)
	RetryCooldown time.Duration
}

// JaNeural 是一個行程內共用的日文驗證 worker 宿主。多個日文 session 透過
// 同一個 Reranker/Verifier 共用它；第一次轉換才啟動，啟動失敗或逾時都只
// 讓那一次退回統計轉換，不阻塞輸入。
type JaNeural struct {
	cfg JaNeuralConfig

	mu       sync.Mutex
	rt       *JaRuntime
	opening  bool
	closed   bool
	lastErr  error
	reason   string
	retryAt  time.Time
	openOnce sync.WaitGroup
	opens    int
}

// NewJaNeural 建立共享宿主；worker 不會在這裡啟動。
func NewJaNeural(c JaNeuralConfig) *JaNeural {
	if c.RetryCooldown <= 0 {
		c.RetryCooldown = defaultJaRetryGap
	}
	return &JaNeural{cfg: c}
}

// Enabled 回報設定是否允許神經層。實際可用性看 State。
func (n *JaNeural) Enabled() bool {
	return n != nil && !n.cfg.Disabled && n.cfg.Open != nil
}

// VerifyContext 把請求交給共享 worker。模型尚未就緒時立刻回
// ErrUnavailable，呼叫端保留統計轉換結果。
func (n *JaNeural) VerifyContext(ctx context.Context, req JaRequest) (JaResult, error) {
	if n == nil || !n.Enabled() {
		return JaResult{}, ErrUnavailable
	}
	rt, err := n.runtime()
	if rt == nil {
		if err == nil {
			err = ErrUnavailable
		}
		return JaResult{}, err
	}
	return rt.VerifyContext(ctx, req)
}

// Verify 是沒有 context 的便利函式。
func (n *JaNeural) Verify(req JaRequest) (JaResult, error) {
	return n.VerifyContext(context.Background(), req)
}

// runtime 取得或觸發背景啟動。永遠不等待模型載入；尚未就緒時回
// ErrUnavailable，呼叫端照統計結果繼續。
func (n *JaNeural) runtime() (*JaRuntime, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil, ErrUnavailable
	}
	if n.rt != nil {
		return n.rt, nil
	}
	if n.opening {
		return nil, ErrUnavailable
	}
	if n.lastErr != nil && time.Now().Before(n.retryAt) {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, n.reason)
	}
	n.opening = true
	n.opens++
	n.openOnce.Add(1)
	go n.open()
	return nil, ErrUnavailable
}

func (n *JaNeural) open() {
	defer n.openOnce.Done()
	rt, err := n.cfg.Open()
	if rt == nil && err == nil {
		err = errors.New("japanese neural opener returned no runtime")
	}

	n.mu.Lock()
	n.opening = false
	if n.closed {
		n.mu.Unlock()
		if rt != nil {
			rt.Close()
		}
		return
	}
	if err != nil {
		n.lastErr = err
		n.reason = describeJaModelError(err)
		n.retryAt = time.Now().Add(n.cfg.RetryCooldown)
		n.mu.Unlock()
		return
	}
	n.rt = rt
	n.lastErr = nil
	n.reason = ""
	n.retryAt = time.Time{}
	n.mu.Unlock()
}

// State 回報目前狀態。模型載入中是 starting，不是錯誤；啟動失敗後是
// fallback，Reason 有固定代碼。
func (n *JaNeural) State() string {
	if n == nil || !n.Enabled() {
		return JaNeuralDisabled
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return JaNeuralClosed
	}
	if n.rt != nil {
		if n.rt.Ready() {
			return JaNeuralReady
		}
		if n.rt.StartupError() != nil {
			return JaNeuralFallback
		}
		return JaNeuralStarting
	}
	if n.opening {
		return JaNeuralStarting
	}
	if n.lastErr != nil {
		return JaNeuralFallback
	}
	return JaNeuralStarting
}

// Reason 回傳最近一次啟動失敗的固定代碼；沒有失敗時為空字串。
func (n *JaNeural) Reason() string {
	if n == nil {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.rt != nil {
		if err := n.rt.StartupError(); err != nil {
			return describeJaModelError(err)
		}
	}
	return n.reason
}

// Stats 回傳底層 worker 的累計統計；尚未啟動時為零值。
func (n *JaNeural) Stats() JaStats {
	if n == nil {
		return JaStats{}
	}
	n.mu.Lock()
	rt := n.rt
	n.mu.Unlock()
	if rt == nil {
		return JaStats{}
	}
	return rt.Stats()
}

// WaitReady 等待第一次握手完成。主要給啟動檢查與測試使用；正常轉換路徑
// 不呼叫它，模型載入期間照統計結果顯示。
func (n *JaNeural) WaitReady(ctx context.Context) error {
	if n == nil || !n.Enabled() {
		return ErrUnavailable
	}
	for {
		rt, err := n.runtime()
		if rt != nil {
			return rt.WaitReady(ctx)
		}
		if err == nil {
			err = ErrUnavailable
		}
		if !errors.Is(err, ErrUnavailable) {
			return err
		}
		n.mu.Lock()
		last := n.lastErr
		closed := n.closed
		n.mu.Unlock()
		if closed {
			return ErrUnavailable
		}
		if last != nil {
			return last
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Close 停止共享 worker 並等待背景啟動結束。可重複呼叫。
func (n *JaNeural) Close() {
	if n == nil {
		return
	}
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}
	n.closed = true
	n.mu.Unlock()

	n.openOnce.Wait()

	n.mu.Lock()
	rt := n.rt
	n.rt = nil
	n.mu.Unlock()
	if rt != nil {
		rt.Close()
	}
}
