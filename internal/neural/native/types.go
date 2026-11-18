package native

import (
	"errors"
	"time"
)

// Config describes one native backend instance. It uses the same package
// directory, manifest and vocab.json as the model package.
type Config struct {
	Package          string
	DictionarySHA256 string
	Threads          int
	Timeout          time.Duration
	// LibraryPath is the ONNX Runtime shared library. Empty uses the binding's
	// default (onnxruntime.so / onnxruntime.dll next to the executable).
	LibraryPath string
	// Tokenizer is the SentencePiece model for a 0c.model.manifest/1
	// punctuation package. Empty searches the package and manifest datasets.
	Tokenizer string
}

// Stats mirrors neural.Stats so the residency manager can report either
// backend with the same health fields.
type Stats struct {
	Calls       int   `json:"calls"`
	Scored      int   `json:"scored"`
	Timeouts    int   `json:"timeouts"`
	Errors      int   `json:"errors"`
	Unavailable int   `json:"unavailable"`
	TotalUS     int64 `json:"total_us"`
}

// Sentinel errors let the host classify failures without parsing messages.
var (
	// ErrNativeUnavailable means this build has no native ONNX backend
	// (CGO disabled or the onnxruntime build tag not set).
	ErrNativeUnavailable = errors.New("native backend is not built")

	// ErrRuntimeMissing means the ONNX Runtime shared library could not be
	// loaded or initialized.
	ErrRuntimeMissing = errors.New("onnx runtime is missing")

	ErrPackageContract  = errors.New("native package contract mismatch")
	ErrPackageHash      = errors.New("native package hash mismatch")
	ErrPackageCorrupt   = errors.New("native package is corrupt")
	ErrUnsupportedModel = errors.New("native model layout is not supported")
)

// MaxThreads matches the package manifest's accepted range.
const MaxThreads = 8
