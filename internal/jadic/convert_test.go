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

func convertSamplePack(t *testing.T) []byte {
	t.Helper()
	return encodeTestPack(t, []testEntry{
		{surface: "明日", reading: "あした", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		{surface: "明日", reading: "あす", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 2500},
		{surface: "食べる", reading: "たべる", pos: "動詞,一般,*,*", conjType: "五段", conjForm: "終止形-一般", cost: -300},
		{surface: "謎", reading: "なぞ", pos: "未知品詞,*,*,*", conjType: "*", conjForm: "*", cost: 500},
	}, []string{"名詞,普通名詞,一般,*", "動詞,一般,*,*", "未知品詞,*,*,*"}, []string{"*", "五段"}, []string{"*", "終止形-一般"})
}

func TestConvertToProductTSV(t *testing.T) {
	p, err := Parse(convertSamplePack(t))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	stats, err := Convert(&buf, p, ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "#") {
		t.Fatal("缺少註解行")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 {
		t.Fatalf("行數 = %d：\n%s", len(lines), out)
	}
	if !strings.Contains(out, "明日\tあ し た\t1\tnoun\t"+DefaultOrigin+"\t\n") {
		t.Fatalf("明日 詞條格式不符：\n%s", out)
	}
	if !strings.Contains(out, "食べる\tた べ る\t0\tverb\t"+DefaultOrigin+"\tgodan\n") {
		t.Fatalf("負成本或活用型不符：\n%s", out)
	}
	if !strings.Contains(out, "謎\tな ぞ\t0.5\tother\t") {
		t.Fatalf("未知 POS 應映射成 other：\n%s", out)
	}
	if stats.Entries != 4 || stats.Surfaces != 3 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.NegativeCostClamped != 1 || stats.UnknownPOS != 1 || stats.UnknownConj != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.MaxReadingUnits != 3 {
		t.Fatalf("MaxReadingUnits = %d", stats.MaxReadingUnits)
	}

	// 產出的 TSV 必須能直接被產品日文詞典載入。
	lex, err := jalexicon.Load(strings.NewReader(out))
	if err != nil {
		t.Fatalf("產品詞典載入失敗：%v", err)
	}
	if got := lex.Lookup("明日"); len(got) != 2 {
		t.Fatalf("Lookup(明日) = %#v", got)
	}
	if got := lex.Lookup("食べる"); len(got) != 1 || got[0].Cost != 0 {
		t.Fatalf("Lookup(食べる) = %#v", got)
	}
}

func TestConvertRejectsOverlongReading(t *testing.T) {
	long := strings.Repeat("あ", 65)
	data := encodeTestPack(t, []testEntry{
		{surface: "長", reading: long, pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1},
	}, nil, nil, nil)
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Convert(&buf, p, ConvertOptions{MaxReadingUnits: 64}); err == nil {
		t.Fatal("超過讀音上限應失敗")
	}
}

func TestImportWritesDataDir(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "ja-lexicon-public-core.pack")
	if err := os.WriteFile(packPath, convertSamplePack(t), 0o644); err != nil {
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

	connPath := filepath.Join(dir, "connection.tsv")
	connBody := []byte("<s>\tnoun\t0.1\nnoun\t</s>\t0.2\n")
	if err := os.WriteFile(connPath, connBody, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out")
	res, err := Import(p, loaded, ImportOptions{
		OutDir:          out,
		PackPath:        packPath,
		ManifestPath:    manifestPath,
		ConnectionsPath: connPath,
		Origin:          "ja-jadic-test",
		CostScale:       0.002,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Entries != 4 || res.Stats.Surfaces != 3 {
		t.Fatalf("stats = %+v", res.Stats)
	}
	words, err := os.ReadFile(filepath.Join(out, "lexicon", "words.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jalexicon.Load(bytes.NewReader(words)); err != nil {
		t.Fatalf("輸出詞典無法載入：%v", err)
	}
	if got, err := os.ReadFile(filepath.Join(out, "connection.tsv")); err != nil || !bytes.Equal(got, connBody) {
		t.Fatalf("connection.tsv = %q, %v", got, err)
	}
	receipt, err := os.ReadFile(filepath.Join(out, "jadic-import.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r ImportReceipt
	if err := json.Unmarshal(receipt, &r); err != nil {
		t.Fatal(err)
	}
	if r.Schema != ImportSchema || r.Pack.SHA256 != p.FileSHA256() || r.Pack.Path != packPath {
		t.Fatalf("receipt = %+v", r)
	}
	if r.Import.Origin != "ja-jadic-test" || r.Import.CostScale != 0.002 || r.Import.NegativeCostClamped != 1 {
		t.Fatalf("receipt import = %+v", r.Import)
	}
	if len(r.Import.Needs) != 0 {
		t.Fatalf("已提供 connection.tsv，needs = %v", r.Import.Needs)
	}

	// 未加 Force 不得覆寫。
	if _, err := Import(p, loaded, ImportOptions{OutDir: out}); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("重複匯入錯誤 = %v", err)
	}
	if _, err := Import(p, loaded, ImportOptions{OutDir: out, Force: true}); err != nil {
		t.Fatalf("Force 匯入失敗：%v", err)
	}
}

func TestImportWithoutConnectionsRecordsNeed(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "p.pack")
	if err := os.WriteFile(packPath, convertSamplePack(t), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	m := sampleManifest(p)
	out := filepath.Join(dir, "out")
	res, err := Import(p, m, ImportOptions{OutDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Receipt.Import.Needs) != 1 || !strings.Contains(res.Receipt.Import.Needs[0], "connection.tsv") {
		t.Fatalf("needs = %v", res.Receipt.Import.Needs)
	}
}
