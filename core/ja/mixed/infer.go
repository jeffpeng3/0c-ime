package mixed

import "github.com/0c-biggo/0c-ime/core/lang"

// LiteralFor 回報一段獨立的 ASCII 按鍵會不會被混打組字器當成照字面文字。
//
// 它重用 Composer 的判定：大寫、全角英數、網址、email、識別字、已知英文詞、
// 學過的詞，以及打不成合法羅馬字的情況都算照字面。回傳 false 代表這段按鍵
// 可以完整組成かな，因此可以交給日文 runtime。
//
// 這個函式只判斷整段按鍵，不保存狀態；session 用它決定是否把待定按鍵從
// 注音／英文路徑轉到日文路徑。
func LiteralFor(raw string, cfg Config) bool {
	if raw == "" {
		return true
	}
	c := newComposer(lang.InputMethod{}, cfg)
	literal, _ := c.literalFor(raw)
	return literal
}

// KanaFor 回傳 raw 組成的かな與尚未完成的羅馬字。ok 為 false 表示這段按鍵
// 不是合法的羅馬字，或會被照字面處理。
func KanaFor(raw string, cfg Config) (kana, pending string, ok bool) {
	if raw == "" {
		return "", "", false
	}
	c := newComposer(lang.InputMethod{}, cfg)
	if literal, valid := c.literalFor(raw); !valid || literal {
		return "", "", false
	}
	shadow, valid := c.replay(raw)
	if !valid {
		return "", "", false
	}
	return sequenceText(shadow.Units()), shadow.Pending(), true
}
