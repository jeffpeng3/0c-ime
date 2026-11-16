package rewrite_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/rewrite"
)

func TestNumberCandidatesTable(t *testing.T) {
	tests := []struct {
		input string
		want  map[string]string
		// absent 是不得出現的註解類型。
		absent []string
	}{
		{
			input: "123",
			want: map[string]string{
				"百二十三":      rewrite.AnnotationKanji,
				"壱百弐拾参":     rewrite.AnnotationDaiji,
				"0x7b":      rewrite.AnnotationHex,
				"0173":      rewrite.AnnotationOct,
				"0b1111011": rewrite.AnnotationBin,
			},
			absent: []string{rewrite.AnnotationCircled, rewrite.AnnotationRoman},
		},
		{
			input: "12",
			want: map[string]string{
				"十二":     rewrite.AnnotationKanji,
				"壱拾弐":    rewrite.AnnotationDaiji,
				"⑫":      rewrite.AnnotationCircled,
				"Ⅻ":      rewrite.AnnotationRoman,
				"ⅻ":      rewrite.AnnotationRomanS,
				"0xc":    rewrite.AnnotationHex,
				"014":    rewrite.AnnotationOct,
				"0b1100": rewrite.AnnotationBin,
			},
		},
		{
			input: "0",
			want: map[string]string{
				"零": rewrite.AnnotationKanji,
				"⓪": rewrite.AnnotationCircled,
			},
			absent: []string{rewrite.AnnotationDaiji, rewrite.AnnotationRoman, rewrite.AnnotationHex, rewrite.AnnotationOct, rewrite.AnnotationBin},
		},
		{
			input: "50",
			want: map[string]string{
				"五十":       rewrite.AnnotationKanji,
				"五拾":       rewrite.AnnotationDaiji,
				"㊿":        rewrite.AnnotationCircled,
				"0x32":     rewrite.AnnotationHex,
				"062":      rewrite.AnnotationOct,
				"0b110010": rewrite.AnnotationBin,
			},
			absent: []string{rewrite.AnnotationRoman},
		},
		{
			input: "51",
			want: map[string]string{
				"五十一":      rewrite.AnnotationKanji,
				"五拾壱":      rewrite.AnnotationDaiji,
				"0x33":     rewrite.AnnotationHex,
				"063":      rewrite.AnnotationOct,
				"0b110011": rewrite.AnnotationBin,
			},
			absent: []string{rewrite.AnnotationCircled, rewrite.AnnotationRoman},
		},
		{
			input: "1000",
			want: map[string]string{
				"千":  rewrite.AnnotationKanji,
				"壱阡": rewrite.AnnotationDaiji,
			},
		},
		{
			input: "10000",
			want: map[string]string{
				"一万": rewrite.AnnotationKanji,
				"壱萬": rewrite.AnnotationDaiji,
			},
		},
		{
			input: "123456789",
			want: map[string]string{
				"一億二千三百四十五万六千七百八十九": rewrite.AnnotationKanji,
				"壱億弐阡参百四拾五萬六阡七百八拾九": rewrite.AnnotationDaiji,
				"0x75bcd15":                     rewrite.AnnotationHex,
				"0726746425":                    rewrite.AnnotationOct,
				"0b111010110111100110100010101": rewrite.AnnotationBin,
			},
			absent: []string{rewrite.AnnotationCircled, rewrite.AnnotationRoman},
		},
		{
			input: "１２０",
			want: map[string]string{
				"百二十":       rewrite.AnnotationKanji,
				"壱百弐拾":      rewrite.AnnotationDaiji,
				"0x78":      rewrite.AnnotationHex,
				"0170":      rewrite.AnnotationOct,
				"0b1111000": rewrite.AnnotationBin,
			},
		},
		{
			input: "abc123",
			absent: []string{
				rewrite.AnnotationKanji, rewrite.AnnotationDaiji, rewrite.AnnotationCircled,
				rewrite.AnnotationRoman, rewrite.AnnotationHex, rewrite.AnnotationOct, rewrite.AnnotationBin,
			},
		},
		{
			input: "18446744073709551616", // uint64 溢位
			absent: []string{
				rewrite.AnnotationKanji, rewrite.AnnotationDaiji, rewrite.AnnotationCircled,
				rewrite.AnnotationRoman, rewrite.AnnotationHex,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			cands := rewrite.New().Candidates(tc.input, nil)
			got := annotationsBySurface(cands)
			for surface, annotation := range tc.want {
				if got[surface] != annotation {
					t.Errorf("candidate %q annotation = %q, want %q (all: %#v)", surface, got[surface], annotation, got)
				}
			}
			for _, annotation := range tc.absent {
				for surface, gotAnnotation := range got {
					if gotAnnotation == annotation {
						t.Errorf("unexpected %s candidate %q", annotation, surface)
					}
				}
			}
		})
	}
}

func TestNumberCandidatesOrder(t *testing.T) {
	cands := rewrite.New().Candidates("12", nil)
	want := []string{"１２", "十二", "壱拾弐", "⑫", "Ⅻ", "ⅻ", "0xc", "014", "0b1100"}
	if len(cands) != len(want) {
		t.Fatalf("got %d candidates, want %d: %#v", len(cands), len(want), cands)
	}
	for i, surface := range want {
		if cands[i].Surface != surface {
			t.Errorf("candidate %d = %q, want %q", i, cands[i].Surface, surface)
		}
	}
}

// TestMozcComparisonCases 對照 MOZC.md 記錄的案例，避免文件與實作漂移。
func TestMozcComparisonCases(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"123", []string{"百二十三", "壱百弐拾参", "0x7b", "0173", "0b1111011"}},
		{"12", []string{"１２", "十二", "壱拾弐", "⑫", "Ⅻ", "ⅻ", "0xc", "014", "0b1100"}},
		{"1000", []string{"千", "壱阡"}},
		{"10000", []string{"一万", "壱萬"}},
		{"123456789", []string{"一億二千三百四十五万六千七百八十九", "壱億弐阡参百四拾五萬六阡七百八拾九", "0x75bcd15", "0726746425", "0b111010110111100110100010101"}},
	}
	for _, tc := range tests {
		cands := rewrite.New().Candidates(tc.input, nil)
		got := surfaces(cands)
		for _, want := range tc.want {
			if !contains(got, want) {
				t.Errorf("%s: missing %q in %#v", tc.input, want, got)
			}
		}
	}
}
