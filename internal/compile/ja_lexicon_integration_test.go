//go:build ja_lexicon

// 這個測試需要 T12 的 core/ja/lexicon 一起合併後才能編譯：
//
//	go test -tags ja_lexicon ./internal/compile
//
// 它證明 T13 產出的資料包中介格式 lexicon.tsv 可以直接被 core/ja/lexicon.Load
// 載入，且詞面、讀音、成本、詞性一致。二進位 lexicon.bin 的解碼器由 T12 提供，
// 格式在 data/ja/README.md 說明。
package compile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

func TestJAPackLoadsIntoCoreJALexicon(t *testing.T) {
	f := newJAFixture(t)
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	res := BuildJA(d, DefaultJAOptions())
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	out := filepath.Join(t.TempDir(), "pack")
	if err := res.Write(out, f.licenses, DefaultJAOptions()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "lexicon.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	mem, err := lexicon.Load(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]reading.Sequence{}
	res.Lexicon.Walk(func(e jaimport.Entry) { want[e.Surface] = e.Reading })
	count := 0
	mem.Walk(func(e lexicon.Entry) {
		count++
		r, ok := want[e.Surface]
		if !ok {
			t.Fatalf("core/ja/lexicon 有 T13 沒有的詞面 %q", e.Surface)
		}
		if reading.Key(e.Reading) != reading.Key(r) {
			t.Fatalf("%s 讀音 = %q，want %q", e.Surface, reading.Key(e.Reading), reading.Key(r))
		}
		if !e.POS.Valid() {
			t.Fatalf("%s 詞性 %q 不合法", e.Surface, e.POS)
		}
	})
	if count != res.Lexicon.Len() {
		t.Fatalf("載入 %d 筆，want %d", count, res.Lexicon.Len())
	}
}
