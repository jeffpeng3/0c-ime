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
// 檢查的是所有語言共通的部分：非空、合法 UTF-8、不含空白與分隔字元。
// 語言特定的合法性（例如注音音節結構）由該語言的套件判斷。
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
	return true
}
