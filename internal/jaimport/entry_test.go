package jaimport

import (
	"math"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
)

func TestNormalizeReading(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"アシタ", "あした"},
		{"ヴァイオリン", "ゔぁいおりん"},
		{"スーパー", "すーぱー"},
		{"あ し た", "あした"},
		{"ヵヶ", "ゕゖ"},
	} {
		seq, err := NormalizeReading(tc.in)
		if err != nil {
			t.Fatalf("NormalizeReading(%q): %v", tc.in, err)
		}
		if got := strings.ReplaceAll(reading.Key(seq), " ", ""); got != tc.want {
			t.Errorf("NormalizeReading(%q) = %q，want %q", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "  ", "abc", "あ\tた", "、", "。", "！", "？", "・", "あ。た"} {
		if _, err := NormalizeReading(bad); err == nil {
			t.Errorf("NormalizeReading(%q) 應失敗", bad)
		}
	}
	for _, punct := range []string{"、", "。", "！", "？", "・"} {
		_, err := NormalizeReading(punct)
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), "かな") {
			t.Errorf("NormalizeReading(%q) 的錯誤應說明只接受かな：%v", punct, err)
		}
	}
}

func TestOkuriganaLength(t *testing.T) {
	for _, tc := range []struct {
		surface string
		reading string
		want    int
	}{
		{"食べる", "たべる", 2},
		{"書く", "かく", 1},
		{"明日", "あした", 0},
		{"行った", "いった", 2},
		{"こんにちは", "こんにちは", 5},
		{"アヒル", "あひる", 3},
	} {
		seq, err := NormalizeReading(tc.reading)
		if err != nil {
			t.Fatal(err)
		}
		if got := OkuriganaLength(tc.surface, seq); got != tc.want {
			t.Errorf("OkuriganaLength(%q, %q) = %d，want %d", tc.surface, tc.reading, got, tc.want)
		}
	}
}

func TestMapPOSAndConjugation(t *testing.T) {
	for in, want := range map[string]POS{
		"名詞,固有名詞,地域,一般": POSNoun,
		"動詞,自立,*,*":     POSVerb,
		"形容詞,自立,*,*":    POSAdjective,
		"形容動詞,非自立,*,*":  POSAdjectivalNoun,
		"形状詞,一般,*,*":    POSAdjectivalNoun,
		"形状,*,*,*":      POSAdjectivalNoun,
		"shape":         POSAdjectivalNoun,
		"shape_noun":    POSAdjectivalNoun,
		"助詞,格助詞,一般,*":   POSParticle,
		"助動詞,*,*,*":     POSAuxiliary,
		"接頭詞,名詞接続,*,*":  POSPrefix,
		"接尾,助数詞,*,*":    POSSuffix,
		"連体詞,*,*,*":     POSDeterminer,
		"感動詞,*,*,*":     POSInterjection,
		"数詞,*,*,*":      POSNumber,
		"記号,句点,*,*":     POSSymbol,
		"unknown-pos":   POSOther,
		"noun":          POSNoun,
	} {
		if got := MapPOS(in); got != want {
			t.Errorf("MapPOS(%q) = %q，want %q", in, got, want)
		}
	}
	for in, want := range map[string]Conjugation{
		"五段-カ行":     ConjGodan,
		"一段-一段動詞":   ConjIchidan,
		"サ行変格":      ConjSuru,
		"カ行変格":      ConjKuru,
		"形容詞・アウオ段":  ConjIAdjective,
		"形容動詞・助動詞":  ConjNaAdjective,
		"*":         ConjNone,
		"-":         ConjNone,
		"godan":     ConjGodan,
		"something": ConjUnknown,
	} {
		if got := MapConjugation(in); got != want {
			t.Errorf("MapConjugation(%q) = %q，want %q", in, got, want)
		}
	}
}

func TestMapPOSShapeNoun(t *testing.T) {
	// UniDic 與 Sudachi 把な形容詞標成形状詞；漏接會落到 POSOther。
	for _, in := range []string{
		"形状詞",
		"形状詞,一般,*,*",
		"形状",
		"形状,一般,*,*",
		"shape",
		"shape_noun",
		"Shape",
		" 形状詞 ,一般,*,*",
	} {
		if got := MapPOS(in); got != POSAdjectivalNoun {
			t.Errorf("MapPOS(%q) = %q，want %q", in, got, POSAdjectivalNoun)
		}
	}
	if got := MapPOS("形状詞,一般,*,*"); got == POSOther {
		t.Fatal("形状詞 不應落到 POSOther")
	}
}

func TestMergeKeepsMinimumCostAndOrigins(t *testing.T) {
	r, _ := NormalizeReading("あした")
	a := MakeEntry("明日", r, 2, POSNoun, []string{"b"}, ConjNone)
	b := MakeEntry("明日", r, 1, POSNoun, []string{"a"}, ConjNone)
	got := Merge([]Entry{a, b})
	if len(got) != 1 {
		t.Fatalf("Merge = %#v", got)
	}
	if got[0].Cost != 1 {
		t.Fatalf("成本 = %v，want 1", got[0].Cost)
	}
	if strings.Join(got[0].Origins, ",") != "a,b" {
		t.Fatalf("來源 = %v", got[0].Origins)
	}
}

func TestTSVKeepsSignedCost(t *testing.T) {
	r, _ := NormalizeReading("ふ")
	out := string(TSV([]Entry{MakeEntry("負", r, -8.313, POSNoun, []string{"curated"}, ConjNone)}))
	if !strings.Contains(out, "負\tふ\t-8.313\tnoun\n") {
		t.Fatalf("TSV = %q", out)
	}
}

func TestEntryValidation(t *testing.T) {
	r, _ := NormalizeReading("あ")
	for _, e := range []Entry{
		{Reading: r, POS: POSNoun},
		{Surface: "あ", POS: POSNoun},
		{Surface: "あ", Reading: reading.Sequence{" "}, POS: POSNoun},
		{Surface: "あ", Reading: r, Cost: MinWordCost - 0.001, POS: POSNoun},
		{Surface: "あ", Reading: r, Cost: MaxWordCost + 0.001, POS: POSNoun},
		{Surface: "あ", Reading: r, Cost: math.NaN(), POS: POSNoun},
		{Surface: "あ", Reading: r, POS: "bad"},
		{Surface: "あ", Reading: r, POS: POSNoun, Okurigana: 2},
	} {
		if err := e.Validate(); err == nil {
			t.Errorf("Validate(%#v) 應失敗", e)
		}
	}
	if err := MakeEntry("あ", r, 1, POSNoun, nil, ConjNone).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := MakeEntry("あ", r, -8.313, POSNoun, nil, ConjNone).Validate(); err != nil {
		t.Fatalf("負成本應被接受：%v", err)
	}
}
