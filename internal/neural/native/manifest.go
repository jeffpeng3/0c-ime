package native

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/internal/neural/native/input"
)

// manifest is the subset of the sequence package manifest the native backend
// needs. It intentionally mirrors the Python worker's checks.
type manifest struct {
	Schema           string             `json:"schema"`
	ContextPolicy    string             `json:"context_policy"`
	DictionarySHA256 string             `json:"dictionary_sha256"`
	AllowPartial     bool               `json:"allow_partial"`
	Weight           float64            `json:"weight"`
	Margin           float64            `json:"margin"`
	TextWeight       float64            `json:"text_weight"`
	NeutralExpand    float64            `json:"neutral_expand_penalty"`
	NeutralTolerant  []string           `json:"neutral_tolerant_last"`
	Files            map[string]string  `json:"files"`
	TonePolicy       *rerank.TonePolicy `json:"tone_policy,omitempty"`
}

type vocabFile struct {
	DictionarySHA256 string   `json:"dictionary_sha256"`
	Bases            []string `json:"bases"`
	Characters       []string `json:"characters"`
}

func loadPackage(pkg, dictionary string) (manifest, input.Maps, error) {
	var m manifest
	raw, err := os.ReadFile(filepath.Join(pkg, "manifest.json"))
	if err != nil {
		return m, input.Maps{}, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, input.Maps{}, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	if (m.Schema != rerank.SequenceSchemaV1 && m.Schema != rerank.SequenceSchemaV2) ||
		m.ContextPolicy != "confirmed_left_64" || m.DictionarySHA256 != dictionary ||
		!(m.Weight > 0) || !(m.TextWeight >= 0) || !(m.NeutralExpand >= 0) ||
		(m.NeutralExpand > 0 && len(m.NeutralTolerant) == 0) ||
		!rerank.ValidTonePolicy(m.Schema, m.TonePolicy) {
		return m, input.Maps{}, ErrPackageContract
	}
	for _, name := range []string{"model.onnx", "vocab.json"} {
		digest, err := fileSHA(filepath.Join(pkg, name))
		if err != nil {
			return m, input.Maps{}, err
		}
		if digest != m.Files[name] {
			return m, input.Maps{}, ErrPackageHash
		}
	}
	vraw, err := os.ReadFile(filepath.Join(pkg, "vocab.json"))
	if err != nil {
		return m, input.Maps{}, err
	}
	var v vocabFile
	if err := json.Unmarshal(vraw, &v); err != nil {
		return m, input.Maps{}, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	if v.DictionarySHA256 != dictionary {
		return m, input.Maps{}, ErrPackageContract
	}
	maps := input.Maps{Bases: make(map[string]int64, len(v.Bases)), Characters: make(map[string]int64, len(v.Characters))}
	for i, base := range v.Bases {
		maps.Bases[base] = int64(i + 2)
	}
	for i, char := range v.Characters {
		maps.Characters[char] = int64(i + 8)
	}
	return m, maps, nil
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
