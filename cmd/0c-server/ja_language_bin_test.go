package main

import (
	"os"
	"path/filepath"
	"testing"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

func writeJaPack(t *testing.T, dir string) {
	t.Helper()
	entries := []jaimport.Entry{
		{Surface: "今日", Reading: reading.Sequence{"き", "ょ", "う"}, Cost: 1, POS: jaimport.POSNoun},
		{Surface: "京", Reading: reading.Sequence{"き", "ょ", "う"}, Cost: 3, POS: jaimport.POSNoun},
		{Surface: "は", Reading: reading.Sequence{"は"}, Cost: 1, POS: jaimport.POSParticle},
	}
	conns := []jaimport.Connection{
		{Left: jaimport.POSBOS, Right: jaimport.POSNoun, Cost: 0.2},
		{Left: jaimport.POSNoun, Right: jaimport.POSParticle, Cost: 0.2},
		{Left: jaimport.POSParticle, Right: jaimport.POSEOS, Cost: 0.2},
		{Left: jaimport.POSNoun, Right: jaimport.POSEOS, Cost: 0.2},
	}
	lex, err := jaimport.NewLexicon(entries, conns)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "lexicon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lexicon", "words.tsv"), jaimport.TSV(lex.Entries()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lexicon.bin"), lex.Encode(), 0o644); err != nil {
		t.Fatal(err)
	}
	connTSV := "<s>\tnoun\t0.2\nnoun\tparticle\t0.2\nparticle\t</s>\t0.2\nnoun\t</s>\t0.2\n"
	if err := os.WriteFile(filepath.Join(dir, "connection.tsv"), []byte(connTSV), 0o644); err != nil {
		t.Fatal(err)
	}
}

func convert(t *testing.T, dec *jadecode.Decoder) string {
	t.Helper()
	res, err := dec.Convert(reading.Sequence{"き", "ょ", "う", "は"}, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	return jadecode.ResultSurface(res.Clauses)
}

// 同一個目錄有 lexicon.bin 與 lexicon/words.tsv 時走二進位載入器；
// 移除 lexicon.bin 後退回 TSV 路徑，轉換結果不變。
func TestLoadJaDecoderPrefersBin(t *testing.T) {
	dir := t.TempDir()
	writeJaPack(t, dir)

	binDec, binLex, binConn, err := loadJaDecoder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := binLex.(*jalexicon.Bin); !ok {
		t.Fatalf("lexicon = %T, want *lexicon.Bin", binLex)
	}
	if binConn == nil || binLex.(*jalexicon.Bin).Matrix() != binConn {
		t.Fatal("二進位路徑的連接矩陣不一致")
	}
	if got := convert(t, binDec); got != "今日は" {
		t.Fatalf("bin 轉換 = %q", got)
	}

	if err := os.Remove(filepath.Join(dir, "lexicon.bin")); err != nil {
		t.Fatal(err)
	}
	tsvDec, tsvLex, _, err := loadJaDecoder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tsvLex.(*jalexicon.Mem); !ok {
		t.Fatalf("lexicon = %T, want *lexicon.Mem", tsvLex)
	}
	if got := convert(t, tsvDec); got != convert(t, binDec) {
		t.Fatalf("TSV 轉換 = %q，bin = %q", got, convert(t, binDec))
	}
}

// 直接把日文包目錄當 dataDir 時，manifest language=ja 也能辨識；
// 注音包根目錄的 lexicon.bin 不可以被當成日文詞典。
func TestJaDataDirRejectsChinesePackRoot(t *testing.T) {
	zh := t.TempDir()
	if err := os.WriteFile(filepath.Join(zh, "lexicon.bin"), []byte("zh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zh, "manifest.json"), []byte(`{"format":2,"language":"zh"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := jaDataDir(zh); ok {
		t.Fatal("注音包根目錄被當成日文資料")
	}

	ja := t.TempDir()
	if err := os.WriteFile(filepath.Join(ja, "manifest.json"), []byte(`{"format":2,"language":"ja"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ja, "lexicon.bin"), []byte("bin"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir, ok := jaDataDir(ja); !ok || dir != ja {
		t.Fatalf("jaDataDir = %q, %v", dir, ok)
	}
}

// 日文資料在 <data>/ja 時優先使用它，不讀上層注音包的 lexicon.bin。
func TestJaDataDirPrefersSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lexicon.bin"), []byte("zh"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "ja")
	if err := os.MkdirAll(filepath.Join(sub, "lexicon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "lexicon", "words.tsv"), []byte("今日\tきょう\t1\tnoun\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "connection.tsv"), []byte("noun\tnoun\t1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir, ok := jaDataDir(root); !ok || dir != sub {
		t.Fatalf("jaDataDir = %q, %v", dir, ok)
	}
}

func TestJaDataDirMissing(t *testing.T) {
	if _, ok := jaDataDir(t.TempDir()); ok {
		t.Fatal("空目錄應回報沒有日文資料")
	}
	if _, ok := jaDataDir(filepath.Join(t.TempDir(), "absent")); ok {
		t.Fatal("不存在的目錄應回報沒有日文資料")
	}
	// connection.tsv 存在但沒有詞典檔：仍視為缺少日文資料。
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "connection.tsv"), []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := jaDataDir(dir); ok {
		t.Fatal("只有 connection.tsv 不應載入日文")
	}
}
