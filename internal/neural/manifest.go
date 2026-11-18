// Package neural hosts the on-device ONNX model manager. Every executor is the
// native ONNX Runtime backend; there is no interpreter or worker process in the
// product runtime. Any load failure falls back to the statistical engine.
package neural

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// ErrUnavailable is returned when a model has not loaded yet or cannot be
// used. Callers keep the statistical result.
var ErrUnavailable = errors.New("neural model unavailable")

// Package validation sentinels. The residency manager maps these to stable
// reason codes so status can distinguish a missing file, a corrupt package and
// a dictionary mismatch without exposing file names or input text.
var (
	errCorruptPackage       = errors.New("neural package is corrupt")
	errPackageHashMismatch  = errors.New("neural package hash mismatch")
	errPackageContract      = errors.New("neural manifest contract mismatch")
	errPunctPackageHash     = errors.New("punct package hash mismatch")
	errPunctPackageContract = errors.New("punct manifest contract mismatch")
)

const (
	ReasonFailed = "failed"
	// ReasonRuntimeMissing: the native ONNX Runtime shared library is missing.
	ReasonRuntimeMissing = "runtime_missing"
	// P2-30 package validation categories.
	ReasonMissing         = "missing"
	ReasonCorrupt         = "corrupt"
	ReasonHashMismatch    = "hash_mismatch"
	ReasonVersionMismatch = "version_mismatch"
	// P2-43: the binary has no native backend (CGO/onnxruntime tag off) or the
	// model shape is unsupported. Both fall back to the statistical engine.
	ReasonNativeUnavailable = "native_unavailable"
	ReasonUnsupportedModel  = "unsupported_model"
)

// Config locates one model package and the execution budget.
type Config struct {
	Package, DictionarySHA256 string
	Threads                   int
	Timeout                   time.Duration
}

// Manifest is the package's manifest.json.
type Manifest struct {
	AllowPartial     bool              `json:"allow_partial"`
	Schema           string            `json:"schema"`
	ContextPolicy    string            `json:"context_policy"`
	DictionarySHA256 string            `json:"dictionary_sha256"`
	Weight           float64           `json:"weight"`
	Margin           float64           `json:"margin"`
	HintGap          float64           `json:"hint_gap"`
	TextWeight       float64           `json:"text_weight"`
	TextMargin       float64           `json:"text_margin"`
	NeutralExpand    float64           `json:"neutral_expand_penalty"`
	NeutralTolerant  []string          `json:"neutral_tolerant_last"`
	Files            map[string]string `json:"files"`
	// Placeholder/Quality come from P2-25P and P2-27 manifests. A placeholder
	// model must never be treated as trained or shipped in a release.
	Placeholder bool   `json:"placeholder,omitempty"`
	Quality     string `json:"quality,omitempty"`
	// TonePolicy appears in v2 packages only (zuin-tone-mask-v1).
	TonePolicy *rerank.TonePolicy `json:"tone_policy,omitempty"`
}

// Stats reports executor counters without input text.
type Stats struct {
	Calls       int   `json:"calls"`
	Scored      int   `json:"scored"`
	Timeouts    int   `json:"timeouts"`
	Errors      int   `json:"errors"`
	Unavailable int   `json:"unavailable"`
	TotalUS     int64 `json:"total_us"`
}

// neutralBases enables candidate expansion only when the package declares
// neutral_expand_penalty; the list holds first-tone (no tone mark) bases.
func neutralBases(m Manifest) map[zhuyin.Syllable]bool {
	if m.NeutralExpand <= 0 {
		return nil
	}
	out := map[zhuyin.Syllable]bool{}
	for _, y := range m.NeutralTolerant {
		out[zhuyin.Syllable(y).Base()] = true
	}
	return out
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Package contract: the encoder never reads context; sequence models only see
// the shell-confirmed committed text (at most 64 runes). v2 adds per-syllable
// unknown-tone handling (tone_policy).
var contracts = map[string]string{
	"zuin-encoder-runtime-v1": "empty",
	rerank.SequenceSchemaV1:   "confirmed_left_64",
	rerank.SequenceSchemaV2:   "confirmed_left_64",
}

func validContract(m Manifest) bool {
	return m.Schema != "" && contracts[m.Schema] == m.ContextPolicy && rerank.ValidTonePolicy(m.Schema, m.TonePolicy)
}

func contextRunes(m Manifest) int {
	if m.ContextPolicy == "confirmed_left_64" {
		return rerank.MaxContextRunes
	}
	return 0
}

// loadManifest verifies settings, package contract, dictionary pairing and
// model hashes without loading the model.
func loadManifest(c Config) (Manifest, error) {
	var m Manifest
	if c.Timeout <= 0 || c.Timeout > 100*time.Millisecond {
		return m, errors.New("neural timeout must be in (0,100ms]")
	}
	if c.Threads < 1 || c.Threads > MaxThreads {
		return m, errors.New("invalid neural threads")
	}
	b, err := os.ReadFile(filepath.Join(c.Package, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("%w: %v", errCorruptPackage, err)
	}
	if !validContract(m) || len(m.DictionarySHA256) != 64 || m.DictionarySHA256 != c.DictionarySHA256 || !(m.Weight > 0) || math.IsInf(m.Weight, 0) || !(m.Margin >= 0) || math.IsInf(m.Margin, 0) || !(m.HintGap >= 0) || math.IsInf(m.HintGap, 0) ||
		!(m.TextWeight >= 0) || math.IsInf(m.TextWeight, 0) || !(m.TextMargin >= 0) || math.IsInf(m.TextMargin, 0) ||
		!(m.NeutralExpand >= 0) || math.IsInf(m.NeutralExpand, 0) || m.NeutralExpand > 0 && len(m.NeutralTolerant) == 0 {
		return m, errPackageContract
	}
	for _, name := range []string{"model.onnx", "vocab.json"} {
		digest, e := fileSHA(filepath.Join(c.Package, name))
		if e != nil {
			return m, e
		}
		if digest != m.Files[name] {
			return m, errPackageHashMismatch
		}
	}
	return m, nil
}

// optionsFor turns a package manifest into core/rerank options. The caller
// fills in Scorer.
func optionsFor(m Manifest) rerank.Options {
	return rerank.Options{Weight: m.Weight, Margin: m.Margin, AllowPartial: m.AllowPartial,
		ContextRunes: contextRunes(m), HintGap: m.HintGap,
		TextWeight: m.TextWeight, TextMargin: m.TextMargin,
		NeutralExpand: m.NeutralExpand, NeutralBases: neutralBases(m)}
}

// OptionsFor exposes the manifest-to-rerank translation for development tools
// that open the native backend directly.
func OptionsFor(m Manifest) rerank.Options { return optionsFor(m) }
