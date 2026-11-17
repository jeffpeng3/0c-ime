//go:build !windows

package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenBackendMarkers 是 P2-01 要從正式路徑移除的後端字串。掃描的是
// 非測試原始碼（.go／.py／.json），不是文件；文件可以說明移除歷史。
// 「Zenzai」是演算法名稱，大小寫不同，不會誤中。
var forbiddenBackendMarkers = []string{"llama", "gguf", "GGUF", "zenz", "JaZenz", "JaLlama"}

// P2-01：正式路徑不得再 import 或啟動 llama.cpp／GGUF／zenz。spike 只能
// 留在 scripts/spike，不進 production 目錄。
func TestProductionSourceHasNoLlamaBackend(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dirs := []string{
		filepath.Join(root, "cmd", "0c-server"),
		filepath.Join(root, "internal", "neural"),
		filepath.Join(root, "core", "ja", "rerank"),
		filepath.Join(root, "platforms", "neural"),
	}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".py", ".json":
			default:
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, marker := range forbiddenBackendMarkers {
				if bytes.Contains(b, []byte(marker)) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s 仍含正式路徑禁止的後端字串 %q", rel, marker)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// P2-01 驗收：正式 binary 不含 llama.cpp／GGUF／zenz 的協定或模型字串。
func TestServerBinaryHasNoLlamaBackend(t *testing.T) {
	b, err := os.ReadFile(buildServer(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"llama-server", "ja-zenz-verify-v1", "ggml-model", "model_hash_mismatch",
		"japanese neural runtime", "JaZenzModelSHA256",
	} {
		if bytes.Contains(b, []byte(marker)) {
			t.Errorf("0c-server binary 仍含 %q", marker)
		}
	}
}
