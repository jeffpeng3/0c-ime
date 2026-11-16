package punct_test

import (
	"strings"
	"testing"

	japunct "github.com/0c-biggo/0c-ime/core/ja/punct"
)

func enabled() japunct.Options {
	opts := japunct.DefaultOptions()
	opts.Enabled = true
	return opts
}

func complete(t *testing.T, text string, opts japunct.Options) (string, []japunct.Edit) {
	t.Helper()
	c := japunct.NewCompleter(opts)
	edits := c.Edits(text)
	out := c.Complete(text)
	if out != mustApply(t, text, edits) {
		t.Fatalf("Complete(%q) = %q，與 Edits 套用結果不一致", text, out)
	}
	return out, edits
}

func mustApply(t *testing.T, text string, edits []japunct.Edit) string {
	t.Helper()
	out, ok := japunct.Apply(text, edits)
	if len(edits) > 0 && !ok {
		t.Fatalf("Apply(%q, %v) 被拒絕", text, edits)
	}
	if len(edits) == 0 && ok {
		t.Fatalf("Apply(%q, 空) 不應回 ok", text)
	}
	return out
}

func TestDisabledByDefault(t *testing.T) {
	opts := japunct.DefaultOptions()
	if opts.Enabled {
		t.Fatal("預設必須關閉")
	}
	for _, text := range []string{"今日は寒い", "元気ですか", "彼は「今日", "しかし今日は寒い"} {
		if got := japunct.NewCompleter(opts).Complete(text); got != text {
			t.Fatalf("關閉時 %q -> %q", text, got)
		}
		if edits := japunct.NewCompleter(japunct.Options{}).Edits(text); edits != nil {
			t.Fatalf("零值 Options 不得插入：%v", edits)
		}
	}
}

func TestSentenceFinalMarks(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		opts  func(*japunct.Options)
		want  string
		marks []string
	}{
		{name: "period", in: "今日は寒い", want: "今日は寒い。", marks: []string{"。"}},
		{name: "question", in: "元気ですか", want: "元気ですか？", marks: []string{"？"}},
		{name: "question kana", in: "行くかな", want: "行くかな？", marks: []string{"？"}},
		{name: "exclamation", in: "すごいよ", want: "すごいよ！", marks: []string{"！"}},
		{name: "exclamation ne", in: "そうだね", want: "そうだね！", marks: []string{"！"}},
		{name: "existing period", in: "今日は寒い。", want: "今日は寒い。"},
		{name: "existing question", in: "元気ですか？", want: "元気ですか？"},
		{name: "question off", in: "元気ですか", opts: func(o *japunct.Options) { o.Question = false }, want: "元気ですか。", marks: []string{"。"}},
		{name: "exclamation off", in: "すごいよ", opts: func(o *japunct.Options) { o.Exclamation = false }, want: "すごいよ。", marks: []string{"。"}},
		{name: "period off", in: "今日は寒い", opts: func(o *japunct.Options) { o.Period = false }, want: "今日は寒い"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := enabled()
			if tc.opts != nil {
				tc.opts(&opts)
			}
			out, edits := complete(t, tc.in, opts)
			if out != tc.want {
				t.Fatalf("got %q，want %q", out, tc.want)
			}
			if len(edits) != len(tc.marks) {
				t.Fatalf("edits = %v，want %v", edits, tc.marks)
			}
			for i, mark := range tc.marks {
				if edits[i].Text != mark {
					t.Fatalf("edit %d = %q，want %q", i, edits[i].Text, mark)
				}
			}
			assertInsertionOnly(t, tc.in, out, edits)
		})
	}
}

func TestCommaRules(t *testing.T) {
	cases := []string{
		"しかし今日は寒い",
		"今日は寒いが明日は晴れる",
		"雨が降ったので試合は中止だ",
	}
	wants := []string{
		"しかし、今日は寒い。",
		"今日は寒いが、明日は晴れる。",
		"雨が降ったので、試合は中止だ。",
	}
	for i, in := range cases {
		out, edits := complete(t, in, enabled())
		if out != wants[i] {
			t.Fatalf("%q -> %q，want %q", in, out, wants[i])
		}
		if !strings.Contains(out, "、") {
			t.Fatalf("%q 沒有插入逗號", in)
		}
		assertInsertionOnly(t, in, out, edits)
	}
	// 已經有逗號或後面是標點時不重複插入。
	for _, in := range []string{"しかし、今日は寒い", "今日は寒いが、明日は晴れる"} {
		if out, _ := complete(t, in, enabled()); out != in+"。" {
			t.Fatalf("%q -> %q", in, out)
		}
	}
	// 逗號關閉時只補句點。
	opts := enabled()
	opts.Comma = false
	if out, _ := complete(t, "しかし今日は寒い", opts); out != "しかし今日は寒い。" {
		t.Fatalf("Comma=false -> %q", out)
	}
}

func TestBracketCompletion(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"彼は「今日", "彼は「今日。」"},
		{"彼は「今日は寒い」と", "彼は「今日は寒い」と。"},
		{"これは（とても大事", "これは（とても大事。）"},
		{"彼は「（今日", "彼は「（今日。）」"},
		{"彼は「", "彼は「"},
	}
	for _, tc := range cases {
		out, edits := complete(t, tc.in, enabled())
		if out != tc.want {
			t.Fatalf("%q -> %q，want %q", tc.in, out, tc.want)
		}
		assertInsertionOnly(t, tc.in, out, edits)
	}
	opts := enabled()
	opts.Brackets = false
	if out, _ := complete(t, "彼は「今日", opts); out != "彼は「今日。" {
		t.Fatalf("Brackets=false -> %q", out)
	}
}

func TestOnlyInsertsNeverChangesUserText(t *testing.T) {
	inputs := []string{
		"今日は寒い",
		"しかし今日は寒いが明日は晴れる",
		"彼は「今日は寒い」と",
		"これは（とても大事",
		"元気ですか",
		"すごいよ",
		"あ",
	}
	for _, in := range inputs {
		out, edits := complete(t, in, enabled())
		assertInsertionOnly(t, in, out, edits)
		for _, e := range edits {
			if len([]rune(e.Text)) != 1 {
				t.Fatalf("%q 的 edit %q 不是單一標點", in, e.Text)
			}
			if e.Offset < 0 || e.Offset > len([]rune(in)) {
				t.Fatalf("%q 的位移 %d 越界", in, e.Offset)
			}
		}
	}
}

func TestDoesNotTouchChinesePunctuation(t *testing.T) {
	// 沒有かな的文字不補日文標點，也不產生中文標點。
	for _, in := range []string{
		"今天天氣很好",
		"今天天氣很好，不是嗎",
		"請記得帶資料。",
		"Hello, world",
	} {
		if out, edits := complete(t, in, enabled()); out != in || len(edits) != 0 {
			t.Fatalf("純中文／英文 %q 被改動：%q %v", in, out, edits)
		}
	}
	// 日文文字裡既有的中文標點原樣保留，只另外插入日文標點。
	in := "彼は「今日は寒い」，と言った"
	out, edits := complete(t, in, enabled())
	if !strings.Contains(out, "，") {
		t.Fatalf("既有的中文逗號被移除：%q", out)
	}
	assertInsertionOnly(t, in, out, edits)
}

func TestApplyRejectsInvalidEdits(t *testing.T) {
	for _, edits := range [][]japunct.Edit{
		{{Offset: -1, Text: "。"}},
		{{Offset: 9, Text: "。"}},
		{{Offset: 2, Text: "。"}, {Offset: 1, Text: "、"}},
		{{Offset: 1, Text: "，"}},
		{{Offset: 1, Text: ".."}},
		{{Offset: 1, Text: ""}},
	} {
		if out, ok := japunct.Apply("今日は寒い", edits); ok || out != "今日は寒い" {
			t.Fatalf("不合規 edit %v 被套用：%q", edits, out)
		}
	}
}

func TestApplySameOffsetKeepsOrder(t *testing.T) {
	out, ok := japunct.Apply("彼は「今日", []japunct.Edit{
		{Offset: 5, Text: "。"},
		{Offset: 5, Text: "」"},
	})
	if !ok || out != "彼は「今日。」" {
		t.Fatalf("got %q ok=%v", out, ok)
	}
}

func TestMinRunes(t *testing.T) {
	opts := enabled()
	opts.MinRunes = 3
	if out, edits := complete(t, "あい", opts); out != "あい" || len(edits) != 0 {
		t.Fatalf("短於 MinRunes 不得補：%q %v", out, edits)
	}
	if out, _ := complete(t, "あいう", opts); out != "あいう。" {
		t.Fatalf("MinRunes 邊界 -> %q", out)
	}
}

// assertInsertionOnly 驗證輸出只是原文加上 edits 的內容：長度相符、把插入的
// rune 依位移拿掉後與原文完全相同。
func assertInsertionOnly(t *testing.T, in, out string, edits []japunct.Edit) {
	t.Helper()
	wantLen := len([]rune(in))
	for _, e := range edits {
		wantLen += len([]rune(e.Text))
	}
	if len([]rune(out)) != wantLen {
		t.Fatalf("%q -> %q 不是純插入：長度 %d，want %d", in, out, len([]rune(out)), wantLen)
	}
	r := []rune(out)
	// 由後往前移除，位移才不會被前面的插入影響。
	for i := len(edits) - 1; i >= 0; i-- {
		n := len([]rune(edits[i].Text))
		at := edits[i].Offset
		for j := 0; j < i; j++ {
			at += len([]rune(edits[j].Text))
		}
		if at+n > len(r) || string(r[at:at+n]) != edits[i].Text {
			t.Fatalf("%q 的插入 %q 不在位移 %d", out, edits[i].Text, at)
		}
		r = append(r[:at], r[at+n:]...)
	}
	if string(r) != in {
		t.Fatalf("移除插入後 = %q，want 原文 %q", string(r), in)
	}
}
