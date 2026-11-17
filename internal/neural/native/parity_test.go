//go:build cgo && onnxruntime

package native_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/internal/neural/native"
	"github.com/0c-biggo/0c-ime/internal/neural/native/input"
	"github.com/0c-biggo/0c-ime/internal/spm"
)

type oracleCaseRequest struct {
	Context    string   `json:"context"`
	Reading    []string `json:"reading"`
	Candidates []struct {
		Text     string  `json:"text"`
		LogScore float64 `json:"log_score"`
		Segments []struct {
			Start      int    `json:"Start"`
			End        int    `json:"End"`
			Word       string `json:"Word"`
			Partial    bool   `json:"Partial"`
			Correction bool   `json:"Correction"`
		} `json:"segments"`
	} `json:"candidates"`
	AllowPartial      bool   `json:"allow_partial"`
	TextOnly          bool   `json:"text_only"`
	NeutralLast       bool   `json:"neutral_last"`
	ToneKnown         []bool `json:"tone_known"`
	ToneKnownSet      bool   `json:"tone_known_set"`
	AllowUnknownTones bool   `json:"allow_unknown_tones"`
}

type oracleArrays struct {
	IDs        []int64   `json:"ids"`
	Bases      []int64   `json:"bases"`
	Tones      []int64   `json:"tones"`
	Types      []int64   `json:"types"`
	Positions  []int64   `json:"positions"`
	Mask       [][]bool  `json:"mask"`
	ProjPos    []int64   `json:"proj_pos"`
	TargetNode [][]int64 `json:"target_node"`
	TargetIDs  [][]int64 `json:"target_ids"`
}

type oracleCase struct {
	LineIndex int               `json:"line_index"`
	Request   oracleCaseRequest `json:"request"`
	Arrays    oracleArrays      `json:"arrays"`
	FlatIDs   []int64           `json:"flat_ids"`
	Score     float64           `json:"score"`
	SPMIDs    []int32           `json:"spm_ids"`
}

type oracleFile struct {
	Bases           []string     `json:"bases"`
	Characters      []string     `json:"characters"`
	Texts           int          `json:"texts"`
	Cases           int          `json:"cases"`
	Seq             int          `json:"seq"`
	VocabCap        int          `json:"vocab_cap"`
	PythonMSPerCase float64      `json:"python_latency_ms_per_case"`
	PythonMSTotal   float64      `json:"python_latency_ms_total"`
	CasesData       []oracleCase `json:"cases_data"`
}

type parityReport struct {
	Texts           int     `json:"texts"`
	Cases           int     `json:"cases"`
	Seq             int     `json:"seq"`
	VocabCap        int     `json:"vocab_cap"`
	TokenMismatches int     `json:"token_mismatches"`
	SPMMismatches   int     `json:"spm_mismatches"`
	ArrayMismatches int     `json:"array_mismatches"`
	MaxAbsDiff      float64 `json:"max_abs_score_diff"`
	MeanAbsDiff     float64 `json:"mean_abs_score_diff"`
	GoMSPerCase     float64 `json:"go_latency_ms_per_case"`
	PythonMSPerCase float64 `json:"python_latency_ms_per_case"`
	GoMSTotal       float64 `json:"go_latency_ms_total"`
	PythonMSTotal   float64 `json:"python_latency_ms_total"`
}

// TestParity200 runs the Python sequence worker's prepare() and the native
// backend on the same int8 placeholder model for real Japanese lines.
//
// Required environment:
//
//	NEURAL_PARITY_PYTHON           python with numpy/onnxruntime/sentencepiece
//	NEURAL_PARITY_SPM              public_core_ja.model
//	NEURAL_PARITY_TEXTS            sample-ja.txt (at least 200 lines)
//	NEURAL_PARITY_ONNX             reranker-public-core...int8.onnx
//	NEURAL_PARITY_LIB              libonnxruntime.so.1.32.0
//	NEURAL_PARITY_OUT              optional report JSON path
func TestParity200(t *testing.T) {
	python := os.Getenv("NEURAL_PARITY_PYTHON")
	spmPath := os.Getenv("NEURAL_PARITY_SPM")
	textsPath := os.Getenv("NEURAL_PARITY_TEXTS")
	onnxPath := os.Getenv("NEURAL_PARITY_ONNX")
	libPath := os.Getenv("NEURAL_PARITY_LIB")
	if python == "" || spmPath == "" || textsPath == "" || onnxPath == "" || libPath == "" {
		t.Skip("set NEURAL_PARITY_PYTHON/SPM/TEXTS/ONNX/LIB to run the 200-line parity test")
	}
	worker := os.Getenv("NEURAL_PARITY_SEQUENCE_RUNTIME")
	if worker == "" {
		worker = filepath.Join("..", "..", "..", "platforms", "neural", "runtime", "sequence_runtime.py")
	}
	oraclePath := filepath.Join(t.TempDir(), "oracle.json")
	script := filepath.Join("testdata", "parity_oracle.py")
	cmd := exec.Command(python, script,
		"--sequence-runtime", worker,
		"--spm", spmPath,
		"--onnx", onnxPath,
		"--texts", textsPath,
		"--limit", "200",
		"--seq", "32",
		"--threads", "2",
		"--vocab-cap", "14080",
		"--out", oraclePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("python oracle: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(oraclePath)
	if err != nil {
		t.Fatal(err)
	}
	var oracle oracleFile
	if err := json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	if oracle.Cases < 200 {
		t.Fatalf("oracle produced %d cases, want at least 200", oracle.Cases)
	}

	maps := input.Maps{Bases: map[string]int64{}, Characters: map[string]int64{}}
	for i, b := range oracle.Bases {
		maps.Bases[b] = int64(i + 2)
	}
	for i, c := range oracle.Characters {
		maps.Characters[c] = int64(i + 8)
	}

	// The native backend needs a sequence-shaped package contract; the model
	// itself is the same placeholder reranker the Python side uses.
	pkg := t.TempDir()
	dict := strings.Repeat("a", 64)
	if err := os.Symlink(onnxPath, filepath.Join(pkg, "model.onnx")); err != nil {
		if err := copyFile(onnxPath, filepath.Join(pkg, "model.onnx")); err != nil {
			t.Fatal(err)
		}
	}
	vocab := struct {
		DictionarySHA256 string   `json:"dictionary_sha256"`
		Bases            []string `json:"bases"`
		Characters       []string `json:"characters"`
	}{dict, oracle.Bases, oracle.Characters}
	vraw, _ := json.Marshal(vocab)
	if err := os.WriteFile(filepath.Join(pkg, "vocab.json"), vraw, 0o644); err != nil {
		t.Fatal(err)
	}
	modelSHA, err := fileSHA(filepath.Join(pkg, "model.onnx"))
	if err != nil {
		t.Fatal(err)
	}
	vocabSHA, err := fileSHA(filepath.Join(pkg, "vocab.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema": "zuin-sequence-runtime-v1", "context_policy": "confirmed_left_64",
		"dictionary_sha256": dict, "weight": 1.0, "margin": 0.0,
		"files":       map[string]string{"model.onnx": modelSHA, "vocab.json": vocabSHA},
		"placeholder": true, "quality": "untrained",
	}
	mraw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(pkg, "manifest.json"), mraw, 0o644); err != nil {
		t.Fatal(err)
	}

	backend, err := native.Open(native.Config{Package: pkg, DictionarySHA256: dict, Threads: 2, LibraryPath: libPath})
	if err != nil {
		t.Fatalf("native.Open: %v", err)
	}
	defer backend.Close()

	enc, err := loadSPM(spmPath)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := readLines(textsPath)
	if err != nil {
		t.Fatal(err)
	}

	report := parityReport{Texts: oracle.Texts, Cases: oracle.Cases, Seq: oracle.Seq,
		VocabCap: oracle.VocabCap, PythonMSPerCase: oracle.PythonMSPerCase, PythonMSTotal: oracle.PythonMSTotal}
	started := time.Now()
	var sumDiff float64
	for _, tc := range oracle.CasesData {
		req := input.Request{Context: tc.Request.Context, Reading: tc.Request.Reading,
			AllowPartial: tc.Request.AllowPartial, TextOnly: tc.Request.TextOnly,
			NeutralLast: tc.Request.NeutralLast, ToneKnown: tc.Request.ToneKnown,
			ToneKnownSet: tc.Request.ToneKnownSet, AllowUnknownTones: tc.Request.AllowUnknownTones}
		for _, c := range tc.Request.Candidates {
			candidate := input.Candidate{Text: c.Text, LogScore: c.LogScore}
			for _, s := range c.Segments {
				candidate.Segments = append(candidate.Segments, input.Segment{Start: s.Start, End: s.End,
					Word: s.Word, Partial: s.Partial, Correction: s.Correction})
			}
			req.Candidates = append(req.Candidates, candidate)
		}
		got, err := input.Prepare(req, maps)
		if err != nil {
			t.Fatalf("case %d prepare: %v", tc.LineIndex, err)
		}
		if !reflect.DeepEqual(got.IDs, tc.Arrays.IDs) {
			report.TokenMismatches++
			report.ArrayMismatches++
		}
		if !reflect.DeepEqual(got.Bases, tc.Arrays.Bases) || !reflect.DeepEqual(got.Tones, tc.Arrays.Tones) ||
			!reflect.DeepEqual(got.Types, tc.Arrays.Types) || !reflect.DeepEqual(got.Positions, tc.Arrays.Positions) ||
			!reflect.DeepEqual(got.Mask, tc.Arrays.Mask) || !reflect.DeepEqual(got.ProjPos, tc.Arrays.ProjPos) ||
			!reflect.DeepEqual(got.TargetNode, tc.Arrays.TargetNode) || !reflect.DeepEqual(got.TargetIDs, tc.Arrays.TargetIDs) {
			report.ArrayMismatches++
		}

		flat := append([]int64(nil), got.IDs...)
		if len(flat) > oracle.Seq {
			flat = flat[:oracle.Seq]
		}
		capped := make([]int32, len(flat))
		for i, id := range flat {
			if oracle.VocabCap > 0 {
				id %= int64(oracle.VocabCap)
			}
			capped[i] = int32(id)
		}
		if !reflect.DeepEqual(capped, toInt32(tc.FlatIDs)) {
			report.TokenMismatches++
		}

		if tc.LineIndex < len(lines) {
			if spmGot := enc.Encode(lines[tc.LineIndex]); !reflect.DeepEqual(spmGot, tc.SPMIDs) {
				report.SPMMismatches++
				report.TokenMismatches++
			}
		}

		score, err := backend.ScoreTokens(capped, oracle.Seq, oracle.VocabCap)
		if err != nil {
			t.Fatalf("case %d native score: %v", tc.LineIndex, err)
		}
		diff := math.Abs(score - tc.Score)
		sumDiff += diff
		if diff > report.MaxAbsDiff {
			report.MaxAbsDiff = diff
		}
	}
	report.GoMSTotal = float64(time.Since(started).Nanoseconds()) / 1e6
	report.GoMSPerCase = report.GoMSTotal / float64(report.Cases)
	if report.Cases > 0 {
		report.MeanAbsDiff = sumDiff / float64(report.Cases)
	}
	t.Logf("parity: cases=%d token_mismatches=%d spm_mismatches=%d array_mismatches=%d max_abs_diff=%.9g mean_abs_diff=%.9g go_ms/case=%.3f python_ms/case=%.3f",
		report.Cases, report.TokenMismatches, report.SPMMismatches, report.ArrayMismatches,
		report.MaxAbsDiff, report.MeanAbsDiff, report.GoMSPerCase, report.PythonMSPerCase)
	if out := os.Getenv("NEURAL_PARITY_OUT"); out != "" {
		b, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
			t.Errorf("write report: %v", err)
		}
	}
	if report.TokenMismatches != 0 || report.SPMMismatches != 0 || report.ArrayMismatches != 0 {
		t.Fatalf("token/array mismatch: tokens=%d spm=%d arrays=%d", report.TokenMismatches, report.SPMMismatches, report.ArrayMismatches)
	}
	if report.MaxAbsDiff > 1e-4 {
		t.Fatalf("max absolute score difference %.9g exceeds 1e-4", report.MaxAbsDiff)
	}
}

func toInt32(v []int64) []int32 {
	out := make([]int32, len(v))
	for i, x := range v {
		out[i] = int32(x)
	}
	return out
}

func loadSPM(path string) (*spm.Encoder, error) {
	m, err := spm.ParseModel(path)
	if err != nil {
		return nil, err
	}
	return spm.NewEncoder(m)
}

func readLines(path string) ([]string, error) {
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
