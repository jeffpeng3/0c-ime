package jaimport

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
)

func entryWith(t *testing.T, surface, kana string, pos POS, conj Conjugation) Entry {
	t.Helper()
	r, err := NormalizeReading(kana)
	if err != nil {
		t.Fatal(err)
	}
	return MakeEntry(surface, r, 1, pos, []string{"curated"}, conj)
}

func formMap(entries []Entry) map[string]string {
	m := map[string]string{}
	for _, e := range entries {
		m[e.Surface] = reading.Key(e.Reading)
	}
	return m
}

func TestExpandGodan(t *testing.T) {
	got := formMap(ExpandConjugations(entryWith(t, "書く", "かく", POSVerb, ConjGodan)))
	want := map[string]string{
		"書かない": "か か な い",
		"書きます": "か き ま す",
		"書いた":  "か い た",
		"書けば":  "か け ば",
		"書こう":  "か こ う",
		"書け":   "か け",
		"書いて":  "か い て",
	}
	for surface, wantReading := range want {
		if got[surface] != wantReading {
			t.Errorf("書く 活用 %q = %q，want %q（全部：%v）", surface, got[surface], wantReading, got)
		}
	}
	for surface := range got {
		if e := got[surface]; len(e) == 0 {
			t.Errorf("%q 沒有讀音", surface)
		}
	}
}

func TestExpandIchidanSuruKuru(t *testing.T) {
	for _, tc := range []struct {
		entry  Entry
		expect map[string]string
	}{
		{
			entryWith(t, "食べる", "たべる", POSVerb, ConjIchidan),
			map[string]string{"食べない": "た べ な い", "食べます": "た べ ま す", "食べた": "た べ た", "食べて": "た べ て", "食べれば": "た べ れ ば", "食べよう": "た べ よ う", "食べろ": "た べ ろ"},
		},
		{
			entryWith(t, "する", "する", POSVerb, ConjSuru),
			map[string]string{"しない": "し な い", "します": "し ま す", "した": "し た", "して": "し て", "すれば": "す れ ば", "しよう": "し よ う", "しろ": "し ろ"},
		},
		{
			entryWith(t, "来る", "くる", POSVerb, ConjKuru),
			map[string]string{"来ない": "こ な い", "来ます": "き ま す", "来た": "き た", "来て": "き て", "来れば": "く れ ば", "来よう": "こ よ う", "来い": "こ い"},
		},
		{
			entryWith(t, "勉強する", "べんきょうする", POSVerb, ConjSuru),
			map[string]string{"勉強しない": "べ ん き ょ う し な い", "勉強します": "べ ん き ょ う し ま す"},
		},
	} {
		got := formMap(ExpandConjugations(tc.entry))
		for surface, want := range tc.expect {
			if got[surface] != want {
				t.Errorf("%s 活用 %q = %q，want %q", tc.entry.Surface, surface, got[surface], want)
			}
		}
	}
}

func TestExpandAdjectives(t *testing.T) {
	got := formMap(ExpandConjugations(entryWith(t, "高い", "たかい", POSAdjective, ConjIAdjective)))
	for surface, want := range map[string]string{
		"高く": "た か く", "高くない": "た か く な い", "高かった": "た か か っ た",
		"高ければ": "た か け れ ば", "高くて": "た か く て",
	} {
		if got[surface] != want {
			t.Errorf("高い 活用 %q = %q，want %q", surface, got[surface], want)
		}
	}
	got = formMap(ExpandConjugations(entryWith(t, "静か", "しずか", POSAdjectivalNoun, ConjNaAdjective)))
	for surface, want := range map[string]string{
		"静かだ": "し ず か だ", "静かな": "し ず か な", "静かに": "し ず か に",
		"静かで": "し ず か で", "静かじゃない": "し ず か じ ゃ な い",
	} {
		if got[surface] != want {
			t.Errorf("静か 活用 %q = %q，want %q", surface, got[surface], want)
		}
	}
}

func TestExpandSkipsUnknownAndKeepsMetadata(t *testing.T) {
	if got := ExpandConjugations(entryWith(t, "明日", "あした", POSNoun, ConjNone)); len(got) != 0 {
		t.Fatalf("無活用詞條不應展開：%#v", got)
	}
	base := entryWith(t, "書く", "かく", POSVerb, ConjGodan)
	forms := ExpandConjugations(base)
	if len(forms) == 0 {
		t.Fatal("沒有產生活用形")
	}
	for _, e := range forms {
		if e.Conjugation != ConjNone {
			t.Errorf("產生的活用形不應再展開：%#v", e)
		}
		if e.Cost <= base.Cost {
			t.Errorf("活用形成本 %v 應高於原形 %v", e.Cost, base.Cost)
		}
		if len(e.Origins) == 0 || e.Origins[len(e.Origins)-1] != conjRulePrefix {
			t.Errorf("活用形缺少來源標記：%v", e.Origins)
		}
		if e.Okurigana == 0 && e.Surface != "書け" && e.Surface != "書こう" {
			t.Errorf("活用形缺少送り仮名：%#v", e)
		}
	}
}
