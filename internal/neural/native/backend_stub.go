//go:build !cgo || !onnxruntime

package native

import (
	"context"

	"github.com/0c-biggo/0c-ime/core/rerank"
)

// Available reports whether this binary contains the native ONNX backend.
func Available() bool { return false }

// Backend is the build-tag stub used when CGO or the onnxruntime tag is off.
// The default product build therefore stays CGO-free and keeps using Python.
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
