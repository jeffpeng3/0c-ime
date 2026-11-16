package mixed_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/mixed"
)

func TestSpaceTextDefaultNoSpace(t *testing.T) {
	cases := []string{
		"今日はhello",
		"hello今日",
		"API文件",
		"こんにちはworld",
	}
	for _, text := range cases {
		if got := mixed.SpaceText(text, mixed.SpacingNone); got != text {
			t.Errorf("SpaceText(%q, none) = %q，日文預設不應補空白", text, got)
		}
	}
}

func TestSpaceTextAlways(t *testing.T) {
	cases := []struct{ in, want string }{
		{"今日はhello", "今日は hello"},
		{"hello今日", "hello 今日"},
		{"API文件", "API 文件"},
		{"こんにちはworld", "こんにちは world"},
		{"今日はhello世界", "今日は hello 世界"},
		{"hello world", "hello world"},        // 英文內部的空白照舊
		{"今日は hello", "今日は hello"},            // 已有空白不重複
		{"今日は、hello", "今日は、hello"},            // 標點旁不補
		{"Python3と3月", "Python3と3月"},          // 數字不算英文字
		{"ＡＢＣ文件", "ＡＢＣ 文件"},                   // 全角英文也算拉丁字母
		{"今日はhello、world", "今日は hello、world"}, // 全角標點隔開
		{"ＡＢＣ１２３", "ＡＢＣ１２３"},                  // 全角數字不算字母
		{"", ""},
	}
	for _, tc := range cases {
		if got := mixed.SpaceText(tc.in, mixed.SpacingAlways); got != tc.want {
			t.Errorf("SpaceText(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := mixed.SpaceText(tc.want, mixed.SpacingAlways); got != tc.want {
			t.Errorf("SpaceText 不是幂等：%q → %q", tc.want, got)
		}
	}
}

func TestNeedsSpaceBetween(t *testing.T) {
	cases := []struct {
		left, right rune
		want        bool
	}{
		{'日', 'h', true},
		{'h', '日', true},
		{'ぁ', 'A', true},
		{'ー', 'x', true},
		{'Ａ', '日', true},
		{'日', 'Ａ', true},
		{'日', '本', false},
		{'h', 'i', false},
		{'日', '3', false},
		{'3', 'h', false},
		{'、', 'h', false},
		{'h', '、', false},
	}
	for _, tc := range cases {
		if got := mixed.NeedsSpaceBetween(tc.left, tc.right); got != tc.want {
			t.Errorf("NeedsSpaceBetween(%q, %q) = %v, want %v", tc.left, tc.right, got, tc.want)
		}
	}
}

func TestSpacingString(t *testing.T) {
	if got := mixed.DefaultSpacing; got != mixed.SpacingNone {
		t.Fatalf("DefaultSpacing = %v，日文預設應為 none", got)
	}
	if mixed.SpacingNone.String() != "none" || mixed.SpacingAlways.String() != "always" {
		t.Fatalf("Spacing.String = %q, %q", mixed.SpacingNone, mixed.SpacingAlways)
	}
}
