package session

import (
	"unicode"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lang/ja"
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

// JapaneseKeyPolicy 是日文 profile 的按鍵語意表。實作放在 core/lang/ja，
// core/session 只保留這個別名給既有呼叫端使用。
type JapaneseKeyPolicy = ja.Policy

func singlePrintable(key string) bool {
	r, size := utf8.DecodeRuneInString(key)
	return size > 0 && size == len(key) && unicode.IsPrint(r)
}
