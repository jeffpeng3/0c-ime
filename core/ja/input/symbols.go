package input

import "github.com/0c-biggo/0c-ime/core/lang"

// Symbols 是可設定的標點與記号鍵對照表。
//
// 組字層的閱讀單位只放かな，因此標點不進入 reading.Sequence。
// 外殼可以在送出按鍵前先問 Symbols，或把 Symbols 當成 lang.Punctuator 使用。
type Symbols struct {
	table map[string]string
}

var _ lang.Punctuator = (*Symbols)(nil)

// DefaultSymbols 回傳內建的日文標點對照表。
func DefaultSymbols() *Symbols { return defaultSymbols }

// NewSymbols 建立自訂對照表。空鍵或空值會被忽略。
func NewSymbols(table map[string]string) *Symbols {
	copied := make(map[string]string, len(table))
	for key, text := range table {
		if key == "" || text == "" {
			continue
		}
		copied[key] = text
	}
	return &Symbols{table: copied}
}

// Lookup 查詢按鍵對應的標點或記号。
func (s *Symbols) Lookup(key string) (string, bool) {
	if s == nil {
		return "", false
	}
	text, ok := s.table[key]
	return text, ok
}

// Insert 實作 lang.Punctuator；沒有對照的輸入原樣回傳。
func (s *Symbols) Insert(text string) string {
	if mapped, ok := s.Lookup(text); ok {
		return mapped
	}
	return text
}

var defaultSymbols = NewSymbols(map[string]string{
	".":  "。",
	",":  "、",
	"?":  "？",
	"!":  "！",
	"[":  "「",
	"]":  "」",
	"{":  "『",
	"}":  "』",
	"(":  "（",
	")":  "）",
	"/":  "・",
	"\\": "￥",
	"~":  "〜",
	":":  "：",
	";":  "；",
	"<":  "＜",
	">":  "＞",
	"@":  "＠",
	"#":  "＃",
	"$":  "＄",
	"%":  "％",
	"&":  "＆",
	"*":  "＊",
	"+":  "＋",
	"=":  "＝",
	"_":  "＿",
	"|":  "｜",
	"`":  "｀",
	"^":  "＾",
})
