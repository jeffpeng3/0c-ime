package session

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/english"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
)

const crossBenchWord = "kanjikanjikanjikanji" // 20 keys, four complete かんじ

func crossBenchSession(b *testing.B) *Session {
	b.Helper()
	s := New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	s.MixedInput = true
	s.English = english.New([]english.Word{{Text: crossBenchWord, Weight: 60}})
	if !s.EnableJapanese(japaneseCrossLexicon(b), nil) {
		b.Fatal("EnableJapanese = false")
	}
	return s
}

// 每個按鍵後重建日英並列清單的額外成本（不重新 insert 按鍵）。
func BenchmarkJapaneseCrossKeyUpdate(b *testing.B) {
	s := crossBenchSession(b)
	s.mixed.raw = crossBenchWord
	if !s.startJapaneseCross() {
		b.Fatal("cross did not start")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.japaneseCross.raw = crossBenchWord
		s.japaneseCross.english = crossBenchWord
		if !s.rebuildJapaneseCross() {
			b.Fatal("rebuild failed")
		}
	}
}

// 20 鍵同形字串第一次開清單：重播按鍵、かな変換、建立跨語言候選。
func BenchmarkJapaneseCrossOpen20(b *testing.B) {
	s := crossBenchSession(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if r, ok := s.runtime(lang.Ja); ok {
			r.reset()
		}
		s.languageID = lang.Zh
		s.japaneseRun = false
		s.japaneseCross = japaneseCrossState{}
		s.mixed = pendingKeys{raw: crossBenchWord}
		if !s.startJapaneseCross() {
			b.Fatal("cross did not start")
		}
	}
}
