package mixed_test

import (
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/input"
	"github.com/0c-biggo/0c-ime/core/ja/mixed"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

func typeMixed(t *testing.T, c *mixed.Composer, keys string) {
	t.Helper()
	for _, r := range keys {
		if _, ok := c.Insert(string(r)); !ok {
			t.Fatalf("Insert(%q) = false", string(r))
		}
	}
}

func TestComposerMatchesJapaneseRomaji(t *testing.T) {
	inputs := []string{
		"kuukigaii", "kyouha", "konnichiwa", "gakkou", "nihongo",
		"arigatou", "kanji", "sake", "ame", "shinjuku",
	}
	for _, keys := range inputs {
		mc := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
		jc := input.NewRomajiComposer()
		for _, r := range keys {
			if _, ok := mc.Insert(string(r)); !ok {
				t.Fatalf("%q: mixed 拒絕 %q", keys, string(r))
			}
			if _, ok := jc.Insert(string(r)); !ok {
				t.Fatalf("%q: ja/input 拒絕 %q", keys, string(r))
			}
		}
		if got, want := reading.Key(mc.Units()), reading.Key(jc.Units()); got != want {
			t.Errorf("%q: mixed units %q, want %q", keys, got, want)
		}
		if got, want := mc.Pending(), jc.Pending(); got != want {
			t.Errorf("%q: mixed pending %q, want %q", keys, got, want)
		}
		if got, want := mc.DisplayText(), jc.DisplayText(); got != want {
			t.Errorf("%q: mixed display %q, want %q", keys, got, want)
		}
	}
}

func TestComposerSplitsJapanesePrefixBeforeEnglish(t *testing.T) {
	c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "kyouha")
	if _, literal := c.Run(); literal {
		t.Fatal("kyouha 應先當かな")
	}
	typeMixed(t, c, "API")
	if got := c.DisplayText(); got != "きょうはAPI" {
		t.Fatalf("日文後直接打英文 = %q", got)
	}
	if run, literal := c.Run(); run != "API" || !literal {
		t.Fatalf("英數段 = %q, %v", run, literal)
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "kyouha")
	typeMixed(t, c, "https://example.com/x")
	if got := c.DisplayText(); got != "きょうはhttps://example.com/x" {
		t.Fatalf("日文後直接打網址 = %q", got)
	}
}

func TestComposerEnglishLiteral(t *testing.T) {
	cases := []struct {
		keys string
		want string
	}{
		{"hello", "hello"},
		{"GitHub", "GitHub"},
		{"kubectl", "kubectl"},
		{"API", "API"},
		{"123", "123"},
		{"https://example.com/a?b=1", "https://example.com/a?b=1"},
		{"foo@example.com", "foo@example.com"},
		{"user_name", "user_name"},
		{"ＡＢＣ１２３", "ＡＢＣ１２３"},
	}
	for _, tc := range cases {
		c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
		typeMixed(t, c, tc.keys)
		if got := sequenceString(c.Units()) + c.Pending(); got != tc.want {
			t.Errorf("%q: display %q, want %q", tc.keys, got, tc.want)
		}
		if run, literal := c.Run(); run != tc.want || !literal {
			t.Errorf("%q: Run = %q, %v", tc.keys, run, literal)
		}
	}
}

func TestComposerEnglishWordTurnsLiteralAtBoundary(t *testing.T) {
	c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "hello")
	if _, literal := c.Run(); !literal {
		t.Fatal("hello 應照字面")
	}
	if _, ok := c.Insert("、"); ok {
		t.Fatal("日文標點不應被英數段接受")
	}
	if got := c.DisplayText(); got != "hello" {
		t.Fatalf("標點邊界後 display = %q，英文段應保留", got)
	}
}

func TestComposerKanaMode(t *testing.T) {
	c := mixed.NewComposer(lang.InputMethod{ID: "kana"}, mixed.Config{})
	if _, ok := c.Insert("きょう"); !ok {
		t.Fatal("かな模式拒絕きょう")
	}
	if got := sequenceString(c.Units()); got != "きょう" {
		t.Fatalf("かな模式 units = %q", got)
	}
	typeMixed(t, c, "hello")
	if got := c.DisplayText(); got != "きょうhello" {
		t.Fatalf("かな模式混打 = %q", got)
	}
}

func TestComposerSuggestionAndAccept(t *testing.T) {
	c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "hel")
	word, ok := c.Suggestion()
	if !ok || word != "hello" {
		t.Fatalf("Suggestion = %q, %v", word, ok)
	}
	if !c.AcceptSuggestion() {
		t.Fatal("AcceptSuggestion = false")
	}
	if got := c.DisplayText(); got != "hello" {
		t.Fatalf("接受補完後 = %q", got)
	}
	if c.AcceptSuggestion() {
		t.Fatal("沒有下一個補完時應回傳 false")
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "go")
	if _, literal := c.Run(); literal {
		t.Fatal("go 應先當かな（預設最短英文長度 3）")
	}
	if !c.AcceptEnglish() {
		t.Fatal("AcceptEnglish = false")
	}
	if _, literal := c.Run(); !literal || c.DisplayText() != "go" {
		t.Fatalf("AcceptEnglish 後 = %q, %v", c.DisplayText(), literal)
	}
}

func TestComposerLearnAndCandidates(t *testing.T) {
	c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "kubectl")
	if !c.LearnCurrent() {
		t.Fatal("LearnCurrent = false")
	}
	if !c.English().Learned("kubectl") {
		t.Fatal("composer 沒有學到 kubectl")
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "hel")
	cands := c.Candidates(8)
	if len(cands) < 2 || cands[0].Surface != "hel" || cands[0].Source != mixed.SourceLiteral {
		t.Fatalf("Candidates = %#v", cands)
	}
	found := false
	for _, cand := range cands {
		if cand.Surface == "hello" && cand.Source == mixed.SourceEnglish {
			found = true
		}
	}
	if !found {
		t.Fatalf("候選應含英文補完 hello：%#v", cands)
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "https://example.com/x")
	cands = c.Candidates(8)
	if len(cands) != 1 || cands[0].Surface != "https://example.com/x" {
		t.Fatalf("網址候選應只有原樣一項：%#v", cands)
	}
}

func TestComposerBackspace(t *testing.T) {
	c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "hello")
	if !c.Backspace() || c.DisplayText() != "hell" {
		t.Fatalf("英文 backspace = %q", c.DisplayText())
	}
	for i := 0; i < 4; i++ {
		c.Backspace()
	}
	if c.DisplayText() != "" || len(c.Units()) != 0 {
		t.Fatalf("刪完後 display=%q units=%q", c.DisplayText(), reading.Key(c.Units()))
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "ky")
	if !c.Backspace() || c.Pending() != "k" || c.DisplayText() != "k" {
		t.Fatalf("羅馬字 backspace = display %q pending %q", c.DisplayText(), c.Pending())
	}
	if !c.Backspace() || c.DisplayText() != "" {
		t.Fatalf("刪完羅馬字 = %q", c.DisplayText())
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "ka")
	if !c.Backspace() || len(c.Units()) != 0 {
		t.Fatalf("かな backspace 後 units = %q", reading.Key(c.Units()))
	}
}

func TestComposerFlush(t *testing.T) {
	c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "kyouha")
	if _, ok := c.Flush(); !ok {
		t.Fatal("Flush = false")
	}
	if got := c.DisplayText(); got != "きょうは" {
		t.Fatalf("Flush 後 = %q", got)
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "n")
	if _, ok := c.Flush(); !ok || c.DisplayText() != "ん" {
		t.Fatalf("n Flush = %q", c.DisplayText())
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "hello")
	if _, ok := c.Flush(); !ok || c.DisplayText() != "hello" {
		t.Fatalf("英文 Flush = %q", c.DisplayText())
	}
}

func TestComposerSpaceOnlyAfterLiteral(t *testing.T) {
	c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "hello")
	if _, ok := c.Insert(" "); !ok {
		t.Fatal("英文字後的空格應被接受")
	}
	if got := c.DisplayText(); got != "hello " {
		t.Fatalf("英文字後空格 = %q", got)
	}
	typeMixed(t, c, "world")
	if got := c.DisplayText(); got != "hello world" {
		t.Fatalf("英文字之間的空格 = %q", got)
	}

	c = mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.Config{})
	typeMixed(t, c, "ka")
	if _, ok := c.Insert(" "); ok {
		t.Fatal("かな段後的空格應交還外殼")
	}
}

func sequenceString(seq reading.Sequence) string {
	var b strings.Builder
	for _, u := range seq {
		b.WriteString(string(u))
	}
	return b.String()
}
