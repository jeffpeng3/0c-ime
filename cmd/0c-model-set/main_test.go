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
	if err := run([]string{"list", "-json", "-lock", repoLock(t)}); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseCheckUsesSinglePinnedPackage(t *testing.T) {
	lockPath := repoLock(t)
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	pkg := lock.ModelPackage
	if pkg == nil || len(pkg.Artifacts) != 2 {
		t.Fatalf("正式 lock 應只有一個模型包：%+v", pkg)
	}
	// 模型包已 pin：development 與 release 都應通過。
	if err := run([]string{"check", "-lock", lockPath}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "-lock", lockPath, "-release"}); err != nil {
		t.Fatalf("已 pin 的模型包應通過 release：%v", err)
	}
	if err := run([]string{"validate", "-lock", lockPath, "-release",
		"-pack-sha256", pkg.Dictionary.ManifestSHA256}); err != nil {
		t.Fatalf("validate release 應通過：%v", err)
	}
	if err := run([]string{"show", "-lock", lockPath}); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseCheckFailsWhenRevisionOrHashRemoved(t *testing.T) {
	raw, err := os.ReadFile(repoLock(t))
	if err != nil {
		t.Fatal(err)
	}
	revision := "7450e10fe8b673560044c5db08ad3006a06d7a6e"
	hash := "4b74ae019b15443f2a30936e9cf53959e60716a30548f17bb1f0d8569fa978e9"
	path := filepath.Join(t.TempDir(), "pairing.lock.json")

	if !bytes.Contains(raw, []byte(revision)) || !bytes.Contains(raw, []byte(hash)) {
		t.Fatal("正式 lock 缺少預期 revision 或 SHA-256")
	}
	if err := os.WriteFile(path, bytes.Replace(raw, []byte(revision), nil, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "-lock", path, "-release"}); err == nil {
		t.Fatal("移除 revision 後 release 檢查應失敗")
	}
	if err := os.WriteFile(path, bytes.Replace(raw, []byte(hash), nil, 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "-lock", path, "-release"}); err == nil {
		t.Fatal("移除檔案 SHA-256 後 release 檢查應失敗")
	}
}

func TestValidateAllowsFixturePlaceholderOnlyWhenExplicit(t *testing.T) {
	lockPath := filepath.Join("..", "..", "testdata", "models", "placeholder", "pairing.lock.json")
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	pkg := lock.ModelPackage
	if err := run([]string{"validate", "-lock", lockPath,
		"-pack-sha256", pkg.Dictionary.ManifestSHA256}); err == nil {
		t.Fatal("placeholder 未允許時應失敗")
	}
	if err := run([]string{"validate", "-lock", lockPath,
		"-pack-sha256", pkg.Dictionary.ManifestSHA256, "-allow-placeholders"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"validate", "-lock", lockPath,
		"-pack-sha256", pkg.Dictionary.ManifestSHA256, "-allow-placeholders", "-release"}); err == nil {
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
