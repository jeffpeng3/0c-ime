package main

import (
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
)

func TestIsCheckedInData(t *testing.T) {
	for _, out := range []string{"data", "data/ja", filepath.Join("data", "ja", "lexicon"), "./data/ja"} {
		if !isCheckedInData(out) {
			t.Errorf("%q 應視為 checked-in data", out)
		}
	}
	for _, out := range []string{"build/ja-jadic", "../data", "/tmp/ja", "database"} {
		if isCheckedInData(out) {
			t.Errorf("%q 不應視為 checked-in data", out)
		}
	}
}

func TestInputSequence(t *testing.T) {
	got, err := inputSequence("kyouha", true)
	if err != nil {
		t.Fatal(err)
	}
	if key := reading.Key(got); key != "き ょ う は" {
		t.Fatalf("羅馬字 kyouha = %q", key)
	}
	got, err = inputSequence("あした", false)
	if err != nil {
		t.Fatal(err)
	}
	if key := reading.Key(got); key != "あ し た" {
		t.Fatalf("かな あした = %q", key)
	}
	if _, err := inputSequence("ashita!", false); err == nil {
		t.Fatal("非かな輸入應報錯")
	}
	if _, err := inputSequence("kyouh", true); err == nil {
		t.Fatal("未完成的羅馬字應報錯")
	}
}
