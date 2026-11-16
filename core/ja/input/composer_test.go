package input_test

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/ja/input"
	"github.com/0c-biggo/0c-ime/core/reading"
)

const romajiCasesPath = "../../../testdata/ja/input/romaji.tsv"

type romajiCase struct {
	keys    string
	units   reading.Sequence
	pending string
}

func loadRomajiCases(t *testing.T) []romajiCase {
	t.Helper()
	f, err := os.Open(romajiCasesPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var cases []romajiCase
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("%s:%d: got %d fields, want 3", romajiCasesPath, lineNo, len(fields))
		}
		tc := romajiCase{keys: fields[0], pending: fields[2]}
		if tc.pending == "-" {
			tc.pending = ""
		}
		if fields[1] != "-" {
			for _, chunk := range strings.Split(fields[1], " ") {
				for _, r := range chunk {
					tc.units = append(tc.units, reading.Unit(string(r)))
				}
			}
		}
		cases = append(cases, tc)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestRomajiComposerTable(t *testing.T) {
	cases := loadRomajiCases(t)
	if len(cases) < 200 {
		t.Fatalf("got %d cases, want at least 200", len(cases))
	}

	for i, tc := range cases {
		t.Run(strings.ReplaceAll(tc.keys, "'", "_"), func(t *testing.T) {
			c := input.NewRomajiComposer()
			var completed reading.Sequence
			for _, r := range tc.keys {
				key := string(r)
				result, ok := c.Insert(key)
				if !ok {
					t.Fatalf("case %d: Insert(%q) rejected after %q", i, key, completed)
				}
				completed = append(completed, result.Completed...)
				if result.Pending != c.Pending() {
					t.Fatalf("case %d: result pending %q, composer pending %q", i, result.Pending, c.Pending())
				}
			}

			if got, want := reading.Key(c.Units()), reading.Key(tc.units); got != want {
				t.Fatalf("case %d %q: units %q, want %q", i, tc.keys, got, want)
			}
			if got := c.Pending(); got != tc.pending {
				t.Fatalf("case %d %q: pending %q, want %q", i, tc.keys, got, tc.pending)
			}
			if got, want := reading.Key(completed), reading.Key(c.Units()); got != want {
				t.Fatalf("case %d %q: Completed %q does not add up to Units %q", i, tc.keys, got, want)
			}

			var wantDisplay strings.Builder
			for _, unit := range tc.units {
				wantDisplay.WriteString(string(unit))
			}
			wantDisplay.WriteString(tc.pending)
			if got := c.DisplayText(); got != wantDisplay.String() {
				t.Fatalf("case %d %q: display %q, want %q", i, tc.keys, got, wantDisplay.String())
			}
			if got, want := c.Cursor(), utf8.RuneCountInString(wantDisplay.String()); got != want {
				t.Fatalf("case %d %q: cursor %d, want %d", i, tc.keys, got, want)
			}

			segments := c.Segments()
			var display strings.Builder
			for _, segment := range segments {
				display.WriteString(segment.Text)
				if segment.Pending != (segment.Text == tc.pending && tc.pending != "") {
					t.Fatalf("case %d %q: segment %q has wrong Pending flag", i, tc.keys, segment.Text)
				}
				if !segment.Pending && reading.Key(segment.Reading) != reading.Key(tc.units) {
					t.Fatalf("case %d %q: segment reading %q, want %q", i, tc.keys, reading.Key(segment.Reading), reading.Key(tc.units))
				}
			}
			if got := display.String(); got != wantDisplay.String() {
				t.Fatalf("case %d %q: segments display %q, want %q", i, tc.keys, got, wantDisplay.String())
			}
		})
	}
}

func TestBackspace(t *testing.T) {
	c := input.NewRomajiComposer()
	if c.Backspace() {
		t.Fatal("Backspace on empty composer returned true")
	}

	if _, ok := c.Insert("k"); !ok {
		t.Fatal("Insert(k) rejected")
	}
	if !c.Backspace() {
		t.Fatal("Backspace did not remove pending key")
	}
	if c.Pending() != "" || len(c.Units()) != 0 {
		t.Fatalf("after backspace pending=%q units=%q", c.Pending(), reading.Key(c.Units()))
	}

	for _, key := range []string{"k", "a", "k"} {
		if _, ok := c.Insert(key); !ok {
			t.Fatalf("Insert(%q) rejected", key)
		}
	}
	if got := reading.Key(c.Units()); got != "か" {
		t.Fatalf("units %q, want か", got)
	}
	if c.Pending() != "k" {
		t.Fatalf("pending %q, want k", c.Pending())
	}
	if !c.Backspace() || c.Pending() != "" || reading.Key(c.Units()) != "か" {
		t.Fatal("first backspace did not remove pending k")
	}
	if !c.Backspace() || reading.Key(c.Units()) != "" {
		t.Fatal("second backspace did not remove completed kana")
	}
	if c.Backspace() {
		t.Fatal("Backspace on empty composer returned true")
	}

	// nn 會先完成「ん」，Backspace 要能刪掉這個かな。
	c = input.NewRomajiComposer()
	for _, key := range []string{"n", "n"} {
		if _, ok := c.Insert(key); !ok {
			t.Fatalf("Insert(%q) rejected", key)
		}
	}
	if got := reading.Key(c.Units()); got != "ん" {
		t.Fatalf("nn produced units %q, want ん", got)
	}
	if !c.Backspace() || len(c.Units()) != 0 || c.Pending() != "" {
		t.Fatal("Backspace did not remove ん produced by nn")
	}
}

func TestFlush(t *testing.T) {
	tests := []struct {
		keys    string
		flushed reading.Sequence
		units   reading.Sequence
		pending string
		ok      bool
	}{
		{keys: "n", flushed: reading.Sequence{"ん"}, units: reading.Sequence{"ん"}, ok: true},
		{keys: "nn", units: reading.Sequence{"ん"}, ok: false},
		{keys: "nnn", flushed: reading.Sequence{"ん"}, units: reading.Sequence{"ん", "ん"}, ok: true},
		{keys: "k", ok: false, pending: "k"},
		{keys: "", ok: false},
	}
	for _, tc := range tests {
		c := input.NewRomajiComposer()
		for _, r := range tc.keys {
			if _, ok := c.Insert(string(r)); !ok {
				t.Fatalf("%q: Insert(%q) rejected", tc.keys, string(r))
			}
		}
		got, ok := c.Flush()
		if ok != tc.ok {
			t.Fatalf("%q: Flush ok=%v, want %v", tc.keys, ok, tc.ok)
		}
		if gotKey, wantKey := reading.Key(got), reading.Key(tc.flushed); gotKey != wantKey {
			t.Fatalf("%q: Flush returned %q, want %q", tc.keys, gotKey, wantKey)
		}
		if gotKey, wantKey := reading.Key(c.Units()), reading.Key(tc.units); gotKey != wantKey {
			t.Fatalf("%q: units %q, want %q", tc.keys, gotKey, wantKey)
		}
		if c.Pending() != tc.pending {
			t.Fatalf("%q: pending %q, want %q", tc.keys, c.Pending(), tc.pending)
		}
	}
}

func TestCancel(t *testing.T) {
	c := input.NewRomajiComposer()
	if c.Cancel() {
		t.Fatal("Cancel on empty composer returned true")
	}
	for _, key := range []string{"k", "a", "n"} {
		if _, ok := c.Insert(key); !ok {
			t.Fatalf("Insert(%q) rejected", key)
		}
	}
	if !c.Cancel() {
		t.Fatal("Cancel did not clear composition")
	}
	if c.Pending() != "" || len(c.Units()) != 0 || c.DisplayText() != "" || c.Cursor() != 0 {
		t.Fatalf("composition not empty after Cancel: pending=%q units=%q", c.Pending(), reading.Key(c.Units()))
	}
	if c.Cancel() {
		t.Fatal("second Cancel returned true")
	}
}

func TestInvalidCombinations(t *testing.T) {
	c := input.NewRomajiComposer()
	if _, ok := c.Insert("k"); !ok {
		t.Fatal("Insert(k) rejected")
	}
	if _, ok := c.Insert("z"); ok {
		t.Fatal("Insert(z) after k was accepted")
	}
	if c.Pending() != "k" || len(c.Units()) != 0 {
		t.Fatalf("rejected key changed state: pending=%q units=%q", c.Pending(), reading.Key(c.Units()))
	}
	if result, ok := c.Insert("a"); !ok || reading.Key(result.Completed) != "か" {
		t.Fatalf("recovery after rejected key failed: %v %q", ok, reading.Key(result.Completed))
	}

	for _, key := range []string{"@", " ", "ka", "あ"} {
		c := input.NewRomajiComposer()
		if _, ok := c.Insert(key); ok {
			t.Fatalf("Insert(%q) accepted in romaji mode", key)
		}
	}

	c = input.NewRomajiComposer()
	for _, key := range []string{"n", "@"} {
		_, _ = c.Insert(key)
	}
	if c.Pending() != "n" {
		t.Fatalf("n + invalid key changed pending to %q", c.Pending())
	}

	c = input.NewRomajiComposer()
	for _, key := range []string{"K", "A"} {
		if _, ok := c.Insert(key); !ok {
			t.Fatalf("uppercase Insert(%q) rejected", key)
		}
	}
	if got := reading.Key(c.Units()); got != "か" {
		t.Fatalf("uppercase input produced %q, want か", got)
	}
}

func TestKanaComposer(t *testing.T) {
	c := input.NewKanaComposer()
	if c.Mode() != input.ModeKana {
		t.Fatalf("mode %q, want kana", c.Mode())
	}

	tests := []struct {
		key  string
		want reading.Sequence
	}{
		{"き", reading.Sequence{"き"}},
		{"ゃ", reading.Sequence{"ゃ"}},
		{"カ", reading.Sequence{"か"}},
		{"ヴ", reading.Sequence{"ゔ"}},
		{"ー", reading.Sequence{"ー"}},
		{"きゃ", reading.Sequence{"き", "ゃ"}},
	}
	var want reading.Sequence
	for _, tc := range tests {
		result, ok := c.Insert(tc.key)
		if !ok {
			t.Fatalf("Insert(%q) rejected", tc.key)
		}
		if reading.Key(result.Completed) != reading.Key(tc.want) {
			t.Fatalf("Insert(%q) completed %q, want %q", tc.key, reading.Key(result.Completed), reading.Key(tc.want))
		}
		want = append(want, tc.want...)
		if c.Pending() != "" {
			t.Fatalf("kana mode pending %q, want empty", c.Pending())
		}
	}
	if reading.Key(c.Units()) != reading.Key(want) {
		t.Fatalf("units %q, want %q", reading.Key(c.Units()), reading.Key(want))
	}

	before := reading.Key(c.Units())
	for _, key := range []string{"abc", "", "漢", "ｱ"} {
		if _, ok := c.Insert(key); ok {
			t.Fatalf("kana Insert(%q) accepted", key)
		}
		if reading.Key(c.Units()) != before {
			t.Fatalf("rejected kana Insert(%q) changed units", key)
		}
	}

	if !c.Backspace() || reading.Key(c.Units()) == before {
		t.Fatal("kana Backspace did not remove last unit")
	}
}

func TestUnitsAreCopied(t *testing.T) {
	c := input.NewRomajiComposer()
	c.Insert("k")
	c.Insert("a")
	units := c.Units()
	units[0] = "x"
	if got := reading.Key(c.Units()); got != "か" {
		t.Fatalf("caller mutated composer units: %q", got)
	}
}

func TestCustomRomajiTable(t *testing.T) {
	table := input.NewTable(map[string]string{
		"zj": "ず",
		"ka": "か",
	})
	c := input.NewComposerWithTable(input.ModeRomaji, table)
	for _, key := range []string{"z", "j"} {
		if _, ok := c.Insert(key); !ok {
			t.Fatalf("custom Insert(%q) rejected", key)
		}
	}
	if got := reading.Key(c.Units()); got != "ず" {
		t.Fatalf("custom table produced %q, want ず", got)
	}
	if _, ok := c.Insert("a"); ok {
		t.Fatal("custom table accepted missing entry a")
	}
}

func TestSymbols(t *testing.T) {
	symbols := input.DefaultSymbols()
	tests := map[string]string{
		".": "。",
		",": "、",
		"?": "？",
		"!": "！",
		"[": "「",
		"]": "」",
		"/": "・",
	}
	for key, want := range tests {
		got, ok := symbols.Lookup(key)
		if !ok || got != want {
			t.Fatalf("Lookup(%q) = %q, %v; want %q, true", key, got, ok, want)
		}
		if got := symbols.Insert(key); got != want {
			t.Fatalf("Punctuator Insert(%q) = %q, want %q", key, got, want)
		}
	}
	if got := symbols.Insert("kana"); got != "kana" {
		t.Fatalf("unknown symbol changed to %q", got)
	}

	custom := input.NewSymbols(map[string]string{"x": "※"})
	if got, ok := custom.Lookup("x"); !ok || got != "※" {
		t.Fatalf("custom Lookup = %q, %v", got, ok)
	}
	if got := custom.Insert("."); got != "." {
		t.Fatalf("custom table leaked default mapping: %q", got)
	}
}
