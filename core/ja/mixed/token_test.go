package mixed_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/mixed"
)

func TestClassify(t *testing.T) {
	e := mixed.NewEnglish(nil)
	cases := []struct {
		text string
		want mixed.Kind
	}{
		{"hello", mixed.KindEnglish},
		{"world", mixed.KindEnglish},
		{"sake", mixed.KindLatin},
		{"don't", mixed.KindLatin},
		{"API", mixed.KindEnglish}, // 原型詞典有 api，大小寫不分
		{"user_name", mixed.KindIdentifier},
		{`C:\Users`, mixed.KindIdentifier},
		{"search?q=1", mixed.KindIdentifier},
		{"abc123", mixed.KindIdentifier},
		{"C++", mixed.KindIdentifier},
		{"3d", mixed.KindIdentifier},
		{"https://example.com/a?b=1", mixed.KindURL},
		{"http://日本語", mixed.KindOther},
		{"www.example.com", mixed.KindURL},
		{"example.com", mixed.KindURL},
		{"foo@example.com", mixed.KindEmail},
		{"foo@bar", mixed.KindIdentifier},
		{"ＡＢＣ１２３", mixed.KindFullWidthAlnum},
		{"123", mixed.KindNumber},
		{"今日は", mixed.KindJapanese},
		{"hello.", mixed.KindEnglish},
		{"", mixed.KindOther},
	}
	for _, tc := range cases {
		if got := mixed.Classify(tc.text, e); got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestLiteralKinds(t *testing.T) {
	for _, kind := range []mixed.Kind{
		mixed.KindEnglish, mixed.KindLatin, mixed.KindURL, mixed.KindEmail,
		mixed.KindIdentifier, mixed.KindFullWidthAlnum, mixed.KindNumber,
	} {
		if !kind.Literal() {
			t.Errorf("%v 應照字面", kind)
		}
	}
	for _, kind := range []mixed.Kind{mixed.KindJapanese, mixed.KindOther} {
		if kind.Literal() {
			t.Errorf("%v 不應照字面", kind)
		}
	}
}

func TestIsURLEmailIdentifier(t *testing.T) {
	urls := []string{
		"https://example.com",
		"http://example.com/a?b=c#d",
		"ftp://files.example.com/x",
		"//example.com/x",
		"www.example.com",
		"example.com",
		"example.com:8080/x",
		"https://example.com.",
	}
	for _, u := range urls {
		if !mixed.IsURL(u) {
			t.Errorf("IsURL(%q) = false", u)
		}
	}
	for _, u := range []string{"hello", "hello world", "foo@example.com", "今日はhttps://example.com", "example"} {
		if mixed.IsURL(u) {
			t.Errorf("IsURL(%q) = true", u)
		}
	}

	emails := []string{"foo@example.com", "a.b+c@example.co.jp", "user_name@example.com."}
	for _, m := range emails {
		if !mixed.IsEmail(m) {
			t.Errorf("IsEmail(%q) = false", m)
		}
	}
	for _, m := range []string{"foo@bar", "@example.com", "foo@@example.com", "foo@example", "今日@example.com"} {
		if mixed.IsEmail(m) {
			t.Errorf("IsEmail(%q) = true", m)
		}
	}

	ids := []string{"user_name", `C:\Users`, "search?q=1", "abc123", "C++", "hello-world", "foo@bar"}
	for _, id := range ids {
		if !mixed.IsIdentifier(id) {
			t.Errorf("IsIdentifier(%q) = false", id)
		}
	}
	for _, id := range []string{"hello", "今日は", "123", "---"} {
		if mixed.IsIdentifier(id) {
			t.Errorf("IsIdentifier(%q) = true", id)
		}
	}
}

func TestScan(t *testing.T) {
	e := mixed.NewEnglish(nil)
	text := "今日はhello. APIとpython3の話。"
	tokens := mixed.Scan(text, e)
	var got []string
	for _, tok := range tokens {
		if text[tok.Start:tok.End] != tok.Text {
			t.Errorf("token offset 不一致：%#v", tok)
		}
		got = append(got, tok.Kind.String()+":"+tok.Text)
	}
	want := []string{
		"japanese:今日は",
		"english:hello",
		"other:.",
		"english:API",
		"japanese:と",
		"identifier:python3",
		"japanese:の話",
		"other:。",
	}
	if len(got) != len(want) {
		t.Fatalf("Scan = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestScanURLAndEmailBoundaries(t *testing.T) {
	e := mixed.NewEnglish(nil)
	text := "URLはhttps://example.com/a?b=1です。mailはfoo@example.com, まで。"
	tokens := mixed.Scan(text, e)
	kinds := map[string]mixed.Kind{}
	for _, tok := range tokens {
		kinds[tok.Text] = tok.Kind
	}
	if kinds["https://example.com/a?b=1"] != mixed.KindURL {
		t.Errorf("網址沒有被完整辨認：%#v", tokens)
	}
	if kinds["foo@example.com"] != mixed.KindEmail {
		t.Errorf("email 沒有被完整辨認：%#v", tokens)
	}
	if kinds[","] != mixed.KindOther {
		t.Errorf("email 後面的逗號應獨立：%#v", tokens)
	}
}
