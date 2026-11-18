package punct

import "testing"

func TestApplyJapaneseInsertsAfterKanaAndAtEnd(t *testing.T) {
	text := "今日は天気です"
	got, ok := ApplyJapanese(text, []Edit{{Offset: 3, Text: "、"}, {Offset: 7, Text: "。"}})
	if !ok || got != "今日は、天気です。" {
		t.Fatalf("got %q %v", got, ok)
	}
	// Removing only the inserted marks recovers the original string exactly.
	if len([]rune(got)) != len([]rune(text))+2 {
		t.Fatalf("changed characters: %q -> %q", text, got)
	}
	for _, bad := range [][]Edit{
		{{Offset: 0, Text: "。"}},
		{{Offset: 9, Text: "。"}},
		{{Offset: 2, Text: "."}},
		{{Offset: 4, Text: "、"}}, // past the end of a shorter input
	} {
		if got, ok := ApplyJapanese("今日は", bad); ok || got != "今日は" {
			t.Fatalf("invalid Japanese edits accepted: %v -> %q", bad, got)
		}
	}
}

func TestApplyInsertsOnlyBetweenHan(t *testing.T) {
	got, ok := Apply("明天早上九點開會請記得帶資料", []Edit{{Offset: 8, Text: "，"}})
	if !ok || got != "明天早上九點開會，請記得帶資料" {
		t.Fatalf("got %q %v", got, ok)
	}
	for _, bad := range [][]Edit{
		{{Offset: 0, Text: "，"}},                         // 開頭
		{{Offset: 3, Text: "，"}},                         // 段尾（「好嗎」後面）以外但落在 len 上
		{{Offset: 1, Text: "."}},                         // 不允許的符號
		{{Offset: 2, Text: "，"}, {Offset: 1, Text: "。"}}, // 非遞增
		{{Offset: 1, Text: "，，"}},                        // 多字元
	} {
		if got, ok := Apply("好嗎?", bad); ok || got != "好嗎?" {
			t.Fatalf("invalid edits must be rejected: %v -> %q", bad, got)
		}
	}
	if got, ok := Apply("價格100元", []Edit{{Offset: 2, Text: "，"}}); ok || got != "價格100元" {
		t.Fatalf("must not insert next to non-Han: %q", got)
	}
}
