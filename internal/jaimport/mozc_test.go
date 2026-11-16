package jaimport

import (
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
)

func TestParseMozc(t *testing.T) {
	src := "# 讀音\t左ID\t右ID\t成本\t詞面\t詞性\n" +
		"いち\t1\t1\t1000\t一\t名詞,数,*,*,*,*,一,イチ,イチ\n" +
		"かく\t2\t2\t1200\t書く\t動詞,自立,*,*,五段・カ行イ音便,基本形,書く,カク,カク\n" +
		"タベル\t3\t3\t900\t食べる\t動詞,自立,*,*,一段,基本形,食べる,タベル,タベル\n"
	entries, err := ParseMozc(strings.NewReader(src), DefaultMozcOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d", len(entries))
	}
	bySurface := map[string]Entry{}
	for _, e := range entries {
		bySurface[e.Surface] = e
	}
	if got := reading.Key(bySurface["一"].Reading); got != "い ち" {
		t.Errorf("一 讀音 = %q", got)
	}
	if bySurface["一"].POS != POSNoun || bySurface["一"].Cost != 1 {
		t.Errorf("一 = %#v", bySurface["一"])
	}
	if bySurface["書く"].POS != POSVerb || bySurface["書く"].Conjugation != ConjGodan {
		t.Errorf("書く = %#v", bySurface["書く"])
	}
	if bySurface["書く"].Okurigana != 1 {
		t.Errorf("書く 送り仮名 = %d", bySurface["書く"].Okurigana)
	}
	if got := reading.Key(bySurface["食べる"].Reading); got != "た べ る" {
		t.Errorf("片假名讀音未轉平假名：%q", got)
	}
	for _, e := range entries {
		if len(e.Origins) != 1 || e.Origins[0] != "ja-mozc" {
			t.Errorf("來源 = %v", e.Origins)
		}
	}
}

func TestParseMozcUsesIDDef(t *testing.T) {
	idDef, err := LoadIDDef(strings.NewReader("10\t名詞,一般,*,*,*,*,*\n11\t動詞,自立,*,*,五段・ラ行,基本形,*\n"))
	if err != nil {
		t.Fatal(err)
	}
	opt := DefaultMozcOptions()
	opt.POSByID = idDef
	src := "あした\t10\t10\t1000\t明日\nとる\t11\t11\t1200\t取る\n"
	entries, err := ParseMozc(strings.NewReader(src), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].POS != POSNoun {
		t.Errorf("明日 POS = %q", entries[0].POS)
	}
	if entries[1].POS != POSVerb || entries[1].Conjugation != ConjGodan {
		t.Errorf("取る = %#v", entries[1])
	}
}

func TestParseMozcRejectsBadRows(t *testing.T) {
	for _, src := range []string{
		"あ\t1\t1\t1000\n",
		"あ\t1\t1\t-1\t亜\n",
		"あ\t1\t1\tabc\t亜\n",
		"あ\t1\t1\t1000\t\n",
		"\t1\t1\t1000\t亜\n",
	} {
		if _, err := ParseMozc(strings.NewReader(src), DefaultMozcOptions()); err == nil {
			t.Errorf("ParseMozc(%q) 應失敗", src)
		}
	}
}

func TestLoadIDDefRejectsBadRows(t *testing.T) {
	for _, src := range []string{"abc\t名詞\n", "1\n", "1\t\n"} {
		if _, err := LoadIDDef(strings.NewReader(src)); err == nil {
			t.Errorf("LoadIDDef(%q) 應失敗", src)
		}
	}
}
