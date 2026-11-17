package compile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/rewrite"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

type jaFixture struct {
	root, dir, licenses string
}

func newJAFixture(t *testing.T) jaFixture {
	t.Helper()
	root := t.TempDir()
	f := jaFixture{root: root, dir: filepath.Join(root, "ja"), licenses: filepath.Join(root, "sources", "licenses")}
	mustWrite(t, filepath.Join(root, "sources", "ledger.tsv"), jaLedger("yes"))
	mustWrite(t, filepath.Join(f.licenses, "curated.txt"), "Curated fixture license\n")
	mustWrite(t, filepath.Join(f.licenses, "ja-mozc.txt"), "Mozc mixed license fixture\n")
	mustWrite(t, filepath.Join(f.dir, "lexicon", "words.tsv"), `# 詞面	讀音	成本	詞性	來源	活用型
明日	あした	1	noun	curated	-
書く	かく	1.2	verb	curated	godan
食べる	たべる	1	verb	curated	ichidan
は	は	0.2	particle	curated	-
`)
	mustWrite(t, filepath.Join(f.dir, "connection.tsv"), `<s>	noun	0.1
noun	</s>	0.2
<s>	verb	0.15
particle	verb	0.5
`)
	mustWrite(t, filepath.Join(f.dir, "lm", "unigram.tsv"), `明日	100	curated
一	80	curated
は	500	curated
`)
	mustWrite(t, filepath.Join(f.dir, "lm", "bigram.tsv"), `明日	は	0.7	curated
一	は	0.6	curated
`)
	mustWrite(t, filepath.Join(f.dir, "import", "ja-mozc.id.def"), "1\t名詞,数,*,*,*,*,*\n")
	mustWrite(t, filepath.Join(f.dir, "import", "ja-mozc.mozc.tsv"), "いち\t1\t1\t1000\t一\t名詞,数,*,*,*,*,一,イチ,イチ\n")
	return f
}

func jaLedger(dist string) string {
	return "# id\t名稱\tURL\t版本\t授權\t可散布\t姓名標示\n" +
		"curated\t0c 人工維護的詞條\thttps://0c.biggo.com\tfixture-v1\tCC-BY-SA-4.0\tyes\tFunmula Co., Limited\n" +
		"ja-mozc\tMozc OSS 日文詞典\thttps://example.org/mozc\tfixture-v1\tBSD-3-Clause AND NAIST-IPAdic AND ICOT-Free-Software\t" + dist + "\tMozc authors\n"
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildJAFixture(t *testing.T) (*JAData, *JAResult) {
	t.Helper()
	f := newJAFixture(t)
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) > 0 {
		t.Fatalf("LoadJA errors: %v", errs)
	}
	res := BuildJA(d, DefaultJAOptions())
	if len(res.Errors) > 0 {
		t.Fatalf("BuildJA errors: %v", res.Errors)
	}
	return d, res
}

func TestLoadAndBuildJA(t *testing.T) {
	d, res := buildJAFixture(t)
	if len(d.Entries) != 5 {
		t.Fatalf("來源詞條 = %d，want 5", len(d.Entries))
	}
	if res.Report.Entries != 5 {
		t.Fatalf("編譯詞條 = %d，want 5", res.Report.Entries)
	}
	if res.Report.Connections != 4 || res.Report.Bigrams != 2 {
		t.Fatalf("連接/bigram = %d/%d", res.Report.Connections, res.Report.Bigrams)
	}
	if res.Report.LexiconBytes == 0 || res.Report.LMBytes == 0 || res.Report.RuntimeEstimate == 0 {
		t.Fatalf("大小報告未填：%+v", res.Report)
	}
	var ids []string
	for _, s := range res.Sources {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "curated,ja-mozc" {
		t.Fatalf("來源 = %v", ids)
	}
	if got := res.Lexicon.Exact(mustReading(t, "あした")); len(got) != 1 || got[0].Surface != "明日" {
		t.Fatalf("Exact = %#v", got)
	}
	if got := res.Lexicon.Exact(mustReading(t, "いち")); len(got) != 1 || got[0].POS != "noun" {
		t.Fatalf("Mozc 詞條 = %#v", got)
	}
}

func TestJAAcceptsSignedWordCost(t *testing.T) {
	f := newJAFixture(t)
	mustWrite(t, filepath.Join(f.dir, "lexicon", "words.tsv"), `# 詞面	讀音	成本	詞性	來源	活用型
負	ふ	-8.313	noun	curated	-
零	れい	0	noun	curated	-
正	せい	2.5	noun	curated	-
`)
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	res := BuildJA(d, DefaultJAOptions())
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	for reading, want := range map[string]float64{"ふ": -8.313, "れい": 0, "せい": 2.5} {
		got := res.Lexicon.Exact(mustReading(t, reading))
		if len(got) != 1 || got[0].Cost != want {
			t.Fatalf("%s = %#v，want cost %v", reading, got, want)
		}
	}
}

func TestJARejectsWordCostOutOfRange(t *testing.T) {
	for _, cost := range []string{"-32.768", "32.768", "NaN", "+Inf"} {
		f := newJAFixture(t)
		mustWrite(t, filepath.Join(f.dir, "lexicon", "words.tsv"), "# 詞面\t讀音\t成本\t詞性\t來源\t活用型\n負\tふ\t"+cost+"\tnoun\tcurated\t-\n")
		_, errs, err := LoadJA(f.dir, f.root)
		if err != nil {
			t.Fatalf("cost %s：%v", cost, err)
		}
		if len(errs) == 0 {
			t.Errorf("cost %s 應被拒絕", cost)
		}
	}
}

func TestJAConjugationExpansion(t *testing.T) {
	f := newJAFixture(t)
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	o := DefaultJAOptions()
	o.ExpandConjugations = true
	res := BuildJA(d, o)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	if res.Report.Entries <= 5 || res.Report.Conjugated == 0 {
		t.Fatalf("活用展開沒有增加詞條：%+v", res.Report)
	}
	if got := res.Lexicon.Exact(mustReading(t, "かいた")); len(got) != 1 || got[0].Surface != "書いた" {
		t.Fatalf("書いた = %#v", got)
	}
}

func TestJAWritePublicAndDeterministic(t *testing.T) {
	f := newJAFixture(t)
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	o := DefaultJAOptions()
	o.Public = true
	res := BuildJA(d, o)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	out1 := filepath.Join(t.TempDir(), "pack")
	out2 := filepath.Join(t.TempDir(), "pack")
	for _, out := range []string{out1, out2} {
		res2 := BuildJA(d, o)
		if err := res2.Write(out, f.licenses, o); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{"lexicon.bin", "lm.bin", "lexicon.tsv", "manifest.json", "NOTICE.txt", "report.txt"}
	for _, name := range names {
		a, err := os.ReadFile(filepath.Join(out1, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(out2, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%s 兩次編譯不同", name)
		}
	}
	mb, err := os.ReadFile(filepath.Join(out1, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m JAManifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatal(err)
	}
	if m.Format != 2 || m.Language != "ja" {
		t.Fatalf("manifest = %+v", m)
	}
	if strings.Join(m.InputMethods, ",") != "romaji,kana" || strings.Join(m.OutputVariants, ",") != "ja" {
		t.Fatalf("manifest 軸 = %+v", m)
	}
	if m.Limits.MaxReadingUnits != 64 || m.Limits.MaxWordUnits != 32 || m.Limits.MaxCandidates != 16 {
		t.Fatalf("limits = %+v", m.Limits)
	}
	for name, info := range m.Files {
		b, err := os.ReadFile(filepath.Join(out1, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if info.SHA256 != hex.EncodeToString(sum[:]) || info.Size != len(b) {
			t.Fatalf("%s 雜湊不符", name)
		}
	}
	if len(m.Inputs) != 6 {
		t.Fatalf("inputs = %+v", m.Inputs)
	}
	if m.SourceHash == "" || len(m.Sources) != 2 {
		t.Fatalf("manifest 來源 = %+v", m)
	}
	notice, _ := os.ReadFile(filepath.Join(out1, "NOTICE.txt"))
	if !strings.Contains(string(notice), "Mozc mixed license fixture") ||
		!strings.Contains(string(notice), "Curated fixture license") {
		t.Fatalf("NOTICE 缺少授權全文：%s", notice)
	}
	if got, err := ManifestLanguage(mb); err != nil || got != "ja" {
		t.Fatalf("ManifestLanguage = %q, %v", got, err)
	}
}

func TestJAPublicRejectsIncompleteSources(t *testing.T) {
	f := newJAFixture(t)
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	o := DefaultJAOptions()
	o.Public = true
	res := BuildJA(d, o)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	if err := os.Remove(filepath.Join(f.licenses, "ja-mozc.txt")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "pack")
	if err := res.Write(out, f.licenses, o); err == nil {
		t.Fatal("缺少授權全文仍出版")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("拒絕出版卻建立輸出目錄")
	}

	o.Public = false
	if err := res.Write(out, f.licenses, o); err != nil {
		t.Fatal("研究包不應被授權檢查阻擋：", err)
	}
}

func TestJARejectsRestrictedSource(t *testing.T) {
	f := newJAFixture(t)
	mustWrite(t, filepath.Join(f.root, "sources", "ledger.tsv"), jaLedger("no"))
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	o := DefaultJAOptions()
	o.Public = true
	res := BuildJA(d, o)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	if err := res.Write(filepath.Join(t.TempDir(), "pack"), f.licenses, o); err == nil {
		t.Fatal("不可散布來源仍出版")
	}
}

func TestJABuildRejectsBadData(t *testing.T) {
	f := newJAFixture(t)
	o := DefaultJAOptions()
	o.Limits.MaxWordUnits = 2
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	res := BuildJA(d, o)
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Error(), "超過上限") {
		t.Fatalf("讀音上限未生效：%v", res.Errors)
	}

	f = newJAFixture(t)
	mustWrite(t, filepath.Join(f.dir, "lexicon", "words.tsv"), "幽霊\tゆうれい\t1\tnoun\tghost\t-\n")
	d, errs, err = LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	res = BuildJA(d, DefaultJAOptions())
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Error(), "不在帳本內") {
		t.Fatalf("未知來源未報錯：%v", res.Errors)
	}
}

func TestLoadJARejectsUnknownImportOrigin(t *testing.T) {
	f := newJAFixture(t)
	mustWrite(t, filepath.Join(f.dir, "import", "ghost.mozc.tsv"), "あ\t1\t1\t1000\t亜\t名詞\n")
	_, errs, err := LoadJA(f.dir, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "不在帳本內") {
		t.Fatalf("未知匯入來源 = %v", errs)
	}
}

func TestManifestLanguageDefaultsToZhuyin(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		want string
	}{
		{"舊格式", `{"format":1,"files":{}}`, "zh"},
		{"缺語言", `{"format":2,"runtime":{}}`, "zh"},
		{"空語言", `{"format":2,"language":""}`, "zh"},
		{"注音", `{"format":2,"language":"zh"}`, "zh"},
		{"日文", `{"format":2,"language":"ja"}`, "ja"},
	} {
		if got, err := ManifestLanguage([]byte(tc.json)); err != nil || got != tc.want {
			t.Errorf("%s：ManifestLanguage = %q, %v，want %q", tc.name, got, err, tc.want)
		}
	}
	if _, err := ManifestLanguage([]byte("{")); err == nil {
		t.Fatal("壞掉的 JSON 應失敗")
	}
}

func TestSourceHashIgnoresPaths(t *testing.T) {
	a := map[string]FileInfo{"ja/a.tsv": {SHA256: "aa", Size: 1}, "ja/b.tsv": {SHA256: "bb", Size: 2}}
	b := map[string]FileInfo{"x/a.tsv": {SHA256: "aa", Size: 1}, "y/b.tsv": {SHA256: "bb", Size: 2}}
	if sourceHash(a) != sourceHash(b) {
		t.Fatal("來源雜湊不應隨路徑改變")
	}
	b["x/b.tsv"] = FileInfo{SHA256: "cc", Size: 2}
	if sourceHash(a) == sourceHash(b) {
		t.Fatal("內容不同應得到不同雜湊")
	}
}

func TestJASeedLexiconReadingsValid(t *testing.T) {
	root := filepath.Join("..", "..", "data")
	d, errs, err := LoadJA(filepath.Join(root, "ja"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) > 0 {
		t.Fatalf("種子詞典載入失敗：%v", errs)
	}
	if len(d.Entries) == 0 {
		t.Fatal("種子詞典沒有詞條")
	}
	for _, e := range d.Entries {
		if e.POS == jaimport.POSSymbol && slices.Contains(e.Origins, "curated") {
			t.Fatalf("curated 語音詞典不應有 symbol 詞條：%q", e.Surface)
		}
		for _, u := range e.Reading {
			if !u.Valid() {
				t.Fatalf("%q 的讀音單位 %q 未通過 reading.Unit.Valid", e.Surface, u)
			}
		}
	}
}

func TestJARejectsPunctuationReading(t *testing.T) {
	f := newJAFixture(t)
	mustWrite(t, filepath.Join(f.dir, "lexicon", "words.tsv"), `# 詞面	讀音	成本	詞性	來源	活用型
明日	あした	1	noun	curated	-
。	。	0.2	symbol	curated	-
`)
	_, errs, err := LoadJA(f.dir, f.root)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(errs)
	if len(errs) == 0 {
		t.Fatal("標點讀音沒有被拒絕")
	}
	for _, want := range []string{"讀音", "かな", "。"} {
		if !strings.Contains(got, want) {
			t.Fatalf("錯誤訊息缺少 %q：%s", want, got)
		}
	}
}

func TestJABuildRejectsPunctuationReading(t *testing.T) {
	d, _ := buildJAFixture(t)
	d.Entries = append(d.Entries, jaimport.MakeEntry("。", reading.Sequence{"。"}, 0.2, jaimport.POSSymbol, []string{"curated"}, jaimport.ConjNone))
	res := BuildJA(d, DefaultJAOptions())
	if len(res.Errors) == 0 {
		t.Fatal("BuildJA 接受標點讀音")
	}
	if !strings.Contains(fmt.Sprint(res.Errors), "讀音單位") {
		t.Fatalf("BuildJA 錯誤沒有指出讀音單位：%v", res.Errors)
	}
}

func TestJASymbolCandidatesComeFromRewriter(t *testing.T) {
	symbols, err := rewrite.LoadSymbolsFS(os.DirFS(filepath.Join("..", "..", "data", "ja")), "symbols")
	if err != nil {
		t.Fatal(err)
	}
	if symbols.Len() == 0 {
		t.Fatal("T22 記号辭典是空的")
	}
	for _, r := range []string{"きごう", "かおもじ"} {
		if len(symbols.Lookup(r)) == 0 {
			t.Fatalf("T22 記号辭典查不到 %q", r)
		}
	}
}

func mustReading(t *testing.T, s string) reading.Sequence {
	t.Helper()
	var out reading.Sequence
	for _, r := range strings.ReplaceAll(s, " ", "") {
		out = append(out, reading.Unit(r))
	}
	return out
}
