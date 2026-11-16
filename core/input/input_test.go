package input

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// Hyp 的讀音欄位是核心讀音單位，注音音節與かな都能放進來。
func TestHypUsesReadingUnit(t *testing.T) {
	var h Hyp
	var _ reading.Unit = h.Syl // 編譯期確認欄位型別

	lat := Lattice{
		{{Syl: reading.Unit("き")}},
		{{Syl: reading.Unit("ょ")}},
		{{Syl: reading.Unit("う")}},
	}
	seq := make(reading.Sequence, len(lat))
	for i, hs := range lat {
		seq[i] = hs[0].Syl
	}
	if got := reading.Key(seq); got != "き ょ う" {
		t.Errorf("Key = %q, want %q", got, "き ょ う")
	}
}

// 精確通道的輸出也要是 reading.Unit，既有的注音行為不變。
func TestExactProducesReadingUnits(t *testing.T) {
	lat := Exact{}.Expand(zhuyin.ParseReading("ㄓㄨˋ ㄧ"))
	if len(lat) != 2 || len(lat[0]) != 1 || len(lat[1]) != 1 {
		t.Fatalf("Lattice = %+v", lat)
	}
	if got := reading.Key(reading.Sequence{lat[0][0].Syl, lat[1][0].Syl}); got != "ㄓㄨˋ ㄧ" {
		t.Errorf("Key = %q", got)
	}
}
