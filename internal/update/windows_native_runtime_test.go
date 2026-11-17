package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

var nativeRuntimeFiles = []string{"onnxruntime.dll", "vcruntime140.dll", "vcruntime140_1.dll",
	"msvcp140.dll", "msvcp140_1.dll"}

// nativeRuntimeFixture is a Windows bundle whose install layout includes the
// pinned native ONNX Runtime at the version-directory root, next to
// 0c-server.exe. 0c-server loads onnxruntime.dll from its own directory.
func nativeRuntimeFixture(t *testing.T, version string) string {
	t.Helper()
	root := bundleFixture(t, version)
	file := filepath.Join(root, "release.json")
	var release Release
	if err := readJSONFile(file, &release); err != nil {
		t.Fatal(err)
	}
	for _, name := range nativeRuntimeFiles {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(version+" "+name), 0o600); err != nil {
			t.Fatal(err)
		}
		release.Files[name], _ = Hash(path)
	}
	if err := writeJSON(file, release); err != nil {
		t.Fatal(err)
	}
	return root
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// P2-43: upgrade, rollback and uninstall keep the runtime layout intact and
// always leave onnxruntime.dll beside 0c-server.exe.
func TestWindowsNativeRuntimeInstallUpgradeRollbackUninstall(t *testing.T) {
	base := t.TempDir()
	reg := &fakeRegistrar{}
	first, _, err := InstallWindows(nativeRuntimeFixture(t, "0.1.0-dev.1"), base, "amd64", reg)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := InstallWindows(nativeRuntimeFixture(t, "0.1.0-dev.2"), base, "amd64", reg)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("upgrade did not switch version directories")
	}
	for _, dir := range []string{first, second} {
		for _, name := range nativeRuntimeFiles {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Fatalf("version layout %s missing %s: %v", dir, name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "data", "manifest.json")); err != nil {
			t.Fatalf("version layout %s missing data: %v", dir, err)
		}
	}
	// The runtime must be a sibling of the server, not nested below data/.
	if _, err := os.Stat(filepath.Join(second, "data", "onnxruntime.dll")); !os.IsNotExist(err) {
		t.Fatal("onnxruntime.dll must not be inside data/")
	}
	if _, err = RollbackWindows(base, reg, false); err != nil {
		t.Fatal(err)
	}
	if reg.path != filepath.Join(first, "0c_tsf.dll") {
		t.Fatalf("rollback registered %q, want the first version", reg.path)
	}
	for _, name := range nativeRuntimeFiles {
		if _, err := os.Stat(filepath.Join(first, name)); err != nil {
			t.Fatalf("rollback layout lost %s: %v", name, err)
		}
	}
	if _, err := UninstallWindows(base, reg); err != nil {
		t.Fatal(err)
	}
	if reg.path != "" {
		t.Fatal("uninstall left the DLL registered")
	}
}

// P2-43: a bundle that lists the runtime DLLs but does not contain them is
// rejected before anything is registered.
func TestWindowsNativeRuntimeMissingFileRejected(t *testing.T) {
	root := nativeRuntimeFixture(t, "0.1.0-dev.3")
	if err := os.Remove(filepath.Join(root, "onnxruntime.dll")); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(root, "windows", "amd64"); err == nil {
		t.Fatal("verified a bundle with a missing runtime DLL")
	}
}
