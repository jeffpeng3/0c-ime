package main

import (
	"path/filepath"
	"testing"
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
