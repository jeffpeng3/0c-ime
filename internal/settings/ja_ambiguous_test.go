package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P2-52：日英同形時的優先順序。預設 english 保留舊行為；kana 是明確的選項。
func TestJaAmbiguousDefaultsToEnglishAndPersistsKana(t *testing.T) {
	s := Default()
	if s.JaAmbiguous != JaAmbiguousEnglish || s.JaAmbiguousKanaFirst() {
		t.Fatalf("預設應是英文優先：%+v", s)
	}
	if v, ok := s.Value("ja-ambiguous"); !ok || v != JaAmbiguousEnglish {
		t.Fatalf("Value(ja-ambiguous) = %q %v", v, ok)
	}
	if err := s.Set("ja-ambiguous", JaAmbiguousKana); err != nil {
		t.Fatal(err)
	}
	if s.JaAmbiguous != JaAmbiguousKana || !s.JaAmbiguousKanaFirst() {
		t.Fatalf("かな優先沒有套用：%+v", s)
	}
	path := filepath.Join(t.TempDir(), FileName)
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil || back != s || !back.JaAmbiguousKanaFirst() {
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
	if r.Settings.JaAmbiguous != JaAmbiguousEnglish || len(r.Notes) == 0 {
		t.Fatalf("讀不懂的值應退回英文並說明：%+v %v", r.Settings, r.Notes)
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
	if item.Key != "ja-ambiguous" || item.Default != JaAmbiguousEnglish || item.Value != JaAmbiguousEnglish {
		t.Fatalf("日文選項應有 ja-ambiguous：%+v", item)
	}
	if len(item.Languages) != 1 || item.Languages[0] != LanguageJa {
		t.Fatalf("ja-ambiguous 只在日文模式出現：%v", item.Languages)
	}
	if item.ValueLabels[JaAmbiguousEnglish] == "" || item.ValueLabels[JaAmbiguousKana] == "" {
		t.Fatalf("選項應有顯示名稱：%v", item.ValueLabels)
	}
	if _, ok := Keys()["ja-ambiguous"]; !ok {
		t.Fatal("命令列說明應列出 ja-ambiguous")
	}
}
