package segment_test

import (
	"encoding/json"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

func TestSpan(t *testing.T) {
	span := segment.Span{Start: 2, End: 5}
	if !span.Valid() || span.Len() != 3 {
		t.Fatalf("span = %#v, Valid=%v Len=%d", span, span.Valid(), span.Len())
	}
	if !span.Contains(2) || !span.Contains(4) || span.Contains(5) || span.Contains(1) {
		t.Errorf("Contains 邊界錯誤")
	}
	if (segment.Span{Start: -1, End: 2}).Valid() {
		t.Error("負起點應為無效")
	}
	if (segment.Span{Start: 3, End: 2}).Valid() {
		t.Error("終點小於起點應為無效")
	}
}

func TestScriptNamesAndStrength(t *testing.T) {
	tests := []struct {
		script segment.Script
		name   string
		strong bool
	}{
		{segment.ScriptUnknown, "unknown", false},
		{segment.ScriptZhuyin, "zhuyin", true},
		{segment.ScriptKana, "kana", true},
		{segment.ScriptLatin, "latin", false},
		{segment.ScriptDigit, "digit", false},
		{segment.ScriptSymbol, "symbol", false},
		{segment.ScriptHan, "han", false},
		{segment.ScriptMixed, "mixed", false},
	}
	for _, tt := range tests {
		if got := tt.script.String(); got != tt.name {
			t.Errorf("Script(%d).String() = %q, want %q", tt.script, got, tt.name)
		}
		if got := tt.script.Strong(); got != tt.strong {
			t.Errorf("%s.Strong() = %v, want %v", tt.name, got, tt.strong)
		}
		got, err := segment.ParseScript(tt.name)
		if err != nil || got != tt.script {
			t.Errorf("ParseScript(%q) = %v, %v", tt.name, got, err)
		}
	}
	if _, err := segment.ParseScript("emoji"); err == nil {
		t.Error("未知 script 應回傳錯誤")
	}
}

func TestScriptJSONRoundTrip(t *testing.T) {
	type wrapper struct {
		Script segment.Script `json:"script"`
	}
	b, err := json.Marshal(wrapper{Script: segment.ScriptZhuyin})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"script":"zhuyin"}` {
		t.Fatalf("marshal = %s", b)
	}
	var got wrapper
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Script != segment.ScriptZhuyin {
		t.Fatalf("unmarshal = %v", got.Script)
	}
}

func TestSegmentValid(t *testing.T) {
	seg := segment.Segment{
		Span:     segment.Span{Start: 0, End: 2},
		Script:   segment.ScriptZhuyin,
		Reading:  reading.Sequence{"ㄓㄨˋ", "ㄧㄣ"},
		Surface:  "注音",
		Language: lang.Tag(lang.Zh).WithConfidence(0.9),
		Variant:  lang.VariantZhHant,
	}
	if !seg.Valid() {
		t.Fatal("合法片段被判為無效")
	}
	if got := seg.ReadingKey(); got != "ㄓㄨˋ ㄧㄣ" {
		t.Errorf("ReadingKey() = %q", got)
	}

	bad := []segment.Segment{
		{Span: segment.Span{Start: -1, End: 1}},
		{Span: segment.Span{Start: 0, End: 1}, Variant: "zh-TW"},
		{Span: segment.Span{Start: 0, End: 1}, Language: lang.LanguageTag{Language: lang.Zh, Confidence: 2}},
		{Span: segment.Span{Start: 0, End: 1}, Reading: reading.Sequence{"x"}},
	}
	for i, s := range bad {
		if s.Valid() {
			t.Errorf("bad[%d] 應為無效: %#v", i, s)
		}
	}
}

func TestSessionSegmentsKeepsPerSegmentLanguages(t *testing.T) {
	var _ segment.SessionSegments = (*memSession)(nil)
	s := &memSession{segs: []segment.Segment{
		{Span: segment.Span{Start: 0, End: 2}, Script: segment.ScriptZhuyin, Language: lang.Tag(lang.Zh)},
		{Span: segment.Span{Start: 2, End: 5}, Script: segment.ScriptKana, Language: lang.Tag(lang.Ja)},
		{Span: segment.Span{Start: 5, End: 10}, Script: segment.ScriptLatin, Language: lang.Tag(lang.En)},
	}}
	s.active = 0
	if got := s.ActiveSegment(); got != 0 {
		t.Fatalf("ActiveSegment = %d, want 0", got)
	}
	if !s.SetCursor(3) || s.ActiveSegment() != 1 {
		t.Fatalf("游標移到日文片段後 active = %d", s.ActiveSegment())
	}
	if !s.SetCursor(7) || s.ActiveSegment() != 2 {
		t.Fatalf("游標移到英文片段後 active = %d", s.ActiveSegment())
	}
	if s.SetCursor(-1) {
		t.Error("負游標應被拒絕")
	}
	if s.SetCursor(11) {
		t.Error("超出範圍的游標應被拒絕")
	}
	if got := s.Segments(); len(got) != 3 || got[0].Language.Language != lang.Zh || got[1].Language.Language != lang.Ja || got[2].Language.Language != lang.En {
		t.Fatalf("片段語言標籤被改變: %#v", got)
	}
	if !s.ReplaceSegment(1, segment.Segment{Span: segment.Span{Start: 2, End: 5}, Surface: "は", Language: lang.Tag(lang.Ja)}) {
		t.Fatal("ReplaceSegment 失敗")
	}
	if s.ReplaceSegment(9, segment.Segment{}) {
		t.Fatal("越界 ReplaceSegment 應失敗")
	}
	if !s.InsertSegment(3, segment.Segment{Span: segment.Span{Start: 10, End: 11}}) {
		t.Fatal("InsertSegment 尾端失敗")
	}
	if !s.RemoveSegment(3) {
		t.Fatal("RemoveSegment 失敗")
	}
}

type memSession struct {
	segs   []segment.Segment
	cursor int
	active int
}

func (m *memSession) Segments() []segment.Segment {
	return append([]segment.Segment(nil), m.segs...)
}

func (m *memSession) Cursor() int { return m.cursor }

func (m *memSession) ActiveSegment() int {
	if len(m.segs) == 0 {
		return -1
	}
	return m.active
}

func (m *memSession) SegmentAt(pos int) (int, bool) {
	for i, s := range m.segs {
		if s.Span.Contains(pos) {
			return i, true
		}
	}
	return 0, false
}

func (m *memSession) SetCursor(pos int) bool {
	if pos < 0 {
		return false
	}
	if len(m.segs) == 0 {
		if pos != 0 {
			return false
		}
		m.cursor, m.active = 0, -1
		return true
	}
	if pos > m.segs[len(m.segs)-1].Span.End {
		return false
	}
	m.cursor = pos
	if i, ok := m.SegmentAt(pos); ok {
		m.active = i
	}
	return true
}

func (m *memSession) ReplaceSegment(index int, seg segment.Segment) bool {
	if index < 0 || index >= len(m.segs) {
		return false
	}
	m.segs[index] = seg
	return true
}

func (m *memSession) InsertSegment(index int, seg segment.Segment) bool {
	if index < 0 || index > len(m.segs) {
		return false
	}
	m.segs = append(m.segs, segment.Segment{})
	copy(m.segs[index+1:], m.segs[index:])
	m.segs[index] = seg
	return true
}

func (m *memSession) RemoveSegment(index int) bool {
	if index < 0 || index >= len(m.segs) {
		return false
	}
	m.segs = append(m.segs[:index], m.segs[index+1:]...)
	if m.active >= len(m.segs) {
		m.active = len(m.segs) - 1
	}
	return true
}
