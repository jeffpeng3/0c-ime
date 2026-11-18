package jadic

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	jaRewrite "github.com/0c-biggo/0c-ime/core/ja/rewrite"
)

// symbolSamplePack 混入記号、片假名真詞與負成本記号，固定 P2-45E 的過濾契約。
func symbolSamplePack(t *testing.T) []byte {
	t.Helper()
	return encodeTestPack(t, []testEntry{
		{surface: "明日", reading: "あした", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		{surface: "メガ", reading: "めが", pos: "名詞,普通名詞,助数詞可能,*", conjType: "*", conjForm: "*", cost: 2787},
		{surface: "ア", reading: "あ", pos: "記号,一般,*,*", conjType: "*", conjForm: "*", cost: 7595},
		{surface: "㋐", reading: "あ", pos: "記号,一般,*,*", conjType: "*", conjForm: "*", cost: 4439},
		{surface: "𝕏", reading: "えっくす", pos: "記号,文字,*,*", conjType: "*", conjForm: "*", cost: -1609},
	}, []string{
		"名詞,普通名詞,一般,*",
		"名詞,普通名詞,助数詞可能,*",
		"記号,一般,*,*",
		"記号,文字,*,*",
	}, nil, nil)
}

func TestConvertFiltersSymbolsToSeparateFile(t *testing.T) {
	p, err := Parse(symbolSamplePack(t))
	if err != nil {
		t.Fatal(err)
	}
	var words, symbols bytes.Buffer
	stats, err := Convert(&words, p, ConvertOptions{Symbols: &symbols})
	if err != nil {
		t.Fatal(err)
	}

	out := words.String()
	for _, surface := range []string{"ア\t", "㋐\t", "𝕏\t"} {
		if strings.Contains(out, surface) {
			t.Fatalf("記号 %q 不應寫進 words.tsv：\n%s", strings.TrimSuffix(surface, "\t"), out)
		}
	}
	if !strings.Contains(out, "メガ\tめ が\t2.787\tnoun\t") {
		t.Fatalf("片假名名詞應保留：\n%s", out)
	}
	if stats.Entries != 2 || stats.Surfaces != 2 {
		t.Fatalf("words stats = %+v", stats)
	}
	if stats.FilteredSymbols != 3 || stats.SymbolEntries != 2 || stats.SymbolNegativeCosts != 1 {
		t.Fatalf("symbol stats = %+v", stats)
	}
	if stats.NegativeCosts != 0 {
		t.Fatalf("NegativeCosts 只算 words.tsv，得到 %d", stats.NegativeCosts)
	}
	if len(stats.KatakanaSymbols) != 1 || !strings.Contains(stats.KatakanaSymbols[0], "ア") {
		t.Fatalf("片假名記号樣本 = %#v", stats.KatakanaSymbols)
	}

	want := strings.Join([]string{
		"# 由 0c-jadic pack 匯入；讀音\t詞面\t描述\t分類",
		"あ\tア\t一般\t記号",
		"あ\t㋐\t一般\t記号",
		"",
	}, "\n")
	if symbols.String() != want {
		t.Fatalf("symbols TSV = %q，want %q", symbols.String(), want)
	}
	// 負成本記号 𝕏 必須丟棄，不能以任何形式出現在記号辭典。
	if strings.Contains(symbols.String(), "𝕏") || strings.Contains(symbols.String(), "えっくす") {
		t.Fatalf("負成本記号不應輸出：\n%s", symbols.String())
	}

	// 產出的四欄格式必須能直接被產品的記号辭典載入。
	entries, err := jaRewrite.LoadSymbols(strings.NewReader(symbols.String()))
	if err != nil {
		t.Fatalf("記号辭典載入失敗：%v", err)
	}
	if len(entries) != 2 || entries[0].Category != SymbolCategory {
		t.Fatalf("entries = %#v", entries)
	}

	// words.tsv 仍要能被產品詞典載入。
	if _, err := jalexicon.Load(strings.NewReader(out)); err != nil {
		t.Fatalf("words.tsv 載入失敗：%v", err)
	}
}

func TestConvertWithoutSymbolWriterStillFilters(t *testing.T) {
	p, err := Parse(symbolSamplePack(t))
	if err != nil {
		t.Fatal(err)
	}
	var words bytes.Buffer
	stats, err := Convert(&words, p, ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 2 || stats.FilteredSymbols != 3 || stats.SymbolEntries != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	if strings.Contains(words.String(), "㋐") {
		t.Fatalf("沒有 Symbols writer 時仍不得把記号寫進 words.tsv：\n%s", words.String())
	}
}

func TestImportWritesSymbolFileAndReceipt(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "ja-lexicon-public-core.pack")
	if err := os.WriteFile(packPath, symbolSamplePack(t), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	m := sampleManifest(p)
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, append(mb, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out")
	res, err := Import(p, loaded, ImportOptions{
		OutDir:       out,
		PackPath:     packPath,
		ManifestPath: manifestPath,
		Origin:       "ja-jadic-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.SymbolsPath != filepath.Join(out, "symbols", "jadic.tsv") {
		t.Fatalf("SymbolsPath = %q", res.SymbolsPath)
	}
	symbols, err := os.ReadFile(res.SymbolsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(symbols), "あ\tア\t一般\t記号\n") {
		t.Fatalf("symbols/jadic.tsv = %q", symbols)
	}
	words, err := os.ReadFile(res.WordsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(words), "㋐") {
		t.Fatalf("words.tsv 含記号：\n%s", words)
	}

	receipt, err := os.ReadFile(res.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var r ImportReceipt
	if err := json.Unmarshal(receipt, &r); err != nil {
		t.Fatal(err)
	}
	if r.Schema != ImportSchema {
		t.Fatalf("schema = %q", r.Schema)
	}
	if r.Import.Entries != 2 || r.Import.FilteredSymbols != 3 ||
		r.Import.SymbolEntries != 2 || r.Import.SymbolNegativeCosts != 1 {
		t.Fatalf("receipt import = %+v", r.Import)
	}
	if !containsString(r.Import.Files, "symbols/jadic.tsv") {
		t.Fatalf("receipt files = %v", r.Import.Files)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
