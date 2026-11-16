package mixed_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/english"
	"github.com/0c-biggo/0c-ime/core/ja/mixed"
	"github.com/0c-biggo/0c-ime/core/lang"
)

func TestEnglishSuggestAndComplete(t *testing.T) {
	e := mixed.NewEnglish(english.Prototype())
	if !e.Has("hello") || e.Has("nope") {
		t.Fatalf("Has 判斷錯誤：hello=%v nope=%v", e.Has("hello"), e.Has("nope"))
	}
	suggestions := e.Suggestions("hel", 8)
	if len(suggestions) == 0 || suggestions[0].Text != "hello" {
		t.Fatalf("Suggestions(hel) = %#v，第一個應是 hello", suggestions)
	}
	if word, ok := e.Complete("hel"); !ok || word != "hello" {
		t.Fatalf("Complete(hel) = %q, %v", word, ok)
	}
	if _, ok := e.Complete("hello"); ok {
		t.Fatal("完整單字不應再補完")
	}
	if suggestions := e.Suggestions("HEL", 8); len(suggestions) == 0 || suggestions[0].Text != "HELLO" {
		t.Fatalf("全大寫補完 = %#v，應保留全大寫", suggestions)
	}
}

func TestEnglishLearnAndForget(t *testing.T) {
	e := mixed.NewEnglish(nil)
	if e.Learned("kubectl") {
		t.Fatal("還沒學就回報學過")
	}
	if !e.Learn("kubectl") {
		t.Fatal("Learn(kubectl) = false")
	}
	e.Learn("kubectl")
	e.Learn("kubeadm")
	if !e.Learned("KUBECTL") || !e.Has("Kubectl") {
		t.Fatal("學習詞應不分大小寫")
	}
	suggestions := e.Suggestions("kube", 8)
	if len(suggestions) < 2 || !suggestions[0].Learned || suggestions[0].Text != "kubectl" {
		t.Fatalf("學習詞應優先：%#v", suggestions)
	}
	if words := e.LearnedWords(); len(words) != 2 || words[0] != "kubectl" {
		t.Fatalf("LearnedWords = %#v", words)
	}
	if !e.Forget("KUBECTL") || e.Learned("kubectl") {
		t.Fatal("Forget(kubectl) 沒有移除")
	}
	if e.Forget("kubectl") {
		t.Fatal("重複 Forget 應回傳 false")
	}
	for _, bad := range []string{"", "   ", "has space", "line\nbreak"} {
		if e.Learn(bad) {
			t.Errorf("Learn(%q) 應被拒", bad)
		}
	}
}

func TestEnglishCandidates(t *testing.T) {
	e := mixed.NewEnglish(nil)
	e.Learn("kubectl")
	cands := e.Candidates("kub", 8)
	if len(cands) == 0 || cands[0].Surface != "kubectl" {
		t.Fatalf("Candidates(kub) = %#v", cands)
	}
	if cands[0].Source != mixed.SourceEnglish || cands[0].Annotation != mixed.AnnotationLearned {
		t.Fatalf("學習詞候選標記錯誤：%#v", cands[0])
	}
	all := true
	for _, c := range cands {
		if c.Source != mixed.SourceEnglish {
			all = false
		}
	}
	if !all {
		t.Fatalf("英文候選的 Source 應一致：%#v", cands)
	}
}

func TestEnglishCandidateTypes(t *testing.T) {
	e := mixed.NewEnglish(nil)
	var _ lang.Candidate = e.Candidates("hel", 4)[0]
}
