package session

import (
	"unicode"

	"github.com/0c-biggo/0c-ime/core/proto"
)

// 候選窗與選取模式是兩個獨立狀態（P2-51）。cand != nil 只表示畫面要顯示候選；
// navigating 才表示使用者已按 ↓ 進入明確的候選選取模式。顯示中但還沒進入選取模式時，
// 左右鍵與 Home／End 移動組字游標，候選清單跟著新片段重算；文字鍵照常繼續組字。

// shownCandidateKey 處理「候選窗顯示中、但還沒進入選取模式」的按鍵。
// done=false 表示這是一般的文字鍵：呼叫端要關掉舊清單、照正常組字路徑處理。
func (s *Session) shownCandidateKey(k string) (handled bool, commit string, done bool) {
	if s.paletteKey(k) || s.symbolMenuKey(k) {
		return true, "", true // 清單已切到別的分頁；仍由候選處理
	}
	switch k {
	case proto.KeyLeft, proto.KeyRight, proto.KeyHome, proto.KeyEnd:
		s.moveShownCandidateCursor(k)
		return true, "", true
	case proto.KeyDown:
		// ↓ 立刻進入選取模式，反白停在目前這一項，不必再按一次。
		s.navigating = true
		return true, "", true
	case proto.KeyUp:
		s.navigating = true
		if s.candidateColumns() > 1 {
			s.gridMove(k)
		} else {
			s.moveHighlight(-1)
		}
		return true, "", true
	case proto.KeyPageDown:
		s.navigating = true
		s.cand.cursor = (s.candidatePage() + 1) % s.pages() * s.pageSize()
		return true, "", true
	case proto.KeyPageUp:
		s.navigating = true
		s.cand.cursor = (s.candidatePage() + s.pages() - 1) % s.pages() * s.pageSize()
		return true, "", true
	case " ":
		if s.InputStyle.CandidateSpace == "select" {
			s.selectCandidate(s.candidateID(s.cand.cursor))
		} else {
			s.navigating = true
			s.cand.cursor = (s.candidatePage() + 1) % s.pages() * s.pageSize()
		}
		return true, "", true
	case proto.KeyEnter:
		s.selectCandidate(s.candidateID(s.cand.cursor))
		if s.InputStyle.CandidateEnter == "commit" {
			return true, s.commit(), true
		}
		return true, "", true
	case proto.KeyEscape, proto.KeyBackspace, proto.KeyDelete:
		s.cand = nil // 關閉候選、保留組字；Backspace／Del 不刪字（#106）
		s.navigating, s.keepCandidates = false, false
		return true, "", true
	case proto.KeyShiftDelete, proto.KeyShiftBackspace:
		s.forgetCandidate()
		return true, "", true
	case proto.KeyTab:
		switch s.InputStyle.CandidateTab {
		case "grid":
			s.styleGrid = true
			s.navigating = true
		case "next":
			s.moveHighlight(1)
			s.navigating = true
		default:
			s.nextCandidateLength()
		}
		return true, "", true
	}
	if n := s.selectionIndex(k); n >= 0 {
		s.selectCandidate(s.candidateID(s.candidatePage()*s.pageSize() + n))
		return true, "", true
	}
	if k == "j" { // 選字鍵優先；asdfghjkl; 時上面已選字（#193）
		s.stepCandidate(-1)
		return true, "", true
	}
	if k == "k" {
		s.stepCandidate(1)
		return true, "", true
	}
	if len([]rune(k)) == 1 && unicode.IsPrint([]rune(k)[0]) {
		return false, "", false // 文字鍵：離開顯示狀態、繼續組字
	}
	return true, "", true // 沒有作用的具名鍵仍由組字區吃掉
}

// moveShownCandidateCursor 在候選窗只是顯示時，讓 ←／→／Home／End 移動組字游標。
// 候選清單換成新片段的內容，頁碼回到第一頁、反白回到第一項（不沿用舊片段的反白）。
func (s *Session) moveShownCandidateCursor(k string) {
	s.flushMixed()
	if s.english {
		s.insertPendingLiteral()
	}
	s.cand = nil
	s.navigating = false
	switch k {
	case proto.KeyLeft:
		s.moveTo(s.literalStep(s.cursor, -1))
	case proto.KeyRight:
		s.moveTo(s.literalStep(s.cursor, 1))
	case proto.KeyHome:
		s.moveTo(0)
	default:
		s.moveTo(len(s.syls))
	}
	if s.english || !s.comp.Empty() {
		return
	}
	s.reopenShownCandidates()
}

// reopenShownCandidates 在繼續打字後把候選窗留在畫面上：只重算目前游標片段的候選，
// 不會進入選取模式。沒有完整音節、混打暫存或英文模式時不開。
func (s *Session) reopenShownCandidates() {
	if !s.keepCandidates || s.cand != nil || s.english || s.private || s.paused {
		return
	}
	if s.mixed.raw != "" || !s.comp.Empty() || len(s.syls) == 0 {
		return
	}
	s.openCandidates(0)
	s.navigating = false
}
