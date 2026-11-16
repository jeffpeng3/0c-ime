package reading

import "testing"

func TestKey(t *testing.T) {
	cases := []struct {
		seq  Sequence
		want string
	}{
		{nil, ""},
		{Sequence{}, ""},
		{Sequence{"ㄓㄨˋ"}, "ㄓㄨˋ"},
		{Sequence{"ㄓㄨˋ", "ㄧ", "ㄉㄧㄢˇ"}, "ㄓㄨˋ ㄧ ㄉㄧㄢˇ"},
		{Sequence{"き", "ょ", "う"}, "き ょ う"},
		// 可変長：兩個字的詞可以對上四個單位。
		{Sequence{"よ", "う", "せ", "い"}, "よ う せ い"},
	}
	for _, c := range cases {
		if got := Key(c.seq); got != c.want {
			t.Errorf("Key(%v) = %q, want %q", c.seq, got, c.want)
		}
	}
}

func TestValid(t *testing.T) {
	for _, u := range []Unit{"ㄓㄨˋ", "き", "ゃ", "ー", "々", "a"} {
		if !Valid(u) {
			t.Errorf("%q 應為合法", u)
		}
	}
	for _, u := range []Unit{"", "き ょ", "き\tょ", "き\n", Unit("\xff")} {
		if Valid(u) {
			t.Errorf("%q 應為不合法", u)
		}
	}
}

// 注音的 Unit 仍是 zhuyin.Syllable，結構檢查照舊。
func TestUnitZhuyinValidity(t *testing.T) {
	if !Unit("ㄓㄨˋ").Valid() {
		t.Error("合法注音音節應通過結構檢查")
	}
	if Unit("").Valid() {
		t.Error("空音節不應通過結構檢查")
	}
}
