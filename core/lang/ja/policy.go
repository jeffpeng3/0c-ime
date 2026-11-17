package ja

import (
	"unicode"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/proto"
)

// Policy 是日文 profile 的按鍵語意表。
//
// Space／Convert 開始変換或換下一個候選；Enter 確定；Tab／Shift+Tab 換文節；
// Escape／無変換回到かな；F6–F10 轉換目前文節的表記。
type Policy struct{}

var _ lang.KeyPolicy = Policy{}

func (Policy) Handle(key string, st lang.KeyState) (lang.Action, bool) {
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
	if r, size := utf8.DecodeRuneInString(key); size > 0 && size == len(key) && unicode.IsPrint(r) {
		return lang.ActionInsert, true
	}
	return lang.ActionNone, false
}
