package main

import (
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/internal/compile"
)

// 用 repo 內真正的 data/ja 編譯，再匯出 0c-server 實際載入的 runtime 布局，
// 最後走正式日文 session 做一次轉換。這條路徑不使用測試詞庫。
func TestJaRuntimePackFromRealData(t *testing.T) {
	d, errs, err := compile.LoadJAWithMozcScale(filepath.Join("..", "..", "data", "ja"),
		filepath.Join("..", "..", "data"), 1.0/1000.0)
	if err != nil || len(errs) > 0 {
		t.Fatalf("載入 data/ja：%v %v", err, errs)
	}
	o := compile.DefaultJAOptions()
	res := compile.BuildJA(d, o)
	if len(res.Errors) > 0 {
		t.Fatalf("編譯 data/ja：%v", res.Errors)
	}
	out := t.TempDir()
	if err := res.Write(out, filepath.Join("..", "..", "data", "sources", "licenses"), o); err != nil {
		t.Fatal(err)
	}
	if err := compile.WriteJARuntime(out, out); err != nil {
		t.Fatal(err)
	}

	w, err := newJaLanguage(out)
	if err != nil {
		t.Fatal(err)
	}
	if !w.Available() {
		t.Fatal("日文 profile 未載入")
	}
	s := newJaLanguageTestSession(t, w)
	if commit := jaLanguageCommit(t, s, "kyouha"); commit.Commit != "今日は" {
		t.Fatalf("統計轉換 = %q，want 今日は", commit.Commit)
	}
}
