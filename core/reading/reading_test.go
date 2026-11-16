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

func TestUnitValidKana(t *testing.T) {
	for _, u := range []Unit{"き", "ゃ", "ー", "々", "ゝ", "ゔ", "カ", "ヿ", "ｷ", "ｶﾞ", "き\u3099"} {
		if !u.Valid() {
			t.Errorf("%q 應為合法かな單位", u)
		}
		if !Valid(u) {
			t.Errorf("Valid(%q) 應與 Unit.Valid 一致", u)
		}
	}
}

func TestUnitValidZhuyin(t *testing.T) {
	for _, u := range []Unit{"ㄓㄨˋ", "ㄧ", "ㄇㄜ˙", "ㄔ", "ㄐㄩㄥˇ"} {
		if !u.Valid() {
			t.Errorf("%q 應為合法注音音節", u)
		}
	}
}

func TestUnitInvalid(t *testing.T) {
	for _, u := range []Unit{
		"",
		"き ょ",
		"き\tょ",
		"き\n",
		Unit("\xff"),
		"a", "1", "。", "きょ", // 拉丁、數字、標點、兩個かな都不是單一讀音單位
		"ㄉㄨㄥx", "ㄅㄆ", "ˉ", // 混合或結構不合法
	} {
		if u.Valid() {
			t.Errorf("%q 應為不合法", u)
		}
	}
}

// 注音的行為仍以 zhuyin.Syllable.Valid 為準，reading 的複本不得放寬。
func TestUnitZhuyinMatchesStructure(t *testing.T) {
	for _, s := range []string{"ㄓㄨˋ", "ㄇㄜ˙", "ㄔ", "ㄐㄩㄥˇ"} {
		if !Unit(s).Valid() {
			t.Errorf("%q 應合法", s)
		}
	}
	for _, s := range []string{"ㄅㄆ", "ㄚㄅ", "ˉ", "ㄓˋˊ", "ㄓx"} {
		if Unit(s).Valid() {
			t.Errorf("%q 應不合法", s)
		}
	}
}
