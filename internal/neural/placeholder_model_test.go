package neural

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlaceholderModelProbe 用 P2-25P 的 real int8 ONNX 檔驗證 runtime，而不只
// 是 synthetic fixture。預設跳過；設定 OC_PLACEHOLDER_MODELS 指向 reference/models
// 後才執行，OC_PLACEHOLDER_PYTHON 可指定裝有 numpy、onnxruntime 的 Python。
func TestPlaceholderModelProbe(t *testing.T) {
	root := os.Getenv("OC_PLACEHOLDER_MODELS")
	if root == "" {
		t.Skip("set OC_PLACEHOLDER_MODELS to the placeholder reference/models directory")
	}
	python := os.Getenv("OC_PLACEHOLDER_PYTHON")
	if python == "" {
		python = "python3"
	}
	probe, err := filepath.Abs(filepath.Join("..", "..", "platforms", "neural", "runtime", "onnx_bundle_probe.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		artifact string
		kind     string
		output   []int
	}{
		{"reranker-public-core", "reranker", []int{1}},
		{"punctuation-public-core", "punctuation", []int{1, 32, 4}},
	} {
		t.Run(c.artifact, func(t *testing.T) {
			manifest := filepath.Join(root, c.artifact, "manifest.json")
			artifact, err := ValidateArtifact(manifest, root, "int8")
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if !artifact.Manifest.Placeholder || artifact.Manifest.ProductionEligible || artifact.Manifest.Quality != "untrained" {
				t.Fatalf("placeholder metadata not surfaced: %+v", artifact.Manifest)
			}
			if artifact.Bytes < 50<<20 {
				t.Fatalf("unexpected model size %d", artifact.Bytes)
			}
			cmd := exec.Command(python, probe, "--manifest", manifest, "--artifact-root", root,
				"--format", "int8", "--threads", "1", "--runs", "10", "--seq", "32")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("probe: %v\n%s", err, stderr.String())
			}
			var result struct {
				Ready            bool     `json:"ready"`
				Kind             string   `json:"kind"`
				Placeholder      bool     `json:"placeholder"`
				Quality          string   `json:"quality"`
				Format           string   `json:"format"`
				SHA256           string   `json:"sha256"`
				Provider         []string `json:"provider"`
				LoadMS           float64  `json:"load_ms"`
				FirstInferenceMS float64  `json:"first_inference_ms"`
				ReadyMS          float64  `json:"ready_ms"`
				MedianMS         float64  `json:"median_ms"`
				OutputShape      []int    `json:"output_shape"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("probe output: %v\n%s", err, stdout.String())
			}
			if !result.Ready || result.Kind != c.kind || !result.Placeholder || result.Quality != "untrained" ||
				result.Format != "int8" || result.SHA256 != artifact.SHA256 || result.LoadMS <= 0 || result.FirstInferenceMS <= 0 {
				t.Fatalf("probe result %+v", result)
			}
			if len(result.Provider) != 1 || result.Provider[0] != "CPUExecutionProvider" {
				t.Fatalf("providers %v", result.Provider)
			}
			if len(result.OutputShape) != len(c.output) {
				t.Fatalf("output shape %v", result.OutputShape)
			}
			for i := range c.output {
				if result.OutputShape[i] != c.output[i] {
					t.Fatalf("output shape %v, want %v", result.OutputShape, c.output)
				}
			}
			t.Logf("%s: cold ready %.1f ms (session load %.1f ms), first inference %.1f ms, median %.1f ms, %s, %s",
				c.artifact, result.ReadyMS, result.LoadMS, result.FirstInferenceMS, result.MedianMS, strings.Join(result.Provider, ","), result.SHA256[:12])
		})
	}
}
