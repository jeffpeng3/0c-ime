package jadic

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// 參考 pack 不進 repo。設 OC_JADIC_REFERENCE_PACK 指向
// ja-lexicon-public-core.pack（manifest.json 放在旁邊）才會執行。
const referencePackEnv = "OC_JADIC_REFERENCE_PACK"

// referenceWordsSHA256 是參考 pack 以 mapping D 匯入兩次都必須相同的 words.tsv 雜湊。
// 它同時固定 signed 成本、同 surface 排序與 bytes 輸出格式。
const referenceWordsSHA256 = "5b6718fdf558d3883a1b807f63b1dce17485c86276ab54dda98bfbe99ef99f5f"

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

func TestReferencePackImportStable(t *testing.T) {
	path := os.Getenv(referencePackEnv)
	if path == "" {
		t.Skipf("未設定 %s，跳過真實 pack 測試", referencePackEnv)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(filepath.Dir(path), "manifest.json")
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("載入 manifest：%v", err)
	}

	var sums []string
	for run := 0; run < 2; run++ {
		out := t.TempDir()
		res, err := Import(p, m, ImportOptions{
			OutDir:       out,
			PackPath:     path,
			ManifestPath: manifestPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Stats.Entries != 1147021 || res.Stats.Surfaces != 870298 {
			t.Fatalf("run %d counts = %d / %d", run, res.Stats.Entries, res.Stats.Surfaces)
		}
		if res.Stats.NegativeCosts != 114 {
			t.Fatalf("run %d 負成本 = %d，want 114", run, res.Stats.NegativeCosts)
		}

		data, err := os.ReadFile(res.WordsPath)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		sums = append(sums, hex.EncodeToString(sum[:]))

		negatives := 0
		var institute []string
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) < 3 {
				t.Fatalf("words.tsv 欄位不足：%q", line)
			}
			cost, err := strconv.ParseFloat(f[2], 64)
			if err != nil {
				t.Fatalf("成本 %q：%v", f[2], err)
			}
			if cost < 0 {
				negatives++
			}
			if f[0] == "研究所" {
				institute = append(institute, line)
			}
		}
		if negatives != 114 {
			t.Fatalf("run %d words.tsv 負成本列 = %d，want 114", run, negatives)
		}
		if len(institute) != 2 || !strings.HasPrefix(institute[0], "研究所\tけ ん き ゅ う じ ょ\t-4.215\t") {
			t.Fatalf("研究所 順序 = %#v", institute)
		}
	}
	if sums[0] != sums[1] {
		t.Fatalf("兩次匯入 SHA 不同：%s vs %s", sums[0], sums[1])
	}
	if sums[0] != referenceWordsSHA256 {
		t.Fatalf("words.tsv SHA = %s，want %s", sums[0], referenceWordsSHA256)
	}
}
