package lang

// KeyState 是處理按鍵當下的核心狀態。KeyPolicy 只依這個狀態決定語意，
// 不自行讀取 session 內部欄位。
type KeyState struct {
	// Composing 表示有組字內容，包含尚未完成的讀音。
	Composing bool
	// Converting 表示日文変換中；注音沒有這個狀態。
	Converting bool
	// Candidates 表示候選視窗開著。
	Candidates bool
	// ClauseIndex 是目前選取的文節索引，ClauseCount 是文節總數。
	ClauseIndex int
	ClauseCount int
}

// Action 是 KeyPolicy 判定的語言動作。核心依 Action 執行，不依按鍵字面判斷。
type Action int

const (
	// ActionNone 表示這個按鍵不產生語言動作。
	ActionNone Action = iota
	// ActionInsert 把按鍵交給 composer 或輸入法。
	ActionInsert
	// ActionConvert 是変換：開候選，或前進到下一個候選。
	ActionConvert
	// ActionCommit 確定目前文節；沒有変換時送出整段組字。
	ActionCommit
	// ActionNextClause 移到下一個文節。
	ActionNextClause
	// ActionPrevClause 移到上一個文節。
	ActionPrevClause
	// ActionCancel 清除整段組字，或從変換回到かな。
	ActionCancel
	// ActionSelect 依按鍵上的數字選候選。
	ActionSelect
	// ActionComplete 接受預測補全。
	ActionComplete
	// ActionToHiragana 轉成ひらがな。
	ActionToHiragana
	// ActionToKatakana 轉成カタカナ。
	ActionToKatakana
	// ActionToHalfWidthKana 轉成半角カナ。
	ActionToHalfWidthKana
	// ActionToFullWidthLatin 轉成全角英數。
	ActionToFullWidthLatin
	// ActionToHalfWidthLatin 轉成半角英數。
	ActionToHalfWidthLatin
)

// KeyPolicy 把按鍵解讀成語言動作。
//
// Handle 回傳的 bool 表示這個按鍵是否由核心處理；false 時外殼自行處理或忽略。
// key 使用與 proto 相同的按鍵名稱（例如 "Space"、"Shift+Tab"、"Convert"、"F6"），
// 或單一可列印字元。
type KeyPolicy interface {
	Handle(key string, state KeyState) (Action, bool)
}

// Punctuator 把輸入的標點換成語言的標點形式。
type Punctuator interface {
	Insert(text string) string
}
