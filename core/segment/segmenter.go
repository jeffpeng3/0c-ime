package segment

import (
	"unicode"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// Splitter 是預設的腳本分段器。
//
// Splitter 沒有狀態，可以同時給多個 session 使用，也可以重複呼叫。它只依
// 輸入字元判定書寫系統，不讀取主要語言、前文或任何全域模式。
type Splitter struct{}

// New 回傳預設的腳本分段器。
func New() Segmenter { return Splitter{} }

// NewSegmenter 是 New 的別名，讓呼叫端用較完整的名字表達意圖。
func NewSegmenter() Segmenter { return New() }

// Split 是 Splitter.Split 的套件層級便利函式。
func Split(input string) []Segment { return Splitter{}.Split(input) }

// Detect 判定單一字元的書寫系統。
//
// 這是 Split 使用的同一個判定，供測試與診斷使用；正式流程仍應呼叫 Split，
// 因為邊界規則會合併相鄰的 Latin 與數字，並保留連續同類字元的 span。
func Detect(r rune) Script { return scriptOf(r) }

// Split 把 input 切成依序覆蓋所有 rune 的片段。
//
// 邊界規則：
//   - 相鄰且同書寫系統的字元合併成一個 span；
//   - 相鄰的 Latin 與數字合併成一個混合 span，因為 abc123 之類的識別字不能
//     在中間拆成兩種語言候選；
//   - 注音、かな、漢字、Latin、數字與符號之間只要換類就切開，注音與かな的
//     強訊號不會被混合片段蓋掉；
//   - 空白、標點與其他符號合併成符號 span。
//
// 空輸入回傳 nil。片段只填 Span、Script 與 Language，Reading、Surface 與
// Variant 留給後續的 lattice 與輸出組裝。
func (Splitter) Split(input string) []Segment {
	runes := []rune(input)
	if len(runes) == 0 {
		return nil
	}

	segments := make([]Segment, 0, len(runes))
	for i := 0; i < len(runes); {
		script := scriptOf(runes[i])
		if script == ScriptLatin || script == ScriptDigit {
			end, latin, digit := scanAlnum(runes, i)
			segments = append(segments, newSegment(i, end, alnumScript(latin, digit)))
			i = end
			continue
		}

		end := i + 1
		for end < len(runes) && scriptOf(runes[end]) == script {
			end++
		}
		segments = append(segments, newSegment(i, end, script))
		i = end
	}
	return segments
}

// scanAlnum 從 start 起掃過連續的 Latin 與數字，回報結束位置與出現過的種類。
func scanAlnum(runes []rune, start int) (end int, latin, digit bool) {
	end = start
	for end < len(runes) {
		switch scriptOf(runes[end]) {
		case ScriptLatin:
			latin = true
		case ScriptDigit:
			digit = true
		default:
			return end, latin, digit
		}
		end++
	}
	return end, latin, digit
}

func alnumScript(latin, digit bool) Script {
	switch {
	case latin && digit:
		return ScriptMixed
	case digit:
		return ScriptDigit
	default:
		return ScriptLatin
	}
}

func newSegment(start, end int, script Script) Segment {
	return Segment{
		Span:     Span{Start: start, End: end},
		Script:   script,
		Language: languageOf(script),
	}
}

// languageOf 只回傳強訊號能確定的語言。Latin 可能同時是英文或羅馬字，
// 漢字可能同時是中文或日文，因此都保留未指定，交給 lattice 依候選決定。
func languageOf(script Script) lang.LanguageTag {
	switch script {
	case ScriptZhuyin:
		return lang.Tag(lang.Zh)
	case ScriptKana:
		return lang.Tag(lang.Ja)
	default:
		return lang.LanguageTag{}
	}
}

func scriptOf(r rune) Script {
	switch {
	case isZhuyin(r):
		return ScriptZhuyin
	case isKana(r):
		return ScriptKana
	case isHan(r):
		return ScriptHan
	case unicode.Is(unicode.Latin, r):
		return ScriptLatin
	case unicode.IsDigit(r):
		return ScriptDigit
	case unicode.IsLetter(r) || unicode.IsNumber(r):
		// 不在本專案語言範圍內的字母或數字，保留為未判定，不冒充 Latin
		// 或數字，避免產生錯誤的語言候選。
		return ScriptUnknown
	default:
		return ScriptSymbol
	}
}

func isZhuyin(r rune) bool {
	return zhuyin.ClassOf(r) != zhuyin.None || unicode.Is(unicode.Bopomofo, r)
}

func isKana(r rune) bool {
	if unicode.In(r, unicode.Hiragana, unicode.Katakana) {
		return true
	}
	switch r {
	case 'ー', 'ｰ', // 長音符
		'ゝ', 'ゞ', 'ヽ', 'ヾ', // 踊り字
		'\u3099', '\u309A', '\u309B', '\u309C': // 濁點與半濁點
		return true
	}
	return false
}

func isHan(r rune) bool {
	return unicode.Is(unicode.Han, r) || r == '々' || r == '〆'
}
