package native

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindRuntimeLibraryLayouts(t *testing.T) {
	for _, c := range []struct {
		name, goos, file string
	}{
		{"root", "windows", "onnxruntime.dll"},
		{"runtime subdirectory", "windows", "runtime/onnxruntime.dll"},
		{"legacy neural runtime", "windows", "neural/runtime/onnxruntime.dll"},
		{"linux root", "linux", "onnxruntime.so"},
		{"darwin root", "darwin", "libonnxruntime.dylib"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, filepath.FromSlash(c.file)))
			if got := findRuntimeLibrary(dir, c.goos); got != filepath.Join(dir, filepath.FromSlash(c.file)) {
				t.Fatalf("findRuntimeLibrary = %q", got)
			}
		})
	}
}

func TestFindRuntimeLibraryMissing(t *testing.T) {
	if got := findRuntimeLibrary(t.TempDir(), "windows"); got != "" {
		t.Fatalf("found %q without a runtime", got)
	}
}
