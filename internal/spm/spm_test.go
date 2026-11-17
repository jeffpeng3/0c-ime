package spm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// testModel builds a small unigram model in memory so the encoder can be
// tested without the real 600 KiB tokenizer artifact.
func testModel(byteFallback bool) *Model {
	m := &Model{
		ModelType:              1,
		UnkID:                  0,
		BosID:                  1,
		EosID:                  2,
		PadID:                  3,
		ByteFallback:           byteFallback,
		AddDummyPrefix:         true,
		RemoveExtraWhitespaces: true,
		EscapeWhitespaces:      true,
	}
	add := func(text string, score float32, typ PieceType) {
		m.Pieces = append(m.Pieces, Piece{Text: text, Score: score, Type: typ})
	}
	add("<unk>", 0, PieceUnknown)
	add("<s>", 0, PieceControl)
	add("</s>", 0, PieceControl)
	add("<pad>", 0, PieceControl)
	add("\xe2\x96\x81", -1.0, PieceNormal)   // ▁
	add("\xe2\x96\x81a", -2.0, PieceNormal)  // ▁a
	add("a", -3.0, PieceNormal)              // a
	add("b", -4.0, PieceNormal)              // b
	add("\xe2\x96\x81ab", -5.0, PieceNormal) // ▁ab
	add("ab", -6.0, PieceNormal)             // ab
	if byteFallback {
		// UTF-8 bytes of "あ" (E3 81 82) and "、"(E3 80 81).
		for _, name := range []string{"<0xE3>", "<0x81>", "<0x82>", "<0x80>"} {
			add(name, 0, PieceByte)
		}
	}
	return m
}

func mustEncoder(t *testing.T, m *Model) *Encoder {
	t.Helper()
	e, err := NewEncoder(m)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	return e
}

func TestNormalize(t *testing.T) {
	e := mustEncoder(t, testModel(false))
	cases := map[string]string{
		"":        "",
		"   ":     "",
		"a":       "▁a",
		"  a  b ": "▁a▁b",
		"a b":     "▁a▁b",
		"a  b  c": "▁a▁b▁c",
		"\t a":    "▁\t▁a",
	}
	for in, want := range cases {
		if got := e.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeViterbi(t *testing.T) {
	e := mustEncoder(t, testModel(false))
	cases := []struct {
		in   string
		want []int32
	}{
		{"", nil},
		{"a", []int32{5}},         // ▁a beats ▁+a
		{"ab", []int32{8}},        // ▁ab beats ▁a+b
		{"a b", []int32{5, 4, 7}}, // ▁a ▁ b (no ▁b piece in this model)
	}
	for _, c := range cases {
		if got := e.Encode(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Encode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEncodeUnknown(t *testing.T) {
	// Without byte fallback an unknown rune is a single unk piece.
	e := mustEncoder(t, testModel(false))
	if got := e.Encode("あ"); !reflect.DeepEqual(got, []int32{4, 0}) {
		t.Fatalf("no byte fallback: got %v, want [4 0]", got)
	}

	// With byte fallback the unknown rune expands to its UTF-8 byte pieces.
	e = mustEncoder(t, testModel(true))
	ids := make(map[string]int32)
	for i, p := range e.Model.Pieces {
		ids[p.Text] = int32(i)
	}
	want := []int32{4, ids["<0xE3>"], ids["<0x81>"], ids["<0x82>"]}
	if got := e.Encode("あ"); !reflect.DeepEqual(got, want) {
		t.Fatalf("byte fallback: got %v, want %v", got, want)
	}
}

type golden struct {
	SPMModel  string    `json:"spm_model"`
	SPMSHA256 string    `json:"spm_sha256"`
	Texts     int       `json:"texts"`
	Tokens    [][]int32 `json:"tokens"`
}

// TestGolden200 compares the pure-Go encoder with ids produced by Python
// sentencepiece. The 200 input strings are corpus text and stay outside the
// repository, so the test is skipped unless SPM_MODEL and SPM_TEXTS are set.
func TestGolden200(t *testing.T) {
	modelPath := os.Getenv("SPM_MODEL")
	textsPath := os.Getenv("SPM_TEXTS")
	if modelPath == "" || textsPath == "" {
		t.Skip("set SPM_MODEL and SPM_TEXTS to run the 200-string oracle comparison")
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "tokens-200.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want golden
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	m, err := ParseModel(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncoder(m)
	if err != nil {
		t.Fatal(err)
	}
	texts, err := os.ReadFile(textsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(string(texts))
	if len(lines) < want.Texts {
		t.Fatalf("%s has %d lines, golden needs %d", textsPath, len(lines), want.Texts)
	}
	mismatches := 0
	for i := 0; i < want.Texts; i++ {
		got := enc.Encode(lines[i])
		if len(got) == 0 && len(want.Tokens[i]) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want.Tokens[i]) {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("line %d: got %v, want %v", i+1, got, want.Tokens[i])
			}
		}
	}
	if mismatches > 0 {
		t.Fatalf("%d of %d lines differ from the Python oracle", mismatches, want.Texts)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
