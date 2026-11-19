package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/internal/enginedir"
)

const testServer = "0c-server.exe"

func writeInstallFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func receipt(t *testing.T, base, directory string, active bool) {
	t.Helper()
	body := `{"schema":1,"product":"0c","version":"0.2.13-preview.1","directory":"` + directory +
		`","active":` + map[bool]string{true: "true", false: "false"}[active] + `,"files":{}}`
	writeInstallFile(t, filepath.Join(base, "install-receipt.json"), body)
}

// 收據記錄的版本目錄有引擎與字典時，使用那一份；不再回 base\data。
func TestWindowsInstallUsesTheReceiptVersionDirectory(t *testing.T) {
	base := t.TempDir()
	dir := "versions/0.2.13-preview.1-83ff48cf6e9b"
	writeInstallFile(t, filepath.Join(base, "versions/0.2.13-preview.1-83ff48cf6e9b/0c-server.exe"), "server")
	writeInstallFile(t, filepath.Join(base, "versions/0.2.13-preview.1-83ff48cf6e9b/data/manifest.json"), "{}")
	receipt(t, base, dir, true)

	in, ok := windowsInstallAt(base, testServer, "0c-settings.exe", "0c-update.exe")
	if !ok {
		t.Fatal("找不到收據記錄的安裝")
	}
	if want := filepath.Join(base, filepath.FromSlash(dir), testServer); in.Server != want {
		t.Fatalf("server %q != %q", in.Server, want)
	}
	if want := filepath.Join(base, filepath.FromSlash(dir), "data"); in.Data != want {
		t.Fatalf("data %q != %q", in.Data, want)
	}
	if !in.Active || in.Kind != "personal" || in.Updates != "self" {
		t.Fatalf("install %+v", in)
	}
}

// 核心更新後 engine.json 指向另一個版本目錄：模型頁要跟引擎用同一份，不看收據的舊版本。
func TestWindowsInstallHonoursTheEnginePointer(t *testing.T) {
	base := t.TempDir()
	writeInstallFile(t, filepath.Join(base, "versions/old/0c-server.exe"), "old")
	writeInstallFile(t, filepath.Join(base, "versions/old/data/manifest.json"), "{}")
	writeInstallFile(t, filepath.Join(base, "versions/new/0c-server.exe"), "new")
	writeInstallFile(t, filepath.Join(base, "versions/new/data/manifest.json"), "{}")
	receipt(t, base, "versions/old", true)
	if err := enginedir.Write(base, enginedir.Current{Directory: "versions/new", Version: "new"}); err != nil {
		t.Fatal(err)
	}

	in, ok := windowsInstallAt(base, testServer, "0c-settings.exe", "0c-update.exe")
	if !ok {
		t.Fatal("找不到使用中的安裝")
	}
	if want := filepath.Join(base, "versions/new", testServer); in.Server != want {
		t.Fatalf("server %q != %q", in.Server, want)
	}
	if want := filepath.Join(base, "versions/new/data"); in.Data != want {
		t.Fatalf("data %q != %q", in.Data, want)
	}
}

// 收據指向不存在的版本目錄時，退回 bin 穩定副本與舊的 Zuin\data，而不是回一個不存在的路徑。
func TestWindowsInstallFallsBackWhenTheVersionDirectoryIsMissing(t *testing.T) {
	base := t.TempDir()
	writeInstallFile(t, filepath.Join(base, "bin/0c-server.exe"), "stable")
	writeInstallFile(t, filepath.Join(base, "data/manifest.json"), "{}")
	writeInstallFile(t, filepath.Join(base, "bin/0c-settings.exe"), "settings")
	writeInstallFile(t, filepath.Join(base, "bin/0c-update.exe"), "updater")
	receipt(t, base, "versions/gone", true)

	in, ok := windowsInstallAt(base, testServer, "0c-settings.exe", "0c-update.exe")
	if !ok {
		t.Fatal("沒有退回 bin 穩定副本")
	}
	if want := filepath.Join(base, "bin", testServer); in.Server != want {
		t.Fatalf("server %q != %q", in.Server, want)
	}
	if want := filepath.Join(base, "data"); in.Data != want {
		t.Fatalf("data %q != %q", in.Data, want)
	}
	if in.Settings != filepath.Join(base, "bin/0c-settings.exe") || in.Updater != filepath.Join(base, "bin/0c-update.exe") {
		t.Fatalf("install %+v", in)
	}
}

// 沒有收據（例如手動解壓的開發目錄）時，仍找得到 bin 或根目錄的引擎；都沒有時不假造安裝。
func TestWindowsInstallWithoutAReceipt(t *testing.T) {
	base := t.TempDir()
	writeInstallFile(t, filepath.Join(base, "bin/0c-server.exe"), "stable")
	writeInstallFile(t, filepath.Join(base, "data/manifest.json"), "{}")
	in, ok := windowsInstallAt(base, testServer, "0c-settings.exe", "0c-update.exe")
	if !ok || in.Server != filepath.Join(base, "bin", testServer) {
		t.Fatalf("沒有收據時應退回 bin：%+v %v", in, ok)
	}

	empty := t.TempDir()
	if _, ok := windowsInstallAt(empty, testServer, "0c-settings.exe", "0c-update.exe"); ok {
		t.Fatal("沒有任何引擎時不應回報安裝")
	}
	b, err := os.ReadFile(filepath.Join(empty, "install-receipt.json"))
	if err == nil || len(b) != 0 {
		t.Fatal("測試不應建立收據")
	}
}
