package segment

// SessionSegments 是 core/session 在 Phase 2 要實作的片段狀態介面。
//
// 契約刻意沒有 Language()、SetLanguage() 或 ActiveLanguage 這類方法：
// 語言只存在每個 Segment 的 Language 欄位，session 不保存單一 active language。
// 游標所在片段只是目前編輯位置，不是語言模式；移動游標不得清空其他語言的片段。
//
// 索引以 Segments() 的切片位置表示，位置以原始輸入的 rune 索引表示。
type SessionSegments interface {
	// Segments 回傳目前的片段。實作必須回傳副本或不可變視圖，
	// 呼叫端修改回傳切片不得影響 session 狀態。
	Segments() []Segment
	// Cursor 回傳游標的 rune 位置。
	Cursor() int
	// ActiveSegment 回傳游標所在片段的索引；沒有片段時回傳 -1。
	ActiveSegment() int
	// SegmentAt 回傳包含輸入位置 pos 的片段索引。
	SegmentAt(pos int) (int, bool)
	// SetCursor 移動游標，並讓包含新位置的片段成為目前片段。
	// pos 超出可編輯範圍時回傳 false，游標不變。
	SetCursor(pos int) bool
	// ReplaceSegment 替換索引 index 的片段，保留其他片段與它們的語言標籤。
	ReplaceSegment(index int, seg Segment) bool
	// InsertSegment 在索引 index 前插入片段；index 等於片段數時附加在尾端。
	InsertSegment(index int, seg Segment) bool
	// RemoveSegment 移除索引 index 的片段。實作可合併相鄰同語言片段，
	// 但不得因此改變其他片段的語言標籤。
	RemoveSegment(index int) bool
}
