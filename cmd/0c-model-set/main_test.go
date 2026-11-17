package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/internal/modelsets"
)

func repoLock(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "models", "pairing.lock.json")
}

func TestListReadsProductLock(t *testing.T) {
	if err := run([]string{"list", "-lock", repoLock(t)}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSelectsOneSet(t *testing.T) {
	lockPath := repoLock(t)
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	set, err := lock.Set("ja-public-core")
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"validate", "-lock", lockPath, "-set", "ja-public-core",
		"-pack-sha256", set.Dictionary.ManifestSHA256, "-allow-placeholders"}); err != nil {
		t.Fatal(err)
	}
	// 沒有 -allow-placeholders 必須拒絕 placeholder 模型組。
	if err := run([]string{"validate", "-lock", lockPath, "-set", "ja-public-core",
		"-pack-sha256", set.Dictionary.ManifestSHA256}); err == nil {
		t.Fatal("placeholder 未允許時應失敗")
	}
}

func TestPackManifestFindsJAData(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ja"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "ja", "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"language":"ja"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := packManifest(dir)
	if err != nil || got != manifest {
		t.Fatalf("packManifest %q %v", got, err)
	}
	if _, _, err := dictionary(dir, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dictionary("", ""); err == nil {
		t.Fatal("沒有字典來源時應失敗")
	}
}
