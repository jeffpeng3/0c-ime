package session

import (
	"unicode"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/proto"
)

// ZhuyinKeyPolicy 是注音 profile 的按鍵語意表。
//
// 它只描述語意，實際的注音狀態仍由 session 既有的路徑處理；保留這份 policy 是
// 為了讓語言切換與外殼能用同一張表比較注音與日文的差異：
//   - Space 在注音是開候選（変換），在日文是変換。
//   - Tab 在注音是補全，在日文（変換中）是下一個文節。
type ZhuyinKeyPolicy struct{}

var _ lang.KeyPolicy = ZhuyinKeyPolicy{}

func (ZhuyinKeyPolicy) Handle(key string, st lang.KeyState) (lang.Action, bool) {
	switch key {
	case " ", proto.KeyShiftSpace:
		if st.Composing || st.Candidates {
			return lang.ActionConvert, true
		}
		return lang.ActionInsert, true
	case proto.KeyEnter:
		if st.Composing || st.Candidates {
			return lang.ActionCommit, true
		}
	case proto.KeyTab:
		if st.Composing || st.Candidates {
			return lang.ActionComplete, true
		}
	case proto.KeyBackTab, "Shift+Tab":
		return lang.ActionNone, false
	case proto.KeyEscape:
		if st.Composing || st.Candidates {
			return lang.ActionCancel, true
		}
	case proto.KeyConvert, proto.KeyNonConvert,
		proto.KeyHiragana, proto.KeyKatakana, proto.KeyHalfWidthKana,
		proto.KeyFullWidthLatin, proto.KeyHalfWidthLatin:
		return lang.ActionNone, false
	}
	if singlePrintable(key) {
		return lang.ActionInsert, true
	}
	return lang.ActionNone, false
}

// JapaneseKeyPolicy 是日文 profile 的按鍵語意表。
//
// Space／Convert 開始変換或換下一個候選；Enter 確定；Tab／Shift+Tab 換文節；
// Escape／無変換回到かな；F6–F10 轉換目前文節的表記。
type JapaneseKeyPolicy struct{}

var _ lang.KeyPolicy = JapaneseKeyPolicy{}

func (JapaneseKeyPolicy) Handle(key string, st lang.KeyState) (lang.Action, bool) {
	switch key {
	case " ", proto.KeyShiftSpace, proto.KeyConvert:
		if st.Composing || st.Converting || st.Candidates {
			return lang.ActionConvert, true
		}
		return lang.ActionNone, false
	case proto.KeyEnter:
		if st.Composing || st.Converting {
			return lang.ActionCommit, true
		}
		return lang.ActionNone, false
	case proto.KeyTab:
		if st.Converting {
			return lang.ActionNextClause, true
		}
		return lang.ActionNone, false
	case proto.KeyBackTab, "Shift+Tab":
		if st.Converting {
			return lang.ActionPrevClause, true
		}
		return lang.ActionNone, false
	case proto.KeyEscape, proto.KeyNonConvert:
		if st.Composing || st.Converting {
			return lang.ActionCancel, true
		}
		return lang.ActionNone, false
	case proto.KeyHiragana:
		if st.Composing || st.Converting {
			return lang.ActionToHiragana, true
		}
		return lang.ActionNone, false
	case proto.KeyKatakana:
		if st.Composing || st.Converting {
			return lang.ActionToKatakana, true
		}
		return lang.ActionNone, false
	case proto.KeyHalfWidthKana:
		if st.Composing || st.Converting {
			return lang.ActionToHalfWidthKana, true
		}
		return lang.ActionNone, false
	case proto.KeyFullWidthLatin:
		if st.Composing || st.Converting {
			return lang.ActionToFullWidthLatin, true
		}
		return lang.ActionNone, false
	case proto.KeyHalfWidthLatin:
		if st.Composing || st.Converting {
			return lang.ActionToHalfWidthLatin, true
		}
		return lang.ActionNone, false
	}
	if len([]rune(key)) == 1 && unicode.IsDigit([]rune(key)[0]) && st.Converting {
		return lang.ActionSelect, true
	}
	if singlePrintable(key) {
		return lang.ActionInsert, true
	}
	return lang.ActionNone, false
}

func singlePrintable(key string) bool {
	r, size := utf8.DecodeRuneInString(key)
	return size > 0 && size == len(key) && unicode.IsPrint(r)
}
