package rewrite_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/rewrite"
)

func TestToHiraganaTable(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"きょう", "きょう"},
		{"キョウ", "きょう"},
		{"ｷｮｳ", "きょう"},
		{"ｶﾞｯｺｳ", "がっこう"},
		{"ヴァイオリン", "ゔぁいおりん"},
		{"ABC123", "ABC123"},
		{"ー", "ー"},
	}
	for _, tc := range tests {
		if got := rewrite.ToHiragana(tc.in); got != tc.want {
			t.Errorf("ToHiragana(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToKatakanaTable(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"きょう", "キョウ"},
		{"キョウ", "キョウ"},
		{"ｷｮｳ", "キョウ"},
		{"がっこう", "ガッコウ"},
		{"ゔぁいおりん", "ヴァイオリン"},
		{"ABC123", "ABC123"},
	}
	for _, tc := range tests {
		if got := rewrite.ToKatakana(tc.in); got != tc.want {
			t.Errorf("ToKatakana(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToHalfwidthKanaTable(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"きょう", "ｷｮｳ"},
		{"キョウ", "ｷｮｳ"},
		{"がっこう", "ｶﾞｯｺｳ"},
		{"ヴァイオリン", "ｳﾞｧｲｵﾘﾝ"},
		{"ぱぴぷぺぽ", "ﾊﾟﾋﾟﾌﾟﾍﾟﾎﾟ"},
		{"ｶﾞｯｺｳ", "ｶﾞｯｺｳ"},
		{"今日", "今日"},
	}
	for _, tc := range tests {
		if got := rewrite.ToHalfwidthKana(tc.in); got != tc.want {
			t.Errorf("ToHalfwidthKana(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLatinWidthTable(t *testing.T) {
	full := []struct{ in, want string }{
		{"abc123", "ａｂｃ１２３"},
		{"ABC-123!", "ＡＢＣ－１２３！"},
		{"a b", "ａ　ｂ"},
		{"あいう", "あいう"},
	}
	for _, tc := range full {
		if got := rewrite.ToFullwidthLatin(tc.in); got != tc.want {
			t.Errorf("ToFullwidthLatin(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	half := []struct{ in, want string }{
		{"ａｂｃ１２３", "abc123"},
		{"ＡＢＣ－１２３！", "ABC-123!"},
		{"ａ　ｂ", "a b"},
		{"あいう", "あいう"},
	}
	for _, tc := range half {
		if got := rewrite.ToHalfwidthLatin(tc.in); got != tc.want {
			t.Errorf("ToHalfwidthLatin(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestKanaCandidatesTable(t *testing.T) {
	tests := []struct {
		name    string
		surface string
		reading string
		want    map[string]string
	}{
		{
			name:    "平假名片假名半角",
			surface: "今日",
			reading: "きょう",
			want: map[string]string{
				"きょう": rewrite.AnnotationHiragana,
				"キョウ": rewrite.AnnotationKatakana,
				"ｷｮｳ": rewrite.AnnotationHalfwidthKana,
			},
		},
		{
			name:    "全角英數",
			surface: "abc123",
			want: map[string]string{
				"ａｂｃ１２３": rewrite.AnnotationFullwidthLatin,
			},
		},
		{
			name:    "半角英數",
			surface: "ＡＢＣ１２３",
			want: map[string]string{
				"ABC123": rewrite.AnnotationHalfwidthLatin,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := rewrite.New()
			cands := r.Candidates(tc.surface, units(tc.reading))
			got := annotationsBySurface(cands)
			for surface, annotation := range tc.want {
				if got[surface] != annotation {
					t.Errorf("candidate %q annotation = %q, want %q (all: %#v)", surface, got[surface], annotation, got)
				}
			}
			for _, c := range cands {
				if c.Source != rewrite.SourceRewrite {
					t.Errorf("candidate %q source = %q, want %q", c.Surface, c.Source, rewrite.SourceRewrite)
				}
				if c.Annotation == "" {
					t.Errorf("candidate %q has empty annotation", c.Surface)
				}
			}
		})
	}
}
