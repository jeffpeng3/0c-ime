package jaimport

import (
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
)

func TestParseSudachiCSV(t *testing.T) {
	src := "" +
		"明日,名詞,副詞可能,*,*,*,*,アシタ,明日,明日,0,0,0,0,*,*,*\n" +
		"書く,動詞,自立,*,*,五段-カ行,終止形-一般,カク,書く,書く,0,0,0,0,*,*,*\n" +
		"静か,形容動詞,語幹,*,*,*,*,シズカ,静か,静か,0,0,0,0,*,*,*\n" +
		"綺麗,形状詞,一般,*,*,*,*,キレイ,綺麗,綺麗,0,0,0,0,*,*,*\n"
	entries, err := ParseSudachi(strings.NewReader(src), DefaultSudachiOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %#v", entries)
	}
	by := map[string]Entry{}
	for _, e := range entries {
		by[e.Surface] = e
	}
	if got := reading.Key(by["明日"].Reading); got != "あ し た" {
		t.Errorf("明日 讀音 = %q", got)
	}
	if by["明日"].POS != POSNoun {
		t.Errorf("明日 POS = %q", by["明日"].POS)
	}
	if by["書く"].POS != POSVerb || by["書く"].Conjugation != ConjGodan {
		t.Errorf("書く = %#v", by["書く"])
	}
	if by["静か"].POS != POSAdjectivalNoun || by["静か"].Conjugation != ConjNaAdjective {
		t.Errorf("静か = %#v", by["静か"])
	}
	if by["綺麗"].POS != POSAdjectivalNoun || by["綺麗"].Conjugation != ConjNaAdjective {
		t.Errorf("綺麗 = %#v", by["綺麗"])
	}
	for _, e := range entries {
		if e.Cost <= 0 {
			t.Errorf("%s 成本 = %v", e.Surface, e.Cost)
		}
		if len(e.Origins) != 1 || e.Origins[0] != "ja-sudachidict" {
			t.Errorf("%s 來源 = %v", e.Surface, e.Origins)
		}
	}
}

func TestParseSudachiCustomCostAndErrors(t *testing.T) {
	opt := DefaultSudachiOptions()
	opt.Cost = func(string, POS, int) float64 { return 7.5 }
	entries, err := ParseSudachi(strings.NewReader(
		"山,名詞,一般,*,*,*,*,ヤマ,山,山,0,0,0,0,*,*,*\n"), opt)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Cost != 7.5 {
		t.Fatalf("成本 = %v", entries[0].Cost)
	}
	for _, src := range []string{
		"山,名詞\n",
		"山,名詞,一般,*,*,*,*,,山,山,0,0,0,0,*,*,*\n",
	} {
		if _, err := ParseSudachi(strings.NewReader(src), DefaultSudachiOptions()); err == nil {
			t.Errorf("ParseSudachi(%q) 應失敗", src)
		}
	}
	// 空行與註解略過。
	entries, err = ParseSudachi(strings.NewReader("\n# comment\n山,名詞,一般,*,*,*,*,ヤマ,山,山,0,0,0,0,*,*,*\n"), DefaultSudachiOptions())
	if err != nil || len(entries) != 1 {
		t.Fatalf("註解列：%v, %#v", err, entries)
	}
}
