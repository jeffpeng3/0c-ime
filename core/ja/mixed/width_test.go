package mixed_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/mixed"
)

func TestWidthConversion(t *testing.T) {
	cases := []struct{ in, half, full string }{
		{"ＡＢＣ１２３", "ABC123", "ＡＢＣ１２３"},
		{"API123", "API123", "ＡＰＩ１２３"},
		{"ＡＢc１２", "ABc12", "ＡＢｃ１２"},
		{"今日はＡＰＩ", "今日はAPI", "今日はＡＰＩ"},
	}
	for _, tc := range cases {
		if got := mixed.ToHalfWidthLatin(tc.in); got != tc.half {
			t.Errorf("ToHalfWidthLatin(%q) = %q, want %q", tc.in, got, tc.half)
		}
		if got := mixed.ToFullWidthLatin(tc.in); got != tc.full {
			t.Errorf("ToFullWidthLatin(%q) = %q, want %q", tc.in, got, tc.full)
		}
	}
}

func TestWidthVariant(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"API", "ＡＰＩ", true},
		{"ＡＰＩ", "API", true},
		{"API１２", "ＡＰＩ１２", true},
		{"今日は", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := mixed.WidthVariant(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("WidthVariant(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestWidthDetectors(t *testing.T) {
	if !mixed.HasASCIIAlnum("abc") || mixed.HasASCIIAlnum("今日") {
		t.Error("HasASCIIAlnum 判斷錯誤")
	}
	if !mixed.HasFullWidthAlnum("ＡＢＣ") || mixed.HasFullWidthAlnum("ABC") {
		t.Error("HasFullWidthAlnum 判斷錯誤")
	}
	for _, r := range "ＡＺａｚ０９" {
		if !mixed.IsFullWidthAlnum(r) {
			t.Errorf("IsFullWidthAlnum(%q) = false", r)
		}
	}
	if mixed.IsFullWidthAlnum('A') || mixed.IsFullWidthAlnum('日') {
		t.Error("IsFullWidthAlnum 應只認全角英數")
	}
}
