package rewrite_test

import (
	"os"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/rewrite"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

func units(s string) reading.Sequence {
	var seq reading.Sequence
	for _, r := range s {
		seq = append(seq, reading.Unit(string(r)))
	}
	return seq
}

func annotationsBySurface(cands []lang.Candidate) map[string]string {
	out := make(map[string]string, len(cands))
	for _, c := range cands {
		out[c.Surface] = c.Annotation
	}
	return out
}

func surfaces(cands []lang.Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Surface
	}
	return out
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestLoadSymbolsFS(t *testing.T) {
	symbols, err := rewrite.LoadSymbolsFS(os.DirFS("../../../data/ja"), "symbols")
	if err != nil {
		t.Fatal(err)
	}
	if symbols.Len() < 150 {
		t.Fatalf("got %d symbols, want at least 150", symbols.Len())
	}

	tests := []struct {
		reading    string
		surface    string
		annotation string
	}{
		{"きごう", "※", "米印"},
		{"やじるし", "→", "右矢印"},
		{"すうがく", "∞", "無限大"},
		{"ずけい", "★", "黒星"},
		{"つうか", "￥", "円"},
		{"たんい", "℃", "摂氏"},
		{"かおもじ", "(^^)", "笑顔の顔文字"},
		{"キゴウ", "※", "米印"},
	}
	for _, tc := range tests {
		found := false
		for _, entry := range symbols.Lookup(tc.reading) {
			if entry.Surface == tc.surface && entry.Annotation == tc.annotation {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Lookup(%q) missing %q (%q)", tc.reading, tc.surface, tc.annotation)
		}
	}

	kaomoji := symbols.Lookup("かおもじ")
	if len(kaomoji) < 40 {
		t.Fatalf("got %d kaomoji, want at least 40", len(kaomoji))
	}
	for _, entry := range kaomoji {
		if entry.Category != "顔文字" {
			t.Errorf("%q category = %q, want 顔文字", entry.Surface, entry.Category)
		}
		if entry.Annotation == "" {
			t.Errorf("%q has empty annotation", entry.Surface)
		}
	}
}

func TestLoadSymbolsTable(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []rewrite.Symbol
		wantErr string
	}{
		{
			name:  "valid",
			input: "# comment\n\nきごう\t※\t米印\t記号\nやじるし\t→\t右矢印\t記号\n",
			want: []rewrite.Symbol{
				{Reading: "きごう", Surface: "※", Annotation: "米印", Category: "記号"},
				{Reading: "やじるし", Surface: "→", Annotation: "右矢印", Category: "記号"},
			},
		},
		{
			name:    "too few fields",
			input:   "きごう\t※\t米印\n",
			wantErr: "want 4",
		},
		{
			name:    "empty annotation",
			input:   "きごう\t※\t\t記号\n",
			wantErr: "empty annotation",
		},
		{
			name:    "empty reading",
			input:   "\t※\t米印\t記号\n",
			wantErr: "empty reading or surface",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rewrite.LoadSymbols(strings.NewReader(tc.input))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got error %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d entries, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("entry %d = %#v, want %#v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestSymbolCandidates(t *testing.T) {
	symbols, err := rewrite.LoadSymbolsFS(os.DirFS("../../../data/ja"), "symbols")
	if err != nil {
		t.Fatal(err)
	}
	r := rewrite.NewWithSymbols(symbols)

	cands := r.Candidates("記号", units("きごう"))
	got := annotationsBySurface(cands)
	if got["※"] != "米印" {
		t.Errorf("※ annotation = %q, want 米印", got["※"])
	}
	for _, c := range cands {
		if c.Annotation == "" {
			t.Errorf("candidate %q has empty annotation", c.Surface)
		}
		if c.Source != rewrite.SourceRewrite && c.Source != lang.SourceSymbol {
			t.Errorf("candidate %q source = %q", c.Surface, c.Source)
		}
	}
	for _, c := range cands {
		if c.Surface == "※" && c.Source != lang.SourceSymbol {
			t.Errorf("symbol candidate %q source = %q, want %q", c.Surface, c.Source, lang.SourceSymbol)
		}
		if c.Surface == "きごう" && c.Source != rewrite.SourceRewrite {
			t.Errorf("kana candidate %q source = %q, want %q", c.Surface, c.Source, rewrite.SourceRewrite)
		}
	}

	kaomoji := r.Candidates("顔文字", units("かおもじ"))
	if got := annotationsBySurface(kaomoji); got["(^^)"] != "笑顔の顔文字" {
		t.Errorf("(^^) annotation = %q, want 笑顔の顔文字", got["(^^)"])
	}
}

func TestCandidatesSkippedWhenUnchanged(t *testing.T) {
	cands := rewrite.New().Candidates("きょう", units("きょう"))
	for surface := range annotationsBySurface(cands) {
		if surface == "きょう" {
			t.Errorf("unchanged hiragana candidate should be skipped: %#v", surfaces(cands))
		}
	}
}
