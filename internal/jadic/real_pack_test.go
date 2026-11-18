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

// referenceWordsSHA256 是參考 pack 在 P2-45F 記号與非日文詞面過濾後，以 mapping D
// 匯入兩次都必須相同的 words.tsv 雜湊。它同時固定 signed 成本、同 surface 排序與
// bytes 輸出格式。
const referenceWordsSHA256 = "c6470e46daabc6c439238e44556aae1152a0067ed158b3033d190ab3ad15aad4"

// P2-45E 之後，words.tsv 不再包含 POS 為記号的 334 筆；其中 1 筆負成本記号丟棄，
// 333 筆寫進 symbols/jadic.tsv。P2-45F 再丟棄詞面不含日文字元的 8792 筆，
// 包含 9 筆負成本垃圾詞；片假名與漢字的負成本詞仍保留。
const (
	referenceWordEntries    = 1137895
	referenceWordSurfaces   = 861582
	referenceSymbolEntries  = 333
	referenceFilteredSyms   = 334
	referenceFilteredNonJap = 8792
	referenceSymbolNeg      = 1
	referenceWordNegatives  = 104
)

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
		if res.Stats.Entries != referenceWordEntries || res.Stats.Surfaces != referenceWordSurfaces {
			t.Fatalf("run %d counts = %d / %d", run, res.Stats.Entries, res.Stats.Surfaces)
		}
		if res.Stats.FilteredSymbols != referenceFilteredSyms ||
			res.Stats.SymbolEntries != referenceSymbolEntries ||
			res.Stats.SymbolNegativeCosts != referenceSymbolNeg {
			t.Fatalf("run %d symbol stats = %+v", run, res.Stats)
		}
		if res.Stats.FilteredNonJapanese != referenceFilteredNonJap {
			t.Fatalf("run %d 非日文詞面 = %d，want %d", run, res.Stats.FilteredNonJapanese, referenceFilteredNonJap)
		}
		if len(res.Stats.NonJapaneseSurfaces) == 0 {
			t.Fatalf("run %d 缺少非日文詞面樣本", run)
		}
		if res.Stats.NegativeCosts != referenceWordNegatives {
			t.Fatalf("run %d 負成本 = %d，want %d", run, res.Stats.NegativeCosts, referenceWordNegatives)
		}

		symbols, err := os.ReadFile(res.SymbolsPath)
		if err != nil {
			t.Fatal(err)
		}
		symbolLines := 0
		for _, line := range strings.Split(string(symbols), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			symbolLines++
			f := strings.Split(line, "\t")
			if len(f) != 4 || f[3] != SymbolCategory {
				t.Fatalf("symbols 欄位不符：%q", line)
			}
		}
		if symbolLines != referenceSymbolEntries {
			t.Fatalf("run %d symbols 行數 = %d，want %d", run, symbolLines, referenceSymbolEntries)
		}
		if strings.Contains(string(symbols), "𝕏") {
			t.Fatalf("run %d 負成本記号 𝕏 不應輸出", run)
		}

		data, err := os.ReadFile(res.WordsPath)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		sums = append(sums, hex.EncodeToString(sum[:]))

		negatives := 0
		var institute, art []string
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) < 3 {
				t.Fatalf("words.tsv 欄位不足：%q", line)
			}
			if !containsJapanese(f[0]) {
				t.Fatalf("run %d words.tsv 含非日文詞面 %q", run, f[0])
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
			if f[0] == "アート" {
				art = append(art, line)
			}
		}
		if negatives != referenceWordNegatives {
			t.Fatalf("run %d words.tsv 負成本列 = %d，want %d", run, negatives, referenceWordNegatives)
		}
		if len(institute) != 2 || !strings.HasPrefix(institute[0], "研究所\tけ ん き ゅ う じ ょ\t-4.215\t") {
			t.Fatalf("研究所 順序 = %#v", institute)
		}
		if len(art) < 2 || !strings.HasPrefix(art[0], "アート\tあ ー と\t-8.313\t") {
			t.Fatalf("負成本片假名 アート 應保留 = %#v", art)
		}
	}
	if sums[0] != sums[1] {
		t.Fatalf("兩次匯入 SHA 不同：%s vs %s", sums[0], sums[1])
	}
	if sums[0] != referenceWordsSHA256 {
		t.Fatalf("words.tsv SHA = %s，want %s", sums[0], referenceWordsSHA256)
	}
}
