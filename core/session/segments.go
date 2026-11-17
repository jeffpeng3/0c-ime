package session

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/decode"
	"github.com/0c-biggo/0c-ime/core/lang"
	rd "github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// Session 實作 segment.SessionSegments（P2-13）。
//
// 片段不是另一份組字狀態：一般情況由目前的注音路徑、原樣字面或語言 runtime
// 即時推導，所以候選選字、長詞合併與游標移動都會自然反映在片段上。只有
// ReplaceSegment／InsertSegment／RemoveSegment 會建立可編輯的片段副本；
// 下一次 Handle 讓 rev 改變後，片段會重新由組字狀態推導。
var _ segment.SessionSegments = (*Session)(nil)

// Segments 回傳目前片段副本。呼叫端修改回傳值不影響 session。
func (s *Session) Segments() []segment.Segment {
	if s == nil {
		return nil
	}
	if s.segmentManual && s.segmentRev == s.rev {
		return cloneSessionSegments(s.segments)
	}
	return cloneSessionSegments(s.segmentView())
}

// Cursor 回傳游標在目前片段輸入中的 rune 位置。
func (s *Session) Cursor() int {
	if s == nil {
		return 0
	}
	if (s.segmentManual && s.segmentRev == s.rev) || s.segmentCursorSet && s.segmentCursorRev == s.rev {
		return max(0, s.segmentCursor)
	}
	return s.segmentCursorFromComposition()
}

// ActiveSegment 回傳游標所在片段的索引；沒有片段時回傳 -1。
//
// 游標剛好在片段結尾時屬於下一個片段（Span 是半開區間）；在整段輸入結尾時
// 仍停在最後一個片段，因為那裡是使用者正在編輯的位置。
func (s *Session) ActiveSegment() int {
	segs := s.Segments()
	if len(segs) == 0 {
		return -1
	}
	pos := s.Cursor()
	if i, ok := s.SegmentAt(pos); ok {
		return i
	}
	if pos >= segmentEnd(segs) {
		return len(segs) - 1
	}
	return -1
}

// SegmentAt 回傳包含輸入位置 pos 的片段索引。
func (s *Session) SegmentAt(pos int) (int, bool) {
	for i, seg := range s.Segments() {
		if seg.Span.Contains(pos) {
			return i, true
		}
	}
	return 0, false
}

// SetCursor 移動游標，並讓包含新位置的片段成為目前片段。
//
// 注音組字中會同時移動既有的音節游標，因此後續輸入、退格與選字都在新的
// 位置發生。位置超出目前片段結尾時回傳 false，游標不變；空輸入只接受 0。
func (s *Session) SetCursor(pos int) bool {
	if s == nil || pos < 0 {
		return false
	}
	segs := s.Segments()
	if len(segs) == 0 {
		if pos != 0 {
			return false
		}
		s.setSegmentCursor(0)
		return true
	}
	if pos > segmentEnd(segs) {
		return false
	}
	if s.Language() == lang.Zh && !s.segmentManual {
		s.flushMixed()
		if at, ok := s.syllableAtRune(pos, segs); ok {
			s.moveTo(at)
		}
	}
	s.setSegmentCursor(pos)
	return true
}

// ReplaceSegment 替換索引 index 的片段，保留其他片段與它們的語言標籤。
func (s *Session) ReplaceSegment(index int, seg segment.Segment) bool {
	if s == nil || index < 0 || !seg.Valid() {
		return false
	}
	editable := s.editableSegments()
	if index >= len(editable) {
		return false
	}
	editable[index] = cloneSessionSegment(seg)
	s.segments = editable
	s.segmentManual, s.segmentRev = true, s.rev
	return true
}

// InsertSegment 在索引 index 前插入片段；index 等於片段數時附加在尾端。
func (s *Session) InsertSegment(index int, seg segment.Segment) bool {
	if s == nil || index < 0 || !seg.Valid() {
		return false
	}
	editable := s.editableSegments()
	if index > len(editable) {
		return false
	}
	editable = slices.Insert(editable, index, cloneSessionSegment(seg))
	s.segments = editable
	s.segmentManual, s.segmentRev = true, s.rev
	s.clampSegmentCursor()
	return true
}

// RemoveSegment 移除索引 index 的片段。這裡不合併相鄰片段，讓其他片段的
// 邊界與語言標籤保持原樣。
func (s *Session) RemoveSegment(index int) bool {
	if s == nil || index < 0 {
		return false
	}
	editable := s.editableSegments()
	if index >= len(editable) {
		return false
	}
	editable = slices.Delete(editable, index, index+1)
	s.segments = editable
	s.segmentManual, s.segmentRev = true, s.rev
	s.clampSegmentCursor()
	return true
}

// SegmentLocked 回報第 index 個片段是否已被使用者選字鎖定。
func (s *Session) SegmentLocked(index int) bool {
	start, end, ok := s.segmentSyllables(index)
	if !ok {
		return false
	}
	for at, lock := range s.locks {
		if at < end && start < at+lock.Len {
			return true
		}
	}
	return false
}

// LockSegment 鎖定第 index 個片段，鎖定詞面就是目前片段顯示的文字。
// 一般選字會透過 selectCandidate 自動建立同樣的鎖定；這個方法供沒有候選
// 視窗的呼叫端（例如滑鼠選取）使用。
func (s *Session) LockSegment(index int) bool {
	start, end, ok := s.segmentSyllables(index)
	if !ok || start >= end {
		return false
	}
	segs := s.Segments()
	if index < 0 || index >= len(segs) {
		return false
	}
	for at, lock := range s.locks {
		if at < end && start < at+lock.Len {
			delete(s.locks, at)
		}
	}
	s.locks[start] = decode.Lock{Word: segs[index].Surface, Len: end - start}
	s.refresh()
	return true
}

// UnlockSegment 解除第 index 個片段上的選字鎖定。
func (s *Session) UnlockSegment(index int) bool {
	start, end, ok := s.segmentSyllables(index)
	if !ok {
		return false
	}
	changed := false
	for at, lock := range s.locks {
		if at < end && start < at+lock.Len {
			delete(s.locks, at)
			changed = true
		}
	}
	if changed {
		s.refresh()
	}
	return changed
}

// segmentView 依目前語言推導片段，不修改 session 狀態。
func (s *Session) segmentView() []segment.Segment {
	if s.Language() != lang.Zh {
		return s.languageSegments()
	}
	return s.zhuyinSegments()
}

// zhuyinSegments 把注音路徑轉成片段。長詞候選在 path 中本來就是一個 Seg，
// 所以跨音節的詞會自然成為單一片段；原樣英數與符號再依 script 細分。
func (s *Session) zhuyinSegments() []segment.Segment {
	if len(s.path) == 0 {
		return s.fallbackSegments()
	}
	out := make([]segment.Segment, 0, len(s.path))
	at := 0
	for i := 0; i < len(s.path); {
		g := s.path[i]
		n := utf8.RuneCountInString(g.Word)
		if s.literalAt(g.Start) {
			var text strings.Builder
			start := i
			for i < len(s.path) && s.literalAt(s.path[i].Start) && s.path[i].Start == s.path[start].Start+(i-start) {
				text.WriteString(s.path[i].Word)
				i++
			}
			out = append(out, literalSegments(text.String(), at)...)
			at += utf8.RuneCountInString(text.String())
			continue
		}
		out = append(out, segment.Segment{
			Span:     segment.Span{Start: at, End: at + n},
			Script:   wordScript(g.Word),
			Reading:  s.readingAt(g.Start, g.End),
			Surface:  g.Word,
			Language: lang.Tag(lang.Zh),
			Variant:  lang.VariantZhHant,
		})
		at += n
		i++
	}
	return out
}

// fallbackSegments 在整段解不出路徑時，用畫面上實際顯示的字面建立片段。
func (s *Session) fallbackSegments() []segment.Segment {
	if len(s.syls) == 0 {
		return nil
	}
	out := make([]segment.Segment, 0, len(s.syls))
	at := 0
	for i, syl := range s.syls {
		text, _ := s.fallbackDisplay(i, syl)
		if s.literalAt(i) {
			out = append(out, literalSegments(text, at)...)
		} else {
			out = append(out, segment.Segment{
				Span:     segment.Span{Start: at, End: at + utf8.RuneCountInString(text)},
				Script:   wordScript(text),
				Reading:  rd.Sequence{rd.Unit(syl)},
				Surface:  text,
				Language: lang.Tag(lang.Zh),
				Variant:  lang.VariantZhHant,
			})
		}
		at += utf8.RuneCountInString(text)
	}
	return out
}

// languageSegments 把目前語言的 runtime 轉成片段。変換中每個文節是一片；
// かな／羅馬字組字時每個已完成的讀音單位是一片，尚未完成的輸入接在尾端。
func (s *Session) languageSegments() []segment.Segment {
	r, ok := s.activeRuntime()
	if !ok || r.composer == nil {
		return nil
	}
	out := make([]segment.Segment, 0)
	at := 0
	add := func(text string, seq rd.Sequence) {
		if text == "" {
			return
		}
		out = append(out, segment.Segment{
			Span:     segment.Span{Start: at, End: at + utf8.RuneCountInString(text)},
			Script:   wordScript(text),
			Reading:  append(rd.Sequence(nil), seq...),
			Surface:  text,
			Language: lang.Tag(lang.Ja),
			Variant:  lang.VariantJaShinjitai,
		})
		at += utf8.RuneCountInString(text)
	}
	if r.converting {
		for _, c := range r.clauses {
			add(c.Surface, c.Reading)
		}
		return out
	}
	if r.surface != "" {
		add(r.surface, nil)
		return out
	}
	for _, u := range r.composer.Units() {
		add(string(u), rd.Sequence{u})
	}
	if pending := r.composer.Pending(); pending != "" {
		add(pending, nil)
	}
	return out
}

// literalSegments 把一段原樣文字依 script 切成片段。語言標籤只放能從
// script 直接判定的值；Latin 偏好英文輸入，但片段本身仍只是標籤，不是模式。
func literalSegments(text string, offset int) []segment.Segment {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	parts := segment.Split(text)
	out := make([]segment.Segment, 0, len(parts))
	for _, part := range parts {
		relStart, relEnd := part.Span.Start, part.Span.End
		seg := part
		seg.Span.Start += offset
		seg.Span.End += offset
		seg.Surface = string(runes[relStart:relEnd])
		if id, ok := literalLanguage(part.Script); ok {
			seg.Language = lang.Tag(id)
			seg.Variant = lang.DefaultVariant(id)
		}
		out = append(out, seg)
	}
	return out
}

func literalLanguage(script segment.Script) (lang.ID, bool) {
	switch script {
	case segment.ScriptZhuyin, segment.ScriptHan:
		return lang.Zh, true
	case segment.ScriptKana:
		return lang.Ja, true
	case segment.ScriptLatin:
		return lang.En, true
	}
	return lang.Und, false
}

func wordScript(text string) segment.Script {
	for _, r := range text {
		return segment.Detect(r)
	}
	return segment.ScriptUnknown
}

func (s *Session) literalAt(pos int) bool {
	return s.literals != nil && s.literals[pos]
}

func (s *Session) readingAt(start, end int) rd.Sequence {
	end = min(end, len(s.syls))
	if start < 0 || start >= end {
		return nil
	}
	out := make(rd.Sequence, 0, end-start)
	for _, syl := range s.syls[start:end] {
		out = append(out, rd.Unit(zhuyin.Syllable(syl)))
	}
	return out
}

// segmentCursorFromComposition 把既有的音節游標換成顯示文字的 rune 位置。
func (s *Session) segmentCursorFromComposition() int {
	if s.Language() != lang.Zh {
		at := 0
		for _, seg := range s.languageSegments() {
			at = max(at, seg.Span.End)
		}
		return at
	}
	at := 0
	for _, g := range s.path {
		if s.cursor <= g.Start {
			break
		}
		runes := []rune(g.Word)
		if s.cursor < g.End {
			k := min(s.cursor-g.Start, len(runes))
			at += k
			return at
		}
		at += len(runes)
	}
	if s.comp.String() != "" {
		at += utf8.RuneCountInString(s.comp.String())
	}
	if s.mixed.raw != "" {
		at += utf8.RuneCountInString(s.mixedText())
	}
	return at
}

// syllableAtRune 把顯示文字的 rune 位置換回音節位置。
func (s *Session) syllableAtRune(pos int, segs []segment.Segment) (int, bool) {
	if len(s.path) == 0 {
		return s.syllableAtRuneFallback(pos, segs)
	}
	at := 0
	for _, g := range s.path {
		n := utf8.RuneCountInString(g.Word)
		switch {
		case pos == at:
			return g.Start, true
		case pos < at+n:
			off := pos - at
			if off >= g.End-g.Start {
				return g.End, true
			}
			return g.Start + off, true
		}
		at += n
	}
	return len(s.syls), true
}

func (s *Session) syllableAtRuneFallback(pos int, segs []segment.Segment) (int, bool) {
	at := 0
	for i, syl := range s.syls {
		text, _ := s.fallbackDisplay(i, syl)
		n := utf8.RuneCountInString(text)
		if pos < at+n {
			return i, true
		}
		at += n
	}
	return len(s.syls), true
}

func (s *Session) setSegmentCursor(pos int) {
	s.segmentCursor = pos
	s.segmentCursorSet = true
	s.segmentCursorRev = s.rev
	s.segmentManual = s.segmentManual && s.segmentRev == s.rev
}

func (s *Session) editableSegments() []segment.Segment {
	if s.segmentManual && s.segmentRev == s.rev {
		return s.segments
	}
	s.segments = cloneSessionSegments(s.segmentView())
	s.segmentManual = true
	s.segmentRev = s.rev
	return s.segments
}

func (s *Session) clampSegmentCursor() {
	if s.segmentCursor > segmentEnd(s.segments) {
		s.segmentCursor = segmentEnd(s.segments)
	}
}

// segmentSyllables 找片段對應的音節範圍，供鎖定查詢與操作使用。
func (s *Session) segmentSyllables(index int) (int, int, bool) {
	segs := s.Segments()
	if index < 0 || index >= len(segs) {
		return 0, 0, false
	}
	target := segs[index]
	at := 0
	start, end := -1, -1
	for _, g := range s.path {
		n := utf8.RuneCountInString(g.Word)
		if target.Span.Start < at+n && at < target.Span.End {
			if start < 0 {
				start = g.Start
			}
			end = g.End
		}
		at += n
	}
	if start < 0 || end <= start {
		// 保底顯示沒有 path：每個音節以自己的顯示長度佔一段。
		at = 0
		for i, syl := range s.syls {
			n := utf8.RuneCountInString(fallbackText(s, i, syl))
			if target.Span.Start < at+n && at < target.Span.End {
				if start < 0 {
					start = i
				}
				end = i + 1
			}
			at += n
		}
	}
	return start, end, start >= 0 && end > start
}

func fallbackText(s *Session, pos int, syl zhuyin.Syllable) string {
	text, _ := s.fallbackDisplay(pos, syl)
	return text
}

func segmentEnd(segs []segment.Segment) int {
	end := 0
	for _, seg := range segs {
		end = max(end, seg.Span.End)
	}
	return end
}

func cloneSessionSegments(in []segment.Segment) []segment.Segment {
	if in == nil {
		return nil
	}
	out := make([]segment.Segment, len(in))
	for i, seg := range in {
		out[i] = cloneSessionSegment(seg)
	}
	return out
}

func cloneSessionSegment(in segment.Segment) segment.Segment {
	in.Reading = append(rd.Sequence(nil), in.Reading...)
	return in
}
