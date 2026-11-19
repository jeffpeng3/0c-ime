package session

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/english"
	jlex "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	rd "github.com/0c-biggo/0c-ime/core/reading"
)

// crossLongSession 讓かんじ有超過一頁的日文候選，用來驗證 P2-51 的選取模式
// 與 P2-54 的日英並列清單一起運作。
func crossLongSession(t *testing.T) *Session {
	t.Helper()
	lex := jlex.NewMem()
	for _, surface := range []string{
		"漢字", "感じ", "幹事", "完治", "寛治", "勘二", "亘", "桓", "敢", "憾", "患", "緩",
	} {
		if err := lex.Add(jlex.Entry{Surface: surface, Reading: rd.Sequence{"か", "ん", "じ"}, POS: jlex.POSNoun}); err != nil {
			t.Fatal(err)
		}
	}
	s := New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	s.MixedInput = true
	s.English = english.New([]english.Word{{Text: "kanji", Weight: 60}})
	if !s.EnableJapanese(lex, nil) {
		t.Fatal("EnableJapanese = false")
	}
	return s
}

// P2-51 + P2-54：日英並列清單只是顯示；↓ 進入選取模式並換反白，選取模式中
// ←／→ 翻頁，Enter 送出反白的那個日文候選（不是最後的英文原文）。
func TestJapaneseCrossDownEntersSelectionThenArrowsPage(t *testing.T) {
	s := crossLongSession(t)
	typed := typeSessionKeys(s, "kanji")
	if typed.Candidates == nil || typed.Text() != "かんじ" {
		t.Fatalf("kanji 應顯示かな與候選：%q / %+v", typed.Text(), typed.Candidates)
	}
	if s.navigating {
		t.Fatal("清單剛出現時只在顯示狀態，不應是選取模式")
	}
	if typed.Candidates.Page != 0 || typed.Candidates.Highlight != 0 {
		t.Fatalf("初始應是第一頁第一項：%+v", typed.Candidates)
	}

	down := key(s, proto.KeyDown)
	if !s.navigating {
		t.Fatal("↓ 應進入選取模式")
	}
	if down.Candidates == nil || down.Candidates.Page != 0 || down.Candidates.Highlight != 1 {
		t.Fatalf("↓ 應停在第二項（かな之後的第一個日文候選）：%+v", down.Candidates)
	}
	if want := down.Candidates.Items[1].Text; down.Text() != want {
		t.Fatalf("組字應顯示反白的候選 %q：%q", want, down.Text())
	}

	right := key(s, proto.KeyRight)
	if right.Candidates == nil || right.Candidates.Page != 1 || right.Candidates.Highlight != 0 {
		t.Fatalf("選取模式中 → 應換到下一頁：%+v", right.Candidates)
	}
	if want := right.Candidates.Items[0].Text; right.Text() != want {
		t.Fatalf("翻頁後組字應顯示該頁第一項 %q：%q", want, right.Text())
	}
	highlighted := right.Candidates.Items[right.Candidates.Highlight].Text
	pages := right.Candidates.Pages

	left := key(s, proto.KeyLeft)
	if left.Candidates == nil || left.Candidates.Page != 0 || pages < 2 {
		t.Fatalf("選取模式中 ← 應回第一頁：%+v pages=%d", left.Candidates, pages)
	}
	// 再翻回去，Enter 送出該頁反白的日文候選。
	key(s, proto.KeyRight)
	snap := key(s, proto.KeyEnter)
	if snap.Commit != highlighted || snap.Commit == "kanji" {
		t.Fatalf("Enter 應送出反白的日文候選 %q，實際 %q", highlighted, snap.Commit)
	}
	if s.navigating || s.Language() != lang.Zh || snap.Text() != "" {
		t.Fatalf("送出後應離開選取模式與日文路徑：nav=%v lang=%s text=%q", s.navigating, s.Language(), snap.Text())
	}
}

// P2-51 不回歸：多段注音組字中，候選只是顯示時 ←／→ 仍移動組字游標，
// 候選跟著新片段重算，不會翻頁或進入選取模式。
func TestJapaneseCrossKeepsCursorArrowsOnZhuyinCandidates(t *testing.T) {
	s := cursorSession()
	typeSyl(s, "ㄨㄛˇ")
	typeSyl(s, "ㄈㄥ")
	typeSyl(s, "ㄐㄧㄥˇ")
	shown := key(s, " ")
	if shown.Candidates == nil || s.navigating {
		t.Fatalf("空白只開候選：%+v nav=%v", shown.Candidates, s.navigating)
	}
	left := key(s, proto.KeyLeft)
	if !left.Handled || left.Cursor.Rune != 2 || left.Candidates == nil || s.navigating {
		t.Fatalf("← 應移動組字游標並保留候選：%+v nav=%v", left, s.navigating)
	}
	if c := left.Candidates; c.Start != 2 || c.Length != 1 || c.Highlight != 0 {
		t.Fatalf("游標換片段後候選應重算：%+v", c)
	}
	right := key(s, proto.KeyRight)
	if right.Cursor.Rune != 3 || right.Candidates == nil || right.Candidates.Page != 0 {
		t.Fatalf("→ 應把游標移回句尾：%+v", right)
	}
}
