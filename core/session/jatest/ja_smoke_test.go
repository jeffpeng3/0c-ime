package jatest

import (
	"strings"
	"testing"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	jainput "github.com/0c-biggo/0c-ime/core/ja/input"
	jalex "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/layout"
	zhlexicon "github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/session"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

type jaTestProfile struct {
	lex *jalex.Mem
}

func (p jaTestProfile) ID() lang.ID { return lang.Ja }

func (p jaTestProfile) InputMethods() []lang.InputMethod {
	return []lang.InputMethod{{ID: "romaji", Name: "ローマ字"}, {ID: "kana", Name: "かな"}}
}

func (p jaTestProfile) Limits() lang.Limits { return lang.DefaultLimits() }

func (p jaTestProfile) KeyPolicy() lang.KeyPolicy { return session.JapaneseKeyPolicy{} }

func (p jaTestProfile) NewComposer(m lang.InputMethod) lang.Composer {
	if m.ID == "kana" {
		return jainput.NewKanaComposer()
	}
	return jainput.NewRomajiComposer()
}

func (p jaTestProfile) NewConverter() lang.Converter { return &jadecode.Decoder{Lex: p.lex} }

func (p jaTestProfile) NewPunctuator() lang.Punctuator { return nil }

func jaLexicon(t *testing.T) *jalex.Mem {
	t.Helper()
	lex := jalex.NewMem()
	for _, e := range []jalex.Entry{
		{Surface: "今日", Reading: units("き", "ょ", "う"), Cost: 1, POS: jalex.POSNoun},
		{Surface: "京", Reading: units("き", "ょ", "う"), Cost: 3, POS: jalex.POSNoun},
		{Surface: "は", Reading: units("は"), Cost: 1, POS: jalex.POSParticle},
		{Surface: "空気", Reading: units("く", "う", "き"), Cost: 1, POS: jalex.POSNoun},
		{Surface: "が", Reading: units("が"), Cost: 1, POS: jalex.POSParticle},
		{Surface: "いい", Reading: units("い", "い"), Cost: 1, POS: jalex.POSAdjective},
	} {
		if err := lex.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	return lex
}

func units(rs ...string) reading.Sequence {
	out := make(reading.Sequence, len(rs))
	for i, r := range rs {
		out[i] = reading.Unit(r)
	}
	return out
}

func newJapaneseSession(t *testing.T) (*session.Session, jaTestProfile) {
	t.Helper()
	p := jaTestProfile{lex: jaLexicon(t)}
	s := session.New(personal.NewModel(zhlexicon.NewMem(), lm.NewMem()))
	if !s.AddLanguageProfile(p) {
		t.Fatal("AddLanguageProfile(ja) = false")
	}
	if !s.SetLanguage(lang.Ja) {
		t.Fatal("SetLanguage(ja) = false")
	}
	return s, p
}

func handleKey(s *session.Session, k string) proto.Snapshot {
	return s.Handle(proto.Request{Type: proto.Key, Key: k})
}

func typeKeys(s *session.Session, keys string) proto.Snapshot {
	var snap proto.Snapshot
	for _, k := range keys {
		snap = handleKey(s, string(k))
	}
	return snap
}

func TestJapaneseSmokeComposeConvertSelectCommit(t *testing.T) {
	s, _ := newJapaneseSession(t)
	snap := typeKeys(s, "kuukigaii")
	if snap.Text() != "くうきがいい" {
		t.Fatalf("組字 = %q, want くうきがいい (preedit %#v)", snap.Text(), snap.Preedit)
	}
	if snap.Language != "ja" || snap.InputMethod != "romaji" || snap.Mode != proto.ModeJapanese || snap.Converting {
		t.Fatalf("変換前快照 = language %q method %q mode %q converting %v", snap.Language, snap.InputMethod, snap.Mode, snap.Converting)
	}

	snap = handleKey(s, proto.KeyConvert)
	if !snap.Converting || snap.ClauseCount < 2 || snap.ClauseIndex != 0 {
		t.Fatalf("変換快照 = converting %v clause %d/%d preedit %#v", snap.Converting, snap.ClauseIndex, snap.ClauseCount, snap.Preedit)
	}
	if !strings.Contains(snap.Text(), "空気") {
		t.Fatalf("変換結果 = %q，應含空気", snap.Text())
	}
	active := false
	for _, seg := range snap.Preedit {
		if seg.Style == proto.StyleClauseActive {
			active = true
		}
	}
	if !active {
		t.Fatalf("変換中應有 clause_active 樣式：%#v", snap.Preedit)
	}

	before := snap.ClauseIndex
	snap = handleKey(s, proto.KeyTab)
	if snap.ClauseIndex <= before {
		t.Fatalf("Tab 應前進文節：%d → %d", before, snap.ClauseIndex)
	}
	back := snap.ClauseIndex
	snap = handleKey(s, proto.KeyBackTab)
	if snap.ClauseIndex >= back {
		t.Fatalf("Shift+Tab 應退回文節：%d → %d", back, snap.ClauseIndex)
	}

	snap = handleKey(s, " ")
	if snap.Candidates == nil || len(snap.Candidates.Items) == 0 {
		t.Fatalf("第二次 Space 應列出文節候選：%#v", snap.Candidates)
	}
	if snap.Candidates.Start != 0 || snap.Candidates.Length != 4 {
		t.Fatalf("候選範圍 = %d,%d，應是第一個文節（くうきが 四個かな）", snap.Candidates.Start, snap.Candidates.Length)
	}
	if !strings.Contains(snap.Candidates.Items[0].Text, "空気") {
		t.Fatalf("第一個候選 = %q，應含空気", snap.Candidates.Items[0].Text)
	}

	snap = handleKey(s, "1")
	if !strings.Contains(snap.Text(), "空気") {
		t.Fatalf("選字後 = %q", snap.Text())
	}
	if snap.ClauseIndex != 1 {
		t.Fatalf("選字後文節 = %d, want 1", snap.ClauseIndex)
	}

	commit := handleKey(s, proto.KeyEnter)
	if commit.Commit != "空気がいい" {
		t.Fatalf("送出 = %q, want 空気がいい", commit.Commit)
	}
	if commit.Text() != "" || commit.Converting || commit.Language != "ja" {
		t.Fatalf("送出後快照 = text %q converting %v language %q", commit.Text(), commit.Converting, commit.Language)
	}
}

func TestJapaneseCancelReturnsToKana(t *testing.T) {
	s, _ := newJapaneseSession(t)
	typeKeys(s, "kyouha")
	snap := handleKey(s, proto.KeyConvert)
	if !snap.Converting {
		t.Fatal("應進入変換")
	}
	snap = handleKey(s, proto.KeyEscape)
	if snap.Converting || snap.Text() != "きょうは" {
		t.Fatalf("Escape 後 = converting %v text %q", snap.Converting, snap.Text())
	}
	if snap.ClauseCount != 0 || snap.ClauseIndex != 0 {
		t.Fatalf("Escape 後不應有文節：%d/%d", snap.ClauseIndex, snap.ClauseCount)
	}
}

func TestJapaneseF7KatakanaAndF6Hiragana(t *testing.T) {
	s, _ := newJapaneseSession(t)
	typeKeys(s, "kyouha")
	snap := handleKey(s, proto.KeyConvert)
	if !snap.Converting {
		t.Fatal("應進入変換")
	}
	snap = handleKey(s, proto.KeyKatakana)
	if !snap.Converting || !strings.Contains(snap.Text(), "キョウ") {
		t.Fatalf("F7 後 = %q（converting %v）", snap.Text(), snap.Converting)
	}
	snap = handleKey(s, proto.KeyHiragana)
	if !strings.Contains(snap.Text(), "きょう") {
		t.Fatalf("F6 後 = %q", snap.Text())
	}
}

func TestLanguageSwitchKeepsEachComposition(t *testing.T) {
	s, _ := newJapaneseSession(t)
	snap := typeKeys(s, "ky")
	if snap.Text() != "ky" {
		t.Fatalf("羅馬字未完成輸入 = %q", snap.Text())
	}
	if !s.SetLanguage(lang.Zh) {
		t.Fatal("SetLanguage(zh) = false")
	}
	if got := s.Language(); got != lang.Zh {
		t.Fatalf("語言 = %q, want zh", got)
	}
	if snap := s.Handle(proto.Request{Type: proto.Key, Key: "j"}); snap.Text() == "ky" {
		t.Fatalf("切回注音後不應顯示日文組字：%q", snap.Text())
	}
	if !s.SetLanguage(lang.Ja) {
		t.Fatal("SetLanguage(ja) = false")
	}
	snap = s.Handle(proto.Request{Type: proto.Key, Key: "o"})
	if snap.Text() != "きょ" {
		t.Fatalf("切回日文應接續未完成的組字：%q", snap.Text())
	}
	if !s.SetLanguage(lang.Zh) {
		t.Fatal("SetLanguage(zh) = false")
	}
	if snap := s.Handle(proto.Request{Type: proto.Key, Key: " "}); snap.Text() == "" {
		t.Fatal("切回注音應保留原本未完成的注音")
	}
}

func TestJapaneseInputMethodLifecycle(t *testing.T) {
	s, _ := newJapaneseSession(t)
	if got := s.InputMethod().ID; got != "romaji" {
		t.Fatalf("預設輸入方式 = %q", got)
	}
	if !s.SetInputMethod("kana") {
		t.Fatal("空的 session 應可切到 kana")
	}
	snap := typeKeys(s, "きょう")
	if snap.InputMethod != "kana" || snap.Text() != "きょう" {
		t.Fatalf("kana 組字 = method %q text %q", snap.InputMethod, snap.Text())
	}
	if s.SetInputMethod("romaji") {
		t.Fatal("有未完成組字時不應切換輸入方式")
	}
	if snap := handleKey(s, proto.KeyEnter); snap.Commit != "きょう" {
		t.Fatalf("送出 = %q", snap.Commit)
	}
	if !s.SetInputMethod("romaji") {
		t.Fatal("送出後應可切回 romaji")
	}
}

func TestJapaneseCommitEmitsLanguageTaggedEvent(t *testing.T) {
	s, _ := newJapaneseSession(t)
	var learned, completed []string
	s.Events.Learn = func(id lang.ID, text string) {
		learned = append(learned, string(id)+":"+text)
	}
	s.Events.Complete = func(id lang.ID, text string) {
		completed = append(completed, string(id)+":"+text)
	}
	typeKeys(s, "kuukigaii")
	handleKey(s, proto.KeyConvert)
	handleKey(s, " ")
	handleKey(s, "1")
	if len(completed) != 1 || !strings.HasPrefix(completed[0], "ja:") {
		t.Fatalf("補完事件 = %#v, want 一筆 ja", completed)
	}
	handleKey(s, proto.KeyEnter)
	if len(learned) != 1 || !strings.HasPrefix(learned[0], "ja:") {
		t.Fatalf("學習事件 = %#v, want 一筆 ja", learned)
	}

	lex := zhlexicon.NewMem()
	lex.Add(zhlexicon.Entry{Surface: "我", Reading: zhuyin.ReadingUnits(zhuyin.ParseReading("ㄨㄛˇ")), PRW: 1})
	zh := session.New(personal.NewModel(lex, lm.NewMem()))
	var zhLearned []string
	zh.Events.Learn = func(id lang.ID, text string) {
		zhLearned = append(zhLearned, string(id)+":"+text)
	}
	for _, k := range layout.Standard.Keys("ㄨㄛˇ") {
		zh.Handle(proto.Request{Type: proto.Key, Key: string(k)})
	}
	zh.Handle(proto.Request{Type: proto.Key, Key: proto.KeyEnter})
	if len(zhLearned) != 1 || zhLearned[0] != "zh:我" {
		t.Fatalf("注音學習事件 = %#v, want [zh:我]", zhLearned)
	}
}
