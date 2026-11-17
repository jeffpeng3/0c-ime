package session

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	rd "github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

func TestSessionSegmentsFollowZhuyinPathAndCursor(t *testing.T) {
	s := newSession()
	typeSyl(s, "ㄨㄛˇ")
	typeSyl(s, "ㄒㄧㄤˇ")
	typeSyl(s, "ㄔ")

	segs := s.Segments()
	if len(segs) != 3 {
		t.Fatalf("片段數 = %d, want 3: %#v", len(segs), segs)
	}
	want := []string{"我", "想", "吃"}
	for i, w := range want {
		if segs[i].Surface != w {
			t.Errorf("segs[%d].Surface = %q, want %q", i, segs[i].Surface, w)
		}
		if segs[i].Language.Language != lang.Zh {
			t.Errorf("segs[%d] 語言 = %q, want zh", i, segs[i].Language.Language)
		}
		if segs[i].Span.Start != i || segs[i].Span.End != i+1 {
			t.Errorf("segs[%d].Span = %#v", i, segs[i].Span)
		}
	}
	if got := s.Cursor(); got != 3 {
		t.Fatalf("Cursor = %d, want 3", got)
	}
	if got := s.ActiveSegment(); got != 2 {
		t.Fatalf("ActiveSegment = %d, want 2", got)
	}

	if !s.SetCursor(1) {
		t.Fatal("SetCursor(1) 失敗")
	}
	if got := s.Cursor(); got != 1 {
		t.Fatalf("SetCursor(1) 後 Cursor = %d", got)
	}
	if got := s.ActiveSegment(); got != 1 {
		t.Fatalf("游標移到「想」後 ActiveSegment = %d, want 1", got)
	}
	if !s.SetCursor(0) || s.ActiveSegment() != 0 {
		t.Fatalf("游標移到開頭後 ActiveSegment = %d", s.ActiveSegment())
	}
	if !s.SetCursor(3) || s.ActiveSegment() != 2 {
		t.Fatalf("游標移到結尾後 ActiveSegment = %d", s.ActiveSegment())
	}
	if s.SetCursor(-1) || s.SetCursor(4) {
		t.Fatal("超出範圍的 SetCursor 應失敗")
	}
	if !s.Composing() {
		t.Fatal("有片段時應仍在組字")
	}
}

func TestSessionSegmentsKeepLongWordMerged(t *testing.T) {
	s := newSession()
	typeSyl(s, "ㄈㄥ")
	typeSyl(s, "ㄐㄧㄥˇ")

	segs := s.Segments()
	if len(segs) != 1 || segs[0].Surface != "風景" {
		t.Fatalf("長詞應合併成單一片段：%#v", segs)
	}
	if segs[0].Span.Len() != 2 || segs[0].Language.Language != lang.Zh {
		t.Fatalf("長詞片段欄位 = %#v", segs[0])
	}
}

func TestSessionSegmentsTagEnglishLiteralAndSwitching(t *testing.T) {
	s := newSession()
	typeSyl(s, "ㄨㄛˇ")
	s.insertLiteral("abc")
	s.refresh()

	segs := s.Segments()
	if len(segs) != 2 {
		t.Fatalf("片段數 = %d: %#v", len(segs), segs)
	}
	if segs[0].Surface != "我" || segs[0].Language.Language != lang.Zh {
		t.Fatalf("中文片段 = %#v", segs[0])
	}
	if segs[1].Surface != "abc" || segs[1].Language.Language != lang.En || segs[1].Variant != lang.VariantEnglish {
		t.Fatalf("英文片段 = %#v", segs[1])
	}
	if segs[1].Span.Start != 1 || segs[1].Span.End != 4 {
		t.Fatalf("英文片段 span = %#v", segs[1].Span)
	}
	if s.ActiveSegment() != 1 {
		t.Fatalf("游標在英文片段時 ActiveSegment = %d", s.ActiveSegment())
	}
	if !s.SetCursor(1) || s.ActiveSegment() != 1 {
		t.Fatalf("SetCursor(1) 後 ActiveSegment = %d", s.ActiveSegment())
	}
	if !s.SetCursor(0) || s.ActiveSegment() != 0 {
		t.Fatalf("SetCursor(0) 後 ActiveSegment = %d", s.ActiveSegment())
	}
}

func TestSessionSegmentsReturnCopiesAndEditLanguages(t *testing.T) {
	s := newSession()
	typeSyl(s, "ㄨㄛˇ")
	s.insertLiteral("abc")
	s.refresh()

	segs := s.Segments()
	segs[0].Surface = "改"
	segs[0].Reading = nil
	if got := s.Segments()[0].Surface; got != "我" {
		t.Fatalf("修改回傳片段不應影響 session，得到 %q", got)
	}

	replace := s.Segments()[0]
	replace.Surface = "は"
	replace.Language = lang.Tag(lang.Ja)
	replace.Variant = lang.VariantJaShinjitai
	if !s.ReplaceSegment(0, replace) {
		t.Fatal("ReplaceSegment(0) 失敗")
	}
	got := s.Segments()
	if len(got) != 2 || got[0].Language.Language != lang.Ja || got[1].Language.Language != lang.En {
		t.Fatalf("替換後語言標籤 = %#v", got)
	}
	if !s.InsertSegment(2, segment.Segment{Span: segment.Span{Start: 4, End: 5}, Surface: "。", Language: lang.Tag(lang.Zh)}) {
		t.Fatal("InsertSegment 尾端失敗")
	}
	if got = s.Segments(); len(got) != 3 || got[1].Language.Language != lang.En {
		t.Fatalf("插入後片段 = %#v", got)
	}
	if !s.RemoveSegment(2) || len(s.Segments()) != 2 {
		t.Fatal("RemoveSegment 失敗")
	}
	if !s.RemoveSegment(0) || s.Segments()[0].Language.Language != lang.En {
		t.Fatal("移除第一個片段後英文標籤應保留")
	}
	if s.RemoveSegment(9) || s.ReplaceSegment(9, segment.Segment{}) || s.InsertSegment(9, segment.Segment{}) {
		t.Fatal("越界操作應失敗")
	}
}

func TestSessionSegmentSelectionLocksOnlyThatSegment(t *testing.T) {
	s := newSession()
	typeSyl(s, "ㄈㄥ")
	typeSyl(s, "ㄐㄧㄥˇ")
	key(s, proto.KeyDown)
	key(s, "1") // 選第一個候選，通常是「風景」

	segs := s.Segments()
	if len(segs) != 1 {
		t.Fatalf("選字後片段 = %#v", segs)
	}
	if !s.SegmentLocked(0) {
		t.Fatal("選字後片段應被鎖定")
	}
	if !s.UnlockSegment(0) || s.SegmentLocked(0) {
		t.Fatal("UnlockSegment 應解除鎖定")
	}
	typeSyl(s, "ㄨㄛˇ")
	segs = s.Segments()
	if len(segs) != 2 {
		t.Fatalf("再輸入後片段 = %#v", segs)
	}
	if !s.LockSegment(1) || !s.SegmentLocked(1) {
		t.Fatal("LockSegment 應鎖定指定片段")
	}
	if s.SegmentLocked(0) {
		t.Fatal("鎖定第二個片段不應影響第一個片段")
	}
}

func TestSessionSegmentsForLanguageRuntime(t *testing.T) {
	s := New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	if !s.AddLanguageProfile(segmentJaProfile{}) || !s.SetLanguage(lang.Ja) {
		t.Fatal("註冊日文 profile 失敗")
	}
	key(s, "か")
	key(s, "な")

	segs := s.Segments()
	if len(segs) != 2 || segs[0].Surface != "か" || segs[1].Surface != "な" {
		t.Fatalf("日文片段 = %#v", segs)
	}
	for i, seg := range segs {
		if seg.Language.Language != lang.Ja || seg.Variant != lang.VariantJaShinjitai {
			t.Errorf("日文 segs[%d] = %#v", i, seg)
		}
	}
	if s.ActiveSegment() != 1 {
		t.Fatalf("日文 ActiveSegment = %d", s.ActiveSegment())
	}
	if !s.SetCursor(1) || s.ActiveSegment() != 1 {
		t.Fatalf("日文 SetCursor(1) 後 ActiveSegment = %d", s.ActiveSegment())
	}
	if !s.SetCursor(0) || s.ActiveSegment() != 0 {
		t.Fatalf("日文 SetCursor(0) 後 ActiveSegment = %d", s.ActiveSegment())
	}
}

type segmentJaProfile struct{}

func (segmentJaProfile) ID() lang.ID { return lang.Ja }

func (segmentJaProfile) InputMethods() []lang.InputMethod {
	return []lang.InputMethod{{ID: "kana", Name: "かな"}}
}

func (segmentJaProfile) Limits() lang.Limits { return lang.DefaultLimits() }

func (segmentJaProfile) KeyPolicy() lang.KeyPolicy { return noneKeyPolicy{} }

func (segmentJaProfile) NewComposer(lang.InputMethod) lang.Composer { return &segmentJaComposer{} }

func (segmentJaProfile) NewConverter() lang.Converter { return nil }

func (segmentJaProfile) NewPunctuator() lang.Punctuator { return nil }

type segmentJaComposer struct {
	units   []string
	pending string
}

func (c *segmentJaComposer) Insert(key string) (lang.ComposeResult, bool) {
	if key == "" {
		return lang.ComposeResult{Pending: c.pending}, false
	}
	c.units = append(c.units, key)
	return lang.ComposeResult{Completed: rd.Sequence{rd.Unit(key)}, Pending: c.pending}, true
}

func (c *segmentJaComposer) Backspace() bool {
	if len(c.units) == 0 {
		return false
	}
	c.units = c.units[:len(c.units)-1]
	return true
}

func (c *segmentJaComposer) Reset() { c.units, c.pending = nil, "" }

func (c *segmentJaComposer) Units() rd.Sequence {
	out := make(rd.Sequence, len(c.units))
	for i, u := range c.units {
		out[i] = rd.Unit(u)
	}
	return out
}

func (c *segmentJaComposer) Pending() string { return c.pending }

func (c *segmentJaComposer) Segments() []lang.DisplaySegment {
	out := make([]lang.DisplaySegment, 0, len(c.units)+1)
	for _, u := range c.units {
		out = append(out, lang.DisplaySegment{Reading: rd.Sequence{rd.Unit(u)}, Text: u})
	}
	if c.pending != "" {
		out = append(out, lang.DisplaySegment{Reading: rd.Sequence{rd.Unit(c.pending)}, Text: c.pending, Pending: true})
	}
	return out
}
