package neural

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/0c-biggo/0c-ime/internal/neural/native"
)

// Model sources for health reporting.
const (
	SourceBundled = "bundled" // shipped in the install bundle
	SourceUser    = "user"    // personal directory, downloaded/paird locally (#180)
)

// Options locates model packages. Model packages are data only; no
// interpreter or worker script is part of the bundle.
//
//	<data>/../neural/select|punct/                          bundled models
//	<UserDir>/models/<dictionary manifest SHA-256>/select|punct/
//
// Models are searched bundled first and then the personal directory. If none
// exist, the statistical engine selects candidates.
type Options struct {
	DataDir          string
	UserDir          string
	DictionarySHA256 string
}

// Bundled is the located models plus native runtime availability. HasSelect
// and HasPunct mean a package was found; whether it can run depends on the
// native backend and its shared library.
type Bundled struct {
	SelectPackage, PunctPackage string
	SelectSource, PunctSource   string // empty: not found
	HasSelect, HasPunct         bool
	// P2-43: native ONNX Runtime. Native is true when this build contains the
	// native backend and an ONNX Runtime library is installed next to the
	// executable; NativeLibrary is its path.
	Native        bool
	NativeLibrary string
	// NativeBuilt is whether this build contains the native backend,
	// regardless of whether the shared library is present.
	NativeBuilt bool
}

// FindBundled only looks at the install bundle's models.
func FindBundled(dataDir string) Bundled { return Find(Options{DataDir: dataDir}) }

// Find locates bundled and personal model packages; see Options.
func Find(o Options) Bundled {
	dir := filepath.Join(filepath.Dir(filepath.Clean(o.DataDir)), "neural")
	var user string
	if o.UserDir != "" && len(o.DictionarySHA256) == 64 {
		user = filepath.Join(o.UserDir, "models", o.DictionarySHA256)
	}
	var b Bundled
	b.NativeBuilt = native.Available()
	b.NativeLibrary = native.RuntimeLibrary()
	b.Native = b.NativeBuilt && b.NativeLibrary != ""
	find := func(name string) (string, string) {
		if bundled := filepath.Join(dir, name); regular(filepath.Join(bundled, "manifest.json")) {
			return bundled, SourceBundled
		}
		if user != "" {
			if p := filepath.Join(user, name); regular(filepath.Join(p, "manifest.json")) {
				return p, SourceUser
			}
		}
		return filepath.Join(dir, name), ""
	}
	b.SelectPackage, b.SelectSource = find("select")
	b.PunctPackage, b.PunctSource = find("punct")
	b.HasSelect = b.SelectSource != ""
	b.HasPunct = b.PunctSource != ""
	return b
}

func regular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// MaxThreads is the executor's thread cap.
const MaxThreads = 8

// DefaultThreads uses all CPUs up to the cap: inference only runs briefly on
// key presses and commits, so idle time costs nothing.
func DefaultThreads() int {
	return max(1, min(MaxThreads, runtime.NumCPU()))
}
