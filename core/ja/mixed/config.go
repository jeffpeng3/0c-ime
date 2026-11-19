package mixed

import "github.com/0c-biggo/0c-ime/core/english"

// Config 是日英混打共用的設定。零值可用：nil Dictionary 使用內建英文原型
// 詞彙，SpacingNone 不補空白，MinEnglishLen 為 3，MaxCandidates 為 8。
type Config struct {
	// Dictionary 是英文自動補完使用的唯讀詞典；nil 時使用 english.Prototype()。
	Dictionary *english.Dictionary
	// Vocabulary 是可共用的英文查詢層；Profile 用它讓組字器與変換器看到同一
	// 份學習詞。nil 時依 Dictionary 建立。
	Vocabulary *English
	// Spacing 是変換後日文與英文之間的空白規則，零值是 SpacingNone。
	Spacing Spacing
	// MinEnglishLen 是詞典英文詞照字面處理的最短長度，零值代表 3。
	// 兩字母的羅馬字（go、no、to）因此仍以かな為主。
	MinEnglishLen int
	// PreferKana 讓合法羅馬字優先於詞典英文詞（P2-52）：kanji、sake 這類
	// 同時是英文與かな的按鍵組成かな。大寫、全角、網址、識別字、數字與
	// 學過的英文詞仍照字面；零值維持原本的英文優先。
	PreferKana bool
	// MaxCandidates 是組字與単一文節一次列出的候選上限，零值代表 8。
	MaxCandidates int
}

// DefaultConfig 回傳日英混打的預設設定。
func DefaultConfig() Config {
	return Config{
		Spacing:       DefaultSpacing,
		MinEnglishLen: 3,
		MaxCandidates: 8,
	}
}

func (c Config) normalized() Config {
	if c.MinEnglishLen <= 0 {
		c.MinEnglishLen = 3
	}
	if c.MaxCandidates <= 0 {
		c.MaxCandidates = 8
	}
	c.MaxCandidates = min(c.MaxCandidates, MaxSuggestions)
	if c.Spacing != SpacingAlways {
		c.Spacing = SpacingNone
	}
	if c.Vocabulary == nil {
		c.Vocabulary = NewEnglish(c.Dictionary)
	}
	return c
}

// EnglishFor 回傳設定共用的英文查詢層，nil 設定也可用。
func (c Config) EnglishFor() *English { return c.normalized().Vocabulary }
