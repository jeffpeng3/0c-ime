package compile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
)

// 執行期布局必須直接餵給 0c-server 使用的 core/ja/lexicon 載入器。
func TestWriteJARuntimeExportsEngineLayout(t *testing.T) {
	f := newJAFixture(t)
	d, errs, err := LoadJA(f.dir, f.root)
	if err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", err, errs)
	}
	o := DefaultJAOptions()
	res := BuildJA(d, o)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	out := filepath.Join(t.TempDir(), "pack")
	if err := res.Write(out, f.licenses, o); err != nil {
		t.Fatal(err)
	}
	if err := WriteJARuntime(out, out); err != nil {
		t.Fatal(err)
	}

	words, err := os.ReadFile(filepath.Join(out, JARuntimeWords))
	if err != nil {
		t.Fatal(err)
	}
	mem, err := lexicon.Load(bytes.NewReader(words))
	if err != nil {
		t.Fatalf("引擎詞典載入失敗：%v", err)
	}
	if got := mem.Exact(mustReading(t, "あした")); len(got) != 1 || got[0].Surface != "明日" {
		t.Fatalf("あした = %#v", got)
	}

	conns, err := os.Open(filepath.Join(out, JARuntimeConnections))
	if err != nil {
		t.Fatal(err)
	}
	defer conns.Close()
	matrix, err := lexicon.LoadConnections(conns)
	if err != nil {
		t.Fatalf("引擎連接成本載入失敗：%v", err)
	}
	if got := matrix.Cost(lexicon.POSBOS, lexicon.POSNoun); got != 0.1 {
		t.Fatalf("<s>->noun = %v，want 0.1", got)
	}
	if got := matrix.Cost(lexicon.POSAdverb, lexicon.POSNoun); got != lexicon.DefaultConnectionCost {
		t.Fatalf("未指定詞性對 = %v，want %v", got, lexicon.DefaultConnectionCost)
	}
}
