//go:build cgo && onnxruntime

package native

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/punct"
)

type punctOracleWindow struct {
	Line      int     `json:"line"`
	Start     int     `json:"start"`
	IDs       []int64 `json:"ids"`
	Decisions []int   `json:"decisions"`
}

type punctOracleText struct {
	Line      int          `json:"line"`
	IDs       []int64      `json:"ids"`
	Decisions []int        `json:"decisions"`
	Edits     []punct.Edit `json:"edits"`
	Text      string       `json:"text"`
}

type punctOracleFile struct {
	Schema          string              `json:"schema"`
	ModelSHA256     string              `json:"model_sha256"`
	TokenizerSHA256 string              `json:"tokenizer_sha256"`
	Labels          []string            `json:"labels"`
	Window          int                 `json:"window"`
	Stride          int                 `json:"stride"`
	Windows         []punctOracleWindow `json:"windows"`
	Texts           []punctOracleText   `json:"texts"`
}

type punctParityReport struct {
	Windows            int     `json:"windows"`
	WindowIDMismatch   int     `json:"window_id_mismatch"`
	WindowArgmaxDiff   int     `json:"window_argmax_mismatch"`
	Texts              int     `json:"texts"`
	TextIDMismatch     int     `json:"text_id_mismatch"`
	MergedDecisionDiff int     `json:"text_merged_decision_mismatch"`
	EditMismatch       int     `json:"text_edit_mismatch"`
	TextMismatch       int     `json:"text_insertion_mismatch"`
	NoEditChanged      int     `json:"no_edit_changed_characters"`
	ModelSHA256        string  `json:"model_sha256"`
	MedianMS           float64 `json:"median_ms_per_window"`
	P95MS              float64 `json:"p95_ms_per_window"`
}

// TestPunctuationParity200 compares the native scorer with a Python
// SentencePiece + ONNX Runtime oracle on the real exported int8 model.
//
// Required environment:
//
//	NEURAL_PUNCT_PARITY_PYTHON     python with numpy/onnxruntime/sentencepiece
//	NEURAL_PUNCT_PARITY_MANIFEST   punctuation-public-core/manifest.json
//	NEURAL_PUNCT_PARITY_TOKENIZER  public_core_ja.model
//	NEURAL_PUNCT_PARITY_TEXTS      sample-ja.txt (at least 200 lines)
//	NEURAL_PUNCT_PARITY_LIB        libonnxruntime.so.1.32.0
//	NEURAL_PUNCT_PARITY_OUT        optional report JSON path
func TestPunctuationParity200(t *testing.T) {
	python := os.Getenv("NEURAL_PUNCT_PARITY_PYTHON")
	manifest := os.Getenv("NEURAL_PUNCT_PARITY_MANIFEST")
	tokenizer := os.Getenv("NEURAL_PUNCT_PARITY_TOKENIZER")
	textsPath := os.Getenv("NEURAL_PUNCT_PARITY_TEXTS")
	libPath := os.Getenv("NEURAL_PUNCT_PARITY_LIB")
	if python == "" || manifest == "" || tokenizer == "" || textsPath == "" || libPath == "" {
		t.Skip("set NEURAL_PUNCT_PARITY_* to run the real-model punctuation parity test")
	}
	lines, err := punctReadLines(textsPath)
	if err != nil {
		t.Fatal(err)
	}
	oraclePath := filepath.Join(t.TempDir(), "punct-oracle.json")
	script := filepath.Join("testdata", "punct_oracle.py")
	cmd := exec.Command(python, script, "--manifest", manifest, "--tokenizer", tokenizer,
		"--texts", textsPath, "--limit", "240", "--window", "256", "--stride", "128",
		"--threads", "8", "--out", oraclePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("python oracle: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(oraclePath)
	if err != nil {
		t.Fatal(err)
	}
	var oracle punctOracleFile
	if err := json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Windows) < 200 {
		t.Fatalf("oracle produced %d windows, want at least 200", len(oracle.Windows))
	}

	backend, err := OpenPunct(Config{Package: filepath.Dir(manifest), Tokenizer: tokenizer,
		Threads: 8, LibraryPath: libPath})
	if err != nil {
		t.Fatalf("OpenPunct: %v", err)
	}
	defer backend.Close()
	if backend.Window() != oracle.Window || backend.Stride() != oracle.Stride {
		t.Fatalf("window/stride %d/%d, oracle %d/%d", backend.Window(), backend.Stride(), oracle.Window, oracle.Stride)
	}
	report := punctParityReport{Windows: len(oracle.Windows), Texts: len(oracle.Texts), ModelSHA256: oracle.ModelSHA256}

	for _, w := range oracle.Windows {
		if w.Line >= len(lines) {
			t.Fatalf("oracle line %d out of range", w.Line)
		}
		tokens, _ := backend.encoder.EncodeTokens(lines[w.Line])
		if w.Start < 0 || w.Start > len(tokens) {
			t.Fatalf("bad window start %d for %d tokens", w.Start, len(tokens))
		}
		end := min(w.Start+len(w.IDs), len(tokens))
		if end-w.Start != len(w.IDs) {
			t.Fatalf("window id count %d, got %d", len(w.IDs), end-w.Start)
		}
		ids := make([]int64, backend.Window())
		mask := make([]int64, backend.Window())
		for i := w.Start; i < end; i++ {
			ids[i-w.Start] = int64(tokens[i].ID)
			mask[i-w.Start] = 1
		}
		if got := ids[:len(w.IDs)]; !reflect.DeepEqual(got, w.IDs) {
			report.WindowIDMismatch++
			continue
		}
		rows, err := backend.TokenLogits(ids, mask)
		if err != nil {
			t.Fatalf("TokenLogits: %v", err)
		}
		for i := range w.Decisions {
			label, _ := decision(rows[i], backend.spec.marks)
			if label != w.Decisions[i] {
				report.WindowArgmaxDiff++
				break
			}
		}
	}

	for _, tc := range oracle.Texts {
		if tc.Line >= len(lines) {
			t.Fatalf("oracle line %d out of range", tc.Line)
		}
		text := lines[tc.Line]
		tokens, _ := backend.encoder.EncodeTokens(text)
		gotIDs := make([]int64, len(tokens))
		for i := range tokens {
			gotIDs[i] = int64(tokens[i].ID)
		}
		if !reflect.DeepEqual(gotIDs, tc.IDs) {
			report.TextIDMismatch++
			continue
		}
		// Recompute merged decisions through the same ORT session.
		starts := windowStarts(len(tokens), backend.Window(), backend.Stride())
		rows := make([][][]float32, 0, len(starts))
		for _, start := range starts {
			ids := make([]int64, backend.Window())
			mask := make([]int64, backend.Window())
			for i := start; i < min(start+backend.Window(), len(tokens)); i++ {
				ids[i-start] = int64(tokens[i].ID)
				mask[i-start] = 1
			}
			windowRows, err := backend.TokenLogits(ids, mask)
			if err != nil {
				t.Fatalf("TokenLogits: %v", err)
			}
			rows = append(rows, windowRows)
		}
		merged := mergeWindowRows(len(tokens), backend.Window(), starts, rows)
		decisions := make([]int, len(tokens))
		for i := range decisions {
			decisions[i], _ = decision(merged[i], backend.spec.marks)
		}
		if !reflect.DeepEqual(decisions, tc.Decisions) {
			report.MergedDecisionDiff++
		}
		gotEdits, err := backend.Punctuate(text)
		if err != nil {
			t.Fatalf("Punctuate: %v", err)
		}
		if len(gotEdits) != len(tc.Edits) || (len(gotEdits) > 0 && !reflect.DeepEqual(gotEdits, tc.Edits)) {
			report.EditMismatch++
			t.Logf("edit mismatch line %d: go=%v oracle=%v", tc.Line, gotEdits, tc.Edits)
		}
		gotOut, err := backend.PunctuateText(text)
		if err != nil {
			t.Fatalf("PunctuateText: %v", err)
		}
		if gotOut != tc.Text {
			report.TextMismatch++
			t.Logf("text mismatch line %d: go=%q oracle=%q edits=%v", tc.Line, gotOut, tc.Text, tc.Edits)
		}
		if stripped := stripInserted(gotOut, tc.Edits); stripped != text {
			report.NoEditChanged += changedRunes(text, stripped)
		}
	}

	// Latency of the int8 path at the package window (256 tokens).
	sample := oracle.Windows[min(len(oracle.Windows), 200)-1]
	ids := make([]int64, backend.Window())
	mask := make([]int64, backend.Window())
	if sample.Line < len(lines) {
		tokens, _ := backend.encoder.EncodeTokens(lines[sample.Line])
		for i := 0; i < len(tokens) && i < len(ids); i++ {
			ids[i] = int64(tokens[i].ID)
			mask[i] = 1
		}
	}
	for i := 0; i < 10; i++ {
		if _, err := backend.TokenLogits(ids, mask); err != nil {
			t.Fatal(err)
		}
	}
	// Three passes; report the least-contended pass, like the model manifest.
	report.MedianMS = 1e9
	for pass := 0; pass < 3; pass++ {
		timings := make([]float64, 0, 50)
		for i := 0; i < 50; i++ {
			started := time.Now()
			if _, err := backend.TokenLogits(ids, mask); err != nil {
				t.Fatal(err)
			}
			timings = append(timings, float64(time.Since(started).Microseconds())/1000)
		}
		sort.Float64s(timings)
		median := timings[len(timings)/2]
		if median < report.MedianMS {
			report.MedianMS = median
			report.P95MS = timings[min(len(timings)-1, int(0.95*float64(len(timings))))]
		}
	}

	t.Logf("punctuation parity: windows=%d window_id_mismatch=%d window_argmax_mismatch=%d texts=%d text_id_mismatch=%d merged_decision_mismatch=%d edit_mismatch=%d text_mismatch=%d no_edit_changed=%d model=%s median=%.3fms p95=%.3fms",
		report.Windows, report.WindowIDMismatch, report.WindowArgmaxDiff, report.Texts, report.TextIDMismatch,
		report.MergedDecisionDiff, report.EditMismatch, report.TextMismatch, report.NoEditChanged,
		report.ModelSHA256, report.MedianMS, report.P95MS)
	if out := os.Getenv("NEURAL_PUNCT_PARITY_OUT"); out != "" {
		b, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
			t.Errorf("write report: %v", err)
		}
	}
	if report.WindowIDMismatch != 0 || report.WindowArgmaxDiff != 0 || report.TextIDMismatch != 0 ||
		report.MergedDecisionDiff != 0 || report.EditMismatch != 0 || report.TextMismatch != 0 ||
		report.NoEditChanged != 0 {
		t.Fatalf("parity mismatch: %+v", report)
	}
}

// stripInserted removes exactly the mark runes the reference inserted, leaving
// every original rune in place. It is the no-edit check for inserted texts.
func stripInserted(text string, edits []punct.Edit) string {
	remove := map[int]bool{}
	for i, e := range edits {
		remove[e.Offset+i] = true
	}
	runes := []rune(text)
	var out []rune
	for i, r := range runes {
		if !remove[i] {
			out = append(out, r)
		}
	}
	return string(out)
}

func punctReadLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		out = append(out, strings.TrimSuffix(sc.Text(), "\r"))
	}
	return out, sc.Err()
}

func changedRunes(a, b string) int {
	ar, br := []rune(a), []rune(b)
	n := max(len(ar), len(br))
	changed := 0
	for i := 0; i < n; i++ {
		if i >= len(ar) || i >= len(br) || ar[i] != br[i] {
			changed++
		}
	}
	return max(changed, 1)
}
