package session

import (
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/personal"
	rd "github.com/0c-biggo/0c-ime/core/reading"
)

// 分語言與跨語言學習的 session hooks（P2-15）。
//
// session 只把「游標所在片段」「選了什麼」「前一個片段是什麼語言」交給
// personal.Model；要不要指定語言由核心依詞面與片段標籤推斷。這裡不改候選
// 分數，也不影響注音解碼：語言偏好只是額外提供給宿主與後續仲裁的訊號。

// noteZhSelection 把一次注音選字記到游標所在片段的語言。漢字片段在注音
// 路徑上通常是中文，但使用者若手動把片段改成日文，仍以片段標籤為準。
func (s *Session) noteZhSelection(surface string, start, end int) {
	if s == nil || s.model == nil || surface == "" {
		return
	}
	id := lang.Zh
	if index := s.ActiveSegment(); index >= 0 {
		segs := s.Segments()
		if index < len(segs) && segs[index].Language.Valid() {
			id = segs[index].Language.Language
		}
	}
	s.learnLanguageSelection(surface, s.readingAt(start, end), id, s.previousSegmentLanguage())
}

// noteLanguageSelection 把一次非注音語言的選字記到該語言。previous 是左側
// 片段語言；同一語言的連續選字不會產生跨語言轉移。
func (s *Session) noteLanguageSelection(surface string, reading rd.Sequence, id, previous lang.ID) {
	if s == nil || s.model == nil || surface == "" {
		return
	}
	s.learnLanguageSelection(surface, reading, id, previous)
}

func (s *Session) learnLanguageSelection(surface string, reading rd.Sequence, id, previous lang.ID) {
	if s == nil || s.model == nil || surface == "" {
		return
	}
	left := s.pendingLeft
	if left == "" {
		left = s.documentLeft
	}
	s.languageSelections = append(s.languageSelections, personal.LanguageSelection{
		Surface:  surface,
		Reading:  reading,
		Left:     left,
		Language: id,
		Previous: previous,
	})
}

// flushLanguageSelections 把本次組字累積的選字觀察寫進分語言學習。呼叫端已判斷
// 這次送出是否要學習；無論寫不寫，暫存都清空。注音的讀音不寫進語言紀錄，
// 聲調選填時未定聲調的記號才不會跟著進到個人檔。
func (s *Session) flushLanguageSelections(learn bool) {
	if s == nil || len(s.languageSelections) == 0 {
		return
	}
	if !learn {
		s.languageSelections = nil
		return
	}
	for _, sel := range s.languageSelections {
		if sel.Language == lang.Zh {
			sel.Reading = nil
		}
		s.model.LearnLanguage(sel)
	}
	s.languageSelections = nil
}

// dropLanguageSelections 丟掉尚未送出的選字觀察，例如取消変換或打字把選定的
// 文節換回かな：那次選擇沒有輸出，不該影響下次。
func (s *Session) dropLanguageSelections() {
	if s != nil {
		s.languageSelections = nil
	}
}

// previousSegmentLanguage 回傳游標所在片段前一個有標籤的片段語言；
// 游標在第一個片段或沒有片段時退回上一次送出的最後語言。
func (s *Session) previousSegmentLanguage() lang.ID {
	if s == nil {
		return lang.Und
	}
	index := s.ActiveSegment()
	if index > 0 {
		segs := s.Segments()
		for i := index - 1; i >= 0; i-- {
			if id := segs[i].Language.Language; id != lang.Und {
				return id
			}
		}
	}
	return s.lastLanguage
}

// noteCommittedLanguage 記下這次送出的最後片段語言，供下一次跨語言偏好使用。
// 必須在 reset 之前呼叫。
func (s *Session) noteCommittedLanguage() {
	if s == nil {
		return
	}
	segs := s.Segments()
	for i := len(segs) - 1; i >= 0; i-- {
		if id := segs[i].Language.Language; id != lang.Und {
			s.lastLanguage = id
			return
		}
	}
	if id := s.Language(); id != lang.Und {
		s.lastLanguage = id
	}
}

// LanguagePreference 回傳目前語言在最近送出語言之後的弱偏好（自然對數）。
// 宿主可用它讓候選仲裁偏向使用者常切換到的語言；0 表示沒有證據。
func (s *Session) LanguagePreference(id lang.ID) float64 {
	if s == nil || s.model == nil {
		return 0
	}
	return s.model.LanguagePreference(id, s.lastLanguage)
}

// RankLanguageCandidates 依分語言學習與跨語言偏好穩定重排候選。
// 同分時保留原本順序；詞面計數只在同一語言內生效。
func (s *Session) RankLanguageCandidates(cands []personal.LanguageCandidate) []personal.LanguageCandidate {
	if s == nil || s.model == nil {
		return cands
	}
	return s.model.RankLanguageCandidates(cands, s.lastLanguage)
}

// UndoLanguage 復原最近一次分語言學習。
func (s *Session) UndoLanguage() bool {
	return s != nil && s.model != nil && s.model.UndoLanguage()
}

// ForgetLanguage 忘記某個語言裡的一個詞面。
func (s *Session) ForgetLanguage(id lang.ID, surface string) bool {
	return s != nil && s.model != nil && s.model.ForgetLanguage(id, surface)
}

// ResetLanguage 清掉一個語言的學習與涉及它的跨語言轉移。
func (s *Session) ResetLanguage(id lang.ID) bool {
	return s != nil && s.model != nil && s.model.ResetLanguage(id)
}

// ResetAllLanguages 清掉所有分語言學習與跨語言偏好。
func (s *Session) ResetAllLanguages() int {
	if s == nil || s.model == nil {
		return 0
	}
	return s.model.ResetAllLanguages()
}
