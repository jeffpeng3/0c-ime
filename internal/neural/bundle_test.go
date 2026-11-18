package neural

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Layout: root/data (dictionary), root/neural (bundled models), home (user dir).
type findLayout struct{ root, data, neural, user string }

func newFindLayout(t *testing.T) findLayout {
	t.Helper()
	root := t.TempDir()
	l := findLayout{root: root, data: filepath.Join(root, "data"), neural: filepath.Join(root, "neural"),
		user: filepath.Join(root, "home")}
	os.MkdirAll(l.data, 0o755)
	return l
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

const testDictionary = "91620d9640525e694afe417bd03e500a666441f72b59bc92c1913c83c17a3b9b"

func (l findLayout) find() Bundled {
	return Find(Options{DataDir: l.data, UserDir: l.user, DictionarySHA256: testDictionary})
}

func TestFindBundledModelsWithoutAnyRuntimeScript(t *testing.T) {
	l := newFindLayout(t)
	for _, p := range []string{"select/manifest.json", "punct/manifest.json"} {
		touch(t, filepath.Join(l.neural, p))
	}
	touch(t, filepath.Join(l.user, "models", testDictionary, "select", "manifest.json"))
	for _, b := range []Bundled{l.find(), FindBundled(l.data)} {
		if !b.HasSelect || !b.HasPunct ||
			b.SelectSource != SourceBundled || b.PunctSource != SourceBundled ||
			b.SelectPackage != filepath.Join(l.neural, "select") ||
			b.PunctPackage != filepath.Join(l.neural, "punct") {
			t.Fatalf("bundled models: %+v", b)
		}
	}
}

func TestFindUserModelsByDictionary(t *testing.T) {
	l := newFindLayout(t)
	user := filepath.Join(l.user, "models", testDictionary)
	touch(t, filepath.Join(user, "select", "manifest.json"))
	b := l.find()
	if !b.HasSelect || b.SelectSource != SourceUser || b.SelectPackage != filepath.Join(user, "select") {
		t.Fatalf("user model: %+v", b)
	}
	if b.HasPunct || b.PunctSource != "" {
		t.Fatalf("punct model is not installed: %+v", b)
	}
	// FindBundled ignores personal models.
	if fb := FindBundled(l.data); fb.HasSelect || fb.SelectSource != "" {
		t.Fatalf("FindBundled must ignore user models: %+v", fb)
	}
	// A different dictionary hash must not see the old pairing.
	other := Find(Options{DataDir: l.data, UserDir: l.user, DictionarySHA256: strings.Repeat("0", 64)})
	if other.HasSelect || other.SelectSource != "" {
		t.Fatalf("model paired with another dictionary was used: %+v", other)
	}
	// Bundled wins over personal.
	touch(t, filepath.Join(l.neural, "select", "manifest.json"))
	if b := l.find(); b.SelectSource != SourceBundled || b.SelectPackage != filepath.Join(l.neural, "select") {
		t.Fatalf("bundled model must win over user model: %+v", b)
	}
}

func TestFindDoesNotLookForRuntimeScripts(t *testing.T) {
	l := newFindLayout(t)
	touch(t, filepath.Join(l.neural, "sequence_runtime.py"))
	touch(t, filepath.Join(l.neural, "punct_runtime.py"))
	if b := l.find(); b.HasSelect || b.HasPunct || b.SelectSource != "" || b.PunctSource != "" {
		t.Fatalf("scripts without a model package must not count as installed: %+v", b)
	}
}
