package neural

import "testing"

func TestChooseBackendAutoPrefersNativeAndNeverRequiresPython(t *testing.T) {
	for _, c := range []struct {
		name          string
		built         bool
		library, goos string
		want          Backend
		reason        string
	}{
		{"Windows 有原生 DLL", true, `C:\Zuin\onnxruntime.dll`, "windows", BackendNative, ""},
		{"Windows 沒有 DLL：統計轉換", true, "", "windows", BackendNative, "native_runtime_missing"},
		{"Windows 建置沒有 CGO：統計轉換", false, "", "windows", BackendNative, "native_unavailable"},
		{"Linux 建置沒有原生後端：保留 Python", false, "", "linux", BackendPython, "python"},
		{"Linux 有原生 DLL 但建置沒有 tag", false, "/usr/lib/onnxruntime.so", "linux", BackendPython, "python"},
		{"Linux 有原生後端", true, "/usr/lib/onnxruntime.so", "linux", BackendNative, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ChooseBackend(BackendAuto, c.built, c.library, c.goos)
			if got.Backend != c.want || got.Reason != c.reason {
				t.Fatalf("ChooseBackend = %+v，want backend=%s reason=%q", got, c.want, c.reason)
			}
		})
	}
}

func TestChooseBackendExplicitValues(t *testing.T) {
	if got := ChooseBackend(BackendPython, true, "/x", "windows"); got.Backend != BackendPython || got.Reason != "" {
		t.Fatalf("python: %+v", got)
	}
	if got := ChooseBackend(BackendNative, false, "", "linux"); got.Backend != BackendNative || got.Reason != "native_unavailable" {
		t.Fatalf("native without build: %+v", got)
	}
	if got := ChooseBackend(BackendNative, true, "/x", "linux"); got.Backend != BackendNative || got.Library != "/x" {
		t.Fatalf("native: %+v", got)
	}
}

func TestParseBackendAcceptsAuto(t *testing.T) {
	for _, value := range []string{"auto", " AUTO ", "python", "native"} {
		if _, err := ParseBackend(value); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
	}
	if _, err := ParseBackend("rust"); err == nil {
		t.Fatal("unknown backend accepted")
	}
}
