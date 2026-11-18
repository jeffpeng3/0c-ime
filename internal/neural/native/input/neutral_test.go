package input

import "testing"

// TestNeutralLast mirrors sequence_runtime.Runtime.score's neutral branch
// decision: neutral_expand_penalty > 0 matches the base, otherwise the exact
// listed syllable is required.
func TestNeutralLast(t *testing.T) {
	tolerant := []string{"ㄅㄚ", "ㄇㄚ", "ㄧㄝ"}
	cases := []struct {
		name   string
		mode   string
		last   string
		expand bool
		want   bool
	}{
		{"exact", "", "ㄇㄚ", false, true},
		{"exact-tone-miss", "", "ㄇㄚˊ", false, false},
		{"base-match", "", "ㄇㄚˊ", true, true},
		{"base-match-toneless", "", "ㄇㄚ", true, true},
		{"base-no-match", "", "ㄇㄛ", true, false},
		{"neutral-mark-reference", "", "˙", true, false},
		{"already-neutral", "", "ㄇㄚ˙", true, false},
		{"text-mode", "text", "ㄇㄚ", true, false},
		{"empty-last", "", "", true, false},
		{"unknown-base", "", "ㄓㄨ", true, false},
	}
	for _, tc := range cases {
		if got := NeutralLast(tc.mode, tc.last, tc.expand, tolerant); got != tc.want {
			t.Errorf("%s: NeutralLast(%q, %q, %v) = %v, want %v", tc.name, tc.mode, tc.last, tc.expand, got, tc.want)
		}
	}
}
