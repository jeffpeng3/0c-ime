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
	}
	for _, c := range cases {
		if got := Key(c.seq); got != c.want {
			t.Errorf("Key(%v) = %q, want %q", c.seq, got, c.want)
		}
	}
}

func TestUnitValid(t *testing.T) {
	for _, u := range []Unit{"ㄓㄨˋ", "き", "ゃ", "ー", "々", "a"} {
		if !u.Valid() {
			t.Errorf("%q 應為合法", u)
		}
	}
	for _, u := range []Unit{"", "き ょ", "き\tょ", "き\n", Unit("\xff")} {
		if u.Valid() {
			t.Errorf("%q 應為不合法", u)
		}
	}
}
