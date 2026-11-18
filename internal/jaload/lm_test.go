package jaload

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

func TestLoadLMOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lm.bin")

	if m, err := LoadLM(path); err != nil || m != nil {
		t.Fatalf("缺少 lm.bin = %v, %v；want nil, nil", m, err)
	}

	model := jaimport.NewLM(
		map[string]float64{"私": 1.5},
		map[[2]string]float64{{"私", "は"}: 2},
	)
	if err := os.WriteFile(path, model.Encode(), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := LoadLM(path)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || m.Len() != 1 || m.BigramLen() != 1 {
		t.Fatalf("載入模型 = %#v", m)
	}
	if got := m.Cost("私", "は"); got != 2 {
		t.Fatalf("Cost = %v，want 2", got)
	}
}

func TestLoadLMCorruptFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lm.bin")
	if err := os.WriteFile(path, []byte("not a model"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLM(path); err == nil {
		t.Fatal("損壞的 lm.bin 應回報錯誤")
	}
}
