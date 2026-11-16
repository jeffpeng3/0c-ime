package neural

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func jaWiringRequest() JaRequest {
	return JaRequest{
		Revision: 7, LeftContext: "私の",
		Reading: []string{"き", "ょ", "う", "は"},
		Draft:   &JaCandidate{Surface: "今日は", Reading: []string{"き", "ょ", "う", "は"}},
		Candidates: []JaCandidate{
			{Surface: "今日は", Reading: []string{"き", "ょ", "う", "は"}},
			{Surface: "京は", Reading: []string{"き", "ょ", "う", "は"}},
		},
	}
}

func TestJaNeuralLazyStartSharedWorkerAndClose(t *testing.T) {
	var opens int
	var mu sync.Mutex
	host := NewJaNeural(JaNeuralConfig{Open: func() (*JaRuntime, error) {
		mu.Lock()
		opens++
		mu.Unlock()
		return childJa(t, "success", 100*time.Millisecond), nil
	}})
	defer host.Close()

	if host.State() != JaNeuralStarting {
		t.Fatalf("initial state = %q", host.State())
	}
	// 第一個請求只觸發背景啟動，模型還沒就緒，不能阻塞。
	start := time.Now()
	if _, err := host.Verify(jaWiringRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("first verify err = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("first verify blocked for %v", time.Since(start))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := host.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	if host.State() != JaNeuralReady {
		t.Fatalf("ready state = %q", host.State())
	}

	// 兩個並行請求共用同一個 worker。
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := host.Verify(jaWiringRequest())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("shared verify: %v", err)
		}
	}
	mu.Lock()
	got := opens
	mu.Unlock()
	if got != 1 {
		t.Fatalf("opened %d runtimes, want 1", got)
	}
	host.Close()
	if host.State() != JaNeuralClosed {
		t.Fatalf("closed state = %q", host.State())
	}
}

func TestJaNeuralOpenFailureFallsBack(t *testing.T) {
	want := jaModelError(JaReasonModelMissing, "/models/ja/model.gguf", nil)
	host := NewJaNeural(JaNeuralConfig{Open: func() (*JaRuntime, error) { return nil, want }})
	defer host.Close()
	if _, err := host.Verify(jaWiringRequest()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("verify err = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.WaitReady(ctx); !errors.Is(err, want) {
		t.Fatalf("WaitReady err = %v", err)
	}
	if host.State() != JaNeuralFallback || host.Reason() != JaReasonModelMissing {
		t.Fatalf("state = %q reason = %q", host.State(), host.Reason())
	}
	if stats := host.Stats(); stats.Calls != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestJaNeuralTimeoutFallsBack(t *testing.T) {
	host := NewJaNeural(JaNeuralConfig{Open: func() (*JaRuntime, error) {
		return childJa(t, "timeout", 40*time.Millisecond), nil
	}})
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := host.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := host.Verify(jaWiringRequest())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("verify err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
	if stats := host.Stats(); stats.Timeouts == 0 {
		t.Fatalf("stats = %+v", stats)
	}
}
