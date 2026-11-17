package jadic

import (
	"os"
	"path/filepath"
	"testing"
	"unicode"
)

// 參考 pack 不進 repo。設 OC_JADIC_REFERENCE_PACK 指向
// ja-lexicon-public-core.pack（manifest.json 放在旁邊）才會執行。
const referencePackEnv = "OC_JADIC_REFERENCE_PACK"

func TestReferencePack(t *testing.T) {
	path := os.Getenv(referencePackEnv)
	if path == "" {
		t.Skipf("未設定 %s，跳過真實 pack 測試", referencePackEnv)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.EntryCount() != 1147021 || p.SurfaceCount() != 870298 {
		t.Fatalf("counts = %d / %d", p.EntryCount(), p.SurfaceCount())
	}
	if got, want := p.FileSHA256(), "8e57a18deb64d0fdc8147b53b5e234ecda412fa49feb0e8096addc4349582840"; got != want {
		t.Fatalf("整檔 SHA = %s", got)
	}
	if got, want := p.PayloadSHA256(), "b52ec1fe4224e1ca68d91e6fb82e699d7a12e8a1a610e4e3e6611a7da15ef946"; got != want {
		t.Fatalf("payload SHA = %s", got)
	}
	h := p.Header()
	if h.FormatVersion != 1 || h.POSCount != 71 || h.ConjTypeCount != 140 || h.ConjFormCount != 62 {
		t.Fatalf("header = %+v", h)
	}
	st := p.Stats()
	if st.CostMin != -8313 || st.CostMax != 23263 || st.NegativeCosts != 114 {
		t.Fatalf("stats = %+v", st)
	}
	if st.MaxReadingUnit > 64 {
		t.Fatalf("最長讀音 %d 單位超過產品上限 64", st.MaxReadingUnit)
	}

	manifestPath := filepath.Join(filepath.Dir(path), "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		m, err := LoadManifest(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Verify(p); err != nil {
			t.Fatal(err)
		}
		if m.Lexicon != "public-core" || m.ReadingScript != "hiragana" {
			t.Fatalf("manifest = %+v", m)
		}
	} else {
		t.Logf("找不到 %s，只驗證 pack", manifestPath)
	}

	entries := p.Lookup("明日")
	if len(entries) == 0 {
		t.Fatal("參考 pack 應包含 明日")
	}
	for _, e := range entries {
		for _, r := range e.Reading {
			if !unicode.Is(unicode.Hiragana, r) && r != 'ー' {
				t.Fatalf("讀音 %q 含非平假名字元 %q", e.Reading, r)
			}
		}
	}
}
