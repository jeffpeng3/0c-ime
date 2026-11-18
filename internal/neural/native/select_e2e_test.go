//go:build cgo && onnxruntime

package native_test

// P2-30C gate: prove the native ONNX path fully replaces the legacy reference
// implementation for the real Chinese select model (Funmula/0c-sequence-d512).
// This is a development gate: build-tagged and env-gated, with the reference
// script supplied from an archived checkout outside the product tree. The
// repository ships no Python worker and the test is not a runtime dependency.
//
//	NEURAL_SELECT_PACKAGE            package dir with manifest.json, model.onnx, vocab.json
//	NEURAL_SELECT_ORT_LIB            libonnxruntime.so from the pinned pip wheel
//	NEURAL_SELECT_PYTHON             python with numpy + onnxruntime
//	NEURAL_SELECT_SEQUENCE_RUNTIME   archived sequence_runtime.py reference
//	NEURAL_SELECT_OUT                optional path for the JSON report
//	NEURAL_SELECT_THREADS            optional intra-op threads (default 2)

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/internal/neural/native"
	"github.com/0c-biggo/0c-ime/internal/neural/native/input"
)

type selectOracleArrays struct {
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

type selectOracleCase struct {
	Name       string              `json:"name"`
	Status     string              `json:"status"`
	Error      string              `json:"error"`
	Scores     []float64           `json:"scores"`
	Combined   []float64           `json:"combined"`
	Ranking    []int               `json:"ranking"`
	Chosen     int                 `json:"chosen"`
	ChosenText string              `json:"chosen_text"`
	MS         float64             `json:"ms"`
	Arrays     *selectOracleArrays `json:"arrays"`
}

type selectOracleFile struct {
	Python       string             `json:"python"`
	Onnxruntime  string             `json:"onnxruntime"`
	Numpy        string             `json:"numpy"`
	Threads      int                `json:"threads"`
	ImportMS     float64            `json:"import_ms"`
	InitMS       float64            `json:"init_ms"`
	FirstCaseMS  *float64           `json:"first_case_ms"`
	WarmMedianMS *float64           `json:"warm_median_ms"`
	WarmMeanMS   *float64           `json:"warm_mean_ms"`
	Cases        []selectOracleCase `json:"cases"`
}

type selectReport struct {
	Package          string  `json:"package"`
	ModelSHA256      string  `json:"model_sha256"`
	VocabSHA256      string  `json:"vocab_sha256"`
	Python           string  `json:"python"`
	Onnxruntime      string  `json:"onnxruntime"`
	Cases            int     `json:"cases"`
	Scored           int     `json:"scored_cases"`
	Fallback         int     `json:"fallback_cases"`
	FallbackMismatch int     `json:"fallback_mismatches"`
	FallbackCodes    int     `json:"fallback_code_mismatches"`
	ArrayMismatches  int     `json:"array_mismatches"`
	TokenMismatches  int     `json:"token_mismatches"`
	ScoreMismatches  int     `json:"score_mismatches"`
	MaxAbsDiff       float64 `json:"max_abs_score_diff"`
	MeanAbsDiff      float64 `json:"mean_abs_score_diff"`
	RankingMismatch  int     `json:"ranking_mismatches"`
	ChosenMismatch   int     `json:"chosen_mismatches"`
	NativeOpenMS     float64 `json:"native_open_ms"`
	NativeFirstMS    float64 `json:"native_first_inference_ms"`
	NativeColdMS     float64 `json:"native_cold_start_ms"`
	NativeMedianMS   float64 `json:"native_median_ms"`
	NativeP95MS      float64 `json:"native_p95_ms"`
	NativeMeanMS     float64 `json:"native_mean_ms"`
	PythonInitMS     float64 `json:"python_init_ms"`
	PythonProcessMS  float64 `json:"python_process_wall_ms"`
	PythonFirstMS    float64 `json:"python_first_inference_ms"`
	PythonMedianMS   float64 `json:"python_median_ms"`
	PythonP95MS      float64 `json:"python_p95_ms"`
	PythonMeanMS     float64 `json:"python_mean_ms"`
}

// TestSelectParityGate is the P2-30C end-to-end gate.
func TestSelectParityGate(t *testing.T) {
	pkg := os.Getenv("NEURAL_SELECT_PACKAGE")
	lib := os.Getenv("NEURAL_SELECT_ORT_LIB")
	python := os.Getenv("NEURAL_SELECT_PYTHON")
	worker := os.Getenv("NEURAL_SELECT_SEQUENCE_RUNTIME")
	if pkg == "" || lib == "" || python == "" || worker == "" {
		t.Skip("set NEURAL_SELECT_PACKAGE, NEURAL_SELECT_ORT_LIB, NEURAL_SELECT_PYTHON and NEURAL_SELECT_SEQUENCE_RUNTIME to run the select-model gate")
	}
	threads := 2
	if v := os.Getenv("NEURAL_SELECT_THREADS"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &threads); err != nil || threads < 1 || threads > native.MaxThreads {
			t.Fatalf("invalid NEURAL_SELECT_THREADS %q", v)
		}
	}

	// Verify the real weights before measuring anything.
	modelSHA, vocabSHA := verifySelectWeights(t, pkg)

	m := readSelectManifest(t)
	cases := buildSelectCases(t)
	for _, c := range cases {
		// The gate must run against the package's own operating point. Fallback
		// cases deliberately violate the contract and must be rejected.
		if !c.Fallback && c.Request.AllowPartial != m.AllowPartial {
			t.Fatalf("%s: allow_partial=%v but package says %v", c.Name, c.Request.AllowPartial, m.AllowPartial)
		}
	}

	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	oraclePath := filepath.Join(dir, "oracle.json")
	payload := selectCasesFile{Schema: 1, Corpus: "zhuyin-golden+derived", Package: pkg,
		DictionarySHA256: m.DictionarySHA256, Weight: m.Weight, Margin: m.Margin, Cases: cases}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(casesPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if dump := os.Getenv("NEURAL_SELECT_CASES_OUT"); dump != "" {
		if err := os.WriteFile(dump, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	oracleScript := filepath.Join("testdata", "select_oracle.py")
	cmd := exec.Command(python, oracleScript,
		"--sequence-runtime", worker,
		"--package", pkg,
		"--cases", casesPath,
		"--threads", fmt.Sprint(threads),
		"--out", oraclePath)
	cmd.Env = append(os.Environ(), "ORT_DISABLE_TELEMETRY=1", "ORT_RUNNING_UNIT_TESTS=1")
	procStart := time.Now()
	out, err := cmd.CombinedOutput()
	processMS := float64(time.Since(procStart).Nanoseconds()) / 1e6
	if err != nil {
		t.Fatalf("python oracle: %v\n%s", err, out)
	}
	t.Logf("python oracle: %s", out)
	oracleRaw, err := os.ReadFile(oraclePath)
	if err != nil {
		t.Fatal(err)
	}
	var oracle selectOracleFile
	if err := json.Unmarshal(oracleRaw, &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Cases) != len(cases) {
		t.Fatalf("oracle returned %d cases, want %d", len(oracle.Cases), len(cases))
	}
	byName := make(map[string]selectOracleCase, len(oracle.Cases))
	for _, c := range oracle.Cases {
		byName[c.Name] = c
	}

	report := selectReport{Package: pkg, ModelSHA256: modelSHA, VocabSHA256: vocabSHA,
		Python: oracle.Python, Onnxruntime: oracle.Onnxruntime, Cases: len(cases),
		PythonInitMS: oracle.InitMS, PythonProcessMS: processMS}
	if oracle.FirstCaseMS != nil {
		report.PythonFirstMS = *oracle.FirstCaseMS
	}

	maps := selectMaps(t)
	openStart := time.Now()
	backend, err := native.Open(native.Config{Package: pkg, DictionarySHA256: m.DictionarySHA256,
		Threads: threads, LibraryPath: lib})
	if err != nil {
		t.Fatalf("native.Open: %v", err)
	}
	defer backend.Close()
	report.NativeOpenMS = msSince(openStart)

	var nativeMS, pythonMS []float64
	var sumDiff float64
	firstScored := true
	firstScoredName := ""
	for _, c := range cases {
		want, ok := byName[c.Name]
		if !ok {
			t.Fatalf("%s: missing from oracle", c.Name)
		}
		req := toRerankRequest(c)
		started := time.Now()
		scores, err := backend.Score(req)
		elapsed := msSince(started)
		if c.Fallback {
			report.Fallback++
			bothRejected := err != nil && want.Status == "error"
			if !bothRejected {
				report.FallbackMismatch++
				t.Errorf("%s: fallback mismatch: native err=%v python status=%s err=%q", c.Name, err, want.Status, want.Error)
				continue
			}
			// Requests that survive the shared rerank.Supported contract get to
			// the worker/prepare layer; there the codes must match exactly.
			if rerank.Supported(req) && err.Error() != want.Error {
				report.FallbackCodes++
				t.Errorf("%s: fallback code: native %q, python %q", c.Name, err.Error(), want.Error)
			}
			continue
		}
		report.Scored++
		if err != nil {
			report.ScoreMismatches++
			t.Errorf("%s: native error %v while python scored", c.Name, err)
			continue
		}
		if firstScored {
			report.NativeFirstMS = elapsed
			report.NativeColdMS = report.NativeOpenMS + elapsed
			firstScoredName = c.Name
			firstScored = false
		} else {
			nativeMS = append(nativeMS, elapsed)
		}
		if c.Name != firstScoredName {
			pythonMS = append(pythonMS, want.MS)
		}
		if want.Status != "ok" {
			report.ScoreMismatches++
			t.Errorf("%s: python error %q while native scored", c.Name, want.Error)
			continue
		}
		if len(scores) != len(want.Scores) {
			report.ScoreMismatches++
			t.Errorf("%s: native returned %d scores, python %d", c.Name, len(scores), len(want.Scores))
			continue
		}
		caseMax := 0.0
		for i := range scores {
			d := math.Abs(scores[i] - want.Scores[i])
			sumDiff += d
			if d > caseMax {
				caseMax = d
			}
		}
		if caseMax > report.MaxAbsDiff {
			report.MaxAbsDiff = caseMax
		}

		// Every tensor input must be identical, not just the scores.
		arrays, err := input.Prepare(toInputRequest(c, selectNeutralLast(m, c)), maps)
		if err != nil {
			t.Fatalf("%s: native prepare: %v", c.Name, err)
		}
		if want.Arrays == nil {
			report.ArrayMismatches++
			t.Errorf("%s: python recorded no tensors", c.Name)
		} else {
			if !reflect.DeepEqual(arrays.IDs, want.Arrays.IDs) {
				report.TokenMismatches++
			}
			if !reflect.DeepEqual(arraysOf(arrays), want.Arrays) {
				report.ArrayMismatches++
				t.Errorf("%s: ctx=%q cands=%v: assembled tensors differ from the Python worker: %s",
					c.Name, c.Request.Context, candidateTexts(c.Request.Candidates), diffSelectArrays(arraysOf(arrays), want.Arrays))
			}
		}

		ranking, chosen := rankSelect(req, scores, m.Weight, m.Margin)
		if !reflect.DeepEqual(ranking, want.Ranking) {
			report.RankingMismatch++
			t.Errorf("%s: ranking native %v python %v", c.Name, ranking, want.Ranking)
		}
		if chosen != want.Chosen || req.Candidates[chosen].Text != want.ChosenText {
			report.ChosenMismatch++
			t.Errorf("%s: chosen native %d %q python %d %q", c.Name, chosen,
				req.Candidates[chosen].Text, want.Chosen, want.ChosenText)
		}
	}
	if report.Scored > 0 {
		// Mean over every score that was actually compared.
		n := 0
		for _, c := range cases {
			if !c.Fallback {
				n += len(c.Request.Candidates)
			}
		}
		report.MeanAbsDiff = sumDiff / float64(n)
	}
	if len(nativeMS) > 0 {
		report.NativeMeanMS = mean(nativeMS)
		report.NativeMedianMS = median(nativeMS)
		report.NativeP95MS = p95(nativeMS)
	}
	if len(pythonMS) > 0 {
		report.PythonMeanMS = mean(pythonMS)
		report.PythonMedianMS = median(pythonMS)
		report.PythonP95MS = p95(pythonMS)
	}

	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("select parity report:\n%s", reportJSON)
	if out := os.Getenv("NEURAL_SELECT_OUT"); out != "" {
		if err := os.WriteFile(out, append(reportJSON, '\n'), 0o644); err != nil {
			t.Errorf("write report: %v", err)
		}
	}

	if report.Scored < 200 {
		t.Errorf("scored cases = %d, want at least 200", report.Scored)
	}
	if report.FallbackMismatch != 0 || report.FallbackCodes != 0 {
		t.Errorf("fallback mismatches: behaviour=%d codes=%d", report.FallbackMismatch, report.FallbackCodes)
	}
	if report.TokenMismatches != 0 || report.ArrayMismatches != 0 {
		t.Errorf("tensor mismatches: tokens=%d arrays=%d", report.TokenMismatches, report.ArrayMismatches)
	}
	if report.ScoreMismatches != 0 {
		t.Errorf("score mismatches: %d", report.ScoreMismatches)
	}
	if report.MaxAbsDiff > 1e-4 {
		t.Errorf("max absolute score difference %.9g exceeds 1e-4", report.MaxAbsDiff)
	}
	if report.RankingMismatch != 0 {
		t.Errorf("ranking mismatches: %d", report.RankingMismatch)
	}
	if report.ChosenMismatch != 0 {
		t.Errorf("chosen-text mismatches: %d", report.ChosenMismatch)
	}
}

func verifySelectWeights(t *testing.T, pkg string) (modelSHA, vocabSHA string) {
	t.Helper()
	raw, err := os.ReadFile("../../../models/models.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Models map[string]struct {
			Files map[string]struct {
				SHA256 string `json:"sha256"`
				Size   int64  `json:"size"`
			} `json:"files"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	selectLock, ok := lock.Models["select"]
	if !ok {
		t.Fatal("models.lock.json has no select model")
	}
	for name, entry := range selectLock.Files {
		path := filepath.Join(pkg, name)
		st, err := os.Stat(path)
		if err != nil {
			if name == "model.onnx" || name == "vocab.json" {
				t.Fatalf("%s: %v", path, err)
			}
			continue // LICENSE/README/manifest are not needed by the runtime gate
		}
		if st.Size() != entry.Size {
			t.Fatalf("%s: size %d, want %d", path, st.Size(), entry.Size)
		}
		got, err := fileSHA(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != entry.SHA256 {
			t.Fatalf("%s: sha256 %s, want %s", path, got, entry.SHA256)
		}
		switch name {
		case "model.onnx":
			modelSHA = got
		case "vocab.json":
			vocabSHA = got
		}
	}
	return modelSHA, vocabSHA
}

func selectMaps(t *testing.T) input.Maps {
	t.Helper()
	v := readSelectVocab(t)
	maps := input.Maps{Bases: make(map[string]int64, len(v.Bases)), Characters: make(map[string]int64, len(v.Characters))}
	for i, b := range v.Bases {
		maps.Bases[b] = int64(i + 2)
	}
	for i, c := range v.Characters {
		maps.Characters[c] = int64(i + 8)
	}
	return maps
}

func arraysOf(a *input.Arrays) *selectOracleArrays {
	return &selectOracleArrays{IDs: a.IDs, Bases: a.Bases, Tones: a.Tones, Types: a.Types,
		Positions: a.Positions, Mask: a.Mask, ProjPos: a.ProjPos, TargetNode: a.TargetNode, TargetIDs: a.TargetIDs}
}

// diffSelectArrays names the first differing tensor element so a mismatch is
// diagnosable from the test log instead of a bare deep-equal failure.
func diffSelectArrays(got, want *selectOracleArrays) string {
	if len(got.IDs) != len(want.IDs) {
		return fmt.Sprintf("ids length %d, python %d", len(got.IDs), len(want.IDs))
	}
	if i := firstDiffInt64(got.IDs, want.IDs); i >= 0 {
		return fmt.Sprintf("ids[%d]=%d, python %d", i, got.IDs[i], want.IDs[i])
	}
	if i := firstDiffInt64(got.Bases, want.Bases); i >= 0 {
		return fmt.Sprintf("bases[%d]=%d, python %d", i, got.Bases[i], want.Bases[i])
	}
	if i := firstDiffInt64(got.Tones, want.Tones); i >= 0 {
		return fmt.Sprintf("tones[%d]=%d, python %d", i, got.Tones[i], want.Tones[i])
	}
	if i := firstDiffInt64(got.Types, want.Types); i >= 0 {
		return fmt.Sprintf("types[%d]=%d, python %d", i, got.Types[i], want.Types[i])
	}
	if i := firstDiffInt64(got.Positions, want.Positions); i >= 0 {
		return fmt.Sprintf("positions[%d]=%d, python %d", i, got.Positions[i], want.Positions[i])
	}
	if len(got.Mask) != len(want.Mask) {
		return fmt.Sprintf("mask rows %d, python %d", len(got.Mask), len(want.Mask))
	}
	for r := range got.Mask {
		if len(got.Mask[r]) != len(want.Mask[r]) {
			return fmt.Sprintf("mask[%d] width %d, python %d", r, len(got.Mask[r]), len(want.Mask[r]))
		}
		for c := range got.Mask[r] {
			if got.Mask[r][c] != want.Mask[r][c] {
				return fmt.Sprintf("mask[%d][%d]=%v, python %v", r, c, got.Mask[r][c], want.Mask[r][c])
			}
		}
	}
	if i := firstDiffInt64(got.ProjPos, want.ProjPos); i >= 0 {
		return fmt.Sprintf("proj_pos[%d]=%d, python %d (len %d/%d)", i, got.ProjPos[i], want.ProjPos[i], len(got.ProjPos), len(want.ProjPos))
	}
	if len(got.TargetNode) != len(want.TargetNode) {
		return fmt.Sprintf("target_node rows %d, python %d", len(got.TargetNode), len(want.TargetNode))
	}
	for r := range got.TargetNode {
		if i := firstDiffInt64(got.TargetNode[r], want.TargetNode[r]); i >= 0 {
			return fmt.Sprintf("target_node[%d][%d]=%d, python %d", r, i, got.TargetNode[r][i], want.TargetNode[r][i])
		}
	}
	for r := range got.TargetIDs {
		if i := firstDiffInt64(got.TargetIDs[r], want.TargetIDs[r]); i >= 0 {
			return fmt.Sprintf("target_ids[%d][%d]=%d, python %d", r, i, got.TargetIDs[r][i], want.TargetIDs[r][i])
		}
	}
	return "no element diff"
}

func candidateTexts(cands []selectCandidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Text
	}
	return out
}

func firstDiffInt64(a, b []int64) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return min(len(a), len(b))
	}
	return -1
}

func rankSelect(r rerank.Request, scores []float64, weight, margin float64) ([]int, int) {
	values := make([]float64, len(scores))
	for i := range scores {
		values[i] = r.Candidates[i].LogScore + weight*scores[i]
	}
	order := make([]int, len(scores))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return values[order[a]] > values[order[b]] })
	chosen := rerank.Choose(r.Candidates, scores, weight, margin)
	return order, chosen
}

func msSince(t time.Time) float64 { return float64(time.Since(t).Nanoseconds()) / 1e6 }

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	m := len(c) / 2
	if len(c)%2 == 1 {
		return c[m]
	}
	return (c[m-1] + c[m]) / 2
}

func p95(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	i := int(0.95 * float64(len(c)))
	if i >= len(c) {
		i = len(c) - 1
	}
	return c[i]
}
