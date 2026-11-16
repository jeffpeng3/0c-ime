package golden

import (
	"strings"
	"testing"
)

const testDir = "../../testdata/golden/zh"

// TestBaseline 是 CI 的守門員：任何注音行為改變都會讓這裡失敗並顯示 diff。
func TestBaseline(t *testing.T) {
	s, err := Load(testDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Compare(); err != nil {
		t.Fatal(err)
	}
}

// TestCoverage 守住 T01 的完成條件：至少 40 個案例、10 個長句。
func TestCoverage(t *testing.T) {
	s, err := Load(testDir)
	if err != nil {
		t.Fatal(err)
	}
	long := 0
	for _, c := range s.Cases() {
		if strings.HasPrefix(c.Name, "long-sentence-") {
			long++
		}
	}
	if n := len(s.Cases()); n < 40 {
		t.Errorf("案例只有 %d 個，至少需要 40 個", n)
	}
	if long < 10 {
		t.Errorf("長句只有 %d 個，至少需要 10 個", long)
	}
}

// TestDeterministic 確認同一組輸入連跑兩次輸出逐位元組相同（Output 內建檢查，
// 這裡另外確認兩次獨立的 suite 也一樣）。
func TestDeterministic(t *testing.T) {
	a, err := Load(testDir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(testDir)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range a.Cases() {
		x, err := a.Output(c)
		if err != nil {
			t.Fatal(err)
		}
		y, err := b.Output(b.Cases()[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(x) != string(y) {
			t.Errorf("%s：兩個 suite 的輸出不同", c.Name)
		}
	}
}

// TestDiff 顯示第一處不同的行。
func TestDiff(t *testing.T) {
	got := Diff([]byte("a\nb\nc\n"), []byte("a\nB\nc\n"))
	for _, want := range []string{"第 2 行", "- b", "+ B"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff 缺少 %q：\n%s", want, got)
		}
	}
	if Diff([]byte("a\n"), []byte("a\n")) != "" {
		t.Error("相同內容不該有 diff")
	}
}
