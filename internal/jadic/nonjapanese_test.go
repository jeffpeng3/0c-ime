package jadic

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
)

// nonJapaneseSamplePack 固定 P2-45F 的過濾契約：非日文詞面丟棄，負成本的真日文詞保留。
func nonJapaneseSamplePack(t *testing.T) []byte {
	t.Helper()
	return encodeTestPack(t, []testEntry{
		// 應丟棄：emoji、全形拉丁、ASCII 拉丁、數字、符號。
		{surface: "🍛", reading: "かれー", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: -2934},
		{surface: "🍻", reading: "びーる", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		{surface: "ｆｕｌｌ", reading: "ふる", pos: "形状詞,一般,*,*", conjType: "*", conjForm: "*", cost: -1366},
		{surface: "ＧＲＥＡＴ", reading: "ぐれーと", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		{surface: "ASCII", reading: "あすきー", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		{surface: "123", reading: "ひゃくにじゅうさん", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		{surface: "0", reading: "ぜろ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		{surface: "№", reading: "ばんごー", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		// 應保留：片假名與漢字，即使成本為負。
		{surface: "アート", reading: "あーと", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: -8313},
		{surface: "漢字", reading: "かんじ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		// 半形片假名也是片假名，不能誤丟。
		{surface: "ｱｰﾄ", reading: "あーと", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		// POS 記号優先計入記号過濾，不重複算成非日文詞面。
		{surface: "★", reading: "ほし", pos: "記号,一般,*,*", conjType: "*", conjForm: "*", cost: 500},
	}, []string{
		"名詞,普通名詞,一般,*",
		"形状詞,一般,*,*",
		"記号,一般,*,*",
	}, nil, nil)
}

func TestConvertDropsNonJapaneseSurfaces(t *testing.T) {
	p, err := Parse(nonJapaneseSamplePack(t))
	if err != nil {
		t.Fatal(err)
	}
	var words, symbols bytes.Buffer
	stats, err := Convert(&words, p, ConvertOptions{Symbols: &symbols})
	if err != nil {
		t.Fatal(err)
	}

	out := words.String()
	for _, surface := range []string{"🍛", "🍻", "ｆｕｌｌ", "ＧＲＥＡＴ", "ASCII", "123"} {
		if strings.Contains(out, surface) {
			t.Fatalf("非日文詞面 %q 不應寫進 words.tsv：\n%s", surface, out)
		}
	}
	for _, want := range []string{
		"アート\tあ ー と\t-8.313\tnoun\t",
		"漢字\tか ん じ\t1\tnoun\t",
		"ｱｰﾄ\tあ ー と\t1\tnoun\t",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("應保留的日文詞 %q 不在 words.tsv：\n%s", want, out)
		}
	}

	if stats.Entries != 3 || stats.Surfaces != 3 {
		t.Fatalf("words stats = %+v", stats)
	}
	if stats.FilteredNonJapanese != 8 {
		t.Fatalf("FilteredNonJapanese = %d，want 8", stats.FilteredNonJapanese)
	}
	if stats.FilteredSymbols != 1 || stats.SymbolEntries != 1 {
		t.Fatalf("symbol 過濾必須獨立計數：%+v", stats)
	}
	if stats.NegativeCosts != 1 {
		t.Fatalf("NegativeCosts 只算保留下來的 words，得到 %d", stats.NegativeCosts)
	}
	if len(stats.NonJapaneseSurfaces) != nonJapaneseSampleLimit {
		t.Fatalf("樣本數 = %d，want %d：%#v", len(stats.NonJapaneseSurfaces), nonJapaneseSampleLimit, stats.NonJapaneseSurfaces)
	}
	joined := strings.Join(stats.NonJapaneseSurfaces, "")
	for _, want := range []string{"🍛", "ｆｕｌｌ"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("丟棄樣本缺少 %q：%#v", want, stats.NonJapaneseSurfaces)
		}
	}
	if strings.Contains(joined, "★") {
		t.Fatalf("POS 記号不應出現在非日文樣本：%#v", stats.NonJapaneseSurfaces)
	}
	if !strings.Contains(symbols.String(), "★") {
		t.Fatalf("記号 ★ 應進 symbols 辭典：\n%s", symbols.String())
	}

	// 過濾後仍必須是產品詞典可載入的格式。
	if _, err := jalexicon.Load(strings.NewReader(out)); err != nil {
		t.Fatalf("words.tsv 載入失敗：%v", err)
	}
}

func TestImportReceiptReportsNonJapaneseFilter(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "ja-lexicon-public-core.pack")
	if err := os.WriteFile(packPath, nonJapaneseSamplePack(t), 0o644); err != nil {
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
	if res.Stats.FilteredNonJapanese != 8 || res.Stats.Entries != 3 || res.Stats.Surfaces != 3 {
		t.Fatalf("stats = %+v", res.Stats)
	}
	receipt, err := os.ReadFile(res.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var r ImportReceipt
	if err := json.Unmarshal(receipt, &r); err != nil {
		t.Fatal(err)
	}
	if r.Import.FilteredNonJapanese != 8 || r.Import.Entries != 3 {
		t.Fatalf("receipt import = %+v", r.Import)
	}
	if len(r.Import.NonJapaneseSurfaces) != nonJapaneseSampleLimit {
		t.Fatalf("receipt 樣本 = %#v", r.Import.NonJapaneseSurfaces)
	}
	words, err := os.ReadFile(res.WordsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(words), "🍛") || strings.Contains(string(words), "ASCII") {
		t.Fatalf("words.tsv 含非日文詞面：\n%s", words)
	}
}
