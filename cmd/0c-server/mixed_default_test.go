package main

import "testing"

// P2-40：Windows 只有 Mixed profile，新 session 一律從混打模式開始，使用者不必先切
// 中英或中日；其他平台與舊版外殼仍照設定檔。
func TestMixedInputIsTheWindowsDefault(t *testing.T) {
	if !mixedInputEnabled("windows", false) {
		t.Fatal("Windows Mixed profile 必須直接接受英文")
	}
	if mixedInputEnabled("linux", false) || mixedInputEnabled("darwin", false) {
		t.Fatal("其他平台維持設定檔的 mixed_input")
	}
	if !mixedInputEnabled("linux", true) {
		t.Fatal("設定檔開啟時仍要生效")
	}
}
