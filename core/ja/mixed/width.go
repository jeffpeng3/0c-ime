package mixed

import "strings"

// IsFullWidthAlnum 回報 r 是否為全角英數字。
func IsFullWidthAlnum(r rune) bool {
	return r >= '０' && r <= '９' ||
		r >= 'Ａ' && r <= 'Ｚ' ||
		r >= 'ａ' && r <= 'ｚ'
}

// HasFullWidthAlnum 回報 text 是否含全角英數字。
func HasFullWidthAlnum(text string) bool {
	for _, r := range text {
		if IsFullWidthAlnum(r) {
			return true
		}
	}
	return false
}

// HasASCIIAlnum 回報 text 是否含 ASCII 英數字。
func HasASCIIAlnum(text string) bool {
	for _, r := range text {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

// ToHalfWidthLatin 把全角英數字轉成半角，其他字元（含全角標點）保持原樣。
//
// 網址、email 與識別字預設不呼叫這個函式；只有使用者明確要求換字寬時才用。
func ToHalfWidthLatin(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return r - ('０' - '0')
		case r >= 'Ａ' && r <= 'Ｚ':
			return r - ('Ａ' - 'A')
		case r >= 'ａ' && r <= 'ｚ':
			return r - ('ａ' - 'a')
		}
		return r
	}, text)
}

// ToFullWidthLatin 把 ASCII 英數字轉成全角，其他字元保持原樣。
func ToFullWidthLatin(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9':
			return r + ('０' - '0')
		case r >= 'A' && r <= 'Z':
			return r + ('Ａ' - 'A')
		case r >= 'a' && r <= 'z':
			return r + ('ａ' - 'a')
		}
		return r
	}, text)
}

// WidthVariant 回傳 text 的另一種英數字寬度；沒有英數字時 ok 為 false。
//
// 全角與半角混用時回傳全角（和日文輸入的預設一致），全部全角時回傳半角。
func WidthVariant(text string) (string, bool) {
	hasFull, hasASCII := HasFullWidthAlnum(text), HasASCIIAlnum(text)
	if !hasFull && !hasASCII {
		return "", false
	}
	if hasFull && hasASCII {
		return ToFullWidthLatin(text), true
	}
	if hasFull {
		return ToHalfWidthLatin(text), true
	}
	return ToFullWidthLatin(text), true
}
