package jadic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

// mozcFixture 建立最小可用的 Mozc dictionary_oss 目錄。11 個非 BOS 的 ID
// 各代表一種詞性，讓 POS 聚合與 context 矩陣都能直接算出預期值。
func mozcFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	idDef := strings.Join([]string{
		"0 BOS/EOS,*,*,*,*,*,*",
		"1 名詞,一般,*,*,*,*,*",
		"2 名詞,形容動詞語幹,*,*,*,*,*",
		"3 名詞,数,*,*,*,*,*",
		"4 名詞,接尾,*,*,*,*,*",
		"5 動詞,自立,*,*,五段・ラ行,基本形,*",
		"6 動詞,非自立,*,*,五段・ラ行,基本形,*",
		"7 形容詞,自立,*,*,形容詞・アウオ段,基本形,*",
		"8 助詞,格助詞,一般,*,*,*,*",
		"9 接続詞,*,*,*,*,*,*",
		"10 記号,一般,*,*,*,*,*",
		"11 フィラー,*,*,*,*,*,*",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "id.def"), []byte(idDef), 0o644); err != nil {
		t.Fatal(err)
	}

	dictionary := strings.Join([]string{
		"あした\t1\t1\t1000\t明日",
		"しずか\t2\t2\t2000\t静か",
		"いち\t3\t3\t3000\t一",
		"てき\t4\t4\t4000\t的",
		"かく\t5\t5\t5000\t書く",
		"いく\t6\t6\t6000\t行く",
		"あかい\t7\t7\t7000\t赤い",
		"を\t8\t8\t8000\tを",
		"しかし\t9\t9\t9000\tしかし",
		"!\t10\t10\t100\t!",
		"ふぃらー\t11\t11\t1000\tフィラー",
		"ほげ\t1\t1\t1000\t或るパカ",
		"10がつ\t3\t3\t1000\t10月",
		"あした\t1\t1\t1200\t明日",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "dictionary00.txt"), []byte(dictionary), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 10; i++ {
		path := filepath.Join(dir, fmt.Sprintf("dictionary%02d.txt", i))
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "dictionary_filter.tsv"), []byte("ほげ\t或るパカ\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aux_dictionary.tsv"), []byte("しんご\t新語\tあした\t明日\t-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 12
	var conn strings.Builder
	fmt.Fprintf(&conn, "%d\n", n)
	for rid := 0; rid < n; rid++ {
		for lid := 0; lid < n; lid++ {
			fmt.Fprintf(&conn, "%d\n", rid*100+lid)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "connection_single_column.txt"), []byte(conn.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMapMozcPOS(t *testing.T) {
	cases := map[string]jaimport.POS{
		"名詞,一般,*,*,*,*,*":        jaimport.POSNoun,
		"名詞,形容動詞語幹,*,*,*,*,*":    jaimport.POSAdjectivalNoun,
		"名詞,数,*,*,*,*,*":         jaimport.POSNumber,
		"名詞,接尾,一般,*,*,*,*":       jaimport.POSSuffix,
		"動詞,自立,*,*,五段・ラ行,基本形,*":  jaimport.POSVerb,
		"動詞,非自立,*,*,五段・ラ行,基本形,*": jaimport.POSAuxiliary,
		"動詞,接尾,*,*,*,*,*":        jaimport.POSSuffix,
		"形容詞,自立,*,*,*,*,*":       jaimport.POSAdjective,
		"形容詞,接尾,*,*,*,*,*":       jaimport.POSSuffix,
		"助詞,格助詞,一般,*,*,*,*":      jaimport.POSParticle,
		"助動詞,*,*,*,特殊・ダ,連用形,*":   jaimport.POSAuxiliary,
		"接続詞,*,*,*,*,*,*":        jaimport.POSConjunction,
		"連体詞,*,*,*,*,*,*":        jaimport.POSDeterminer,
		"記号,一般,*,*,*,*,*":        jaimport.POSSymbol,
		"フィラー,*,*,*,*,*,*":       jaimport.POSInterjection,
		"その他,間投,*,*,*,*,*":       jaimport.POSInterjection,
		"BOS/EOS,*,*,*,*,*,*":    jaimport.POSBOS,
		"未知,*,*,*,*,*,*":         jaimport.POSOther,
	}
	for name, want := range cases {
		if got := MapMozcPOS(name); got != want {
			t.Errorf("MapMozcPOS(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestImportMozcWritesRuntimeLayout(t *testing.T) {
	src := mozcFixture(t)
	out := filepath.Join(t.TempDir(), "out")
	res, err := ImportMozc(MozcImportOptions{
		PackageDir:     src,
		OutDir:         out,
		ConnectionMode: "median",
		SourceRepo:     "https://github.com/google/mozc",
		SourceCommit:   "test-commit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.FilteredSymbols != 1 || res.Stats.SymbolEntries != 1 {
		t.Fatalf("symbol stats = %+v", res.Stats)
	}
	if res.Stats.FilteredReading != 1 {
		t.Fatalf("非かな讀音統計 = %+v", res.Stats)
	}
	if res.Stats.AddedAuxEntries != 1 {
		t.Fatalf("aux 統計 = %+v", res.Stats)
	}
	if res.Stats.DuplicateEntries != 1 {
		t.Fatalf("重複統計 = %+v", res.Stats)
	}

	words, err := os.ReadFile(res.WordsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(words)
	for _, want := range []string{
		"明日\tあ し た\t1\tnoun\tja-mozc\t\t1\t1\n",
		"静か\tし ず か\t2\tadjectival_noun\tja-mozc\t\t2\t2\n",
		"一\tい ち\t3\tnumber\tja-mozc\t\t3\t3\n",
		"的\tて き\t4\tsuffix\tja-mozc\t\t4\t4\n",
		"行く\tい く\t6\tauxiliary\tja-mozc\tgodan\t6\t6\n",
		"フィラー\tふ ぃ ら ー\t1\tinterjection\tja-mozc\t\t11\t11\n",
		"新語\tし ん ご\t0.999\tnoun\tja-mozc\t\t1\t1\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("words.tsv 缺少 %q：\n%s", want, text)
		}
	}
	for _, bad := range []string{"或るパカ", "10月", "!\t"} {
		if strings.Contains(text, bad) {
			t.Errorf("words.tsv 不應有 %q：\n%s", bad, text)
		}
	}
	lex, err := jalexicon.Load(strings.NewReader(text))
	if err != nil {
		t.Fatalf("words.tsv 載入失敗：%v", err)
	}
	if got := lex.Lookup("静か"); len(got) != 1 || got[0].POS != jalexicon.POSAdjectivalNoun {
		t.Fatalf("静か = %#v", got)
	}

	symbols, err := os.ReadFile(res.SymbolsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(symbols), "!\t!\t一般\t記号\n") {
		t.Fatalf("symbols = %s", symbols)
	}

	connText, err := os.ReadFile(res.ConnectionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<s>\tnoun\t0.001\n",
		"noun\tparticle\t0.108\n",
		"noun\t</s>\t0.1\n",
	} {
		if !strings.Contains(string(connText), want) {
			t.Errorf("connection.tsv 缺少 %q：\n%s", want, connText)
		}
	}

	// context 矩陣保留原始 12×12 成本，scale 與詞成本相同。
	bf, err := os.Open(filepath.Join(out, "connection.bin"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := jalexicon.LoadContextMatrix(bf)
	bf.Close()
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Size() != 12 || ctx.Scale() != DefaultCostScale {
		t.Fatalf("context size/scale = %d/%v", ctx.Size(), ctx.Scale())
	}
	if got := ctx.ContextCost(1, 8); got != 0.108 {
		t.Fatalf("ContextCost(1,8) = %v", got)
	}

	receipt, err := os.ReadFile(res.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var rec MozcImportReceipt
	if err := json.Unmarshal(receipt, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Repository != "https://github.com/google/mozc" || rec.Commit != "test-commit" {
		t.Fatalf("receipt 來源 = %#v", rec)
	}
	if len(rec.SourceFiles) != 14 {
		t.Fatalf("source files = %d, want 14", len(rec.SourceFiles))
	}
	for _, f := range rec.SourceFiles {
		if f.SHA256 == "" {
			t.Errorf("來源檔雜湊不完整：%#v", f)
		}
	}
}

func TestImportMozcRefusesCheckedInData(t *testing.T) {
	src := mozcFixture(t)
	if _, err := ImportMozc(MozcImportOptions{PackageDir: src, OutDir: "data/ja"}); err == nil {
		t.Fatal("輸出到 data/ja 應失敗")
	}
}
