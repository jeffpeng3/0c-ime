package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/internal/neural"
)

// TestPlaceholderInt8ResidencyBudgets 用 P2-25P 的真實 int8 placeholder 模型核對 Phase 2 的
// 模型檔常駐預算：單語言（一組 reranker + punctuation）低於 250 MB、雙語言（兩組）低於 400 MB。
// 預設跳過；設定 OC_PLACEHOLDER_MODELS 指向 reference/models 後才執行。Python runtime 的 RSS
// 由 `0c-server -model-report` 的 process_tree_rss_bytes 另外量測。
func TestPlaceholderInt8ResidencyBudgets(t *testing.T) {
	root := os.Getenv("OC_PLACEHOLDER_MODELS")
	if root == "" {
		t.Skip("set OC_PLACEHOLDER_MODELS to the placeholder reference/models directory")
	}
	groups := map[string][]string{
		"public-core": {"reranker-public-core", "punctuation-public-core"},
		"cc-by-sa":    {"reranker-cc-by-sa", "punctuation-cc-by-sa"},
	}
	const mib = 1 << 20
	sizes := map[string]int64{}
	for group, artifacts := range groups {
		var total int64
		for _, artifact := range artifacts {
			manifest := filepath.Join(root, artifact, "manifest.json")
			validated, err := neural.ValidateArtifact(manifest, root, "int8")
			if err != nil {
				t.Fatalf("%s: %v", artifact, err)
			}
			if !validated.Manifest.Placeholder || validated.Manifest.Quality != "untrained" || validated.Format != "int8" {
				t.Fatalf("%s 不是 int8 placeholder：%+v", artifact, validated.Manifest)
			}
			t.Logf("%s: %d bytes (%.1f MiB)", artifact, validated.Bytes, float64(validated.Bytes)/mib)
			sizes[artifact] = validated.Bytes
			total += validated.Bytes
		}
		if total > 250*mib {
			t.Fatalf("%s 單語言常駐 %d bytes 超過 250 MiB", group, total)
		}
		t.Logf("%s 單語言合計：%d bytes (%.1f MiB)", group, total, float64(total)/mib)
	}
	var all int64
	for _, size := range sizes {
		all += size
	}
	if all > 400*mib {
		t.Fatalf("雙語言常駐 %d bytes 超過 400 MiB", all)
	}
	t.Logf("雙語言合計（四份 int8）：%d bytes (%.1f MiB)", all, float64(all)/mib)
}
