package decode_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
)

func scriptLexicon(t *testing.T) *lexicon.Mem {
	t.Helper()
	m := lexicon.NewMem()
	for _, e := range []lexicon.Entry{
		{Surface: "私", Reading: units("わたし"), Cost: 7.159, POS: lexicon.POSOther},
		{Surface: "ワタ", Reading: units("わた"), Cost: 7.194, POS: lexicon.POSNoun},
		{Surface: "私話", Reading: units("しわ"), Cost: 5.468, POS: lexicon.POSNoun},
		{Surface: "わ", Reading: units("わ"), Cost: 11.128, POS: lexicon.POSParticle},
		{Surface: "です", Reading: units("です"), Cost: 8.222, POS: lexicon.POSAuxiliary},
		{Surface: "デス", Reading: units("です"), Cost: 2.984, POS: lexicon.POSNoun},
		{Surface: "メール", Reading: units("めーる"), Cost: -0.252, POS: lexicon.POSNoun},
		{Surface: "めーる", Reading: units("めーる"), Cost: 7.769, POS: lexicon.POSNoun},
	} {
		if err := m.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func convertSurface(t *testing.T, dec *decode.Decoder, reading string) string {
	t.Helper()
	res, err := dec.Convert(units(reading), lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	return decode.ResultSurface(res.Clauses)
}

// 平假名讀音遇到成本接近的片假名詞面時，PreferHiragana 必須保留漢字／平假名。
func TestPreferHiraganaKeepsPlainSurfaces(t *testing.T) {
	lex := scriptLexicon(t)
	plain := &decode.Decoder{Lex: lex}
	if got := convertSurface(t, plain, "わたしわ"); got != "ワタ私話" {
		t.Fatalf("沒有偏好時 = %q，want ワタ私話（重現原始排序）", got)
	}
	if got := convertSurface(t, plain, "です"); got != "デス" {
		t.Fatalf("沒有偏好時 = %q，want デス（重現原始排序）", got)
	}

	preferred := &decode.Decoder{Lex: lex, PreferHiragana: true}
	if got := convertSurface(t, preferred, "わたしわ"); got != "私わ" {
		t.Fatalf("PreferHiragana わたしわ = %q，want 私わ", got)
	}
	if got := convertSurface(t, preferred, "です"); got != "です" {
		t.Fatalf("PreferHiragana です = %q，want です", got)
	}
}

// 片假名外來語仍要有原本的成本優勢，不能被平假名偏好改成平假名。
func TestPreferHiraganaKeepsKatakanaLoanwords(t *testing.T) {
	dec := &decode.Decoder{Lex: scriptLexicon(t), PreferHiragana: true}
	if got := convertSurface(t, dec, "めーる"); got != "メール" {
		t.Fatalf("外來語 = %q，want メール", got)
	}
}
