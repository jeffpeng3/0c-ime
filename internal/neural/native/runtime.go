package native

import (
	"os"
	"path/filepath"
	"runtime"
)

// RuntimeLibrary returns the ONNX Runtime shared library that the native
// backend would load, or "" when it is not installed next to the executable.
//
// The Windows bundle ships onnxruntime.dll at the install root (next to
// 0c-server.exe); a runtime/ subdirectory is also accepted. The library is only
// inspected for existence here; loading failures are reported by Open.
func RuntimeLibrary() string {
	exe, err := os.Executable()
	dir := ""
	if err == nil {
		dir = filepath.Dir(exe)
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return findRuntimeLibrary(dir, runtime.GOOS)
}

func findRuntimeLibrary(dir, goos string) string {
	name := "onnxruntime.so"
	if goos == "windows" {
		name = "onnxruntime.dll"
	} else if goos == "darwin" {
		name = "libonnxruntime.dylib"
	}
	for _, candidate := range []string{
		filepath.Join(dir, name),
		filepath.Join(dir, "runtime", name),
		filepath.Join(dir, "neural", "runtime", name),
	} {
		if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}
