package main

import (
	"bytes"
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
	if id, err := lock.DefaultSetID(); err != nil || id != "ja-cc-by-sa" {
		t.Fatalf("default_set=%q %v", id, err)
	}
	// 預設組已 pin：development 與 release 都應通過。
	if err := run([]string{"check", "-lock", lockPath}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "-lock", lockPath, "-release"}); err != nil {
		t.Fatalf("已 pin 的 default_set 應通過 release：%v", err)
	}
	if err := run([]string{"validate", "-lock", lockPath, "-set", "ja-public-core",
		"-pack-sha256", set.Dictionary.ManifestSHA256}); err != nil {
		t.Fatal(err)
	}
	// 省略 -set 時驗證 default_set，且 release 模式同樣通過。
	if err := run([]string{"validate", "-lock", lockPath, "-release",
		"-pack-sha256", set.Dictionary.ManifestSHA256}); err != nil {
		t.Fatalf("validate 省略 -set 應驗 default_set：%v", err)
	}
}

func TestReleaseCheckFailsWhenDefaultRevisionRemoved(t *testing.T) {
	raw, err := os.ReadFile(repoLock(t))
	if err != nil {
		t.Fatal(err)
	}
	revision := "7450e10fe8b673560044c5db08ad3006a06d7a6e"
	if !bytes.Contains(raw, []byte(revision)) {
		t.Fatalf("正式 lock 缺少預期 revision %s", revision)
	}
	withoutRevision := bytes.Replace(raw, []byte(revision), nil, 1)
	path := filepath.Join(t.TempDir(), "pairing.lock.json")
	if err := os.WriteFile(path, withoutRevision, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "-lock", path, "-release"}); err == nil {
		t.Fatal("移除 default_set 的 revision 後 release 檢查應失敗")
	}

	withoutDefault := bytes.Replace(raw, []byte(`    "default_set": "ja-cc-by-sa",`+"\n"), nil, 1)
	if err := os.WriteFile(path, withoutDefault, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "-lock", path, "-release"}); err == nil {
		t.Fatal("缺少 default_set 時 release 檢查應失敗")
	}
}

func TestValidateAllowsFixturePlaceholderOnlyWhenExplicit(t *testing.T) {
	lockPath := filepath.Join("..", "..", "testdata", "models", "placeholder", "pairing.lock.json")
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	set, err := lock.Set("ja-public-core")
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"validate", "-lock", lockPath, "-set", "ja-public-core",
		"-pack-sha256", set.Dictionary.ManifestSHA256}); err == nil {
		t.Fatal("placeholder 未允許時應失敗")
	}
	if err := run([]string{"validate", "-lock", lockPath, "-set", "ja-public-core",
		"-pack-sha256", set.Dictionary.ManifestSHA256, "-allow-placeholders"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"validate", "-lock", lockPath, "-set", "ja-public-core",
		"-pack-sha256", set.Dictionary.ManifestSHA256, "-allow-placeholders", "-release"}); err == nil {
		t.Fatal("placeholder 不得通過 release 驗證")
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
