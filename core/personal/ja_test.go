package personal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	japersonal "github.com/0c-biggo/0c-ime/core/ja/personal"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/atomicfile"
)

func saveJa(t *testing.T, path string, st *japersonal.Store) {
	t.Helper()
	var buf bytes.Buffer
	if err := st.Save(&buf); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.Write(path, buf.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestJaPathUsesSameDirectoryAndSeparateFile(t *testing.T) {
	dir := t.TempDir()
	for _, zh := range []string{"user.json", "personal.json"} {
		got := JaPath(filepath.Join(dir, zh))
		want := filepath.Join(dir, "personal.ja.json")
		if got != want {
			t.Fatalf("JaPath(%s) = %q, want %q", zh, got, want)
		}
	}
}

func TestJaFileNameMatchesJapaneseStore(t *testing.T) {
	if JaFileName != japersonal.FileName {
		t.Fatalf("JaFileName = %q，日文套件是 %q", JaFileName, japersonal.FileName)
	}
}

func TestJapaneseLearningNeverTouchesZhuyinStore(t *testing.T) {
	dir := t.TempDir()
	zhPath := filepath.Join(dir, "user.json")
	zhBytes := []byte(`{"format":7,"bi":{"我\t是":1},"readings":{"我\tㄨㄛˇ":true}}`)
	if err := os.WriteFile(zhPath, zhBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	jaPath := JaPath(zhPath)
	st := japersonal.NewStore()
	st.Record(japersonal.Selection{
		Reading: reading.Sequence{reading.Unit("き"), reading.Unit("ょ"), reading.Unit("う")},
		Surface: "今日",
		Left:    "",
	})
	saveJa(t, jaPath, st)
	if got, err := os.ReadFile(zhPath); err != nil || string(got) != string(zhBytes) {
		t.Fatalf("注音學習檔被改了：%q, %v", got, err)
	}

	// 重置日文學習只清日文檔，注音檔位元組不變。
	reloaded, err := japersonal.LoadFile(os.DirFS(dir), japersonal.FileName)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.Reset()
	saveJa(t, jaPath, reloaded)
	if got, err := os.ReadFile(zhPath); err != nil || string(got) != string(zhBytes) {
		t.Fatalf("重置日文後注音學習檔被改了：%q, %v", got, err)
	}
	if got, err := japersonal.LoadFile(os.DirFS(dir), japersonal.FileName); err != nil || got.Len() != 0 {
		t.Fatalf("日文學習應已清空：%v, %v", got, err)
	}
}
