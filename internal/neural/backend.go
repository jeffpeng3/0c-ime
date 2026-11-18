package neural

import (
	"fmt"
	"strings"

	"github.com/0c-biggo/0c-ime/internal/neural/native"
)

// Backend selects how a model package is executed. The only executor is the
// native ONNX Runtime backend; "auto" resolves to native when this build and
// the installed runtime can provide it, and to the statistical engine
// otherwise. There is no interpreter backend.
type Backend string

const (
	BackendAuto   Backend = "auto"
	BackendNative Backend = "native"
	// BackendStatistical is a resolution result, not a -neural-backend value:
	// the native runtime is unavailable, so the statistical engine serves.
	BackendStatistical Backend = "statistical"
)

// ParseBackend parses the -neural-backend value; unknown values are rejected.
func ParseBackend(value string) (Backend, error) {
	switch backend := Backend(strings.ToLower(strings.TrimSpace(value))); backend {
	case BackendAuto, BackendNative:
		return backend, nil
	}
	return "", fmt.Errorf("unknown neural backend %q", value)
}

// ResolvedBackend is the outcome of backend selection, including a stable
// reason code for health reporting. An empty Reason means native is ready.
type ResolvedBackend struct {
	Backend Backend
	Library string
	// Reason is empty when the requested native backend is ready. Otherwise it
	// is "native_unavailable" (binary built without CGO/onnxruntime) or
	// "native_runtime_missing" (shared library not installed next to the
	// executable). In both cases the statistical engine serves requests.
	Reason string
}

// ChooseBackend resolves -neural-backend against the build and the installed
// runtime. It never returns an interpreter backend.
func ChooseBackend(requested Backend, nativeBuilt bool, library string) ResolvedBackend {
	switch requested {
	case BackendNative, BackendAuto:
		if !nativeBuilt {
			return ResolvedBackend{Backend: BackendStatistical, Reason: ReasonNativeUnavailable}
		}
		if library == "" {
			return ResolvedBackend{Backend: BackendStatistical, Reason: ReasonRuntimeMissing}
		}
		return ResolvedBackend{Backend: BackendNative, Library: library}
	}
	return ResolvedBackend{Backend: BackendStatistical, Reason: "unknown_backend"}
}

// NativeAvailable reports whether this binary contains the native ONNX
// backend. Default CGO-disabled builds return false.
func NativeAvailable() bool { return native.Available() }

// NativeRuntimeLibrary is the ONNX Runtime shared library installed next to
// this executable, or "" when it is absent.
func NativeRuntimeLibrary() string { return native.RuntimeLibrary() }

// DefaultBackend resolves the product default: native when usable, statistical
// otherwise.
func DefaultBackend() (Backend, ResolvedBackend) {
	chosen := ChooseBackend(BackendAuto, NativeAvailable(), NativeRuntimeLibrary())
	return chosen.Backend, chosen
}
