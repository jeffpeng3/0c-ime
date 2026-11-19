package decode_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
)

// TestContextMatrixChangesPath 證明 runtime 真的使用 Mozc 的 left/right ID，
// 而不是只把連接成本聚合成 POS。兩條候選詞面與 POS 相同，只有 context ID
// 不同；POS 矩陣選 A，context 矩陣選 B。
func TestContextMatrixChangesPath(t *testing.T) {
	lex := lexicon.NewMem()
	for _, e := range []lexicon.Entry{
		{Surface: "A", Reading: units("あ"), Cost: 1, POS: lexicon.POSNoun, LeftID: 1, RightID: 1, HasContext: true},
		{Surface: "B", Reading: units("あ"), Cost: 2, POS: lexicon.POSNoun, LeftID: 2, RightID: 2, HasContext: true},
	} {
		if err := lex.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	pos := lexicon.NewMatrix()
	pos.Set(lexicon.POSBOS, lexicon.POSNoun, 0)
	pos.Set(lexicon.POSNoun, lexicon.POSEOS, 0)

	plain := &decode.Decoder{Lex: lex, Conn: pos}
	res, err := plain.Convert(units("あ"), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode.ResultSurface(res.Clauses); got != "A" {
		t.Fatalf("沒有 context 矩陣時 = %q, want A", got)
	}

	ctx, err := lexicon.NewContextMatrix(3, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	// BOS -> A 的左 ID 1 很貴；B 的左 ID 2 免費。句尾都免費。
	ctx.Set(0, 1, 2)
	ctx.Set(0, 2, 0)
	ctx.Set(1, 0, 0)
	ctx.Set(2, 0, 0)
	conn := &lexicon.Connections{POS: pos, Context: ctx}
	withContext := &decode.Decoder{Lex: lex, Conn: conn}
	res, err = withContext.Convert(units("あ"), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode.ResultSurface(res.Clauses); got != "B" {
		t.Fatalf("context 矩陣時 = %q, want B", got)
	}
}
