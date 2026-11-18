package session

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/proto"
	rd "github.com/0c-biggo/0c-ime/core/reading"
)

func scriptSessionLexicon(t *testing.T) *lexicon.Mem {
	t.Helper()
	m := lexicon.NewMem()
	for _, e := range []lexicon.Entry{
		{Surface: "私", Reading: rd.Sequence{"わ", "た", "し"}, Cost: 7.159, POS: lexicon.POSOther},
		{Surface: "ワタ", Reading: rd.Sequence{"わ", "た"}, Cost: 7.194, POS: lexicon.POSNoun},
		{Surface: "私話", Reading: rd.Sequence{"し", "わ"}, Cost: 5.468, POS: lexicon.POSNoun},
		{Surface: "わ", Reading: rd.Sequence{"わ"}, Cost: 11.128, POS: lexicon.POSParticle},
		{Surface: "です", Reading: rd.Sequence{"で", "す"}, Cost: 8.222, POS: lexicon.POSAuxiliary},
		{Surface: "デス", Reading: rd.Sequence{"で", "す"}, Cost: 2.984, POS: lexicon.POSNoun},
	} {
		if err := m.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

// 羅馬字輸入的日文片段要保留平假名／漢字詞面：watashiwa 是 私わ，不是
// 成本相近的片假名拼法 ワタ私話；desu 是 です，不是 デス。
func TestJapaneseRomajiKeepsPlainSurfaces(t *testing.T) {
	s := japaneseSession(t, scriptSessionLexicon(t))

	typeSessionKeys(s, "watashiwa")
	if snap := key(s, proto.KeyConvert); snap.Text() != "私わ" {
		t.Fatalf("watashiwa 変換 = %q，want 私わ", snap.Text())
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "私わ" {
		t.Fatalf("watashiwa 送出 = %q，want 私わ", snap.Commit)
	}

	typeSessionKeys(s, "desu")
	if snap := key(s, proto.KeyConvert); snap.Text() != "です" {
		t.Fatalf("desu 変換 = %q，want です", snap.Text())
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "です" {
		t.Fatalf("desu 送出 = %q，want です", snap.Commit)
	}
}

// 連續的 Latin 識別字不能因為前綴剛好是合法羅馬字就整段被日文 runtime 接手；
// 空白要保留，三個詞各自成為 en 片段。
func TestJapaneseInferenceKeepsSpacedLatinWords(t *testing.T) {
	s := japaneseSession(t, nil)
	typeSessionKeys(s, "kubectl pCloud GitHub ")

	var words []string
	for _, seg := range s.Segments() {
		if seg.Script.String() != "latin" {
			continue
		}
		if seg.Language.Language != "en" {
			t.Fatalf("Latin 片段 %q 語言 = %q，want en", seg.Surface, seg.Language.Language)
		}
		words = append(words, seg.Surface)
	}
	if len(words) != 3 || words[0] != "kubectl" || words[1] != "pCloud" || words[2] != "GitHub" {
		t.Fatalf("Latin 片段 = %#v，want [kubectl pCloud GitHub]", words)
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "kubectl pCloud GitHub " {
		t.Fatalf("送出 = %q，want 完整空白", snap.Commit)
	}
}
