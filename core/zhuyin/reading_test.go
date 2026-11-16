package zhuyin

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
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
		syls := ParseReading(s)
		if got, want := reading.Key(ReadingUnits(syls)), Key(syls); got != want {
			t.Errorf("%q: reading.Key = %q, want %q", s, got, want)
		}
	}
}

func TestReadingUnitsRoundTrip(t *testing.T) {
	syls := ParseReading("ㄓㄨˋ ㄧ ㄉㄧㄢˇ")
	seq := ReadingUnits(syls)
	if len(seq) != len(syls) {
		t.Fatalf("got %d units, want %d", len(seq), len(syls))
	}
	for i, u := range seq {
		if !u.Valid() {
			t.Errorf("unit %q 應為合法", u)
		}
		if got := SyllableFromUnit(u); got != syls[i] {
			t.Errorf("SyllableFromUnit(%q) = %q, want %q", u, got, syls[i])
		}
	}
	back := Syllables(seq)
	for i := range syls {
		if back[i] != syls[i] {
			t.Errorf("Syllables()[%d] = %q, want %q", i, back[i], syls[i])
		}
	}
	if got := ReadingUnits(nil); got != nil {
		t.Errorf("ReadingUnits(nil) = %#v, want nil", got)
	}
	if got := Syllables(nil); got != nil {
		t.Errorf("Syllables(nil) = %#v, want nil", got)
	}
}
