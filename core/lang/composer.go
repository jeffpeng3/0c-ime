package lang

import "github.com/0c-biggo/0c-ime/core/reading"

// Composer 把輸入按鍵組成讀音單位，並保留尚未完成的輸入。
//
// 注音：Insert 收到注音符號，一個音節完成後出現在 ComposeResult.Completed。
// 日文：Insert 收到羅馬字或かな，かな確定後出現在 ComposeResult.Completed。
type Composer interface {
	// Insert 處理一個按鍵。bool 回報按鍵是否被 composer 接受；
	// 被接受不代表完成了單位，完成的單位放在 ComposeResult.Completed。
	Insert(key string) (ComposeResult, bool)
	// Backspace 移除最後一項未完成的輸入；沒有可移除的內容時回傳 false。
	Backspace() bool
	// Reset 清空所有組字狀態。
	Reset()
	// Units 回傳已完成的讀音單位。
	Units() reading.Sequence
	// Pending 回傳尚未完成的輸入，例如 "k"；沒有時為空字串。
	Pending() string
	// Segments 回傳目前組字內容的顯示分段。
	Segments() []DisplaySegment
}

// ComposeResult 是一次 Insert 之後，composer 對外揭露的變化。
type ComposeResult struct {
	// Completed 是這次按鍵完成的讀音單位，依輸入順序。
	Completed reading.Sequence
	// Pending 是尚未完成的輸入，與 Pending() 相同。
	Pending string
}

// DisplaySegment 是 composer 目前組字內容的一段。
//
// 注音通常是一段已完成的音節加上未完成的符號；日文是一段かな或已確定的文節。
// 外殼只負責畫，不自行拆字。
type DisplaySegment struct {
	// Reading 是這一段對應的讀音。
	Reading reading.Sequence
	// Text 是要顯示的文字。
	Text string
	// Pending 表示這一段仍未完成，外殼可加底線或反白。
	Pending bool
}
