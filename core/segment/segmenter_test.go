package segment_test

import (
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/segment"
)

func spanSegment(start, end int, script segment.Script, language lang.ID) segment.Segment {
	return segment.Segment{
		Span:     segment.Span{Start: start, End: end},
		Script:   script,
		Language: lang.Tag(language),
	}
}

func TestSplitTable(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []segment.Segment
	}{
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "single zhuyin syllable",
			input: "ㄓㄨˋ",
			want:  []segment.Segment{spanSegment(0, 3, segment.ScriptZhuyin, lang.Zh)},
		},
		{
			name:  "zhuyin without tone",
			input: "ㄓㄨㄧㄣ",
			want:  []segment.Segment{spanSegment(0, 4, segment.ScriptZhuyin, lang.Zh)},
		},
		{
			name:  "two zhuyin syllables",
			input: "ㄓㄨˋㄧㄣ",
			want:  []segment.Segment{spanSegment(0, 5, segment.ScriptZhuyin, lang.Zh)},
		},
		{
			name:  "hiragana",
			input: "かな",
			want:  []segment.Segment{spanSegment(0, 2, segment.ScriptKana, lang.Ja)},
		},
		{
			name:  "katakana with small kana and long mark",
			input: "キャー",
			want:  []segment.Segment{spanSegment(0, 3, segment.ScriptKana, lang.Ja)},
		},
		{
			name:  "iteration marks",
			input: "ゝゞヽヾ",
			want:  []segment.Segment{spanSegment(0, 4, segment.ScriptKana, lang.Ja)},
		},
		{
			name:  "halfwidth katakana",
			input: "ｱｲｳｰ",
			want:  []segment.Segment{spanSegment(0, 4, segment.ScriptKana, lang.Ja)},
		},
		{
			name:  "ascii latin",
			input: "kubectl",
			want:  []segment.Segment{spanSegment(0, 7, segment.ScriptLatin, lang.Und)},
		},
		{
			name:  "fullwidth latin",
			input: "ＡＢＣ",
			want:  []segment.Segment{spanSegment(0, 3, segment.ScriptLatin, lang.Und)},
		},
		{
			name:  "accented latin",
			input: "café",
			want:  []segment.Segment{spanSegment(0, 4, segment.ScriptLatin, lang.Und)},
		},
		{
			name:  "ascii digits",
			input: "123",
			want:  []segment.Segment{spanSegment(0, 3, segment.ScriptDigit, lang.Und)},
		},
		{
			name:  "fullwidth digits",
			input: "１２３",
			want:  []segment.Segment{spanSegment(0, 3, segment.ScriptDigit, lang.Und)},
		},
		{
			name:  "latin and digits are one mixed span",
			input: "abc123",
			want:  []segment.Segment{spanSegment(0, 6, segment.ScriptMixed, lang.Und)},
		},
		{
			name:  "alternating latin and digits stay mixed",
			input: "a1b2",
			want:  []segment.Segment{spanSegment(0, 4, segment.ScriptMixed, lang.Und)},
		},
		{
			name:  "han",
			input: "漢字",
			want:  []segment.Segment{spanSegment(0, 2, segment.ScriptHan, lang.Und)},
		},
		{
			name:  "japanese han and kana split",
			input: "日本語です",
			want: []segment.Segment{
				spanSegment(0, 3, segment.ScriptHan, lang.Und),
				spanSegment(3, 5, segment.ScriptKana, lang.Ja),
			},
		},
		{
			name:  "han iteration mark stays with han",
			input: "人々",
			want:  []segment.Segment{spanSegment(0, 2, segment.ScriptHan, lang.Und)},
		},
		{
			name:  "punctuation",
			input: "，。！？",
			want:  []segment.Segment{spanSegment(0, 4, segment.ScriptSymbol, lang.Und)},
		},
		{
			name:  "whitespace",
			input: " \t\n",
			want:  []segment.Segment{spanSegment(0, 3, segment.ScriptSymbol, lang.Und)},
		},
		{
			name:  "emoji",
			input: "😀",
			want:  []segment.Segment{spanSegment(0, 1, segment.ScriptSymbol, lang.Und)},
		},
		{
			name:  "unsupported letters stay unknown",
			input: "Ω",
			want:  []segment.Segment{spanSegment(0, 1, segment.ScriptUnknown, lang.Und)},
		},
		{
			name:  "mixed input",
			input: "a ㄓㄨ b",
			want: []segment.Segment{
				spanSegment(0, 1, segment.ScriptLatin, lang.Und),
				spanSegment(1, 2, segment.ScriptSymbol, lang.Und),
				spanSegment(2, 4, segment.ScriptZhuyin, lang.Zh),
				spanSegment(4, 5, segment.ScriptSymbol, lang.Und),
				spanSegment(5, 6, segment.ScriptLatin, lang.Und),
			},
		},
		{
			name:  "symbol breaks an alphanumeric token",
			input: "abc,123",
			want: []segment.Segment{
				spanSegment(0, 3, segment.ScriptLatin, lang.Und),
				spanSegment(3, 4, segment.ScriptSymbol, lang.Und),
				spanSegment(4, 7, segment.ScriptDigit, lang.Und),
			},
		},
		{
			name:  "digit next to han splits",
			input: "3月",
			want: []segment.Segment{
				spanSegment(0, 1, segment.ScriptDigit, lang.Und),
				spanSegment(1, 2, segment.ScriptHan, lang.Und),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := segment.New().Split(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Split(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
			assertCoversInput(t, tt.input, got)
		})
	}
}

func TestSplitNamesAreStable(t *testing.T) {
	tests := []struct {
		input  string
		script segment.Script
	}{
		{"ㄅ", segment.ScriptZhuyin},
		{"あ", segment.ScriptKana},
		{"A", segment.ScriptLatin},
		{"1", segment.ScriptDigit},
		{"漢", segment.ScriptHan},
		{"!", segment.ScriptSymbol},
	}
	for _, tt := range tests {
		got := segment.New().Split(tt.input)
		if len(got) != 1 || got[0].Script != tt.script {
			t.Errorf("Split(%q) = %#v, want one %s segment", tt.input, got, tt.script)
			continue
		}
		if !got[0].Valid() {
			t.Errorf("Split(%q) 回傳無效片段 %#v", tt.input, got[0])
		}
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		r    rune
		want segment.Script
	}{
		{'ㄓ', segment.ScriptZhuyin},
		{'ˋ', segment.ScriptZhuyin},
		{'ㆠ', segment.ScriptZhuyin},
		{'あ', segment.ScriptKana},
		{'ア', segment.ScriptKana},
		{'ｱ', segment.ScriptKana},
		{'ー', segment.ScriptKana},
		{'ゝ', segment.ScriptKana},
		{'ヽ', segment.ScriptKana},
		{'日', segment.ScriptHan},
		{'々', segment.ScriptHan},
		{'〆', segment.ScriptHan},
		{'a', segment.ScriptLatin},
		{'Ａ', segment.ScriptLatin},
		{'é', segment.ScriptLatin},
		{'1', segment.ScriptDigit},
		{'１', segment.ScriptDigit},
		{' ', segment.ScriptSymbol},
		{'。', segment.ScriptSymbol},
		{'😀', segment.ScriptSymbol},
		{'Ω', segment.ScriptUnknown},
		{'①', segment.ScriptUnknown},
	}
	for _, tt := range tests {
		if got := segment.Detect(tt.r); got != tt.want {
			t.Errorf("Detect(%q) = %s, want %s", tt.r, got, tt.want)
		}
	}
}

func TestSplitConstructorsAgree(t *testing.T) {
	input := "abc123 ㄓㄨˋ かな"
	want := segment.New().Split(input)
	if got := segment.NewSegmenter().Split(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("NewSegmenter() = %#v, want %#v", got, want)
	}
	if got := segment.Split(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("Split() = %#v, want %#v", got, want)
	}
}

func TestSplitDoesNotKeepState(t *testing.T) {
	splitter := segment.New()
	first := splitter.Split("a1ㄓ")
	if len(first) != 2 || first[0].Script != segment.ScriptMixed || first[1].Script != segment.ScriptZhuyin {
		t.Fatalf("first = %#v", first)
	}
	second := splitter.Split("かな")
	if len(second) != 1 || second[0].Script != segment.ScriptKana || second[0].Language.Language != lang.Ja {
		t.Fatalf("second = %#v", second)
	}
	first[0].Script = segment.ScriptSymbol
	if third := splitter.Split("a1ㄓ"); third[0].Script != segment.ScriptMixed {
		t.Fatalf("修改回傳片段影響後續結果: %#v", third)
	}
}

func assertCoversInput(t *testing.T, input string, segments []segment.Segment) {
	t.Helper()
	wantLen := utf8.RuneCountInString(input)
	pos := 0
	for i, seg := range segments {
		if !seg.Valid() {
			t.Fatalf("segment[%d] 無效: %#v", i, seg)
		}
		if seg.Span.Start != pos {
			t.Fatalf("segment[%d] 起點 = %d, want %d", i, seg.Span.Start, pos)
		}
		if seg.Span.Len() == 0 {
			t.Fatalf("segment[%d] 是空片段", i)
		}
		pos = seg.Span.End
	}
	if pos != wantLen {
		t.Fatalf("片段覆蓋到 rune %d, 輸入長度 %d", pos, wantLen)
	}
}

func FuzzSplit(f *testing.F) {
	for _, seed := range []string{
		"",
		"abc123",
		"ㄓㄨˋㄧㄣ",
		"日本語です",
		"a ㄓㄨ b",
		"😀かなＡＢ１２",
		"3月",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got := segment.New().Split(input)
		assertCoversInput(t, input, got)
	})
}
