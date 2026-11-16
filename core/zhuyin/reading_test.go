package zhuyin_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// reading.Key 對注音序列必須產生與 Key 完全相同的字串，既有個人學習檔才不會失效。
func TestReadingKeyMatchesZhuyinKey(t *testing.T) {
	for _, s := range []string{
		"",
		"ㄓㄨˋ",
		"ㄓㄨˋ ㄧ ㄉㄧㄢˇ",
		"ㄔ ㄇㄜ˙",
		"ㄐㄧㄥˇ ㄍㄞˋ",
	} {
		syls := zhuyin.ParseReading(s)
		if got, want := reading.Key(zhuyin.ReadingUnits(syls)), zhuyin.Key(syls); got != want {
			t.Errorf("%q: reading.Key = %q, want %q", s, got, want)
		}
	}
}

func TestReadingUnitsRoundTrip(t *testing.T) {
	syls := zhuyin.ParseReading("ㄓㄨˋ ㄧ ㄉㄧㄢˇ")
	seq := zhuyin.ReadingUnits(syls)
	if len(seq) != len(syls) {
		t.Fatalf("got %d units, want %d", len(seq), len(syls))
	}
	for i, u := range seq {
		if !reading.Valid(u) {
			t.Errorf("unit %q 應為合法", u)
		}
		if got := zhuyin.SyllableFromUnit(u); got != syls[i] {
			t.Errorf("SyllableFromUnit(%q) = %q, want %q", u, got, syls[i])
		}
	}
	back := zhuyin.Syllables(seq)
	for i := range syls {
		if back[i] != syls[i] {
			t.Errorf("Syllables()[%d] = %q, want %q", i, back[i], syls[i])
		}
	}
	if got := zhuyin.ReadingUnits(nil); got != nil {
		t.Errorf("ReadingUnits(nil) = %#v, want nil", got)
	}
	if got := zhuyin.Syllables(nil); got != nil {
		t.Errorf("Syllables(nil) = %#v, want nil", got)
	}
}

// reading.Unit.Valid 不能與注音的結構檢查分歧。
func TestReadingUnitValidityMatchesZhuyin(t *testing.T) {
	for _, s := range []string{
		"ㄓㄨˋ", "ㄇㄜ˙", "ㄔ", "ㄐㄩㄥˇ", "ㄧ", "ㄦˊ", "ㄅㄆ", "ㄚㄅ", "ˉ", "ㄓˋˊ", "ㄓx", "", "ㄓ ㄨˋ",
	} {
		got, want := reading.Unit(s).Valid(), zhuyin.Syllable(s).Valid()
		if got != want {
			t.Errorf("%q: reading.Unit.Valid = %v, zhuyin.Syllable.Valid = %v", s, got, want)
		}
	}
}
