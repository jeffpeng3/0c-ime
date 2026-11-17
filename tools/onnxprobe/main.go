// Command onnxprobe is an experimental P2-30S measurement tool. It proves that
// a Go process can load and run an ONNX model through the shared ONNX Runtime
// library with no Python involved, and it exercises the pure-Go SentencePiece
// encoder in ./spm.
//
// This module is standalone (its own go.mod) so the product module never gains
// a CGO dependency and cmd/0c-server is untouched.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/0c-biggo/0c-ime/internal/spm"
)

// processStart is set as early as Go allows, so cold-start numbers include
// runtime start-up, dynamic library load, session creation and first inference.
var processStart = time.Now()

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "tokenize":
		cmdTokenize(os.Args[2:])
	case "bench":
		cmdBench(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `onnxprobe is an experimental measurement tool.

  onnxprobe tokenize -spm <model> -texts <utf8 file> [-out <json>]
  onnxprobe bench    -onnx <model.onnx> -lib <libonnxruntime.so> -spm <model> \
                     -texts <utf8 file> [-seq 32] [-runs 60] [-threads 2] [-out <json>]
`)
	os.Exit(2)
}

type tokenizeOutput struct {
	SPM    string    `json:"spm"`
	Texts  int       `json:"texts"`
	Tokens [][]int32 `json:"tokens"`
}

func cmdTokenize(args []string) {
	fs := flag.NewFlagSet("tokenize", flag.ExitOnError)
	spmPath := fs.String("spm", "", "SentencePiece model path")
	textsPath := fs.String("texts", "", "UTF-8 text file, one string per line")
	limit := fs.Int("limit", 0, "use only the first N lines (0 = all)")
	outPath := fs.String("out", "", "output JSON path (default stdout)")
	fs.Parse(args)
	if *spmPath == "" || *textsPath == "" {
		fs.Usage()
		os.Exit(2)
	}
	enc, texts, err := loadEncoder(*spmPath, *textsPath, *limit)
	check(err)
	out := tokenizeOutput{SPM: *spmPath, Texts: len(texts), Tokens: make([][]int32, len(texts))}
	for i, t := range texts {
		out.Tokens[i] = enc.Encode(t)
		if out.Tokens[i] == nil {
			out.Tokens[i] = []int32{}
		}
	}
	writeJSON(*outPath, out)
}

type benchResult struct {
	ONNX             string    `json:"onnx"`
	SPM              string    `json:"spm"`
	Lib              string    `json:"lib"`
	ORTVersion       string    `json:"ort_version"`
	GoVersion        string    `json:"go_version"`
	NumCPU           int       `json:"num_cpu"`
	EncodeMS         float64   `json:"encode_ms"`
	Texts            int       `json:"texts"`
	Seq              int       `json:"seq"`
	Runs             int       `json:"runs"`
	WarmupRuns       int       `json:"warmup_runs"`
	IntraOpThreads   int       `json:"intra_op_threads"`
	VocabCap         int       `json:"vocab_cap"`
	ColdStartMS      float64   `json:"cold_start_ms"`
	EnvInitMS        float64   `json:"env_init_ms"`
	SessionLoadMS    float64   `json:"session_load_ms"`
	FirstInferenceMS float64   `json:"first_inference_ms"`
	LoadToFirstMS    float64   `json:"load_to_first_ms"`
	RSSAfterLoadKB   int       `json:"rss_after_load_kb"`
	RSSAfterRunsKB   int       `json:"rss_after_runs_kb"`
	MinMS            float64   `json:"min_ms"`
	MedianMS         float64   `json:"median_ms"`
	P95MS            float64   `json:"p95_ms"`
	MaxMS            float64   `json:"max_ms"`
	LatenciesMS      []float64 `json:"latencies_ms"`
	Tokens           [][]int32 `json:"tokens"`
	Scores           []float32 `json:"scores"`
	AllFinite        bool      `json:"all_finite"`
}

func cmdBench(args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	onnxPath := fs.String("onnx", "", "ONNX model path")
	libPath := fs.String("lib", "", "path to libonnxruntime.so")
	spmPath := fs.String("spm", "", "SentencePiece model path")
	textsPath := fs.String("texts", "", "UTF-8 text file, one string per line")
	seq := fs.Int("seq", 32, "fixed sequence length (truncate or pad)")
	runs := fs.Int("runs", 60, "measured runs after warmup")
	threads := fs.Int("threads", 2, "intra-op threads")
	limit := fs.Int("limit", 0, "use only the first N lines (0 = all)")
	vocabCap := fs.Int("vocab-cap", 0, "map ids with id%cap when the model embedding is smaller than the tokenizer vocab")
	outPath := fs.String("out", "", "output JSON path (default stdout)")
	fs.Parse(args)
	if *onnxPath == "" || *libPath == "" || *spmPath == "" || *textsPath == "" {
		fs.Usage()
		os.Exit(2)
	}

	tEncode := time.Now()
	enc, texts, err := loadEncoder(*spmPath, *textsPath, *limit)
	check(err)
	encodeMS := msSince(tEncode)
	res := benchResult{
		ONNX: *onnxPath, SPM: *spmPath, Lib: *libPath,
		GoVersion: runtime.Version(), NumCPU: runtime.NumCPU(),
		EncodeMS: encodeMS, Texts: len(texts), Seq: *seq, Runs: *runs,
		WarmupRuns: 10, IntraOpThreads: *threads,
		VocabCap:  *vocabCap,
		Tokens:    make([][]int32, len(texts)),
		AllFinite: true,
	}
	for i, t := range texts {
		res.Tokens[i] = enc.Encode(t)
		if res.Tokens[i] == nil {
			res.Tokens[i] = []int32{}
		}
	}

	ort.SetSharedLibraryPath(*libPath)
	t0 := time.Now()
	check(ort.InitializeEnvironment())
	defer ort.DestroyEnvironment()
	res.EnvInitMS = msSince(t0)
	res.ORTVersion = ort.GetVersion()

	opts, err := ort.NewSessionOptions()
	check(err)
	defer opts.Destroy()
	check(opts.SetIntraOpNumThreads(*threads))
	check(opts.SetInterOpNumThreads(1))
	check(opts.SetExecutionMode(ort.ExecutionModeSequential))
	check(opts.AddSessionConfigEntry("session.intra_op.allow_spinning", "0"))

	// Fixed shapes: batch 1, the requested sequence length.
	inputIDs := ort.NewShape(1, int64(*seq))
	mask := ort.NewShape(1, int64(*seq))
	scoreShape := ort.NewShape(1)

	ids := make([]int64, *seq)
	maskData := make([]int64, *seq)
	fillInputs(res.Tokens[0], ids, maskData, *vocabCap)

	inTensor, err := ort.NewTensor(inputIDs, ids)
	check(err)
	defer inTensor.Destroy()
	maskTensor, err := ort.NewTensor(mask, maskData)
	check(err)
	defer maskTensor.Destroy()
	outTensor, err := ort.NewEmptyTensor[float32](scoreShape)
	check(err)
	defer outTensor.Destroy()

	tLoad := time.Now()
	session, err := ort.NewAdvancedSession(*onnxPath,
		[]string{"input_ids", "attention_mask"}, []string{"scores"},
		[]ort.Value{inTensor, maskTensor}, []ort.Value{outTensor}, opts)
	check(err)
	defer session.Destroy()
	res.SessionLoadMS = msSince(tLoad)
	res.RSSAfterLoadKB = rssKB()

	tFirst := time.Now()
	check(session.Run())
	res.FirstInferenceMS = msSince(tFirst)
	res.LoadToFirstMS = res.SessionLoadMS + res.FirstInferenceMS
	res.ColdStartMS = msSince(processStart)
	if !allFinite(outTensor.GetData()) {
		fail("first inference returned non-finite scores")
	}

	for i := 0; i < res.WarmupRuns; i++ {
		check(session.Run())
	}

	res.LatenciesMS = make([]float64, 0, *runs)
	for i := 0; i < *runs; i++ {
		start := time.Now()
		check(session.Run())
		res.LatenciesMS = append(res.LatenciesMS, msSince(start))
	}
	res.RSSAfterRunsKB = rssKB()
	sorted := append([]float64(nil), res.LatenciesMS...)
	sort.Float64s(sorted)
	res.MinMS = sorted[0]
	res.MaxMS = sorted[len(sorted)-1]
	res.MedianMS = percentile(sorted, 0.50)
	res.P95MS = percentile(sorted, 0.95)

	// Score every text at the fixed shape for the Python parity comparison.
	res.Scores = make([]float32, len(texts))
	for i, toks := range res.Tokens {
		fillInputs(toks, ids, maskData, *vocabCap)
		copy(inTensor.GetData(), ids)
		copy(maskTensor.GetData(), maskData)
		check(session.Run())
		res.Scores[i] = outTensor.GetData()[0]
		if !isFinite(res.Scores[i]) {
			res.AllFinite = false
		}
	}
	writeJSON(*outPath, res)
}

// fillInputs truncates or pads one token sequence into the fixed buffers.
// A positive cap maps ids with id%cap, which the placeholder reranker needs
// because its embedding table has 14080 rows while the tokenizer has 32768
// pieces. It has no effect on the Go/ONNX Runtime claim itself.
func fillInputs(tokens []int32, ids, mask []int64, cap int) {
	for i := range ids {
		ids[i] = 0
		mask[i] = 0
	}
	n := len(tokens)
	if n > len(ids) {
		n = len(ids)
	}
	for i := 0; i < n; i++ {
		id := tokens[i]
		if cap > 0 {
			id %= int32(cap)
		}
		ids[i] = int64(id)
		mask[i] = 1
	}
}

func loadEncoder(modelPath, textsPath string, limit int) (*spm.Encoder, []string, error) {
	m, err := spm.ParseModel(modelPath)
	if err != nil {
		return nil, nil, err
	}
	enc, err := spm.NewEncoder(m)
	if err != nil {
		return nil, nil, err
	}
	texts, err := readLines(textsPath)
	if err != nil {
		return nil, nil, err
	}
	if limit > 0 && len(texts) > limit {
		texts = texts[:limit]
	}
	return enc, texts, nil
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

func rssKB() int {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.Atoi(fields[1]); err == nil {
					return kb
				}
			}
		}
	}
	return -1
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func msSince(t time.Time) float64 {
	return float64(time.Since(t).Nanoseconds()) / 1e6
}

func isFinite(f float32) bool {
	return f == f && f < 1e38 && f > -1e38
}

func allFinite(v []float32) bool {
	for _, f := range v {
		if !isFinite(f) {
			return false
		}
	}
	return true
}

func writeJSON(path string, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	check(enc.Encode(v))
	if path == "" {
		os.Stdout.Write(buf.Bytes())
		return
	}
	check(os.WriteFile(path, buf.Bytes(), 0o644))
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "onnxprobe:", msg)
	os.Exit(1)
}
