package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// JA rules and reports use a separate schema.  The zhuyin evaluator above is
// intentionally untouched; Japanese conversion has different metrics.

const JASchema = 1

// JACase is one reading, its reference surface, and optional reference
// bunsetsu segments.  ReadingSegments is kept for provenance checks.
type JACase struct {
	ID              string   `json:"id,omitempty"`
	Reading         string   `json:"reading"`
	Surface         string   `json:"surface"`
	Accept          []string `json:"accept,omitempty"`
	ReadingSegments []string `json:"reading_segments,omitempty"`
	Segments        []string `json:"segments,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

// JACorpus is a frozen evaluation set.
type JACorpus struct {
	Name        string            `json:"name"`
	License     string            `json:"license"`
	Source      string            `json:"source"`
	Notes       string            `json:"notes,omitempty"`
	Seed        int               `json:"seed,omitempty"`
	CorpusFiles map[string]string `json:"corpus_files,omitempty"`
	Count       int               `json:"count"`
	Items       []JACase          `json:"items"`
}

// JACorpusRule pins one corpus file in rules.json.
type JACorpusRule struct {
	File          string `json:"file"`
	SHA256        string `json:"sha256"`
	Count         int    `json:"count"`
	BoundaryCases int    `json:"boundary_cases"`
	License       string `json:"license"`
	Source        string `json:"source"`
}

// JAGate is the frozen regression policy.  Only deterministic quality metrics
// are gated; latency depends on the machine and stays informational.
type JAGate struct {
	MaxTop1Drop          float64 `json:"max_top1_drop"`
	MaxTop5Drop          float64 `json:"max_top5_drop"`
	MaxBoundaryExactDrop float64 `json:"max_boundary_exact_drop"`
	MaxBoundaryF1Drop    float64 `json:"max_boundary_f1_drop"`
}

// JARules is testdata/ja/rules.json.  The file hash is recorded in the report
// and in baseline.json, so changing a metric or corpus pin forces a baseline
// refresh instead of silently moving the goalposts.
type JARules struct {
	Schema            int                     `json:"schema"`
	Version           string                  `json:"version"`
	FrozenAt          string                  `json:"frozen_at"`
	Engine            string                  `json:"engine"`
	TopN              int                     `json:"top_n"`
	TimeoutMS         int                     `json:"timeout_ms"`
	Corpora           map[string]JACorpusRule `json:"corpora"`
	MetricDefinitions map[string]string       `json:"metric_definitions"`
	Gate              JAGate                  `json:"gate"`
}

// JACorpusEntry is a loaded corpus plus its pin.
type JACorpusEntry struct {
	Rule   JACorpusRule
	Corpus *JACorpus
	SHA256 string
}

// JAEngineInfo identifies the converter and data pack used for a run.
type JAEngineInfo struct {
	Name             string `json:"name"`
	Data             string `json:"data"`
	LexiconSHA256    string `json:"lexicon_sha256"`
	ConnectionSHA256 string `json:"connection_sha256"`
}

// JACaseResult keeps per-item evidence so a failure can be inspected without
// rerunning the whole corpus.
type JACaseResult struct {
	ID             string   `json:"id,omitempty"`
	Reading        string   `json:"reading"`
	Reference      string   `json:"reference"`
	Output         string   `json:"output"`
	Top5           []string `json:"top5,omitempty"`
	Top1           bool     `json:"top1"`
	Top5Hit        bool     `json:"top5_hit"`
	BoundaryScored bool     `json:"boundary_scored"`
	BoundaryExact  bool     `json:"boundary_exact"`
	BoundaryTP     int      `json:"boundary_tp"`
	BoundaryFP     int      `json:"boundary_fp"`
	BoundaryFN     int      `json:"boundary_fn"`
	DurationMS     float64  `json:"duration_ms"`
	TimedOut       bool     `json:"timed_out"`
	Error          string   `json:"error,omitempty"`
}

// JAMetrics are the metrics required by T33.
type JAMetrics struct {
	N                 int     `json:"n"`
	Top1              int     `json:"top1"`
	Top1Rate          float64 `json:"top1_rate"`
	Top5              int     `json:"top5"`
	Top5Rate          float64 `json:"top5_rate"`
	BoundaryN         int     `json:"boundary_n"`
	BoundaryExact     int     `json:"boundary_exact"`
	BoundaryExactRate float64 `json:"boundary_exact_rate"`
	BoundaryTP        int     `json:"boundary_tp"`
	BoundaryFP        int     `json:"boundary_fp"`
	BoundaryFN        int     `json:"boundary_fn"`
	BoundaryPrecision float64 `json:"boundary_precision"`
	BoundaryRecall    float64 `json:"boundary_recall"`
	BoundaryF1        float64 `json:"boundary_f1"`
	LatencyP50MS      float64 `json:"latency_p50_ms"`
	LatencyP95MS      float64 `json:"latency_p95_ms"`
	Timeouts          int     `json:"timeouts"`
	TimeoutRate       float64 `json:"timeout_rate"`
}

// JACorpusReport is one corpus result.
type JACorpusReport struct {
	Name        string            `json:"name"`
	License     string            `json:"license"`
	Source      string            `json:"source"`
	CorpusFiles map[string]string `json:"corpus_files,omitempty"`
	SHA256      string            `json:"sha256"`
	Rule        JACorpusRule      `json:"rule"`
	Metrics     JAMetrics         `json:"metrics"`
	Items       []JACaseResult    `json:"items"`
}

// JAReport is the complete frozen-report shape.
type JAReport struct {
	Schema  int              `json:"schema"`
	Engine  JAEngineInfo     `json:"engine"`
	Rules   JARulesInfo      `json:"rules"`
	Options JAOptions        `json:"options"`
	Corpora []JACorpusReport `json:"corpora"`
}

// JARulesInfo records which frozen rules produced a report.
type JARulesInfo struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// JAOptions control a run.  TopN defaults to the rules value; Timeout is only
// used to count slow conversions, it does not cancel the search.
type JAOptions struct {
	TopN      int           `json:"top_n"`
	Timeout   time.Duration `json:"-"`
	TimeoutMS int           `json:"timeout_ms"`
}

// JABaselineMetrics is the subset stored in baseline.json.
type JABaselineMetrics struct {
	N                 int     `json:"n"`
	Top1Rate          float64 `json:"top1_rate"`
	Top5Rate          float64 `json:"top5_rate"`
	BoundaryN         int     `json:"boundary_n"`
	BoundaryExactRate float64 `json:"boundary_exact_rate"`
	BoundaryF1        float64 `json:"boundary_f1"`
	LatencyP50MS      float64 `json:"latency_p50_ms_informational"`
	LatencyP95MS      float64 `json:"latency_p95_ms_informational"`
	TimeoutRate       float64 `json:"timeout_rate_informational"`
}

// JABaseline is testdata/ja/baseline.json.  Its RulesSHA256 and engine hashes
// make stale baselines fail closed.
type JABaseline struct {
	Schema      int                          `json:"schema"`
	RulesSHA256 string                       `json:"rules_sha256"`
	Engine      JAEngineInfo                 `json:"engine"`
	MeasuredOn  string                       `json:"measured_on"`
	Corpora     map[string]JABaselineMetrics `json:"corpora"`
}

// LoadJACorpus reads and validates one JSON corpus.
func LoadJACorpus(path string) (*JACorpus, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var c JACorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	if c.Name == "" || c.License == "" || c.Source == "" {
		return nil, "", fmt.Errorf("%s: name, license and source are required", path)
	}
	if len(c.Items) == 0 {
		return nil, "", fmt.Errorf("%s: no items", path)
	}
	if c.Count != 0 && c.Count != len(c.Items) {
		return nil, "", fmt.Errorf("%s: count %d does not match %d items", path, c.Count, len(c.Items))
	}
	for i, item := range c.Items {
		if item.Reading == "" || item.Surface == "" {
			return nil, "", fmt.Errorf("%s: item %d has an empty reading or surface", path, i+1)
		}
		if len(item.Segments) > 0 && strings.Join(item.Segments, "") != item.Surface {
			return nil, "", fmt.Errorf("%s: item %d segments do not join to the surface", path, i+1)
		}
		if len(item.ReadingSegments) > 0 && strings.Join(item.ReadingSegments, "") != item.Reading {
			return nil, "", fmt.Errorf("%s: item %d reading segments do not join to the reading", path, i+1)
		}
		// A conversion reading is a rune sequence.  Whitespace would silently
		// become an unknown word and skew every metric.
		for _, r := range item.Reading {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				return nil, "", fmt.Errorf("%s: item %d reading contains whitespace", path, i+1)
			}
		}
	}
	sum := sha256.Sum256(raw)
	return &c, hex.EncodeToString(sum[:]), nil
}

// LoadJARules reads rules.json and returns its SHA-256.
func LoadJARules(path string) (*JARules, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var r JARules
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	if r.Schema != JASchema {
		return nil, "", fmt.Errorf("%s: schema %d, want %d", path, r.Schema, JASchema)
	}
	if r.Version == "" || r.TopN <= 0 || r.TimeoutMS <= 0 {
		return nil, "", fmt.Errorf("%s: version, top_n and timeout_ms are required", path)
	}
	if len(r.Corpora) == 0 {
		return nil, "", fmt.Errorf("%s: no corpora", path)
	}
	for name, c := range r.Corpora {
		if c.File == "" || c.SHA256 == "" || c.Count <= 0 {
			return nil, "", fmt.Errorf("%s: corpus %q needs file, sha256 and count", path, name)
		}
	}
	sum := sha256.Sum256(raw)
	return &r, hex.EncodeToString(sum[:]), nil
}

// LoadJACorpusSet loads every pinned corpus in a stable name order and checks
// the hash, count and declared provenance against rules.json.
func LoadJACorpusSet(rules *JARules, rulesPath string) ([]JACorpusEntry, error) {
	base := filepath.Dir(rulesPath)
	names := make([]string, 0, len(rules.Corpora))
	for name := range rules.Corpora {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]JACorpusEntry, 0, len(names))
	for _, name := range names {
		rule := rules.Corpora[name]
		path := rule.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		corpus, hash, err := LoadJACorpus(path)
		if err != nil {
			return nil, err
		}
		if hash != rule.SHA256 {
			return nil, fmt.Errorf("%s: sha256 %s, rules pin %s", name, hash, rule.SHA256)
		}
		if corpus.Name != name {
			return nil, fmt.Errorf("%s: corpus name %q does not match rules key", name, corpus.Name)
		}
		if len(corpus.Items) != rule.Count {
			return nil, fmt.Errorf("%s: %d items, rules pin %d", name, len(corpus.Items), rule.Count)
		}
		scored := 0
		for _, item := range corpus.Items {
			if len(item.Segments) > 0 {
				scored++
			}
		}
		if rule.BoundaryCases != 0 && scored != rule.BoundaryCases {
			return nil, fmt.Errorf("%s: %d boundary cases, rules pin %d", name, scored, rule.BoundaryCases)
		}
		if rule.License != "" && corpus.License != rule.License {
			return nil, fmt.Errorf("%s: license %q does not match rules pin %q", name, corpus.License, rule.License)
		}
		if rule.Source != "" && corpus.Source != rule.Source {
			return nil, fmt.Errorf("%s: source %q does not match rules pin %q", name, corpus.Source, rule.Source)
		}
		out = append(out, JACorpusEntry{Rule: rule, Corpus: corpus, SHA256: hash})
	}
	return out, nil
}

func jaSequence(s string) reading.Sequence {
	seq := make(reading.Sequence, 0, len([]rune(s)))
	for _, r := range s {
		seq = append(seq, reading.Unit(string(r)))
	}
	return seq
}

func clauseBoundaries(clauses []lang.Clause, surface string) map[int]bool {
	runes := []rune(surface)
	out := map[int]bool{}
	pos := 0
	for i, c := range clauses {
		pos += len([]rune(c.Surface))
		if i < len(clauses)-1 && pos < len(runes) && !jaPunctBoundary(runes[pos]) {
			out[pos] = true
		}
	}
	// Clauses must cover the whole result.  If they do not, report no
	// predicted boundaries instead of inventing offsets.
	if pos != len(runes) {
		return nil
	}
	return out
}

// jaPunctBoundary reports whether a clause boundary before r should be
// ignored.  Punctuation is not a bunsetsu, and the T12 segmenter emits a
// symbol as its own clause, so scoring that offset would penalize every
// sentence ending in 。
func jaPunctBoundary(r rune) bool {
	switch r {
	case '。', '、', '，', '．', '！', '？', '!', '?', ',', '.', ';', ':', '：', '；',
		'「', '」', '『', '』', '（', '）', '(', ')':
		return true
	}
	return false
}

func segmentBoundaries(segments []string) map[int]bool {
	out := map[int]bool{}
	pos := 0
	for i, s := range segments {
		pos += len([]rune(s))
		if i < len(segments)-1 {
			out[pos] = true
		}
	}
	return out
}

func setCounts(got, want map[int]bool) (tp, fp, fn int) {
	for pos := range got {
		if want[pos] {
			tp++
		} else {
			fp++
		}
	}
	for pos := range want {
		if !got[pos] {
			fn++
		}
	}
	return tp, fp, fn
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func roundMS(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Microsecond)) / 1000
}

// RunJA converts every item in every corpus and returns a deterministic
// quality report.  Duration fields differ between runs and are excluded from
// DeterminismSignature.
func RunJA(dec *decode.Decoder, engine JAEngineInfo, rules *JARules, rulesHash string, corpora []JACorpusEntry, opts JAOptions) JAReport {
	if opts.TopN <= 0 {
		opts.TopN = rules.TopN
	}
	if opts.Timeout <= 0 {
		opts.Timeout = time.Duration(rules.TimeoutMS) * time.Millisecond
	}
	opts.TimeoutMS = int(opts.Timeout / time.Millisecond)

	rep := JAReport{
		Schema:  JASchema,
		Engine:  engine,
		Rules:   JARulesInfo{Version: rules.Version, SHA256: rulesHash},
		Options: opts,
	}
	for _, entry := range corpora {
		cr := JACorpusReport{
			Name:        entry.Corpus.Name,
			License:     entry.Corpus.License,
			Source:      entry.Corpus.Source,
			CorpusFiles: entry.Corpus.CorpusFiles,
			SHA256:      entry.SHA256,
			Rule:        entry.Rule,
		}
		var durations []time.Duration
		var timeout int
		var top1, top5, bN, bExact, tp, fp, fn int
		for _, item := range entry.Corpus.Items {
			res := JACaseResult{
				ID:        item.ID,
				Reading:   item.Reading,
				Reference: item.Surface,
			}
			start := time.Now()
			results, err := dec.NBest(jaSequence(item.Reading), lang.Context{}, opts.TopN)
			dur := time.Since(start)
			res.DurationMS = roundMS(dur)
			durations = append(durations, dur)
			if dur > opts.Timeout {
				res.TimedOut = true
				timeout++
			}
			if err != nil {
				res.Error = err.Error()
			} else if len(results) > 0 {
				res.Output = decode.ResultSurface(results[0].Clauses)
				res.Top1 = jaMatch(res.Output, item.Surface, item.Accept)
				for _, r := range results {
					res.Top5 = append(res.Top5, decode.ResultSurface(r.Clauses))
				}
				for _, candidate := range res.Top5 {
					if jaMatch(candidate, item.Surface, item.Accept) {
						res.Top5Hit = true
						break
					}
				}
				if len(item.Segments) > 0 {
					res.BoundaryScored = true
					got := clauseBoundaries(results[0].Clauses, res.Output)
					want := segmentBoundaries(item.Segments)
					res.BoundaryTP, res.BoundaryFP, res.BoundaryFN = setCounts(got, want)
					res.BoundaryExact = res.BoundaryFP == 0 && res.BoundaryFN == 0
				}
			}
			if res.Top1 {
				top1++
			}
			if res.Top5Hit {
				top5++
			}
			if res.BoundaryScored {
				bN++
				if res.BoundaryExact {
					bExact++
				}
				tp += res.BoundaryTP
				fp += res.BoundaryFP
				fn += res.BoundaryFN
			}
			cr.Items = append(cr.Items, res)
		}
		m := JAMetrics{
			N:             len(entry.Corpus.Items),
			Top1:          top1,
			Top1Rate:      ratio(top1, len(entry.Corpus.Items)),
			Top5:          top5,
			Top5Rate:      ratio(top5, len(entry.Corpus.Items)),
			BoundaryN:     bN,
			BoundaryExact: bExact,
			BoundaryTP:    tp,
			BoundaryFP:    fp,
			BoundaryFN:    fn,
			Timeouts:      timeout,
		}
		m.BoundaryExactRate = ratio(bExact, bN)
		m.BoundaryPrecision = ratio(tp, tp+fp)
		m.BoundaryRecall = ratio(tp, tp+fn)
		m.BoundaryF1 = harmonic(m.BoundaryPrecision, m.BoundaryRecall)
		m.LatencyP50MS = float64(Percentile(durations, 50)) / float64(time.Microsecond) / 1000
		m.LatencyP95MS = float64(Percentile(durations, 95)) / float64(time.Microsecond) / 1000
		m.TimeoutRate = ratio(timeout, len(entry.Corpus.Items))
		cr.Metrics = m
		rep.Corpora = append(rep.Corpora, cr)
	}
	return rep
}

func harmonic(a, b float64) float64 {
	if a+b == 0 {
		return 0
	}
	return 2 * a * b / (a + b)
}

func jaMatch(got, primary string, accept []string) bool {
	if got == primary {
		return true
	}
	for _, x := range accept {
		if got == x {
			return true
		}
	}
	return false
}

// DeterminismSignature returns a JSON encoding of everything that must be
// identical across runs.  Latency and timeout observations are excluded.
func DeterminismSignature(rep JAReport) (string, error) {
	copyRep := rep
	copyRep.Corpora = make([]JACorpusReport, len(rep.Corpora))
	for i, c := range rep.Corpora {
		cc := c
		cc.Metrics.LatencyP50MS = 0
		cc.Metrics.LatencyP95MS = 0
		cc.Metrics.Timeouts = 0
		cc.Metrics.TimeoutRate = 0
		cc.Items = make([]JACaseResult, len(c.Items))
		for j, item := range c.Items {
			item.DurationMS = 0
			item.TimedOut = false
			cc.Items[j] = item
		}
		copyRep.Corpora[i] = cc
	}
	b, err := json.Marshal(copyRep)
	return string(b), err
}

// BaselineFor extracts the gated metrics from a report.
func BaselineFor(rep JAReport) map[string]JABaselineMetrics {
	out := map[string]JABaselineMetrics{}
	for _, c := range rep.Corpora {
		out[c.Name] = JABaselineMetrics{
			N:                 c.Metrics.N,
			Top1Rate:          c.Metrics.Top1Rate,
			Top5Rate:          c.Metrics.Top5Rate,
			BoundaryN:         c.Metrics.BoundaryN,
			BoundaryExactRate: c.Metrics.BoundaryExactRate,
			BoundaryF1:        c.Metrics.BoundaryF1,
			LatencyP50MS:      c.Metrics.LatencyP50MS,
			LatencyP95MS:      c.Metrics.LatencyP95MS,
			TimeoutRate:       c.Metrics.TimeoutRate,
		}
	}
	return out
}

// LoadJABaseline reads baseline.json.
func LoadJABaseline(path string) (*JABaseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b JABaseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if b.Schema != JASchema {
		return nil, fmt.Errorf("%s: schema %d, want %d", path, b.Schema, JASchema)
	}
	if b.RulesSHA256 == "" || len(b.Corpora) == 0 {
		return nil, fmt.Errorf("%s: rules_sha256 and corpora are required", path)
	}
	return &b, nil
}

// JARegression compares deterministic quality metrics with the frozen
// baseline.  Any drop beyond the rule tolerance is a failure.  Latency and
// timeout rate are deliberately not gated: CI runners are too noisy.
func JARegression(rep JAReport, base *JABaseline, rulesHash string, gate JAGate) []string {
	var problems []string
	if base.RulesSHA256 != rulesHash {
		problems = append(problems, fmt.Sprintf("rules hash %s does not match baseline %s; refresh the baseline deliberately", rulesHash, base.RulesSHA256))
	}
	if base.Engine.LexiconSHA256 != rep.Engine.LexiconSHA256 {
		problems = append(problems, fmt.Sprintf("lexicon hash %s does not match baseline %s; refresh the baseline deliberately", rep.Engine.LexiconSHA256, base.Engine.LexiconSHA256))
	}
	if base.Engine.ConnectionSHA256 != rep.Engine.ConnectionSHA256 {
		problems = append(problems, fmt.Sprintf("connection hash %s does not match baseline %s; refresh the baseline deliberately", rep.Engine.ConnectionSHA256, base.Engine.ConnectionSHA256))
	}
	current := BaselineFor(rep)
	names := make([]string, 0, len(base.Corpora))
	for name := range base.Corpora {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want := base.Corpora[name]
		got, ok := current[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: not present in report", name))
			continue
		}
		if got.N != want.N {
			problems = append(problems, fmt.Sprintf("%s: n=%d, baseline %d", name, got.N, want.N))
		}
		checkDrop := func(metric string, g, w, tolerance float64) {
			if g+tolerance < w {
				problems = append(problems, fmt.Sprintf("%s: %s %.6f < baseline %.6f", name, metric, g, w))
			}
		}
		checkDrop("top1_rate", got.Top1Rate, want.Top1Rate, gate.MaxTop1Drop)
		checkDrop("top5_rate", got.Top5Rate, want.Top5Rate, gate.MaxTop5Drop)
		checkDrop("boundary_exact_rate", got.BoundaryExactRate, want.BoundaryExactRate, gate.MaxBoundaryExactDrop)
		checkDrop("boundary_f1", got.BoundaryF1, want.BoundaryF1, gate.MaxBoundaryF1Drop)
	}
	// A new corpus without a baseline must not slip through.
	for name := range current {
		if _, ok := base.Corpora[name]; !ok {
			problems = append(problems, fmt.Sprintf("%s: no baseline entry", name))
		}
	}
	return problems
}

// CheckJACorpusProvenance enforces the T33 license boundary: Anthy data must
// come from corpus.1 only.  It checks the pinned hash and the item set against
// the T03 fixture, so a fixture cannot silently grow a corpus.2/3/5 item.
func CheckJACorpusProvenance(entries []JACorpusEntry, rules *JARules, t03Fixture string) error {
	var anthy *JACorpusEntry
	for i := range entries {
		if entries[i].Corpus.Name == "anthy-corpus1" {
			anthy = &entries[i]
			break
		}
	}
	if anthy == nil {
		return errors.New("anthy-corpus1 corpus is missing")
	}
	rule := rules.Corpora["anthy-corpus1"]
	if rule.SHA256 == "" {
		return errors.New("rules.json does not pin anthy-corpus1")
	}
	if !strings.Contains(strings.ToLower(anthy.Corpus.License), "public domain") {
		return fmt.Errorf("anthy-corpus1 license %q is not public domain", anthy.Corpus.License)
	}
	if len(anthy.Corpus.CorpusFiles) != 1 {
		return fmt.Errorf("anthy-corpus1 fixture lists %d source files, want only corpus.1.txt", len(anthy.Corpus.CorpusFiles))
	}
	sourceHash, ok := anthy.Corpus.CorpusFiles["corpus.1.txt"]
	if !ok || sourceHash == "" {
		return errors.New("anthy-corpus1 fixture does not record the corpus.1.txt hash")
	}
	raw, err := os.ReadFile(t03Fixture)
	if err != nil {
		return fmt.Errorf("read T03 fixture: %w", err)
	}
	var t03 struct {
		CorpusFiles map[string]string `json:"corpus_files"`
		Items       []struct {
			Reading string `json:"reading"`
			Surface string `json:"surface"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &t03); err != nil {
		return fmt.Errorf("parse T03 fixture: %w", err)
	}
	if len(t03.CorpusFiles) != 1 {
		return fmt.Errorf("T03 fixture lists %d corpus files, want only corpus.1.txt", len(t03.CorpusFiles))
	}
	if got, ok := t03.CorpusFiles["corpus.1.txt"]; !ok || got != sourceHash {
		return fmt.Errorf("T03 corpus.1 hash %q does not match fixture source hash %q", got, sourceHash)
	}
	want := map[string]bool{}
	for _, x := range t03.Items {
		want[x.Reading+"\x00"+x.Surface] = true
	}
	for _, x := range anthy.Corpus.Items {
		if !want[x.Reading+"\x00"+x.Surface] {
			return fmt.Errorf("anthy-corpus1 item %q is not in the T03 corpus.1 fixture", x.Surface)
		}
	}
	return nil
}
