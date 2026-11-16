package mixed

import (
	"strings"
	"unicode"
)

// Spacing 是日文與英文之間是否補空白的設定。
//
// 日文通常不在漢字、かな與英文之間加空白，和注音的中英混打預設不同。因此
// 這裡的零值 SpacingNone 就是「不補空白」，要補空白必須明確選 SpacingAlways。
type Spacing int

const (
	// SpacingNone 不在日文與英文之間補空白，是預設值。
	SpacingNone Spacing = iota
	// SpacingAlways 在相鄰的日文字母與拉丁字母之間補一個半形空格。
	SpacingAlways
)

// DefaultSpacing 是日文混打的預設空白規則：不補空白。
const DefaultSpacing = SpacingNone

// String 回傳設定名稱，方便設定檔與測試輸出。
func (s Spacing) String() string {
	switch s {
	case SpacingAlways:
		return "always"
	case SpacingNone:
		return "none"
	default:
		return "unknown"
	}
}

// SpaceText 依設定在日文字母與拉丁字母相連的地方補半形空格。
//
// 只有直接相鄰的兩個字元才會處理：已經有空白、全角或半形標點、數字、換行
// 與其他符號都不補；英文字內部、日文內部與原有空白也不動。因此重複呼叫不會
// 再增加空白，句首、句尾也不會多出空格。
func SpaceText(text string, spacing Spacing) string {
	if spacing != SpacingAlways || text == "" {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 8)
	previous := rune(-1)
	for _, r := range text {
		if previous >= 0 && NeedsSpaceBetween(previous, r) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		previous = r
	}
	return b.String()
}

// NeedsSpaceBetween 回報 left 與 right 直接相連時是否需要補空白。
//
// 規則是「一邊是日文字母、另一邊是拉丁字母」。日文字母包含平假名、片假名、
// 漢字、々、〆、長音符與踊り字；拉丁字母包含 ASCII 與全角英文字母。數字不
// 算英文字，所以「3月」「Python3」不會被拆開。
func NeedsSpaceBetween(left, right rune) bool {
	return isJapaneseLetter(left) && isLatinLetter(right) ||
		isLatinLetter(left) && isJapaneseLetter(right)
}

// IsJapaneseLetter 回報 r 是否為可和英文相鄰的日文字母。
func IsJapaneseLetter(r rune) bool { return isJapaneseLetter(r) }

// IsLatinLetter 回報 r 是否為 ASCII 或全角拉丁字母。
func IsLatinLetter(r rune) bool { return isLatinLetter(r) }

func isJapaneseLetter(r rune) bool {
	switch {
	case unicode.Is(unicode.Hiragana, r):
		return true
	case unicode.Is(unicode.Katakana, r):
		return true
	case unicode.Is(unicode.Han, r):
		return true
	case r == '々', r == '〆', r == 'ー', r == 'ゝ', r == 'ゞ', r == 'ヽ', r == 'ヾ':
		return true
	}
	return false
}

func isLatinLetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
		r >= 'ａ' && r <= 'ｚ' || r >= 'Ａ' && r <= 'Ｚ'
}
