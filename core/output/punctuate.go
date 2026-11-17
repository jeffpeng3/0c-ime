package output

import (
	"unicode"

	"github.com/0c-biggo/0c-ime/core/lang"
)

// LanguagePunctuator 依片段語言把半形標點換成該語言的全形標點。
//
// 只處理緊鄰 CJK 字元的標點，純英文、數字與網址中的標點不動，因此英文
// 片段永遠維持原文。轉換不新增、不刪除、不重排任何字，只替換單一標點
// 字元；已是全形或契約外的標點照原樣保留。
//
// 中文：, . ? ! : ; → ， 。 ？ ！ ： ；
// 日文：, . ? ! : ; → 、 。 ？ ！ ： ；
// 英文與未知語言：原樣回傳。
//
// 零值可用。這裡不載入標點模型；模型式補標點仍由 core/punct 與
// core/ja/punct 在上游處理。
type LanguagePunctuator struct{}

var (
	zhPunctuation = map[rune]rune{
		',': '，',
		'.': '。',
		'?': '？',
		'!': '！',
		':': '：',
		';': '；',
	}
	jaPunctuation = map[rune]rune{
		',': '、',
		'.': '。',
		'?': '？',
		'!': '！',
		':': '：',
		';': '；',
	}
)

// Insert 依 tag 的語言替換文字中的半形標點。未知語言回傳原文。
func (LanguagePunctuator) Insert(text string, tag lang.LanguageTag) string {
	var table map[rune]rune
	switch tag.Language {
	case lang.Zh:
		table = zhPunctuation
	case lang.Ja:
		table = jaPunctuation
	default:
		return text
	}
	r := []rune(text)
	changed := false
	for i, c := range r {
		replacement, ok := table[c]
		if !ok || !punctuationTouchesCJK(r, i) {
			continue
		}
		r[i] = replacement
		changed = true
	}
	if !changed {
		return text
	}
	return string(r)
}

// punctuationTouchesCJK 回報位置 i 的標點是否緊鄰 CJK 字元。標點必須至少
// 一側是 CJK，避免把英文片段的 "Hello, world" 或數字 "3.14" 改寫。
func punctuationTouchesCJK(r []rune, i int) bool {
	if i > 0 && isCJKWord(r[i-1]) {
		return true
	}
	return i+1 < len(r) && isCJKWord(r[i+1])
}

// isCJKWord 回報字元是否為會與 Latin 相鄰的 CJK 文字。注音符號不是輸出
// 字形，不列入；漢字、假名、長音符與踊り字都會觸發標點與間距規則。
func isCJKWord(r rune) bool {
	if unicode.Is(unicode.Han, r) {
		return true
	}
	if unicode.In(r, unicode.Hiragana, unicode.Katakana) {
		return true
	}
	switch r {
	case 'ー', '々', '〆', 'ヶ':
		return true
	}
	return false
}
