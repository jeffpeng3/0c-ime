package output

import "github.com/0c-biggo/0c-ime/core/segment"

// CJKLatinSpacer 在 CJK 與 Latin 詞面直接相鄰時回傳一個半形空格。
//
// CJK 的判定與標點器共用 isCJKWord：漢字、假名、長音符與踊り字。
// Latin 只認 ASCII 英文字母，和既有注音流程的「中英文之間自動加空格」
// 一致；數字、全形英數、標點與既有空白都不補。只看兩個片段的邊界字元，
// 不讀取全域語言模式。
//
// 零值可用。是否啟用由 Assemble 的 Options.SpaceCJKLatin 決定。
type CJKLatinSpacer struct{}

// Between 回傳 left 與 right 之間的分隔字串。只有 left 結尾是 CJK、right
// 開頭是 Latin，或反過來時才回傳一個空白；其餘情況回傳空字串。
func (CJKLatinSpacer) Between(left, right segment.Segment) string {
	l, ok := lastRune(left.Surface)
	if !ok {
		return ""
	}
	r, ok := firstRune(right.Surface)
	if !ok {
		return ""
	}
	if isCJKWord(l) && isASCIILetter(r) || isASCIILetter(l) && isCJKWord(r) {
		return " "
	}
	return ""
}

func firstRune(s string) (rune, bool) {
	for _, r := range s {
		return r, true
	}
	return 0, false
}

func lastRune(s string) (rune, bool) {
	var last rune
	ok := false
	for _, r := range s {
		last, ok = r, true
	}
	return last, ok
}

func isASCIILetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}
