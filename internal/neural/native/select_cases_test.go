package native_test

// Test-only corpus builder for the P2-30C select-model parity gate. It derives
// cases from the committed zhuyin golden corpus and the committed upstream
// vocab/manifest, so no model weights or private text are needed to build them.
// select_e2e_test.go feeds the same cases to the Python worker and the native
// backend.

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/decode"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
	"github.com/0c-biggo/0c-ime/internal/neural/native/input"
)

const (
	selectCorpusPath   = "../../../testdata/eval/zhuyin-golden.json"
	selectVocabPath    = "../../../models/upstream/select/vocab.json"
	selectManifestPath = "../../../models/upstream/select/manifest.json"

	// unknownChar is deliberately outside the 27,857-character vocabulary; the
	// model contract requires the whole case to fall back.
	unknownChar = "\uE000"
)

type selectSegment struct {
	Start      int    `json:"Start"`
	End        int    `json:"End"`
	Word       string `json:"Word"`
	Partial    bool   `json:"Partial"`
	Correction bool   `json:"Correction"`
}

type selectCandidate struct {
	Text     string          `json:"text"`
	LogScore float64         `json:"log_score"`
	Segments []selectSegment `json:"segments"`
}

type selectRequest struct {
	Context      string            `json:"context"`
	Reading      []string          `json:"reading"`
	Candidates   []selectCandidate `json:"candidates"`
	AllowPartial bool              `json:"allow_partial"`
	Mode         string            `json:"mode,omitempty"`
}

type selectCase struct {
	Name        string        `json:"name"`
	Family      string        `json:"family"`
	Fallback    bool          `json:"fallback"`
	ExpectError string        `json:"expect_error,omitempty"`
	Request     selectRequest `json:"request"`
}

func selectNeutralLast(m selectManifest, c selectCase) bool {
	last := ""
	if len(c.Request.Reading) > 0 {
		last = c.Request.Reading[len(c.Request.Reading)-1]
	}
	return input.NeutralLast(c.Request.Mode, last, m.NeutralExpand > 0, m.NeutralTolerant)
}

type selectCasesFile struct {
	Schema           int          `json:"schema"`
	Corpus           string       `json:"corpus"`
	Package          string       `json:"package"`
	DictionarySHA256 string       `json:"dictionary_sha256"`
	Weight           float64      `json:"weight"`
	Margin           float64      `json:"margin"`
	Cases            []selectCase `json:"cases"`
}

type selectManifest struct {
	DictionarySHA256 string   `json:"dictionary_sha256"`
	AllowPartial     bool     `json:"allow_partial"`
	Weight           float64  `json:"weight"`
	Margin           float64  `json:"margin"`
	TextWeight       float64  `json:"text_weight"`
	NeutralExpand    float64  `json:"neutral_expand_penalty"`
	NeutralTolerant  []string `json:"neutral_tolerant_last"`
}

type selectCorpus struct {
	Items []struct {
		ID      string `json:"id"`
		Reading string `json:"reading"`
		Surface string `json:"surface"`
	} `json:"items"`
}

type selectVocab struct {
	Characters []string `json:"characters"`
	Bases      []string `json:"bases"`
}

func readSelectManifest(t *testing.T) selectManifest {
	t.Helper()
	raw, err := os.ReadFile(selectManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m selectManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func readSelectVocab(t *testing.T) selectVocab {
	t.Helper()
	raw, err := os.ReadFile(selectVocabPath)
	if err != nil {
		t.Fatal(err)
	}
	var v selectVocab
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func readSelectCorpus(t *testing.T) selectCorpus {
	t.Helper()
	raw, err := os.ReadFile(selectCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	var c selectCorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

type selectWindow struct {
	reading []string
	surface string
	context string
	source  string
	offset  int
}

// buildSelectCases returns a deterministic case set: at least 200 scored
// requests covering 2..5-syllable windows, multi-word segments, the 64-rune
// context boundary, tones, neutral-last expansion, text mode and whole-case
// fallback.
func buildSelectCases(t *testing.T) []selectCase {
	t.Helper()
	corpus := readSelectCorpus(t)
	vocab := readSelectVocab(t)
	m := readSelectManifest(t)

	known := make(map[rune]bool, len(vocab.Characters))
	for _, c := range vocab.Characters {
		for _, r := range c {
			known[r] = true
		}
	}
	knownBases := make(map[string]bool, len(vocab.Bases))
	for _, b := range vocab.Bases {
		knownBases[b] = true
	}

	// Candidate alternatives are contiguous n-grams of the corpus surface, so
	// every alternative is a plausible word-like string of the right length.
	pools := map[int][]string{}
	seenPool := map[int]map[string]bool{}
	addPool := func(n int, s string) {
		if n < 1 || n > 5 {
			return
		}
		if seenPool[n] == nil {
			seenPool[n] = map[string]bool{}
		}
		r := []rune(s)
		for i := 0; i+n <= len(r); i++ {
			w := string(r[i : i+n])
			if seenPool[n][w] {
				continue
			}
			ok := true
			for _, x := range r[i : i+n] {
				if !known[x] {
					ok = false
					break
				}
			}
			if ok {
				seenPool[n][w] = true
				pools[n] = append(pools[n], w)
			}
		}
	}
	for _, it := range corpus.Items {
		addPool(len([]rune(it.Surface)), it.Surface)
		for i := 1; i <= 5; i++ {
			addPool(i, it.Surface)
		}
	}
	for n := range pools {
		sort.Strings(pools[n])
	}

	neutral := make(map[string]bool, len(m.NeutralTolerant))
	for _, y := range m.NeutralTolerant {
		neutral[baseOf(y)] = true
	}

	var windows []selectWindow
	for _, it := range corpus.Items {
		syls := splitSyllables(it.Reading)
		surface := []rune(it.Surface)
		if len(syls) < 2 || len(syls) != len(surface) {
			continue
		}
		for n := 2; n <= 5 && n <= len(syls); n++ {
			step := 1
			if n >= 4 {
				step = 2
			}
			for start := 0; start+n <= len(syls); start += step {
				text := string(surface[start : start+n])
				ok := true
				for _, r := range surface[start : start+n] {
					if !known[r] {
						ok = false
					}
				}
				if !ok {
					continue
				}
				windows = append(windows, selectWindow{
					reading: append([]string(nil), syls[start:start+n]...),
					surface: text,
					context: string(surface[:start]),
					source:  it.ID,
					offset:  start,
				})
			}
		}
	}

	candidatesFor := func(w selectWindow, n int) []selectCandidate {
		alt := pools[n]
		out := []selectCandidate{candidate(w.surface, 0)}
		for i := 0; i < len(alt) && len(out) < 5; i++ {
			if alt[i] == w.surface {
				continue
			}
			out = append(out, candidate(alt[i], -0.25*float64(len(out))))
		}
		if len(out) < 2 {
			// One-character swap keeps the case valid when the corpus pool is
			// too small for this length.
			r := []rune(w.surface)
			for _, c := range vocab.Characters {
				cr := []rune(c)
				if len(cr) != 1 || !known[cr[0]] || string(cr) == string(r[len(r)-1]) {
					continue
				}
				altText := string(append(append([]rune(nil), r[:len(r)-1]...), cr[0]))
				if altText == w.surface {
					continue
				}
				out = append(out, candidate(altText, -0.25))
				break
			}
		}
		return out
	}

	// Keep the scored set near 240 cases: enough for the gate, bounded runtime.
	const target = 240
	var scored []selectCase
	for i, w := range windows {
		n := len(w.reading)
		cands := candidatesFor(w, n)
		if len(cands) < 2 {
			continue
		}
		family := "corpus"
		if n >= 3 && i%3 == 0 {
			cands = splitCandidates(w, n)
			family = "multisegment"
		}
		context := w.context
		if i%4 == 0 {
			context = ""
		} else if i%11 == 0 {
			context = repeatToRunes(w.surface, 64)
		}
		scored = append(scored, selectCase{
			Name:    "corpus-" + w.source + "-" + strconv.Itoa(w.offset) + "-" + strconv.Itoa(n),
			Family:  family,
			Request: selectRequest{Context: context, Reading: w.reading, Candidates: cands, AllowPartial: m.AllowPartial},
		})
	}
	if len(scored) < 200 {
		t.Fatalf("corpus produced %d scored cases, need at least 200", len(scored))
	}
	keep := make([]selectCase, 0, target)
	stride := float64(len(scored)) / target
	next := 0.0
	for i, c := range scored {
		if float64(i) >= next {
			keep = append(keep, c)
			next += stride
		}
	}
	scored = keep

	// Tone coverage: one case per mark plus an unmarked (first-tone) syllable.
	toneSeen := map[string]bool{}
	for _, c := range append([]selectCase(nil), scored...) {
		if c.Family == "multisegment" {
			continue
		}
		marks := toneMarksIn(c.Request.Reading)
		if len(marks) == 0 {
			marks = []string{""}
		}
		for _, mark := range marks {
			if toneSeen[mark] {
				continue
			}
			toneSeen[mark] = true
			c.Name = "tone-" + markName(mark) + "-" + c.Name
			c.Family = "tones"
			scored = append(scored, c)
			break
		}
	}
	// Neutral-last expansion: last syllable base is in neutral_tolerant_last.
	neutralAdded := 0
	for _, c := range scored {
		if neutralAdded == 6 {
			break
		}
		if c.Family == "tones" || len(c.Request.Reading) == 0 {
			continue
		}
		if !neutral[baseOf(c.Request.Reading[len(c.Request.Reading)-1])] {
			continue
		}
		c.Name = "neutral-" + c.Name
		c.Family = "neutral"
		scored = append(scored, c)
		neutralAdded++
	}
	if neutralAdded == 0 {
		t.Fatal("no neutral-last case: corpus does not end on a neutral_tolerant_last base")
	}

	// The confirmed-context boundary: 0, 1, 16, 63 and 64 runes, plus one
	// 64-rune context containing a character outside the vocabulary (mapped to
	// [UNK] by both implementations, so it still scores).
	base := scored[0]
	contextText := repeatToRunes(base.Request.Candidates[0].Text, 64)
	for _, l := range []int{0, 1, 16, 63, 64} {
		c := base
		c.Name = "ctx64-" + strconv.Itoa(l)
		c.Family = "context64"
		c.Request.Context = string([]rune(contextText)[:l])
		scored = append(scored, c)
	}
	c := base
	c.Name = "ctx64-unknown-char"
	c.Family = "context64"
	ctx := []rune(contextText)
	ctx[10] = []rune(unknownChar)[0]
	c.Request.Context = string(ctx)
	scored = append(scored, c)

	// Text mode: single syllable with confirmed context, character candidates.
	// A window supplies one syllable plus a non-empty context taken from the
	// same corpus surface.
	textAdded := 0
	charPool := make([]string, 0, len(vocab.Characters))
	for _, c := range vocab.Characters {
		if len([]rune(c)) == 1 && known[[]rune(c)[0]] {
			charPool = append(charPool, c)
		}
	}
	sort.Strings(charPool)
	for i := 0; i < len(scored) && textAdded < 8; i++ {
		reading := scored[i].Request.Reading
		if len(reading) < 2 {
			continue
		}
		runes := []rune(scored[i].Request.Candidates[0].Text)
		if len(runes) < 2 {
			continue
		}
		context := scored[i].Request.Context
		if context == "" {
			context = string(runes[1:])
		}
		first := string(runes[0])
		cands := []selectCandidate{candidate(first, 0)}
		for _, ch := range charPool {
			if ch == first {
				continue
			}
			cands = append(cands, candidate(ch, -0.25*float64(len(cands))))
			if len(cands) == 4 {
				break
			}
		}
		scored = append(scored, selectCase{
			Name:   "text-" + strconv.Itoa(textAdded),
			Family: "text",
			Request: selectRequest{Context: context, Reading: reading[:1],
				Candidates: cands, AllowPartial: m.AllowPartial, Mode: rerank.ModeText},
		})
		textAdded++
	}
	t.Logf("text mode cases: %d", textAdded)

	cases := append([]selectCase(nil), scored...)
	cases = append(cases, fallbackCases(t, vocab, knownBases, m)...)
	return cases
}

func candidate(text string, logScore float64) selectCandidate {
	n := len([]rune(text))
	return selectCandidate{Text: text, LogScore: logScore,
		Segments: []selectSegment{{Start: 0, End: n, Word: text}}}
}

func splitCandidates(w selectWindow, n int) []selectCandidate {
	out := []selectCandidate{candidate(w.surface, 0)}
	seen := map[string]bool{w.surface: true}
	for i := 1; i <= 3; i++ {
		r := []rune(w.surface)
		// Build additional words by rotating the runes; still in vocabulary and
		// of the same length, with multi-word segment boundaries.
		alt := string(append(append([]rune(nil), r[i%n:]...), r[:i%n]...))
		if seen[alt] {
			continue
		}
		seen[alt] = true
		out = append(out, selectCandidate{Text: alt, LogScore: -0.25 * float64(len(out)),
			Segments: []selectSegment{
				{Start: 0, End: n / 2, Word: string([]rune(alt)[:n/2])},
				{Start: n / 2, End: n, Word: string([]rune(alt)[n/2:])},
			}})
	}
	return out
}

func fallbackCases(t *testing.T, vocab selectVocab, knownBases map[string]bool, m selectManifest) []selectCase {
	t.Helper()
	chars := make([]string, 0, len(vocab.Characters))
	for _, c := range vocab.Characters {
		if len([]rune(c)) == 1 {
			chars = append(chars, c)
		}
	}
	sort.Strings(chars)
	if len(chars) < 40 {
		t.Fatalf("vocabulary has only %d single characters", len(chars))
	}
	word := func(a, b int) string { return chars[a] + chars[b] }
	reading := []string{"ㄐㄧ", "ㄨㄛˇ"}
	valid := []selectCandidate{
		{Text: word(0, 1), LogScore: 0, Segments: []selectSegment{{Start: 0, End: 1, Word: chars[0]}, {Start: 1, End: 2, Word: chars[1]}}},
		{Text: word(2, 3), LogScore: -0.4, Segments: []selectSegment{{Start: 0, End: 2, Word: word(2, 3)}}},
	}
	out := []selectCase{
		{Name: "fallback-unknown-character", Family: "fallback", Fallback: true, ExpectError: "unknown_character",
			Request: selectRequest{Reading: reading, AllowPartial: m.AllowPartial, Candidates: []selectCandidate{
				{Text: unknownChar + chars[0], LogScore: 0, Segments: []selectSegment{{Start: 0, End: 2, Word: unknownChar + chars[0]}}},
				valid[1],
			}}},
		{Name: "fallback-unknown-base", Family: "fallback", Fallback: true, ExpectError: "unknown_base",
			Request: selectRequest{Reading: []string{"ㄐㄧ", "ㄅ"}, AllowPartial: m.AllowPartial, Candidates: valid}},
		{Name: "fallback-duplicate-candidates", Family: "fallback", Fallback: true, ExpectError: "duplicate_candidates",
			Request: selectRequest{Reading: reading, AllowPartial: m.AllowPartial, Candidates: []selectCandidate{
				valid[0], valid[0],
			}}},
		{Name: "fallback-misaligned-candidate", Family: "fallback", Fallback: true, ExpectError: "candidate_alignment_or_score",
			Request: selectRequest{Reading: reading, AllowPartial: m.AllowPartial, Candidates: []selectCandidate{
				{Text: chars[0], LogScore: 0, Segments: []selectSegment{{Start: 0, End: 1, Word: chars[0]}}},
				valid[1],
			}}},
		{Name: "fallback-incomplete-segments", Family: "fallback", Fallback: true, ExpectError: "incomplete_segments",
			Request: selectRequest{Reading: reading, AllowPartial: m.AllowPartial, Candidates: []selectCandidate{
				{Text: valid[0].Text, LogScore: 0, Segments: []selectSegment{{Start: 0, End: 1, Word: chars[0]}}},
				valid[1],
			}}},
		{Name: "fallback-correction-segment", Family: "fallback", Fallback: true, ExpectError: "unsupported_segment",
			Request: selectRequest{Reading: reading, AllowPartial: m.AllowPartial, Candidates: []selectCandidate{
				{Text: valid[0].Text, LogScore: 0, Segments: []selectSegment{{Start: 0, End: 2, Word: valid[0].Text, Correction: true}}},
				valid[1],
			}}},
		{Name: "fallback-partial-not-allowed", Family: "fallback", Fallback: true, ExpectError: "unsupported_segment",
			Request: selectRequest{Reading: reading, AllowPartial: false, Candidates: []selectCandidate{
				{Text: valid[0].Text, LogScore: 0, Segments: []selectSegment{{Start: 0, End: 2, Word: valid[0].Text, Partial: true}}},
				valid[1],
			}}},
		{Name: "fallback-single-syllable", Family: "fallback", Fallback: true, ExpectError: "unsupported_shape",
			Request: selectRequest{Reading: []string{"ㄐㄧ"}, AllowPartial: m.AllowPartial, Candidates: []selectCandidate{
				{Text: chars[0], LogScore: 0, Segments: []selectSegment{{Start: 0, End: 1, Word: chars[0]}}},
				{Text: chars[1], LogScore: -0.4, Segments: []selectSegment{{Start: 0, End: 1, Word: chars[1]}}},
			}}},
		{Name: "fallback-too-many-candidates", Family: "fallback", Fallback: true, ExpectError: "unsupported_shape",
			Request: selectRequest{Reading: reading, AllowPartial: m.AllowPartial, Candidates: manyCandidates(chars, 17)}},
		{Name: "fallback-text-mode-without-context", Family: "fallback", Fallback: true, ExpectError: "unsupported_shape",
			Request: selectRequest{Reading: []string{"ㄐㄧ"}, AllowPartial: m.AllowPartial, Mode: rerank.ModeText, Candidates: []selectCandidate{
				{Text: chars[0], LogScore: 0, Segments: []selectSegment{{Start: 0, End: 1, Word: chars[0]}}},
				{Text: chars[1], LogScore: -0.4, Segments: []selectSegment{{Start: 0, End: 1, Word: chars[1]}}},
			}}},
	}
	// Guard the fixture assumptions.
	if knownBases["ㄅ"] {
		t.Fatal("ㄅ unexpectedly present in the base vocabulary")
	}
	return out
}

func manyCandidates(chars []string, n int) []selectCandidate {
	out := make([]selectCandidate, 0, n)
	for i := 0; i < n; i++ {
		text := chars[i] + chars[(i+1)%len(chars)]
		out = append(out, candidate(text, -0.1*float64(i)))
	}
	return out
}

func splitSyllables(s string) []string { return strings.Fields(s) }

func baseOf(y string) string {
	r := []rune(y)
	if len(r) == 0 {
		return y
	}
	switch r[len(r)-1] {
	case 'ˊ', 'ˇ', 'ˋ', '˙':
		return string(r[:len(r)-1])
	}
	return y
}

func toneMarksIn(reading []string) []string {
	var out []string
	for _, y := range reading {
		r := []rune(y)
		if len(r) == 0 {
			continue
		}
		switch r[len(r)-1] {
		case 'ˊ', 'ˇ', 'ˋ', '˙':
			out = append(out, string(r[len(r)-1]))
		}
	}
	return out
}

func toneName(mark string) string {
	switch mark {
	case "ˊ":
		return "second"
	case "ˇ":
		return "third"
	case "ˋ":
		return "fourth"
	case "˙":
		return "neutral"
	}
	return "first"
}

// markName is the family key used by the tone coverage assertion.
func markName(mark string) string { return toneName(mark) }

func repeatToRunes(s string, n int) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	out := make([]rune, 0, n)
	for len(out) < n {
		out = append(out, r...)
	}
	return string(out[:n])
}

// TestSelectCaseCorpus is the model-free half of the P2-30C gate: it proves the
// generated cases are deterministic, cover the required families, and that
// every fallback case is actually rejected by the shared contract.
func TestSelectCaseCorpus(t *testing.T) {
	cases := buildSelectCases(t)
	again := buildSelectCases(t)
	if !reflect.DeepEqual(cases, again) {
		t.Fatal("case generation is not deterministic")
	}

	vocab := readSelectVocab(t)
	known := map[rune]bool{}
	for _, c := range vocab.Characters {
		for _, r := range c {
			known[r] = true
		}
	}
	m := readSelectManifest(t)
	maps := input.Maps{Bases: map[string]int64{}, Characters: map[string]int64{}}
	for i, b := range vocab.Bases {
		maps.Bases[b] = int64(i + 2)
	}
	for i, c := range vocab.Characters {
		maps.Characters[c] = int64(i + 8)
	}

	families := map[string]int{}
	scored, fallback := 0, 0
	names := map[string]bool{}
	for _, c := range cases {
		if names[c.Name] {
			t.Errorf("duplicate case name %q", c.Name)
		}
		names[c.Name] = true
		families[c.Family]++
		req := toInputRequest(c, selectNeutralLast(m, c))
		if c.Fallback {
			fallback++
			got, err := input.Prepare(req, maps)
			_ = got
			prepared := err != nil
			if !prepared {
				t.Fatalf("%s: fallback case was accepted by Prepare", c.Name)
			}
			// Requests that pass rerank.Supported must fail with the Python
			// worker's specific code; the rest are rejected by the host
			// contract before the model sees them.
			if rerank.Supported(toRerankRequest(c)) {
				if err.Error() != c.ExpectError {
					t.Errorf("%s: Prepare error %q, want %q", c.Name, err.Error(), c.ExpectError)
				}
			}
			continue
		}
		scored++
		for _, cand := range c.Request.Candidates {
			for _, r := range cand.Text {
				if !known[r] {
					t.Fatalf("%s: candidate %q contains out-of-vocabulary %U", c.Name, cand.Text, r)
				}
			}
		}
		if len(c.Request.Candidates) < 2 || len(c.Request.Candidates) > 16 {
			t.Fatalf("%s: %d candidates", c.Name, len(c.Request.Candidates))
		}
		if _, err := input.Prepare(req, maps); err != nil {
			t.Fatalf("%s: Prepare: %v", c.Name, err)
		}
	}
	if scored < 200 {
		t.Fatalf("scored cases = %d, want at least 200", scored)
	}
	for _, family := range []string{"corpus", "multisegment", "tones", "neutral", "context64", "text", "fallback"} {
		if families[family] == 0 {
			t.Errorf("family %q missing", family)
		}
	}
	if fallback < 8 {
		t.Errorf("fallback cases = %d", fallback)
	}
	for _, mark := range []string{"", "ˊ", "ˇ", "ˋ", "˙"} {
		want := "tone-" + markName(mark) + "-"
		found := false
		for _, c := range cases {
			if strings.HasPrefix(c.Name, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tone coverage %q missing", markName(mark))
		}
	}
	t.Logf("select cases: scored=%d fallback=%d families=%v", scored, fallback, families)
}

func toInputRequest(c selectCase, neutral bool) input.Request {
	req := input.Request{Context: c.Request.Context, Reading: c.Request.Reading,
		AllowPartial: c.Request.AllowPartial, TextOnly: c.Request.Mode == rerank.ModeText, NeutralLast: neutral}
	for _, cand := range c.Request.Candidates {
		ic := input.Candidate{Text: cand.Text, LogScore: cand.LogScore}
		for _, s := range cand.Segments {
			ic.Segments = append(ic.Segments, input.Segment{Start: s.Start, End: s.End, Word: s.Word, Partial: s.Partial, Correction: s.Correction})
		}
		req.Candidates = append(req.Candidates, ic)
	}
	return req
}

func toRerankRequest(c selectCase) rerank.Request {
	req := rerank.Request{Context: c.Request.Context, AllowPartial: c.Request.AllowPartial, Mode: c.Request.Mode}
	for _, y := range c.Request.Reading {
		req.Reading = append(req.Reading, zhuyin.Syllable(y))
	}
	for _, cand := range c.Request.Candidates {
		h := decodeHypothesis(cand)
		req.Candidates = append(req.Candidates, h)
	}
	return req
}

func decodeHypothesis(c selectCandidate) decode.Hypothesis {
	h := decode.Hypothesis{Text: c.Text, LogScore: c.LogScore}
	for _, s := range c.Segments {
		h.Segments = append(h.Segments, decode.Seg{Word: s.Word, Start: s.Start, End: s.End, Partial: s.Partial, Correction: s.Correction})
	}
	return h
}
