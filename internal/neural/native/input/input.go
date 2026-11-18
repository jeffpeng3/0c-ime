// Package input reproduces the reference sequence model's input assembly.
//
// It is a direct port of prepare() in
// the archived reference implementation of sequence input: the prefix tree, candidate
// slots, base/tone/type/position arrays, attention mask, proj_pos and target
// rows. The package is pure Go and has no build tag, so CGO-disabled builds
// compile and test the assembly without ONNX Runtime.
package input

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// MaxCandidates mirrors ime rerank.MaxCandidates: 8 typed readings plus up to
// 8 neutral-tone expansions.
const MaxCandidates = 16

// MaxContextRunes is the confirmed-context window used by the sequence model.
const MaxContextRunes = 64

const (
	toneNeutral = 5
	toneFirst   = 1
)

var toneMarks = map[rune]int64{'ˊ': 2, 'ˇ': 3, 'ˋ': 4, '˙': toneNeutral}

// Package errors use the same fixed strings as the reference implementation so callers
// can fall back with the same classification.
var (
	ErrInvalidReading         = errors.New("invalid_reading")
	ErrUnknownBase            = errors.New("unknown_base")
	ErrInvalidToneKnown       = errors.New("invalid_tone_known")
	ErrUnsupportedUnknownTone = errors.New("unsupported_unknown_tone")
	ErrUnsupportedShape       = errors.New("unsupported_shape")
	ErrDuplicateCandidates    = errors.New("duplicate_candidates")
	ErrInvalidContext         = errors.New("invalid_context")
	ErrCandidateAlignment     = errors.New("candidate_alignment_or_score")
	ErrUnsupportedSegment     = errors.New("unsupported_segment")
	ErrIncompleteSegments     = errors.New("incomplete_segments")
	ErrUnknownCharacter       = errors.New("unknown_character")
)

// Segment is one candidate segment. Start and End are rune offsets, matching
// reference string slicing.
type Segment struct {
	Start      int
	End        int
	Word       string
	Partial    bool
	Correction bool
}

// Candidate is one scored candidate.
type Candidate struct {
	Text     string
	LogScore float64
	Segments []Segment
}

// Maps are the package vocabulary maps: base -> id and character -> id.
type Maps struct {
	Bases      map[string]int64
	Characters map[string]int64
}

// Request is the subset of rerank.Request that prepare() consumes. ToneKnownSet
// distinguishes an omitted tone_known from an explicit null; the reference rejects
// the latter.
type Request struct {
	Context           string
	Reading           []string
	Candidates        []Candidate
	AllowPartial      bool
	TextOnly          bool
	NeutralLast       bool
	ToneKnown         []bool
	ToneKnownSet      bool
	AllowUnknownTones bool
}

// Arrays is the assembled model input. The per-batch tensors are stored
// without their leading dimension 1.
type Arrays struct {
	IDs        []int64
	Bases      []int64
	Tones      []int64
	Types      []int64
	Positions  []int64
	Mask       [][]bool // L x L
	ProjPos    []int64
	TargetNode [][]int64
	TargetIDs  [][]int64
	// Shared is S, the number of shared prefix nodes. Exposed for tests.
	Shared int
}

type sound struct {
	base int64
	tone int64
}

// Prepare ports sequence_runtime.prepare() line by line.
func Prepare(req Request, maps Maps) (*Arrays, error) {
	n := len(req.Reading)
	known, err := validateToneKnown(req.ToneKnown, req.ToneKnownSet, n, req.AllowUnknownTones)
	if err != nil {
		return nil, err
	}
	if req.TextOnly {
		if n != 1 {
			return nil, ErrUnsupportedShape
		}
	} else if n < 2 || n > 32 {
		return nil, ErrUnsupportedShape
	}
	if len(req.Candidates) < 2 || len(req.Candidates) > MaxCandidates {
		return nil, ErrUnsupportedShape
	}
	seen := make(map[string]bool, len(req.Candidates))
	for _, c := range req.Candidates {
		if seen[c.Text] {
			return nil, ErrDuplicateCandidates
		}
		seen[c.Text] = true
	}
	if !utf8.ValidString(req.Context) || (req.TextOnly && req.Context == "") {
		return nil, ErrInvalidContext
	}

	encoded := make([][]int64, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		text := []rune(candidate.Text)
		if len(text) != n || math.IsNaN(candidate.LogScore) || math.IsInf(candidate.LogScore, 0) {
			return nil, ErrCandidateAlignment
		}
		end := 0
		for _, seg := range candidate.Segments {
			if seg.Start != end || seg.End <= end || seg.End > n || (seg.Partial && !req.AllowPartial) ||
				seg.Correction || seg.Word != string(text[end:seg.End]) {
				return nil, ErrUnsupportedSegment
			}
			end = seg.End
		}
		if end != n {
			return nil, ErrIncompleteSegments
		}
		ids := make([]int64, n)
		for i, r := range text {
			id, ok := maps.Characters[string(r)]
			if !ok {
				return nil, ErrUnknownCharacter
			}
			ids[i] = id
		}
		encoded = append(encoded, ids)
	}

	var sounds []sound
	if !req.TextOnly {
		sounds = make([]sound, n)
		for i, y := range req.Reading {
			s, err := syllable(y, maps.Bases)
			if err != nil {
				return nil, err
			}
			if !known[i] {
				s.tone = 0
			}
			sounds[i] = s
		}
	}

	branches := [][]sound{{{base: 0, tone: 0}}}
	if !req.TextOnly {
		branches = [][]sound{{sounds[n-1], {base: 0, tone: 0}}}
		if req.NeutralLast && known[n-1] && sounds[n-1].tone != toneNeutral {
			branches = append(branches, []sound{{base: sounds[n-1].base, tone: toneNeutral}, {base: 0, tone: 0}})
		}
	}

	contextRunes := []rune(req.Context)
	if len(contextRunes) > MaxContextRunes {
		contextRunes = contextRunes[len(contextRunes)-MaxContextRunes:]
	}
	head := make([]int64, 0, len(contextRunes)+2)
	head = append(head, 1) // [BOS]
	for _, r := range contextRunes {
		if id, ok := maps.Characters[string(r)]; ok {
			head = append(head, id)
		} else {
			head = append(head, 4) // [UNK], same as the training data
		}
	}
	head = append(head, 2) // [SEP]

	shared := sounds
	if req.TextOnly {
		shared = nil
	} else {
		shared = sounds[:n-1]
	}
	ids := append([]int64(nil), head...)
	baseIDs := make([]int64, len(head), len(head)+len(shared)+32)
	tones := make([]int64, len(head), len(head)+len(shared)+32)
	types := make([]int64, len(head), len(head)+len(shared)+32)
	for range shared {
		ids = append(ids, 0)
		baseIDs = append(baseIDs, 0)
		tones = append(tones, 0)
		types = append(types, 1)
	}
	for i := range shared {
		baseIDs[len(head)+i] = shared[i].base
		tones[len(head)+i] = shared[i].tone
	}

	positions := make([]int64, len(ids))
	for i := range positions {
		positions[i] = int64(i)
	}
	S := len(ids)
	parent := map[int]int{}
	var nodes []map[string]int

	for _, branch := range branches {
		prev := -1
		for k, s := range branch {
			if prev >= 0 {
				parent[len(ids)] = prev
			}
			prev = len(ids)
			last := k == len(branch)-1
			id, typ := int64(0), int64(1)
			if last {
				id, typ = 3, 0
			}
			ids = append(ids, id)
			baseIDs = append(baseIDs, s.base)
			tones = append(tones, s.tone)
			types = append(types, typ)
			positions = append(positions, int64(S+k))
		}
		P := S + len(branch)
		node := map[string]int{treeKey(nil, 0): prev}
		for _, cids := range encoded {
			for i := 1; i < n; i++ {
				key := treeKey(cids, i)
				if _, ok := node[key]; !ok {
					node[key] = len(ids)
					parent[len(ids)] = node[treeKey(cids, i-1)]
					ids = append(ids, cids[i-1])
					baseIDs = append(baseIDs, 0)
					tones = append(tones, 0)
					types = append(types, 2)
					positions = append(positions, int64(P+i-1))
				}
			}
		}
		nodes = append(nodes, node)
	}

	L := len(ids)
	mask := make([][]bool, L)
	for q := range mask {
		mask[q] = make([]bool, L)
		if q < S {
			for a := 0; a <= q; a++ {
				mask[q][a] = true
			}
		}
	}
	for q := S; q < L; q++ {
		for a := 0; a < S; a++ {
			mask[q][a] = true
		}
		a := q
		for a >= 0 {
			mask[q][a] = true
			next, ok := parent[a]
			if !ok {
				break
			}
			a = next
		}
	}

	projSet := map[int]bool{}
	for _, node := range nodes {
		for _, v := range node {
			projSet[v] = true
		}
	}
	proj := make([]int64, 0, len(projSet))
	for v := range projSet {
		proj = append(proj, int64(v))
	}
	sortInt64(proj)
	index := make(map[int]int, len(proj))
	for i, p := range proj {
		index[int(p)] = i
	}
	targetNode := make([][]int64, 0, len(nodes)*len(encoded))
	targetIDs := make([][]int64, 0, len(nodes)*len(encoded))
	for _, node := range nodes {
		for _, cids := range encoded {
			rows := make([]int64, n)
			for i := 0; i < n; i++ {
				rows[i] = int64(index[node[treeKey(cids, i)]])
			}
			targetNode = append(targetNode, rows)
			idsRow := make([]int64, n)
			for i, id := range cids {
				idsRow[i] = id - 6
			}
			targetIDs = append(targetIDs, idsRow)
		}
	}

	return &Arrays{IDs: ids, Bases: baseIDs, Tones: tones, Types: types, Positions: positions,
		Mask: mask, ProjPos: proj, TargetNode: targetNode, TargetIDs: targetIDs, Shared: S}, nil
}

func validateToneKnown(toneKnown []bool, set bool, count int, allowUnknown bool) ([]bool, error) {
	if !set {
		known := make([]bool, count)
		for i := range known {
			known[i] = true
		}
		return known, nil
	}
	if len(toneKnown) != count {
		return nil, ErrInvalidToneKnown
	}
	if !allowUnknown {
		for _, ok := range toneKnown {
			if !ok {
				return nil, ErrUnsupportedUnknownTone
			}
		}
	}
	return toneKnown, nil
}

func syllable(y string, bases map[string]int64) (sound, error) {
	if y == "" {
		return sound{}, ErrInvalidReading
	}
	runes := []rune(y)
	last := runes[len(runes)-1]
	tone := int64(toneFirst)
	base := y
	if t, ok := toneMarks[last]; ok {
		tone = t
		base = string(runes[:len(runes)-1])
	}
	baseID, ok := bases[base]
	if !ok {
		return sound{}, ErrUnknownBase
	}
	return sound{base: baseID, tone: tone}, nil
}

// treeKey encodes the first n character ids. The key only needs to be stable
// within one call, matching the reference tuple prefix keys.
func treeKey(ids []int64, n int) string {
	if n == 0 {
		return ""
	}
	b := make([]byte, 8*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint64(b[i*8:], uint64(ids[i]))
	}
	return string(b)
}

func sortInt64(v []int64) {
	// Insertion sort: proj is tiny (branches only, at most 2 entries).
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
