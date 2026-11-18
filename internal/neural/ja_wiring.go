package neural

import (
	"context"
	"errors"

	"github.com/0c-biggo/0c-ime/core/ja/rerank"
)

// JAVerifier adapts the residency manager's native Japanese reranker to the
// draft/verify interface used by core/ja/rerank. It never blocks the manager:
// the first request for a lazily loaded model returns ErrUnavailable and the
// statistical conversion stays in place. Cancellation and timeouts map to the
// verifier's fixed timeout sentinel so the caller keeps the statistical order.
type JAVerifier struct {
	Handle *Handle
}

var _ rerank.Verifier = (*JAVerifier)(nil)

func NewJAVerifier(handle *Handle) *JAVerifier { return &JAVerifier{Handle: handle} }

func (v *JAVerifier) Verify(ctx context.Context, req rerank.Request) (rerank.Result, error) {
	if v == nil || v.Handle == nil {
		return rerank.Result{}, rerank.ErrUnavailable
	}
	candidates := make([]string, len(req.Candidates))
	for i, c := range req.Candidates {
		candidates[i] = c.Surface
	}
	scores, err := v.Handle.ScoreTexts(ctx, req.LeftContext, candidates)
	if err != nil {
		return rerank.Result{}, mapJAError(err)
	}
	if len(scores) != len(req.Candidates) {
		return rerank.Result{}, rerank.ErrUnsupported
	}
	result := rerank.Result{Prefix: req.LeftContext}
	for i, c := range req.Candidates {
		result.Verdicts = append(result.Verdicts, rerank.Verdict{
			Surface: c.Surface, LogProb: scores[i], Score: scores[i],
			Prefix: c.Surface, Passed: true, PrefixOK: true,
		})
		result.Scores = append(result.Scores, scores[i])
		result.Passed = append(result.Passed, true)
		result.PrefixOK = append(result.PrefixOK, true)
	}
	return result, nil
}

func mapJAError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return rerank.ErrTimeout
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrDisabled), errors.Is(err, ErrClosed):
		return rerank.ErrUnavailable
	}
	return err
}
