package session

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/proto"
)

// P2-51：候選窗開著但還沒按 ↓ 時，←／→ 移動組字游標，不翻頁。
func TestShownCandidatesLetArrowsMoveCompositionCursor(t *testing.T) {
	s := cursorSession()
	typeSyl(s, "ㄨㄛˇ")
	typeSyl(s, "ㄈㄥ")
	typeSyl(s, "ㄐㄧㄥˇ")
	shown := key(s, " ")
	if shown.Candidates == nil || s.navigating {
		t.Fatalf("空白只開候選、不進入選取模式：%+v nav=%v", shown.Candidates, s.navigating)
	}
	if shown.Cursor.Rune != 3 {
		t.Fatalf("游標應在句尾：%+v", shown.Cursor)
	}
	left := key(s, proto.KeyLeft)
	if !left.Handled || left.Cursor.Rune != 2 || left.Candidates == nil || s.navigating {
		t.Fatalf("← 應移動游標並保留候選窗：%+v nav=%v", left, s.navigating)
	}
	// 游標換到不同片段：清單重算成該片段的內容，頁碼與反白從頭開始。
	home := key(s, proto.KeyHome)
	if home.Cursor.Rune != 0 || home.Candidates == nil {
		t.Fatalf("Home 應移到組字開頭並保留候選：%+v", home)
	}
	if c := home.Candidates; c.Start != 0 || c.Length != 1 || c.Page != 0 || c.Highlight != 0 {
		t.Fatalf("新片段應是第一頁第一項：%+v", c)
	}
	if got := home.Candidates.Items[home.Candidates.Highlight].Text; got != "我" {
		t.Fatalf("游標在「我」時候選應屬於該片段：%q", got)
	}
	end := key(s, proto.KeyEnd)
	if end.Cursor.Rune != 3 || end.Candidates == nil {
		t.Fatalf("End 應移到組字結尾並保留候選：%+v", end)
	}
	if c := end.Candidates; c.Start != 1 || c.Length != 2 || c.Page != 0 || c.Highlight != 0 {
		t.Fatalf("句尾應換成「風景」片段：%+v", c)
	}
	found := false
	for _, item := range end.Candidates.Items {
		found = found || item.Text == "風景"
	}
	if !found {
		t.Fatalf("候選內容沒有跟著片段：%+v", end.Candidates.Items)
	}
}

// P2-51：↓ 才進入明確的選取模式；從那一刻起左右鍵翻頁，第一下 ↓ 不移動反白。
func TestDownEntersSelectionModeThenArrowsPage(t *testing.T) {
	s := manySession("1234567890")
	typeSyl(s, "ㄕˋ")
	shown := key(s, " ")
	if shown.Candidates == nil || s.navigating {
		t.Fatalf("空白只顯示候選：%+v nav=%v", shown.Candidates, s.navigating)
	}
	down := key(s, proto.KeyDown)
	if !s.navigating || down.Candidates == nil || down.Candidates.Page != 0 || down.Candidates.Highlight != 0 {
		t.Fatalf("↓ 應進入選取模式並停在目前反白：%+v nav=%v", down.Candidates, s.navigating)
	}
	if right := key(s, proto.KeyRight); right.Candidates.Page != 1 || right.Text() != "是" {
		t.Fatalf("選取模式中 → 應換頁、不改組字：%+v", right.Candidates)
	}
	if left := key(s, proto.KeyLeft); left.Candidates.Page != 0 {
		t.Fatalf("選取模式中 ← 應回上一頁：%+v", left.Candidates)
	}
}

// P2-51：選取模式中打字會離開模式並繼續組字；候選窗跟著新片段重新出現。
func TestTypingLeavesSelectionModeAndCandidateWindowFollows(t *testing.T) {
	s := cursorSession()
	typeSyl(s, "ㄨㄛˇ")
	typeSyl(s, "ㄈㄥ")
	typeSyl(s, "ㄐㄧㄥˇ")
	key(s, " ")
	key(s, proto.KeyDown)
	if !s.navigating {
		t.Fatal("fixture: ↓ 應進入選取模式")
	}
	snap := typeSyl(s, "ㄊㄚ")
	if s.navigating {
		t.Fatal("打字後應離開選取模式")
	}
	if snap.Text() != "我風景他" {
		t.Fatalf("打字應接在組字尾端：%q", snap.Text())
	}
	if snap.Candidates == nil {
		t.Fatal("候選窗應為新片段重新出現")
	}
	if c := snap.Candidates; c.Start != 3 || c.Length != 1 || c.Page != 0 || c.Highlight != 0 {
		t.Fatalf("新片段的候選應從第一頁第一項開始：%+v", c)
	}
}

// P2-51：Esc 離開選取模式並關閉候選，但保留組字；之後方向鍵回到移動游標。
func TestEscLeavesSelectionMode(t *testing.T) {
	s := manySession("1234567890")
	typeSyl(s, "ㄕˋ")
	key(s, " ")
	key(s, proto.KeyDown)
	if !s.navigating {
		t.Fatal("fixture: ↓ 應進入選取模式")
	}
	snap := key(s, proto.KeyEscape)
	if s.navigating || snap.Candidates != nil || snap.Text() != "是" || snap.Cursor.Rune != 1 {
		t.Fatalf("Esc 應關閉候選、保留組字並離開選取模式：%+v nav=%v", snap, s.navigating)
	}
	left := key(s, proto.KeyLeft)
	if left.Cursor.Rune != 0 || left.Candidates != nil {
		t.Fatalf("Esc 後 ← 應回到移動組字游標：%+v", left)
	}
}

// P2-51：Home／End 在選取模式是第一個／最後一個候選；不在選取模式時是組字頭尾。
func TestHomeEndDependOnSelectionMode(t *testing.T) {
	s := manySession("1234567890")
	typeSyl(s, "ㄕˋ")
	key(s, " ")

	// 只是顯示：Home／End 移動組字游標，不碰候選反白。
	home := key(s, proto.KeyHome)
	if home.Cursor.Rune != 0 || home.Candidates == nil || home.Candidates.Highlight != 0 {
		t.Fatalf("顯示中的 Home 應移到組字開頭：%+v", home)
	}
	end := key(s, proto.KeyEnd)
	if end.Cursor.Rune != 1 || end.Candidates == nil || end.Candidates.Highlight != 0 {
		t.Fatalf("顯示中的 End 應移到組字結尾：%+v", end)
	}

	// ↓ 之後：Home／End 是候選清單的第一個／最後一個。
	key(s, proto.KeyDown)
	last := key(s, proto.KeyEnd)
	if !last.Handled || last.Cursor.Rune != 1 || last.Candidates == nil {
		t.Fatalf("選取模式的 End 不應移動組字游標：%+v", last)
	}
	if c := last.Candidates; c.Page != c.Pages-1 || c.Highlight != len(c.Items)-1 {
		t.Fatalf("選取模式的 End 應到最後一個候選：%+v", c)
	}
	first := key(s, proto.KeyHome)
	if first.Cursor.Rune != 1 || first.Candidates == nil {
		t.Fatalf("選取模式的 Home 不應移動組字游標：%+v", first)
	}
	if c := first.Candidates; c.Page != 0 || c.Highlight != 0 {
		t.Fatalf("選取模式的 Home 應到第一個候選：%+v", c)
	}
}
