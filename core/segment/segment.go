// Package segment 定義 profile-free 的輸入分段契約。
//
// 分段器把原始按鍵或文字切成注音、かな、Latin、數字、符號等片段。片段只帶
// 語言標籤與輸出變體，不帶「目前語言模式」；同一次輸入可以同時含多個語言。
// 這個套件只放型別與介面，實際分段規則由 P2-10 的實作補上。
package segment

import (
	"fmt"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Script 是片段使用的書寫系統。值使用穩定的英文名稱，可寫進 JSON 與測試資料。
type Script uint8

const (
	// ScriptUnknown 表示尚未判定。
	ScriptUnknown Script = iota
	// ScriptZhuyin 是注音符號。
	ScriptZhuyin
	// ScriptKana 是平假名、片假名、長音符與踊り字。
	ScriptKana
	// ScriptLatin 是拉丁字母。它同時可能是英文或羅馬字，不能單獨決定語言。
	ScriptLatin
	// ScriptDigit 是數字。
	ScriptDigit
	// ScriptSymbol 是標點、空白與其他符號。
	ScriptSymbol
	// ScriptHan 是漢字。漢字本身不是語言強訊號。
	ScriptHan
	// ScriptMixed 是無法用單一書寫系統表示的混合片段。
	ScriptMixed
)

// String 回傳穩定的名稱。
func (s Script) String() string {
	switch s {
	case ScriptZhuyin:
		return "zhuyin"
	case ScriptKana:
		return "kana"
	case ScriptLatin:
		return "latin"
	case ScriptDigit:
		return "digit"
	case ScriptSymbol:
		return "symbol"
	case ScriptHan:
		return "han"
	case ScriptMixed:
		return "mixed"
	}
	return "unknown"
}

// Strong 回報這個書寫系統是否為語言的強訊號。
//
// 注音與かな只能來自特定語言，因此是強訊號；Latin 同時可能是英文或羅馬字，
// 數字、符號、漢字與混合片段都不是。
func (s Script) Strong() bool {
	return s == ScriptZhuyin || s == ScriptKana
}

// MarshalText 讓 Script 在 JSON 中以穩定的名稱輸出，而不是數字。
func (s Script) MarshalText() ([]byte, error) {
	if s > ScriptMixed {
		return nil, fmt.Errorf("segment: 未知 script %d", uint8(s))
	}
	return []byte(s.String()), nil
}

// UnmarshalText 解析 Script 名稱。
func (s *Script) UnmarshalText(text []byte) error {
	v, ok := parseScript(string(text))
	if !ok {
		return fmt.Errorf("segment: 未知 script %q", text)
	}
	*s = v
	return nil
}

// ParseScript 把穩定名稱轉成 Script。
func ParseScript(name string) (Script, error) {
	v, ok := parseScript(name)
	if !ok {
		return ScriptUnknown, fmt.Errorf("segment: 未知 script %q", name)
	}
	return v, nil
}

func parseScript(name string) (Script, bool) {
	switch name {
	case "unknown":
		return ScriptUnknown, true
	case "zhuyin":
		return ScriptZhuyin, true
	case "kana":
		return ScriptKana, true
	case "latin":
		return ScriptLatin, true
	case "digit":
		return ScriptDigit, true
	case "symbol":
		return ScriptSymbol, true
	case "han":
		return ScriptHan, true
	case "mixed":
		return ScriptMixed, true
	}
	return ScriptUnknown, false
}

// Span 是輸入中的半開區間 [Start, End)，以 rune 為單位。
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Len 回傳片段長度。
func (s Span) Len() int { return s.End - s.Start }

// Valid 回報區間是否合法。
func (s Span) Valid() bool { return s.Start >= 0 && s.End >= s.Start }

// Contains 回報位置 pos 是否落在區間內。
func (s Span) Contains(pos int) bool { return pos >= s.Start && pos < s.End }

// Segment 是一個輸入到輸出的片段：input span、讀音、詞面、語言標籤與輸出變體。
//
// 腳本分段器只填 Span、Script、Language；lattice 與候選填入 Reading、Surface、
// Variant。片段是 lattice 的邊，但這個型別不決定路徑，也不保存任何全域語言狀態。
type Segment struct {
	// Span 是片段對應的原始輸入範圍。
	Span Span `json:"span"`
	// Script 是主要書寫系統。
	Script Script `json:"script"`
	// Reading 是這段輸入的讀音。尚未轉換時可以是 nil。
	Reading reading.Sequence `json:"reading,omitempty"`
	// Surface 是要輸出的詞面。尚未選字時可以是空字串。
	Surface string `json:"surface,omitempty"`
	// Language 是這段輸入的語言標籤。未判定時為零值。
	Language lang.LanguageTag `json:"language"`
	// Variant 是輸出字形慣例。空字串表示由輸出組裝器依語言標籤決定。
	Variant lang.OutputVariant `json:"variant,omitempty"`
}

// Valid 回報片段的基本欄位是否一致。
//
// 這是結構檢查，不判斷轉換是否正確；Script 與 Language 可以在分段過程中
// 暫時留空，最終候選再由 lattice 補齊。
func (s Segment) Valid() bool {
	if !s.Span.Valid() {
		return false
	}
	if !s.Language.IsZero() && !s.Language.Valid() {
		return false
	}
	if s.Variant != "" && !s.Variant.Valid() {
		return false
	}
	for _, u := range s.Reading {
		if !u.Valid() {
			return false
		}
	}
	return true
}

// ReadingKey 回傳讀音的穩定鍵，供快取與測試使用。
// 空讀音與 reading.Key 一樣回傳空字串。
func (s Segment) ReadingKey() string { return reading.Key(s.Reading) }

// Segmenter 把原始輸入切成片段。
//
// 實作必須讓回傳片段的 Span 依序覆蓋輸入，且不得根據單一「目前語言」丟棄
// 其他書寫系統。Latin 片段要保留給英文與羅馬字兩條路徑。
type Segmenter interface {
	Split(input string) []Segment
}
