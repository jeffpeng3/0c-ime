package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/internal/modelsets"
)

func testDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// writeModelSetFixture 建一個可驗證的模型組安裝目錄與 lock。
func writeModelSetFixture(t *testing.T) (lockPath, root, dataDir, shot string) {
	t.Helper()
	root, dataDir = t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "ja"), 0o755); err != nil {
		t.Fatal(err)
	}
	pack := []byte("{\"format\":2,\"language\":\"ja\",\"lexicon\":\"ja-test\"}\n")
	if err := os.WriteFile(filepath.Join(dataDir, "ja", "manifest.json"), pack, 0o644); err != nil {
		t.Fatal(err)
	}
	shot = testDigest(pack)

	type file struct {
		Path   string `json:"path"`
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	}
	files := map[string]file{}
	artifacts := []map[string]any{}
	for _, spec := range []struct {
		id, kind, output string
		shape            []any
	}{
		{"reranker-public-core", modelsets.KindReranker, "scores", []any{"batch"}},
		{"punctuation-public-core", modelsets.KindPunctuation, "logits", []any{"batch", "seq", 4}},
	} {
		body := []byte("onnx:" + spec.id)
		rel := spec.id + "/" + spec.id + ".placeholder.int8.onnx"
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		f := file{Path: rel, Bytes: int64(len(body)), SHA256: testDigest(body)}
		files[spec.kind] = f
		artifacts = append(artifacts, map[string]any{
			"manifest": spec.id + "/manifest.json", "artifact_id": spec.id, "kind": spec.kind,
			"variant": "public-core", "version": "test-1", "parameters": 42,
			"placeholder": true, "quality": "untrained", "production_eligible": false,
			"dictionary_manifest_sha256": shot,
			"license": map[string]any{"id": "CC-BY-4.0", "scope": "public-core",
				"distribution": "public install", "notice": "legal/CC-BY-4.0.txt"},
			"attribution": []any{"legal/copyright"},
			"source":      map[string]any{"training_data": "none", "generator": "test"},
			"files":       map[string]any{"int8": f},
			"io": map[string]any{
				"inputs": []any{
					map[string]any{"name": "input_ids", "dtype": "int64", "shape": []any{"batch", "seq"}},
					map[string]any{"name": "attention_mask", "dtype": "int64", "shape": []any{"batch", "seq"}},
				},
				"outputs": []any{map[string]any{"name": spec.output, "dtype": "float32", "shape": spec.shape}},
			},
		})
	}
	doc := map[string]any{
		"schema": 1,
		"model_sets": map[string]any{
			"schema": modelsets.Schema, "sets": map[string]any{
				"ja-public-core": map[string]any{
					"variant": "public-core", "language": "ja", "version": "test-1",
					"dictionary": map[string]any{"pack_id": "ja-test", "language": "ja", "format": 2,
						"version": "test", "manifest_sha256": shot, "source": "test"},
					"license":     map[string]any{"id": "CC-BY-4.0", "scope": "public-core", "distribution": "public install", "notice": "legal/CC-BY-4.0.txt"},
					"attribution": []any{"legal/copyright"}, "placeholder": true, "quality": "untrained",
					"production_eligible": false, "artifacts": artifacts,
				},
			},
		},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	lockPath = filepath.Join(t.TempDir(), "pairing.lock.json")
	if err := os.WriteFile(lockPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := modelsets.LoadLock(lockPath); err != nil {
		t.Fatal(err)
	}
	return lockPath, root, dataDir, shot
}

func TestBuildModelSetReport(t *testing.T) {
	lockPath, root, dataDir, shot := writeModelSetFixture(t)
	report := buildModelSetReport("ja-public-core", lockPath, root, dataDir, true)
	if !report.Active || !report.Valid || !report.FilesVerified || !report.DictionaryMatches ||
		report.SetID != "ja-public-core" || report.Variant != "public-core" || !report.Placeholder {
		t.Fatalf("報告 %+v", report)
	}
	if report.Dictionary.ManifestSHA256 != shot || report.Dictionary.InstalledManifestSHA256 != shot {
		t.Fatalf("字典 %+v", report.Dictionary)
	}
	if len(report.Artifacts) != 2 {
		t.Fatalf("模型 %+v", report.Artifacts)
	}

	// placeholder 未允許：仍然是使用中的設定，但報告要說明無效原因。
	refused := buildModelSetReport("ja-public-core", lockPath, root, dataDir, false)
	if !refused.Active || refused.Valid || !strings.Contains(refused.Reason, "placeholder") {
		t.Fatalf("placeholder 報告 %+v", refused)
	}

	// 換一個字典包：必須回報不符，不能默默當成有效。
	if err := os.WriteFile(filepath.Join(dataDir, "ja", "manifest.json"), []byte("{\"format\":2,\"language\":\"ja\",\"lexicon\":\"other\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mismatch := buildModelSetReport("ja-public-core", lockPath, root, dataDir, true)
	if mismatch.Valid || mismatch.DictionaryMatches || !strings.Contains(mismatch.Reason, "字典") {
		t.Fatalf("字典不符報告 %+v", mismatch)
	}
}

func TestBuildModelSetReportWithoutActiveSelection(t *testing.T) {
	lockPath, root, dataDir, _ := writeModelSetFixture(t)
	report := buildModelSetReport("", lockPath, root, dataDir, true)
	if report.Active || report.Valid || report.Reason != "not_configured" {
		t.Fatalf("沒有選定模型組時 %+v", report)
	}
}
