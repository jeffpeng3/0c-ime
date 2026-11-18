package neural

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The Japanese punctuation package uses 0c.model.manifest/1, not the zh
// zuin-punct-runtime-v1 worker manifest. Preparing it through the product
// manager must verify the artifact and keep its trained, non-placeholder
// metadata.
func TestLoadPunctManifestAcceptsJapaneseArtifact(t *testing.T) {
	dir := t.TempDir()
	model := []byte("int8 punctuation model")
	modelPath := filepath.Join(dir, "punctuation-public-core", "model.int8.onnx")
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, model, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(model)
	manifest := map[string]any{
		"schema": "0c.model.manifest/1", "kind": "punctuation", "artifact_id": "punctuation-public-core",
		"placeholder": false, "quality": "trained", "production_eligible": true,
		"files": map[string]any{"int8": map[string]any{
			"path": "punctuation-public-core/model.int8.onnx", "bytes": len(model),
			"sha256": fmt.Sprintf("%x", sum),
		}},
		"io": map[string]any{
			"inputs": []any{
				map[string]any{"name": "input_ids", "dtype": "int64", "shape": []any{"batch", "seq"}},
				map[string]any{"name": "attention_mask", "dtype": "int64", "shape": []any{"batch", "seq"}},
			},
			"outputs": []any{
				map[string]any{"name": "logits", "dtype": "float32", "shape": []any{"batch", "seq", 4}},
			},
		},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	sha, bytes, placeholder, quality, err := loadPunctManifest(Config{Package: dir, Threads: 1, Timeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if sha != fmt.Sprintf("%x", sum) || bytes != int64(len(model)) {
		t.Fatalf("model = %s bytes=%d", sha, bytes)
	}
	if placeholder || quality != "trained" {
		t.Fatalf("metadata placeholder=%v quality=%q", placeholder, quality)
	}
}

// A hash mismatch must fail before any ONNX Runtime load.
func TestLoadPunctManifestRejectsBadJapaneseArtifactHash(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "punctuation-public-core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "punctuation-public-core", "model.int8.onnx"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema": "0c.model.manifest/1", "kind": "punctuation",
		"files": map[string]any{"int8": map[string]any{
			"path": "punctuation-public-core/model.int8.onnx", "sha256": fmt.Sprintf("%064d", 0),
		}},
	}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := loadPunctManifest(Config{Package: dir, Threads: 1, Timeout: 150 * time.Millisecond}); err == nil {
		t.Fatal("bad artifact hash was accepted")
	}
}
