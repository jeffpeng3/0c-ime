// Package reading 定義核心唯一認得的讀音單位。
//
// 注音與日文共用同一種單位，core/session 不直接依賴注音或日文套件。
// 注音的一個單位是一個完整音節（例：ㄓㄨˋ）；日文的一個單位是一個かな字元
// （例：き、ゃ、ー），小寫かな、長音符與踊り字都各自算一個單位。
//
// 單位只描述讀音，不限制詞面長度：一個詞可以有兩個單位，對應三個字的表面。
package reading

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Unit 是一個讀音位置。
//
// 這是獨立的型別，不是 zhuyin.Syllable 的別名：注音與日文都使用它，
// 但 core/reading 不 import 任何語言實作。Unit.Valid 同時接受合法的
// 注音音節與かな，讓兩個語言共用同一個讀音契約。
type Unit string

// Sequence 是一串讀音單位，依輸入順序排列。
type Sequence []Unit

// KeySeparator 是 Key 用來串接單位的分隔字元。
const KeySeparator = " "

// Key 把讀音序列串成可當 map 鍵與個人資料鍵的字串。
// 對注音序列的輸出與 zhuyin.Key 完全相同，既有個人學習檔不必轉換。
func Key(seq Sequence) string {
	if len(seq) == 0 {
		return ""
	}
	parts := make([]string, len(seq))
	for i, u := range seq {
		parts[i] = string(u)
	}
	return strings.Join(parts, KeySeparator)
}

// Valid 回報單位是否為一般形式的單一讀音。
//
// 接受兩種形式：
//   - 一個結構合法的注音音節，例如 "ㄓㄨˋ"、"ㄇㄜ˙"
//   - 一個かな字元，含小寫かな、長音符、踊り字與半角カナ，例如 "き"、"ゃ"、"ー"、"々"
//
// 空字串、含空白、非法 UTF-8 或其他字元都不合法。
// 語言特定的檢查（例如日文詞彙是否真的這樣讀）仍由該語言的套件負責。
func (u Unit) Valid() bool {
	s := string(u)
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return false
		}
	}
	if hasZhuyinRune(s) {
		return validZhuyin(s)
	}
	return validKana(s)
}

// Valid 是 Unit.Valid 的函式形式，供既有的呼叫端使用。
func Valid(u Unit) bool { return u.Valid() }

// 注音音節的結構檢查。這裡不能 import core/zhuyin：zhuyin 的轉接層
// 需要 import 本套件，反向依賴會形成循環。規則與 zhuyin.Syllable.Valid
// 相同：各類符號至多一個、順序正確、至少一個發音符號、聲調在尾。
const (
	zhuyinInitials = "ㄅㄆㄇㄈㄉㄊㄋㄌㄍㄎㄏㄐㄑㄒㄓㄔㄕㄖㄗㄘㄙ"
	zhuyinMedials  = "ㄧㄨㄩ"
	zhuyinFinals   = "ㄚㄛㄜㄝㄞㄟㄠㄡㄢㄣㄤㄥㄦ"
	// zhuyinTone1 是一聲的內部記號；正規字串中不出現。
	zhuyinTone1 = 'ˉ'
	zhuyinTones = "ˉˊˇˋ˙"
)

func hasZhuyinRune(s string) bool {
	for _, r := range s {
		if zhuyinClass(r) != 0 {
			return true
		}
	}
	return false
}

// zhuyinClass 回傳注音符號的類別；不是注音符號回傳 0。
// 數值順序與 zhuyin.Class 相同：聲母 < 介音 < 韻母 < 聲調。
func zhuyinClass(r rune) int {
	switch {
	case strings.ContainsRune(zhuyinInitials, r):
		return 1
	case strings.ContainsRune(zhuyinMedials, r):
		return 2
	case strings.ContainsRune(zhuyinFinals, r):
		return 3
	case strings.ContainsRune(zhuyinTones, r):
		return 4
	}
	return 0
}

func validZhuyin(s string) bool {
	last, phonetic := 0, 0
	for _, r := range s {
		c := zhuyinClass(r)
		if c == 0 || c <= last || r == zhuyinTone1 {
			return false
		}
		if c != 4 {
			phonetic++
		}
		last = c
	}
	return phonetic > 0
}

// validKana 檢查單位是否為單一かな字元，可帶組合濁點。
// 小寫かな、長音符、踊り字與半角カナ都各自算一個單位。
func validKana(s string) bool {
	base := 0
	for _, r := range s {
		switch {
		case isKanaMark(r):
			continue
		case isKanaRune(r):
			base++
			if base > 1 {
				return false
			}
		default:
			return false
		}
	}
	return base == 1
}

// isKanaMark 是接在かな後面的組合濁點／半濁點。
func isKanaMark(r rune) bool {
	return r == 0x3099 || r == 0x309A || r == 0xFF9E || r == 0xFF9F
}

// isKanaRune 是單一かな字元可能出現的字元範圍。
func isKanaRune(r rune) bool {
	switch {
	case r >= 0x3041 && r <= 0x3096: // ひらがな
		return true
	case r >= 0x309D && r <= 0x309F: // ゝゞゟ
		return true
	case r >= 0x30A1 && r <= 0x30FA: // カタカナ
		return true
	case r >= 0x30FC && r <= 0x30FF: // ーヽヾヿ
		return true
	case r >= 0x31F0 && r <= 0x31FF: // カタカナ拡張
		return true
	case r >= 0xFF66 && r <= 0xFF9D: // 半角カナ
		return true
	case r == 0x3005, r == 0x3006, r == 0x303B: // 々〆〻（踊り字）
		return true
	}
	return false
}
