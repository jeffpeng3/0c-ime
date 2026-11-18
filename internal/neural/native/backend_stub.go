//go:build !cgo || !onnxruntime

package native

import (
	"context"

	"github.com/0c-biggo/0c-ime/core/punct"
	"github.com/0c-biggo/0c-ime/core/rerank"
)

// Available reports whether this binary contains the native ONNX backend.
func Available() bool { return false }

// Backend is the build-tag stub used when CGO or the onnxruntime tag is off.
// The Windows product does not use this build; it reports unavailable and the
// server keeps the statistical engine.
type Backend struct{}

func Open(Config) (*Backend, error) { return nil, ErrNativeUnavailable }

func (b *Backend) Score(rerank.Request) ([]float64, error) { return nil, ErrNativeUnavailable }

func (b *Backend) ScoreContext(context.Context, rerank.Request) ([]float64, error) {
	return nil, ErrNativeUnavailable
}

func (b *Backend) ScoreTokens([]int32, int, int) (float64, error) {
	return 0, ErrNativeUnavailable
}

func (b *Backend) Stats() Stats { return Stats{} }

func (b *Backend) State() string { return "fallback" }

func (b *Backend) Reason() string { return "native_unavailable" }

func (b *Backend) Close() error { return nil }

// PunctBackend is the build-tag stub for punctuation packages.
type PunctBackend struct{}

func OpenPunct(Config) (*PunctBackend, error) { return nil, ErrNativeUnavailable }

func (b *PunctBackend) Punctuate(string) ([]punct.Edit, error) {
	return nil, ErrNativeUnavailable
}

func (b *PunctBackend) PunctuateContext(context.Context, string) ([]punct.Edit, error) {
	return nil, ErrNativeUnavailable
}

func (b *PunctBackend) PunctuateText(text string) (string, error) {
	return text, ErrNativeUnavailable
}

func (b *PunctBackend) Stats() Stats { return Stats{} }

func (b *PunctBackend) State() string { return "fallback" }

func (b *PunctBackend) Reason() string { return "native_unavailable" }

func (b *PunctBackend) Close() error { return nil }
