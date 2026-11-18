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
	b, err := os.ReadFile(filepath.Join(c.Package, "manifest.json"))
	if err != nil {
		return "", 0, false, "", err
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
