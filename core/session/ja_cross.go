package session

import (
	"strings"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	jlang "github.com/0c-biggo/0c-ime/core/lang/ja"
	"github.com/0c-biggo/0c-ime/core/proto"
	rd "github.com/0c-biggo/0c-ime/core/reading"
)

// 日英同形的即時候選（P2-54）。
//
// 預設模式 both：一段小寫 ASCII 按鍵如果同時
//   - 能完整組成合法羅馬字（沒有未完成的羅馬字前綴），而且
//   - 是英文詞典的詞或前綴，
//
// 就把這段按鍵交給既有日文 runtime（和 ja-ambiguous=kana 走同一條かな変換），
// 並在同一份候選清單依序列出：かな本身、日文詞典的漢字候選（維持原本順序）、
// 英文原文（最後一項）。清單隨每個按鍵重建，開啟不需要修飾鍵；Enter 直接送
// 目前反白的候選，因此不選字時送かな。
//
// 只有真正同形時才建立這份跨語言清單：打不成羅馬字（hello）、識別字
// （kubectl）、網址、英數混合與大寫開頭都留在原本的英文路徑。明確設定
// english 或 kana 的使用者也不受影響。

type japaneseCrossState struct {
	raw     string
	english string
}

func (s *Session) crossActive() bool {
	return s != nil && s.japaneseCross.raw != ""
}

// japaneseCrossAmbiguous 回報 raw 是否同時是完整合法羅馬字與英文詞／前綴。
func (s *Session) japaneseCrossAmbiguous(raw string) (string, bool) {
	if s == nil || !s.japaneseOn || !s.japaneseAmbiguousBoth() {
		return "", false
	}
	if len(raw) < 2 || raw != strings.ToLower(raw) || !asciiLetters(raw) {
		return "", false
	}
	// 合法注音序列永遠留在注音路徑，注音 golden 不變。
	if _, phonetic := s.mixedComposer(raw); phonetic {
		return "", false
	}
	// KanaFor 的 PreferKana=false 會把英文詞判定成照字面；同形判斷需要的是
	// 「按鍵本身能不能組成かな」，所以用同一個組字器、只關掉英文優先。
	cfg := s.japaneseCfg
	cfg.PreferKana = true
	kana, pending, ok := jlang.KanaFor(raw, cfg)
	if !ok || pending != "" || kana == "" {
		return "", false
	}
	if !s.englishPrior(raw) {
		return "", false
	}
	return kana, true
}

// startJapaneseCross 把目前待定按鍵交給日文 runtime，並建立日英並列候選。
func (s *Session) startJapaneseCross() bool {
	raw := s.mixed.raw
	if _, ok := s.japaneseCrossAmbiguous(raw); !ok {
		return false
	}
	if !s.beginJapaneseRun(raw, true) {
		return false
	}
	s.japaneseCross = japaneseCrossState{raw: raw, english: raw}
	s.navigating = false
	if !s.rebuildJapaneseCross() {
		s.japaneseCross = japaneseCrossState{}
		s.crossEndConversion()
	}
	return true
}

// rebuildJapaneseCross 用目前 composer 的かな讀音重建候選清單，重用既有的
// 日文変換器產生漢字候選；英文原文固定放在最後一項。
func (s *Session) rebuildJapaneseCross() bool {
	if !s.crossActive() {
		return false
	}
	r, ok := s.activeRuntime()
	if !ok || r.composer == nil || r.converter == nil {
		return false
	}
	if r.composer.Pending() != "" {
		return false
	}
	units := append(rd.Sequence(nil), r.composer.Units()...)
	if len(units) == 0 {
		return false
	}
	kana := sequenceText(units)
	items := s.japaneseCrossCandidates(r, units, kana, s.japaneseCross.english)
	if len(items) == 0 {
		return false
	}
	clause := lang.Clause{
		Reading:  append(rd.Sequence(nil), units...),
		Surface:  kana,
		Segments: []lang.CandidateSegment{{Reading: append(rd.Sequence(nil), units...), Surface: kana}},
	}
	r.clauses = []lang.Clause{clause}
	r.clause = 0
	r.converting = true
	r.surface = ""
	r.candidates = items
	r.candidate = 0
	r.candidateFor = 0
	return true
}

func (s *Session) japaneseCrossCandidates(r *languageRuntime, units rd.Sequence, kana, english string) []lang.Candidate {
	items := []lang.Candidate{{
		Surface: kana,
		Reading: append(rd.Sequence(nil), units...),
		Clauses: []lang.Clause{{
			Reading:  append(rd.Sequence(nil), units...),
			Surface:  kana,
			Segments: []lang.CandidateSegment{{Reading: append(rd.Sequence(nil), units...), Surface: kana}},
		}},
		Source: "kana",
	}}
	seen := map[string]bool{kana: true}
	if r.converter != nil {
		if cands, err := r.converter.Candidates(units, lang.Span{Start: 0, Length: len(units)}, s.languageContext()); err == nil {
			for _, cand := range cands {
				if cand.Surface == "" || seen[cand.Surface] || cand.Surface == english {
					continue
				}
				seen[cand.Surface] = true
				items = append(items, cand)
			}
		}
	}
	if english != "" && !seen[english] {
		reading := runeSequence(english)
		items = append(items, lang.Candidate{
			Surface: english,
			Reading: reading,
			Clauses: []lang.Clause{{
				Reading:  append(rd.Sequence(nil), reading...),
				Surface:  english,
				Segments: []lang.CandidateSegment{{Reading: append(rd.Sequence(nil), reading...), Surface: english}},
			}},
			Source: "literal",
		})
	}
	return items
}

func runeSequence(text string) rd.Sequence {
	if text == "" {
		return nil
	}
	out := make(rd.Sequence, 0, utf8.RuneCountInString(text))
	for _, r := range text {
		out = append(out, rd.Unit(string(r)))
	}
	return out
}

func (s *Session) crossEndConversion() {
	if r, ok := s.activeRuntime(); ok {
		r.endConversion()
	}
}

// crossKey 處理日英並列清單開啟時的按鍵：↓ 進入選取模式並換反白、↑ 換反白、
// 選取模式中 ←／→ 翻頁、Home／End 到第一個／最後一個候選、Enter 送出目前候選、
// 退格回到上一個按鍵；字母照日文 runtime 的一般路徑處理，清單即時重建。
// 顯示中但還沒按 ↓ 時，方向鍵不會翻頁（與 P2-51 的候選窗一致）。
func (s *Session) crossKey(k string) (handled bool, commit string, ok bool) {
	if !s.crossActive() || s.Language() != lang.Ja {
		return false, "", false
	}
	switch k {
	case proto.KeyEnter:
		s.navigating = false
		return true, s.languageCommit(), true
	case proto.KeyDown:
		s.navigating = true
		s.crossMove(1)
		return true, "", true
	case proto.KeyUp:
		s.navigating = true
		s.crossMove(-1)
		return true, "", true
	case proto.KeyLeft:
		if !s.navigating {
			return false, "", false
		}
		s.crossPage(-1)
		return true, "", true
	case proto.KeyRight:
		if !s.navigating {
			return false, "", false
		}
		s.crossPage(1)
		return true, "", true
	case proto.KeyHome:
		if !s.navigating {
			return false, "", false
		}
		s.crossEdge(false)
		return true, "", true
	case proto.KeyEnd:
		if !s.navigating {
			return false, "", false
		}
		s.crossEdge(true)
		return true, "", true
	case proto.KeyBackspace:
		return s.crossBackspace(), "", true
	}
	return false, "", false
}

// crossPage 在選取模式中換頁，反白停在該頁第一項；只有一頁時留在原頁。
func (s *Session) crossPage(d int) {
	r, ok := s.activeRuntime()
	if !ok || !r.converting || len(r.candidates) == 0 {
		return
	}
	pages := s.languagePages(r)
	page := ((r.candidate/perPage+d)%pages + pages) % pages
	s.crossApply(r, page*perPage)
}

// crossEdge 在選取模式中跳到第一個／最後一個候選（Home／End）。
func (s *Session) crossEdge(last bool) {
	r, ok := s.activeRuntime()
	if !ok || !r.converting || len(r.candidates) == 0 {
		return
	}
	id := 0
	if last {
		id = len(r.candidates) - 1
	}
	s.crossApply(r, id)
}

func (s *Session) crossMove(d int) {
	r, ok := s.activeRuntime()
	if !ok || !r.converting || len(r.candidates) == 0 {
		return
	}
	n := len(r.candidates)
	s.crossApply(r, (r.candidate+d+n)%n)
}

func (s *Session) crossApply(r *languageRuntime, id int) {
	if id < 0 || id >= len(r.candidates) {
		return
	}
	cand := r.candidates[id]
	s.languageApplyCandidate(r, cand)
	r.candidate = id
	r.candidateFor = r.clause
}

// selectJapaneseCross 是候選點擊：直接套用並送出點選的解釋（P2-54）。
func (s *Session) selectJapaneseCross(id int) (string, bool) {
	r, ok := s.activeRuntime()
	if !ok || !s.crossActive() || !r.converting || id < 0 || id >= len(r.candidates) {
		return "", false
	}
	s.crossApply(r, id)
	return s.languageCommit(), true
}

// crossBackspace 把日英並列的那一串按鍵退一格；退到空字串時回到注音路徑。
func (s *Session) crossBackspace() bool {
	raw := []rune(s.japaneseCross.raw)
	if len(raw) == 0 {
		return false
	}
	r, ok := s.activeRuntime()
	if !ok {
		s.japaneseCross = japaneseCrossState{}
		return false
	}
	next := string(raw[:len(raw)-1])
	if next == "" {
		r.reset()
		s.japaneseCross = japaneseCrossState{}
		s.navigating = false
		s.languageID = lang.Zh
		s.japaneseRun = false
		s.mixed = pendingKeys{}
		s.refresh()
		return true
	}
	r.composer.Reset()
	for _, k := range next {
		r.composer.Insert(string(k))
	}
	r.endConversion()
	s.navigating = false
	if _, ambiguous := s.japaneseCrossAmbiguous(next); ambiguous {
		s.japaneseCross.raw = next
		s.japaneseCross.english = next
		s.rebuildJapaneseCross()
	} else {
		s.japaneseCross = japaneseCrossState{}
	}
	s.refresh()
	return true
}

// afterJapaneseInsert 在日文 runtime 收下一個按鍵後即時更新同形清單。只要目前的
// 英數段又同時是完整羅馬字與英文詞／前綴就重建清單；不是同形時關掉跨語言清單，
// 留在一般日文（かな）或原本的英文 fallback。
func (s *Session) afterJapaneseInsert(string) {
	s.navigating = false // 打字離開選取模式，新的片段重新從かな開始（P2-51／P2-54）
	if s.Language() != lang.Ja {
		s.japaneseCross = japaneseCrossState{}
		return
	}
	if !s.japaneseAmbiguousBoth() || !s.japaneseRun {
		s.japaneseCross = japaneseCrossState{}
		s.crossEndConversion()
		return
	}
	r, ok := s.activeRuntime()
	if !ok || r.composer == nil {
		s.japaneseCross = japaneseCrossState{}
		s.crossEndConversion()
		return
	}
	raw, literal := japaneseComposerRun(r.composer)
	if literal || raw == "" {
		s.japaneseCross = japaneseCrossState{}
		s.crossEndConversion()
		return
	}
	if _, ambiguous := s.japaneseCrossAmbiguous(raw); ambiguous {
		s.japaneseCross.raw = raw
		s.japaneseCross.english = raw
		if !s.rebuildJapaneseCross() {
			s.japaneseCross = japaneseCrossState{}
			s.crossEndConversion()
		}
		return
	}
	// 追加字母後已經不是完整羅馬字，但整串正好是英文詞（sakes）：照英文留下，
	// 不要停在「さけs」這種かな＋未完成羅馬字。單純的英文前綴（kanj）仍留在
	// 日文路徑，讓使用者可以繼續打成かんじ。
	if s.englishExact(raw) {
		s.leaveJapaneseLiteral(raw)
		return
	}
	s.japaneseCross = japaneseCrossState{}
	s.crossEndConversion()
}

func (s *Session) englishExact(text string) bool {
	if s == nil || text == "" {
		return false
	}
	if s.English != nil && s.English.Has(text) {
		return true
	}
	return s.personalEnglishAllowed() && s.model != nil && s.model.EnglishKnown(text)
}

// leaveJapaneseLiteral 把推斷出來的日文段整段改成照字面的英數，回到注音路徑。
func (s *Session) leaveJapaneseLiteral(text string) bool {
	if r, ok := s.activeRuntime(); ok {
		r.reset()
	}
	s.languageID = lang.Zh
	s.japaneseRun = false
	s.japaneseCross = japaneseCrossState{}
	s.mixed = pendingKeys{raw: text}
	s.mixedOrigins = nil
	s.cand = nil
	s.refresh()
	return true
}

type japaneseRunComposer interface {
	Run() (string, bool)
}

func japaneseComposerRun(c lang.Composer) (string, bool) {
	r, ok := c.(japaneseRunComposer)
	if !ok {
		return "", false
	}
	return r.Run()
}
