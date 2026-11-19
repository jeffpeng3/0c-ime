package session

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/english"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
)

// P2-52：kanji、sake、sushi 同時是英文詞與合法羅馬字。預設仍照英文；Shift+Tab
// 可以把目前的待定按鍵明確改成かな，ja-ambiguous=kana 則一進入就優先かな。
func collisionJapaneseSession(t *testing.T, kanaFirst bool) *Session {
	t.Helper()
	s := New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	s.MixedInput = true
	s.English = english.New([]english.Word{
		{Text: "kanji", Weight: 60},
		{Text: "sake", Weight: 35},
		{Text: "sushi", Weight: 60},
		{Text: "hello", Weight: 35},
	})
	s.SetJapaneseKanaFirst(kanaFirst) // 在 EnableJapanese 前設定，profile 才會帶到 PreferKana
	if !s.EnableJapanese(nil, nil) {
		t.Fatal("EnableJapanese = false")
	}
	return s
}

func TestJapaneseAmbiguousDefaultsToEnglishAndShiftTabForcesKana(t *testing.T) {
	for _, tc := range []struct{ word, kana string }{
		{"kanji", "かんじ"},
		{"sake", "さけ"},
		{"sushi", "すし"},
	} {
		s := collisionJapaneseSession(t, false)
		if snap := typeSessionKeys(s, tc.word); snap.Text() != tc.word || s.Language() != "zh" {
			t.Fatalf("%s 預設應照英文：%q / %s", tc.word, snap.Text(), s.Language())
		}
		snap := key(s, proto.KeyBackTab)
		if snap.Text() != tc.kana || s.Language() != "ja" {
			t.Fatalf("Shift+Tab 後 %s = %q / %s，want %s / ja", tc.word, snap.Text(), s.Language(), tc.kana)
		}
		if snap := key(s, proto.KeyEnter); snap.Commit != tc.kana {
			t.Fatalf("%s 送出 %q，want %s", tc.word, snap.Commit, tc.kana)
		}
		if s.Language() != "zh" {
			t.Fatalf("送出後應回到注音推斷：%s", s.Language())
		}
	}
}

func TestJapaneseKanaFirstSettingChangesAmbiguousDefault(t *testing.T) {
	for _, tc := range []struct{ word, kana string }{
		{"kanji", "かんじ"},
		{"sake", "さけ"},
		{"sushi", "すし"},
	} {
		s := collisionJapaneseSession(t, true)
		if snap := typeSessionKeys(s, tc.word); snap.Text() != tc.kana || s.Language() != "ja" {
			t.Fatalf("かな優先時 %s = %q / %s，want %s / ja", tc.word, snap.Text(), s.Language(), tc.kana)
		}
	}
}

func TestJapaneseShiftTabPreservesTypedPrefix(t *testing.T) {
	s := collisionJapaneseSession(t, false)
	if snap := typeSessionKeys(s, "kan"); snap.Text() != "kan" {
		t.Fatalf("前綴 = %q，want kan", snap.Text())
	}
	if snap := key(s, proto.KeyBackTab); snap.Text() != "かn" {
		t.Fatalf("Shift+Tab 保留前綴 = %q，want かn", snap.Text())
	}
	if snap := typeSessionKeys(s, "ji"); snap.Text() != "かんじ" {
		t.Fatalf("接續打的按鍵 = %q，want かんじ", snap.Text())
	}
}

func TestJapaneseShiftTabLeavesEnglishOnlyWordAlone(t *testing.T) {
	s := collisionJapaneseSession(t, true)
	if snap := typeSessionKeys(s, "hello"); snap.Text() != "hello" || s.Language() != "zh" {
		t.Fatalf("hello 不是合法羅馬字，應照英文：%q / %s", snap.Text(), s.Language())
	}
	if snap := key(s, proto.KeyBackTab); snap.Text() != "hello" {
		t.Fatalf("Shift+Tab 不應改動 hello：%q", snap.Text())
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "hello" {
		t.Fatalf("hello 送出 %q", snap.Commit)
	}
}

func TestJapaneseIdentifierAndUppercaseStayLiteral(t *testing.T) {
	for _, word := range []string{"kubectl", "Kanji"} {
		s := collisionJapaneseSession(t, true)
		if snap := typeSessionKeys(s, word); snap.Text() != word || s.Language() != "zh" {
			t.Fatalf("%s 應照字面：%q / %s", word, snap.Text(), s.Language())
		}
		if snap := key(s, proto.KeyBackTab); snap.Text() != word {
			t.Fatalf("Shift+Tab 不應改動 %s：%q", word, snap.Text())
		}
		if snap := key(s, proto.KeyEnter); snap.Commit != word {
			t.Fatalf("%s 送出 %q", word, snap.Commit)
		}
	}
}

func TestJapaneseShiftTabWithoutPendingRunGoesToApp(t *testing.T) {
	s := collisionJapaneseSession(t, false)
	if snap := key(s, proto.KeyBackTab); snap.Handled {
		t.Fatalf("沒有待定按鍵時 Shift+Tab 應交還應用程式：%+v", snap)
	}
}

func TestJapaneseKanaFirstAppliesAfterEnable(t *testing.T) {
	s := collisionJapaneseSession(t, false)
	s.SetJapaneseKanaFirst(true)
	if snap := typeSessionKeys(s, "kanji"); snap.Text() != "かんじ" || s.Language() != "ja" {
		t.Fatalf("EnableJapanese 後改設定 = %q / %s，want かんじ / ja", snap.Text(), s.Language())
	}
}
