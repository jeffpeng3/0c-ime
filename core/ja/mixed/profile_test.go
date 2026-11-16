package mixed_test

import (
	"strings"
	"testing"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	jalex "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/ja/mixed"
	"github.com/0c-biggo/0c-ime/core/lang"
	zhlexicon "github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/session"
)

type jaBaseProfile struct {
	lex *jalex.Mem
}

func (p jaBaseProfile) ID() lang.ID { return lang.Ja }

func (p jaBaseProfile) InputMethods() []lang.InputMethod {
	return []lang.InputMethod{{ID: "romaji", Name: "ローマ字"}, {ID: "kana", Name: "かな"}}
}

func (p jaBaseProfile) Limits() lang.Limits { return lang.DefaultLimits() }

func (p jaBaseProfile) KeyPolicy() lang.KeyPolicy { return session.JapaneseKeyPolicy{} }

func (p jaBaseProfile) NewComposer(lang.InputMethod) lang.Composer { return nil }

func (p jaBaseProfile) NewConverter() lang.Converter { return &jadecode.Decoder{Lex: p.lex} }

func (p jaBaseProfile) NewPunctuator() lang.Punctuator { return nil }

func TestProfileMixedInputThroughSession(t *testing.T) {
	for _, tc := range []struct {
		name    string
		spacing mixed.Spacing
		want    string
	}{
		{"default-no-space", mixed.SpacingNone, "今日はAPI"},
		{"spacing-setting", mixed.SpacingAlways, "今日は API"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMixedSession(t, tc.spacing)
			typeKeys(s, "kyouha")
			typeKeys(s, "API")
			snap := key(s, " ")
			if !snap.Converting {
				t.Fatalf("Space 後應進入変換：%#v", snap)
			}
			if !strings.Contains(snap.Text(), "今日は") || !strings.Contains(snap.Text(), "API") {
				t.Fatalf("変換中 = %q", snap.Text())
			}
			if !hasSegment(snap, "API") {
				t.Fatalf("英文應是獨立文節：%#v", snap.Preedit)
			}
			snap = key(s, proto.KeyEnter)
			if snap.Commit != tc.want {
				t.Fatalf("送出 = %q, want %q", snap.Commit, tc.want)
			}
			if snap.Text() != "" || snap.Converting {
				t.Fatalf("送出後 = text %q converting %v", snap.Text(), snap.Converting)
			}
		})
	}
}

func TestProfileURLThroughSessionIsNotConverted(t *testing.T) {
	s := newMixedSession(t, mixed.SpacingNone)
	typeKeys(s, "kyouha")
	const url = "https://example.com/a?b=1"
	typeKeys(s, url)
	snap := key(s, " ")
	if !snap.Converting {
		t.Fatal("Space 後應進入変換")
	}
	if !hasSegment(snap, url) {
		t.Fatalf("網址應是獨立文節：%#v", snap.Preedit)
	}
	snap = key(s, proto.KeyEnter)
	if snap.Commit != "今日は"+url {
		t.Fatalf("送出 = %q", snap.Commit)
	}
}

func TestProfileEmailThroughSessionIsNotConverted(t *testing.T) {
	s := newMixedSession(t, mixed.SpacingNone)
	typeKeys(s, "kyouha")
	const email = "foo@example.com"
	typeKeys(s, email)
	snap := key(s, " ")
	snap = key(s, proto.KeyEnter)
	if snap.Commit != "今日は"+email {
		t.Fatalf("送出 = %q", snap.Commit)
	}
}

func TestProfileJapaneseOnlyStillWorks(t *testing.T) {
	s := newMixedSession(t, mixed.SpacingNone)
	typeKeys(s, "kyouha")
	snap := key(s, " ")
	if !snap.Converting || !strings.Contains(snap.Text(), "今日") {
		t.Fatalf("日文変換 = %q converting %v", snap.Text(), snap.Converting)
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "今日は" {
		t.Fatalf("日文送出 = %q", snap.Commit)
	}
}

func newMixedSession(t *testing.T, spacing mixed.Spacing) *session.Session {
	t.Helper()
	lex := jalex.NewMem()
	for _, entry := range []jalex.Entry{
		{Surface: "今日", Reading: jaUnits("き", "ょ", "う"), Cost: 1, POS: jalex.POSNoun},
		{Surface: "京", Reading: jaUnits("き", "ょ", "う"), Cost: 3, POS: jalex.POSNoun},
		{Surface: "は", Reading: jaUnits("は"), Cost: 1, POS: jalex.POSParticle},
		{Surface: "です", Reading: jaUnits("で", "す"), Cost: 1, POS: jalex.POSAuxiliary},
	} {
		if err := lex.Add(entry); err != nil {
			t.Fatal(err)
		}
	}
	base := jaBaseProfile{lex: lex}
	profile := mixed.NewProfile(base, mixed.Config{Spacing: spacing})
	s := session.New(personal.NewModel(zhlexicon.NewMem(), lm.NewMem()))
	if !s.AddLanguageProfile(profile) {
		t.Fatal("AddLanguageProfile = false")
	}
	if !s.SetLanguage(lang.Ja) {
		t.Fatal("SetLanguage(ja) = false")
	}
	return s
}

func key(s *session.Session, k string) proto.Snapshot {
	return s.Handle(proto.Request{Type: proto.Key, Key: k})
}

func typeKeys(s *session.Session, keys string) proto.Snapshot {
	var snap proto.Snapshot
	for _, r := range keys {
		snap = key(s, string(r))
	}
	return snap
}

func hasSegment(snap proto.Snapshot, text string) bool {
	for _, seg := range snap.Preedit {
		if strings.TrimSpace(seg.Text) == text {
			return true
		}
	}
	return false
}

func jaUnits(rs ...string) reading.Sequence {
	out := make(reading.Sequence, len(rs))
	for i, r := range rs {
		out[i] = reading.Unit(r)
	}
	return out
}
