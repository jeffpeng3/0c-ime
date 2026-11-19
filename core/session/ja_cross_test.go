package session

import (
	"slices"
	"testing"

	"github.com/0c-biggo/0c-ime/core/english"
	jlex "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	jlang "github.com/0c-biggo/0c-ime/core/lang/ja"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	rd "github.com/0c-biggo/0c-ime/core/reading"
)

func japaneseCrossLexicon(t testing.TB) jlang.Source {
	t.Helper()
	lex := jlex.NewMem()
	for _, e := range []struct {
		surface string
		reading rd.Sequence
	}{
		{"漢字", rd.Sequence{"か", "ん", "じ"}},
		{"感じ", rd.Sequence{"か", "ん", "じ"}},
		{"寿司", rd.Sequence{"す", "し"}},
		{"酒", rd.Sequence{"さ", "け"}},
		{"仮名", rd.Sequence{"か", "な"}},
	} {
		if err := lex.Add(jlex.Entry{Surface: e.surface, Reading: e.reading, POS: jlex.POSNoun}); err != nil {
			t.Fatal(err)
		}
	}
	return lex
}

// crossSession 使用 P2-54 的新預設：不呼叫 SetJapaneseKanaFirst，日英同形是 both。
func crossSession(t *testing.T) *Session {
	t.Helper()
	s := New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	s.MixedInput = true
	s.English = english.New([]english.Word{
		{Text: "kanji", Weight: 60},
		{Text: "sake", Weight: 35},
		{Text: "sakes", Weight: 35},
		{Text: "sushi", Weight: 60},
		{Text: "kana", Weight: 50},
		{Text: "kanjikana", Weight: 50},
		{Text: "hello", Weight: 35},
	})
	if !s.EnableJapanese(japaneseCrossLexicon(t), nil) {
		t.Fatal("EnableJapanese = false")
	}
	return s
}

func crossCandidateTexts(snap proto.Snapshot) []string {
	if snap.Candidates == nil {
		return nil
	}
	var out []string
	for _, it := range snap.Candidates.Items {
		out = append(out, it.Text)
	}
	return out
}

func crossSelectText(s *Session, want string) proto.Snapshot {
	snap := s.View()
	items := crossCandidateTexts(snap)
	for i, text := range items {
		if text == want {
			for range i {
				snap = key(s, proto.KeyDown)
			}
			return snap
		}
	}
	return snap
}

// 同形時同一個組字片段列出かな、漢字與英文；預設反白かな，Enter 直接送日文。
func TestJapaneseCrossListsKanaKanjiAndEnglish(t *testing.T) {
	for _, tc := range []struct{ word, kana, kanji string }{
		{"kanji", "かんじ", "漢字"},
		{"sake", "さけ", "酒"},
		{"sushi", "すし", "寿司"},
		{"kana", "かな", "仮名"},
	} {
		s := crossSession(t)
		snap := typeSessionKeys(s, tc.word)
		if snap.Text() != tc.kana || s.Language() != lang.Ja {
			t.Fatalf("%s 預設應先顯示かな：%q / %s", tc.word, snap.Text(), s.Language())
		}
		if snap.Candidates == nil || snap.Candidates.Highlight != 0 {
			t.Fatalf("%s 應即時開啟日英候選：%+v", tc.word, snap.Candidates)
		}
		items := crossCandidateTexts(snap)
		if len(items) < 3 || items[0] != tc.kana || items[len(items)-1] != tc.word {
			t.Fatalf("%s 候選順序 = %v，want かな…英文", tc.word, items)
		}
		if !slices.Contains(items, tc.kanji) {
			t.Fatalf("%s 候選應含 %s：%v", tc.word, tc.kanji, items)
		}
		if snap := key(s, proto.KeyEnter); snap.Commit != tc.kana || snap.Text() != "" {
			t.Fatalf("%s Enter 應送かな：%q / %q", tc.word, snap.Commit, snap.Text())
		}
		if s.Language() != lang.Zh {
			t.Fatalf("%s 送出後應回到按鍵推斷：%s", tc.word, s.Language())
		}
	}
}

func TestJapaneseCrossUpdatesListAsTypingContinues(t *testing.T) {
	s := crossSession(t)
	snap := typeSessionKeys(s, "kanji")
	if got := crossCandidateTexts(snap); len(got) == 0 || got[len(got)-1] != "kanji" {
		t.Fatalf("kanji 清單 = %v", got)
	}
	snap = typeSessionKeys(s, "kana")
	items := crossCandidateTexts(snap)
	if snap.Text() != "かんじかな" || len(items) == 0 || items[0] != "かんじかな" || items[len(items)-1] != "kanjikana" {
		t.Fatalf("kanjikana 清單 = %q %v", snap.Text(), items)
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "かんじかな" {
		t.Fatalf("kanjikana Enter = %q", snap.Commit)
	}
}

func TestJapaneseCrossEnglishAndKanjiCandidatesCommitAndContinue(t *testing.T) {
	s := crossSession(t)
	typeSessionKeys(s, "sake")
	// 從かな往下走到英文（英文固定最後一項），Enter 直接送英文。
	snap := crossSelectText(s, "sake")
	if snap.Text() != "sake" {
		t.Fatalf("反白英文時組字 = %q", snap.Text())
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "sake" {
		t.Fatalf("英文候選送出 %q", snap.Commit)
	}
	// 英文送出後，下一個同形片段仍照新規則先给かな。
	if snap := typeSessionKeys(s, "sushi"); snap.Text() != "すし" {
		t.Fatalf("送出英文後接續 = %q", snap.Text())
	}
	key(s, proto.KeyEnter)

	// かな之後按 ↓ 選漢字，Enter 送漢字；再打另一個同形片段仍正常。
	typeSessionKeys(s, "kanji")
	snap = crossSelectText(s, "漢字")
	if snap.Text() != "漢字" {
		t.Fatalf("反白漢字時組字 = %q", snap.Text())
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "漢字" {
		t.Fatalf("漢字候選送出 %q", snap.Commit)
	}
	if snap := typeSessionKeys(s, "kana"); snap.Text() != "かな" || snap.Candidates == nil {
		t.Fatalf("送出漢字後接續 = %q / %+v", snap.Text(), snap.Candidates)
	}
}

// 候選點擊直接送出點選的解釋，之後可以立刻打新的片段。
func TestJapaneseCrossCandidateClickCommits(t *testing.T) {
	s := crossSession(t)
	snap := typeSessionKeys(s, "kanji")
	id := -1
	for _, it := range snap.Candidates.Items {
		if it.Text == "漢字" {
			id = it.ID
		}
	}
	if id < 0 {
		t.Fatalf("沒有漢字候選：%v", crossCandidateTexts(snap))
	}
	snap = s.Handle(proto.Request{Type: proto.Select, Index: id})
	if snap.Commit != "漢字" || snap.Text() != "" || s.Language() != lang.Zh {
		t.Fatalf("點選漢字 = %q / %q / %s", snap.Commit, snap.Text(), s.Language())
	}
	if snap := typeSessionKeys(s, "sake"); snap.Text() != "さけ" {
		t.Fatalf("點選後接續 = %q", snap.Text())
	}
}

// 非日英同形維持原本路徑：不是羅馬字、識別字、網址、大寫開頭都只打英文。
func TestJapaneseCrossLeavesUnambiguousInputAlone(t *testing.T) {
	for _, tc := range []struct{ keys, want string }{
		{"hello", "hello"},
		{"kubectl", "kubectl"},
		{"https://example.com", "https://example.com"},
		{"Kanji", "Kanji"},
	} {
		s := crossSession(t)
		snap := typeSessionKeys(s, tc.keys)
		if snap.Text() != tc.want || s.Language() != lang.Zh {
			t.Fatalf("%s 應照英文：%q / %s", tc.keys, snap.Text(), s.Language())
		}
		if snap.Candidates != nil {
			t.Fatalf("%s 不應開日英候選：%v", tc.keys, crossCandidateTexts(snap))
		}
		if snap := key(s, proto.KeyEnter); snap.Commit != tc.want {
			t.Fatalf("%s 送出 %q", tc.keys, snap.Commit)
		}
	}
}

// 同形開始後再多打一個字母，整串不再是完整羅馬字但正好是英文詞（sakes）時
// 改成英文；不會卡在かな＋未完成羅馬字。
func TestJapaneseCrossFallsBackWhenExtraLettersStopBeingRomaji(t *testing.T) {
	s := crossSession(t)
	if snap := typeSessionKeys(s, "sake"); snap.Text() != "さけ" || snap.Candidates == nil {
		t.Fatalf("sake 應先同形：%q / %+v", snap.Text(), snap.Candidates)
	}
	snap := typeSessionKeys(s, "s")
	if snap.Text() != "sakes" || s.Language() != lang.Zh || snap.Candidates != nil {
		t.Fatalf("sakes 應退回英文：%q / %s / %+v", snap.Text(), s.Language(), snap.Candidates)
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "sakes" {
		t.Fatalf("sakes 送出 %q", snap.Commit)
	}
}

// english／kana 是有話語權的舊值：english 不列かな，kana 不列英文；兩者都不必按修飾鍵。
func TestJapaneseCrossHonorsExplicitSettings(t *testing.T) {
	s := crossSession(t)
	s.SetJapaneseKanaFirst(false) // 明確 english
	if snap := typeSessionKeys(s, "kanji"); snap.Text() != "kanji" || snap.Candidates != nil || s.Language() != lang.Zh {
		t.Fatalf("english = %q / %+v / %s", snap.Text(), snap.Candidates, s.Language())
	}

	s = crossSession(t)
	s.SetJapaneseKanaFirst(true) // 明確 kana
	snap := typeSessionKeys(s, "kanji")
	if snap.Text() != "かんじ" || snap.Candidates != nil || s.Language() != lang.Ja {
		t.Fatalf("kana = %q / %+v / %s", snap.Text(), snap.Candidates, s.Language())
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "かんじ" {
		t.Fatalf("kana Enter = %q", snap.Commit)
	}
}
