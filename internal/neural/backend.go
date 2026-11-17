package neural

import "runtime"

// BackendAuto picks the native ONNX Runtime backend when this build contains it
// and its shared library is installed, and otherwise degrades to the
// statistical engine. Python is never required: an auto build without the
// native runtime does not silently start a Python worker.
const BackendAuto Backend = "auto"

// ResolvedBackend is the outcome of backend selection, including a stable
// reason code for health reporting.
type ResolvedBackend struct {
	Backend Backend
	Library string
	// Reason is empty when the requested backend is ready. Otherwise it is one
	// of "native_unavailable" (binary built without CGO/onnxruntime),
	// "native_runtime_missing" (DLL not installed next to the executable), or
	// "python" (auto fell back to the legacy worker on Linux/macOS).
	Reason string
}

// ChooseBackend resolves -neural-backend against the build and the installed
// runtime. goos is passed explicitly so the policy is testable off Windows.
func ChooseBackend(requested Backend, nativeBuilt bool, library, goos string) ResolvedBackend {
	switch requested {
	case BackendPython:
		return ResolvedBackend{Backend: BackendPython}
	case BackendNative:
		if !nativeBuilt {
			return ResolvedBackend{Backend: BackendNative, Reason: "native_unavailable"}
		}
		if library == "" {
			return ResolvedBackend{Backend: BackendNative, Reason: "native_runtime_missing"}
		}
		return ResolvedBackend{Backend: BackendNative, Library: library}
	case BackendAuto:
		if nativeBuilt && library != "" {
			return ResolvedBackend{Backend: BackendNative, Library: library}
		}
		if goos == "windows" || nativeBuilt {
			reason := "native_unavailable"
			if nativeBuilt {
				reason = "native_runtime_missing"
			}
			return ResolvedBackend{Backend: BackendNative, Reason: reason}
		}
		return ResolvedBackend{Backend: BackendPython, Reason: "python"}
	}
	return ResolvedBackend{Backend: BackendPython, Reason: "unknown_backend"}
}

// DefaultBackend is the product default: native when usable, statistical
// otherwise. Linux/macOS keep the bundled Python worker when no native runtime
// is built.
func DefaultBackend() (Backend, ResolvedBackend) {
	chosen := ChooseBackend(BackendAuto, NativeAvailable(), NativeRuntimeLibrary(), runtime.GOOS)
	return chosen.Backend, chosen
}
