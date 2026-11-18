package session

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/punct"
	rd "github.com/0c-biggo/0c-ime/core/reading"
)

// langLearnProfile 是只給學習 hook 測試用的最小日文 profile；選字狀態由測試直接放入
// runtime，不需要真正的 composer/converter。
type langLearnProfile struct{}

func (langLearnProfile) ID() lang.ID                      { return lang.Ja }
func (langLearnProfile) InputMethods() []lang.InputMethod { return []lang.InputMethod{{ID: "kana"}} }
func (langLearnProfile) Limits() lang.Limits              { return lang.DefaultLimits() }
func (langLearnProfile) KeyPolicy() lang.KeyPolicy        { return JapaneseKeyPolicy{} }
func (langLearnProfile) NewComposer(lang.InputMethod) lang.Composer {
	return nil
}
func (langLearnProfile) NewConverter() lang.Converter   { return nil }
func (langLearnProfile) NewPunctuator() lang.Punctuator { return nil }

// 游標所在片段的選字要記到該片段的語言；中文選字不會增加日文計數。
func TestSessionSelectionLearnsCursorSegmentLanguage(t *testing.T) {
	s := newSession()
	typeSyl(s, "ㄨㄛˇ")
	typeSyl(s, "ㄐㄧㄥˇ")
	if got := s.ActiveSegment(); got != 1 {
		t.Fatalf("ActiveSegment = %d, want 1", got)
	}
	s.Handle(proto.Request{Type: proto.Candidates, Length: 1})
	snap := key(s, "2") // 景、井：第二項是井
	if snap.Text() != "我井" {
		t.Fatalf("選字後 = %q, want 我井", snap.Text())
	}
	if got := s.model.LanguageSelections(lang.Zh); got != 0 {
		t.Fatalf("送出前不應寫入持久學習：%d", got)
	}
	key(s, proto.KeyEnter)
	if got := s.model.LanguageSurfaceCount(lang.Zh, "井"); got != 1 {
		t.Fatalf("中文命名空間的井 = %d, want 1", got)
	}
	if got := s.model.LanguageSurfaceCount(lang.Ja, "井"); got != 0 {
		t.Fatalf("中文選字污染日文：%d", got)
	}
	if got := s.model.PrimaryLanguage(); got != lang.Zh {
		t.Fatalf("主要語言 = %q, want zh", got)
	}
	if !s.UndoLanguage() {
		t.Fatal("UndoLanguage = false")
	}
	if got := s.model.LanguageSurfaceCount(lang.Zh, "井"); got != 0 {
		t.Fatalf("反悔後井 = %d, want 0", got)
	}
}

// 日文轉換中選候選也會記到日文命名空間，並把前一次送出的語言當成跨語言前文。
func TestSessionJapaneseSelectionLearnsLanguage(t *testing.T) {
	s := newSession()
	if !s.AddLanguageProfile(langLearnProfile{}) || !s.SetLanguage(lang.Ja) {
		t.Fatal("註冊或切換日文失敗")
	}
	r, ok := s.activeRuntime()
	if !ok {
		t.Fatal("沒有日文 runtime")
	}
	reading := rd.Sequence{rd.Unit("き"), rd.Unit("ょ"), rd.Unit("う")}
	r.clauses = []lang.Clause{{Surface: "京", Reading: reading}}
	r.candidates = []lang.Candidate{{Surface: "今日", Reading: reading}}
	r.converting = true
	r.clause = 0
	if !s.languageSelectIndex(0, false) {
		t.Fatal("languageSelectIndex = false")
	}
	if got := s.model.LanguageSelections(lang.Ja); got != 0 {
		t.Fatalf("送出前不應寫入持久學習：%d", got)
	}
	if got := s.languageCommit(); got != "今日" {
		t.Fatalf("送出 %q, want 今日", got)
	}
	if got := s.model.LanguageSelections(lang.Ja); got != 1 {
		t.Fatalf("日文選字數 = %d, want 1", got)
	}
	if got := s.model.LanguageSelections(lang.Zh); got != 0 {
		t.Fatalf("日文選字污染中文：%d", got)
	}
	if got := s.model.LanguageSurfaceCount(lang.Ja, "今日"); got != 1 {
		t.Fatalf("日文詞面次數 = %d", got)
	}
}

// 日文送出也要補標點：模型可以接在假名或段尾，但只插入標點，原字不變。
func TestLanguageCommitPunctuatesJapaneseWithoutChangingText(t *testing.T) {
	s := newSession()
	if !s.AddLanguageProfile(langLearnProfile{}) || !s.SetLanguage(lang.Ja) {
		t.Fatal("註冊或切換日文失敗")
	}
	r, ok := s.activeRuntime()
	if !ok {
		t.Fatal("沒有日文 runtime")
	}
	text := "今日は天気です"
	r.surface = text
	s.Punct = punctFunc(func(got string) ([]punct.Edit, error) {
		if got != text {
			t.Fatalf("模型收到 %q，want %q", got, text)
		}
		return []punct.Edit{{Offset: 3, Text: "、"}, {Offset: 7, Text: "。"}}, nil
	})
	if got := s.languageCommit(); got != "今日は、天気です。" {
		t.Fatalf("送出 %q", got)
	}
}
