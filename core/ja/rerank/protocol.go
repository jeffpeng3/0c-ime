package rerank

import (
	"context"
	"errors"
	"time"

	"github.com/0c-biggo/0c-ime/core/reading"
)

// 驗證器可回報的固定錯誤類別。core 不能 import internal/neural，所以
// 非 core 的 T14 轉接層要把 neural.ErrTimeout 等錯誤包成這些 sentinel。
var (
	ErrTimeout     = errors.New("rerank verifier timeout")
	ErrUnavailable = errors.New("rerank verifier unavailable")
	ErrUnsupported = errors.New("rerank verifier unsupported request")
)

// Candidate 是一次 verify 的候選。Reading 必須與請求的讀音完全相同。
type Candidate struct {
	Surface string
	Reading reading.Sequence
	Clauses []Clause
}

// Clause 是候選的文節，只用來回報已固定的完整文節數。
type Clause struct {
	Reading reading.Sequence
	Surface string
}

// Verdict 是單一表記的驗證結果。
type Verdict struct {
	Surface  string
	LogProb  float64
	Score    float64
	Prefix   string
	Passed   bool
	PrefixOK bool
}

// Result 是驗證器的完整回覆。
type Result struct {
	Prefix            string
	FixedClauses      int
	FixedReadingUnits int
	Draft             *Verdict
	Verdicts          []Verdict
	Scores            []float64
	Passed            []bool
	PrefixOK          []bool
	Elapsed           time.Duration
}

// Request 是一次 verify。Draft 是統計轉換的最佳結果，Candidates 是要一起
// 評分的候選。
type Request struct {
	Revision    int
	LeftContext string
	Reading     reading.Sequence
	Draft       *Candidate
	Candidates  []Candidate
}

// Verifier 是 T14 runtime 的最小介面。實作可以是 neural.NeuralVerifier，
// 也可以是測試用的 fake。
type Verifier interface {
	Verify(context.Context, Request) (Result, error)
}

// VerifierFunc 讓函式可以直接當 Verifier。
type VerifierFunc func(context.Context, Request) (Result, error)

// Verify 呼叫 f。
func (f VerifierFunc) Verify(ctx context.Context, req Request) (Result, error) {
	return f(ctx, req)
}
