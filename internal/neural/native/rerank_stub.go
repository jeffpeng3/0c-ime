//go:build !cgo || !onnxruntime

package native

import "context"

// RerankBackend is the build-tag stub for Japanese reranker packages.
type RerankBackend struct{}

func OpenRerank(RerankConfig) (*RerankBackend, error) { return nil, ErrNativeUnavailable }

func (b *RerankBackend) ScoreTexts(context.Context, string, []string) ([]float64, error) {
	return nil, ErrNativeUnavailable
}

func (b *RerankBackend) Stats() Stats { return Stats{} }

func (b *RerankBackend) State() string { return "fallback" }

func (b *RerankBackend) Reason() string { return "native_unavailable" }

func (b *RerankBackend) Close() error { return nil }
