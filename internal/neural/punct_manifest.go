package neural

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PunctSchema is the zh punctuation package contract. The native executor
// reads the same manifest and vocab.json that the model package ships.
const PunctSchema = "zuin-punct-runtime-v1"

// loadPunctManifest verifies timeout, threads, manifest contract and file
// hashes without loading the model.
func loadPunctManifest(c Config) (string, int64, bool, string, error) {
	if c.Timeout <= 0 || c.Timeout > 500*time.Millisecond {
		return "", 0, false, "", errors.New("punct timeout must be in (0,500ms]")
	}
	if c.Threads < 1 || c.Threads > MaxThreads {
		return "", 0, false, "", errors.New("invalid punct threads")
	}
	manifestPath := filepath.Join(c.Package, "manifest.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", 0, false, "", err
	}
	var head struct {
		Schema string `json:"schema"`
	}
	if err = json.Unmarshal(b, &head); err != nil {
		return "", 0, false, "", fmt.Errorf("%w: %v", errCorruptPackage, err)
	}
	// Japanese punctuation ships as 0c.model.manifest/1 with per-format file
	// paths and a SentencePiece tokenizer. The native scorer already accepts
	// it; use the same artifact validation here so the product manager can
	// prepare the package before the tokenizer is loaded.
	if head.Schema == "0c.model.manifest/1" {
		artifact, err := ValidateArtifact(manifestPath, "", "")
		if err != nil {
			return "", 0, false, "", err
		}
		if err := checkPunctuationArtifact(artifact.Manifest); err != nil {
			return "", 0, false, "", err
		}
		return artifact.SHA256, artifact.Bytes, artifact.Manifest.Placeholder, artifact.Manifest.Quality, nil
	}
	var m struct {
		Schema      string            `json:"schema"`
		Files       map[string]string `json:"files"`
		Placeholder bool              `json:"placeholder,omitempty"`
		Quality     string            `json:"quality,omitempty"`
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return "", 0, false, "", fmt.Errorf("%w: %v", errCorruptPackage, err)
	}
	if m.Schema != PunctSchema {
		return "", 0, false, "", errPunctPackageContract
	}
	var bytes int64
	for _, name := range []string{"model.onnx", "vocab.json"} {
		digest, e := fileSHA(filepath.Join(c.Package, name))
		if e != nil {
			return "", 0, false, "", e
		}
		if digest != m.Files[name] {
			return "", 0, false, "", errPunctPackageHash
		}
		if info, e := os.Stat(filepath.Join(c.Package, name)); e == nil {
			bytes += info.Size()
		}
	}
	return m.Files["model.onnx"], bytes, m.Placeholder, m.Quality, nil
}
