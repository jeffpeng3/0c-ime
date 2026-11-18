package neural

import "testing"

func TestChooseBackendResolvesNativeOrStatistical(t *testing.T) {
	cases := []struct {
		name    string
		built   bool
		library string
		want    Backend
		reason  string
	}{
		{"native build with library", true, "/usr/lib/onnxruntime.so", BackendNative, ""},
		{"native build without library", true, "", BackendStatistical, ReasonRuntimeMissing},
		{"build without native", false, "", BackendStatistical, ReasonNativeUnavailable},
		{"build without native ignores stray library", false, "/usr/lib/onnxruntime.so", BackendStatistical, ReasonNativeUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ChooseBackend(BackendAuto, c.built, c.library)
			if got.Backend != c.want || got.Reason != c.reason {
				t.Fatalf("ChooseBackend = %+v, want backend=%s reason=%q", got, c.want, c.reason)
			}
			if c.reason == "" && got.Library != c.library {
				t.Fatalf("library %q, want %q", got.Library, c.library)
			}
		})
	}
}

func TestChooseBackendExplicitValues(t *testing.T) {
	if got := ChooseBackend(BackendNative, true, "/x"); got.Backend != BackendNative || got.Library != "/x" || got.Reason != "" {
		t.Fatalf("native: %+v", got)
	}
	if got := ChooseBackend(BackendNative, false, ""); got.Backend != BackendStatistical || got.Reason != ReasonNativeUnavailable {
		t.Fatalf("native unavailable: %+v", got)
	}
}

func TestParseBackendAcceptsAutoAndNativeOnly(t *testing.T) {
	for _, value := range []string{"auto", " AUTO ", "native"} {
		if _, err := ParseBackend(value); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
	}
	for _, value := range []string{"python", "rust", ""} {
		if _, err := ParseBackend(value); err == nil {
			t.Fatalf("%q accepted; the python backend was removed", value)
		}
	}
}
