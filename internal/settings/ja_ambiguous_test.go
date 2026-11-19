package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P2-54：日英同形預設 both，英文與かな並列；english／kana 維持 P2-52 的意義。
func TestJaAmbiguousDefaultsToBothAndPersistsAllModes(t *testing.T) {
	s := Default()
	if s.JaAmbiguous != JaAmbiguousBoth || s.JaAmbiguousKanaFirst() || !s.JaAmbiguousListsBoth() {
		t.Fatalf("預設應是日英並列：%+v", s)
	}
	if v, ok := s.Value("ja-ambiguous"); !ok || v != JaAmbiguousBoth {
		t.Fatalf("Value(ja-ambiguous) = %q %v", v, ok)
	}
	for _, mode := range []string{JaAmbiguousEnglish, JaAmbiguousKana, JaAmbiguousBoth} {
		if err := s.Set("ja-ambiguous", mode); err != nil {
			t.Fatal(err)
		}
		if s.JaAmbiguous != mode {
			t.Fatalf("Set(%q) = %q", mode, s.JaAmbiguous)
		}
		if s.JaAmbiguousKanaFirst() != (mode == JaAmbiguousKana) || s.JaAmbiguousListsBoth() != (mode == JaAmbiguousBoth) {
			t.Fatalf("%s 的引擎投影不對：%+v", mode, s)
		}
	}
	path := filepath.Join(t.TempDir(), FileName)
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil || back != s || !back.JaAmbiguousListsBoth() {
		t.Fatalf("來回不一致：%+v %v", back, err)
	}
}

func TestJaAmbiguousRejectsUnknownValuesAndKeepsOldOnes(t *testing.T) {
	s := Default()
	before := s
	var se *SetError
	if err := s.Set("ja-ambiguous", "maybe"); !errors.As(err, &se) || se.Code != CodeInvalidValue || se.Key != "ja-ambiguous" || s != before {
		t.Fatalf("不認得的值應拒絕：%v", err)
	}
	path := filepath.Join(t.TempDir(), FileName)
	body := `{"schema":2,"ja_ambiguous":"maybe"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Settings.JaAmbiguous != JaAmbiguousBoth || !r.Settings.JaAmbiguousListsBoth() || len(r.Notes) == 0 {
		t.Fatalf("讀不懂的值應退回日英並列並說明：%+v %v", r.Settings, r.Notes)
	}
	if err := Save(path, r.Settings); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"ja_ambiguous": "maybe"`) {
		t.Fatalf("讀不懂的值應原樣保留：%s", b)
	}
}

func TestJaAmbiguousAppearsWithJapaneseSettings(t *testing.T) {
	s := Default()
	var item Item
	for _, it := range s.LanguageItems() {
		if it.Key == "ja-ambiguous" {
			item = it
		}
	}
	if item.Key != "ja-ambiguous" || item.Default != JaAmbiguousBoth || item.Value != JaAmbiguousBoth {
		t.Fatalf("日文選項應有 ja-ambiguous：%+v", item)
	}
	if len(item.Languages) != 1 || item.Languages[0] != LanguageJa {
		t.Fatalf("ja-ambiguous 只在日文模式出現：%v", item.Languages)
	}
	for _, mode := range []string{JaAmbiguousBoth, JaAmbiguousEnglish, JaAmbiguousKana} {
		if item.ValueLabels[mode] == "" {
			t.Fatalf("選項應有顯示名稱：%v", item.ValueLabels)
		}
	}
	if _, ok := Keys()["ja-ambiguous"]; !ok {
		t.Fatal("命令列說明應列出 ja-ambiguous")
	}
}
