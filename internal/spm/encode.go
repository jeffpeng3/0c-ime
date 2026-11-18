package spm

import (
	"fmt"
	"math"
	"strings"
)

// Encoder is a unigram Viterbi encoder over a parsed Model.
type Encoder struct {
	Model *Model

	trie     *trie
	pieceIDs map[string]int32
	byteIDs  [256]int32
	isUser   []bool
	minScore float32
	unkScore float32
}

// PieceToID mirrors SentencePieceProcessor::PieceToId for a single piece.
func (e *Encoder) PieceToID(piece string) int32 {
	if id, ok := e.pieceIDs[piece]; ok {
		return id
	}
	return e.Model.UnkID
}

// NewEncoder builds the trie and score tables.
func NewEncoder(m *Model) (*Encoder, error) {
	if m.ModelType != 1 {
		return nil, fmt.Errorf("spm: only unigram models are supported")
	}
	if m.CharsMapLen != 0 {
		return nil, fmt.Errorf("spm: precompiled charsmap is not supported")
	}
	e := &Encoder{
		Model:    m,
		trie:     newTrie(),
		pieceIDs: make(map[string]int32, len(m.Pieces)),
		isUser:   make([]bool, len(m.Pieces)),
		minScore: float32(math.MaxFloat32),
	}
	for i := range e.byteIDs {
		e.byteIDs[i] = -1
	}
	for i, p := range m.Pieces {
		e.pieceIDs[p.Text] = int32(i)
		if p.Type == PieceUserDefined {
			e.isUser[i] = true
		}
		// The C++ model only inserts NORMAL, USER_DEFINED and UNUSED pieces
		// into the trie, and skips UNUSED matches during encoding.
		if p.Type == PieceNormal || p.Type == PieceUserDefined || p.Type == PieceUnused {
			if p.Type != PieceUnused {
				e.trie.insert(p.Text, int32(i), p.Score)
			}
		}
		if p.Type == PieceNormal && p.Score < e.minScore {
			e.minScore = p.Score
		}
	}
	for b := 0; b < 256; b++ {
		name := fmt.Sprintf("<0x%02X>", b)
		if id, ok := e.pieceIDs[name]; ok {
			e.byteIDs[b] = id
		}
	}
	e.unkScore = e.minScore - 10.0
	return e, nil
}

// Normalize applies the identity normalizer: trim and collapse ASCII spaces,
// add the dummy prefix and escape spaces as U+2581.
func (e *Encoder) Normalize(s string) string {
	m := e.Model
	if s == "" {
		return ""
	}
	if m.RemoveExtraWhitespaces {
		s = strings.TrimLeft(s, " ")
	}
	if s == "" {
		return ""
	}
	space := " "
	if m.EscapeWhitespaces {
		space = "\xe2\x96\x81"
	}
	var b strings.Builder
	b.Grow(len(s) + len(space))
	if m.AddDummyPrefix {
		b.WriteString(space)
	}
	prevSpace := m.RemoveExtraWhitespaces
	for _, r := range s {
		if r == ' ' {
			if prevSpace {
				continue
			}
			b.WriteString(space)
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	out := b.String()
	if m.RemoveExtraWhitespaces {
		for strings.HasSuffix(out, space) {
			out = out[:len(out)-len(space)]
		}
	}
	return out
}

// NormalizeMap returns the same string as Normalize together with the source
// rune index of every normalised rune. The dummy prefix and collapsed spaces
// map to -1, since they do not correspond to a single original rune.
func (e *Encoder) NormalizeMap(s string) (string, []int) {
	m := e.Model
	if s == "" {
		return "", nil
	}
	r := []rune(s)
	first := 0
	if m.RemoveExtraWhitespaces {
		for first < len(r) && r[first] == ' ' {
			first++
		}
	}
	if first == len(r) {
		return "", nil
	}
	escape := " "
	if m.EscapeWhitespaces {
		escape = "\xe2\x96\x81"
	}
	var b strings.Builder
	b.Grow(len(s) + len(escape))
	var mapping []int
	if m.AddDummyPrefix {
		b.WriteString(escape)
		mapping = append(mapping, -1)
	}
	prevSpace := m.RemoveExtraWhitespaces
	for i := first; i < len(r); i++ {
		if r[i] == ' ' {
			if prevSpace {
				continue
			}
			b.WriteString(escape)
			mapping = append(mapping, -1)
			prevSpace = true
			continue
		}
		b.WriteRune(r[i])
		mapping = append(mapping, i)
		prevSpace = false
	}
	if m.RemoveExtraWhitespaces {
		for strings.HasSuffix(b.String(), escape) {
			str := b.String()
			b.Reset()
			b.WriteString(str[:len(str)-len(escape)])
			mapping = mapping[:len(mapping)-1]
		}
	}
	return b.String(), mapping
}

type vnode struct {
	start  int32 // rune index
	length int32 // rune count
	id     int32
	score  float32

	back float32
	prev int32 // index into nodes, -1 for the virtual BOS
}

// Token is one encoded piece. Start and End are rune offsets in the
// normalised string (see Normalize); Byte marks a byte-fallback id, where
// several ids share one source span.
type Token struct {
	ID    int32
	Start int
	End   int
	Byte  bool
}

// Encode returns the piece ids for s, mirroring SentencePieceProcessor::Encode
// with default options (no BOS/EOS, Viterbi, byte fallback when enabled).
func (e *Encoder) Encode(s string) []int32 {
	tokens, _ := e.EncodeTokens(s)
	if len(tokens) == 0 {
		return nil
	}
	out := make([]int32, len(tokens))
	for i, t := range tokens {
		out[i] = t.ID
	}
	return out
}

// EncodeTokens returns the piece ids together with their spans in the
// normalised string. Callers that insert text can map the spans back to the
// original runes with NormalizeMap.
func (e *Encoder) EncodeTokens(s string) ([]Token, string) {
	norm := e.Normalize(s)
	if norm == "" {
		return nil, norm
	}

	// Rune boundaries of the normalized string.
	runeStarts := make([]int, 0, len(norm))
	for i := range norm {
		runeStarts = append(runeStarts, i)
	}
	n := len(runeStarts)
	isBoundary := make([]bool, len(norm)+1)
	for _, off := range runeStarts {
		isBoundary[off] = true
	}
	isBoundary[len(norm)] = true

	nodesAt := make([][]int32, n+1) // nodes starting at rune position
	endsAt := make([][]int32, n+2)  // nodes ending at rune position
	nodes := make([]vnode, 0, n*4)
	var matches []match
	for pos := 0; pos < n; pos++ {
		off := runeStarts[pos]
		matches = e.trie.match(norm[off:], matches[:0])
		hasSingle := false
		for _, mt := range matches {
			if !isBoundary[off+mt.length] {
				continue
			}
			length := runeIndex(runeStarts, off+mt.length) - pos
			score := mt.score
			if e.isUser[mt.id] {
				score = 0.1 * float32(length-1)
			}
			idx := int32(len(nodes))
			nodes = append(nodes, vnode{start: int32(pos), length: int32(length), id: mt.id, score: score, prev: -1})
			nodesAt[pos] = append(nodesAt[pos], idx)
			endsAt[pos+length] = append(endsAt[pos+length], idx)
			if length == 1 {
				hasSingle = true
			}
		}
		if !hasSingle {
			idx := int32(len(nodes))
			nodes = append(nodes, vnode{start: int32(pos), length: 1, id: e.Model.UnkID, score: e.unkScore, prev: -1})
			nodesAt[pos] = append(nodesAt[pos], idx)
			endsAt[pos+1] = append(endsAt[pos+1], idx)
		}
	}

	for pos := 0; pos <= n; pos++ {
		for _, ni := range nodesAt[pos] {
			node := &nodes[ni]
			if pos == 0 {
				node.back = node.score
				continue
			}
			var best float32
			bestPrev := int32(-1)
			for _, ei := range endsAt[pos] {
				cand := nodes[ei].back + node.score
				if bestPrev < 0 || cand > best {
					best = cand
					bestPrev = ei
				}
			}
			if bestPrev < 0 {
				// Unreachable position; the lattice always has an unknown node.
				node.back = float32(math.Inf(-1))
				continue
			}
			node.back = best
			node.prev = bestPrev
		}
	}

	// Find the node reaching the end with the best score (EOS has score 0).
	last := int32(-1)
	var bestScore float32
	for _, ei := range endsAt[n] {
		if last < 0 || nodes[ei].back > bestScore {
			last = ei
			bestScore = nodes[ei].back
		}
	}
	if last < 0 {
		return nil, norm
	}
	var chain []int32
	for i := last; i >= 0; i = nodes[i].prev {
		chain = append(chain, i)
	}
	// Reverse in place.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	out := make([]Token, 0, len(chain)+4)
	for _, i := range chain {
		node := &nodes[i]
		start := int(node.start)
		end := start + int(node.length)
		if node.id != e.Model.UnkID {
			out = append(out, Token{ID: node.id, Start: start, End: end})
			continue
		}
		off := runeStarts[node.start]
		stop := len(norm)
		if end < n {
			stop = runeStarts[end]
		}
		if !e.Model.ByteFallback {
			out = append(out, Token{ID: node.id, Start: start, End: end})
			continue
		}
		for _, b := range []byte(norm[off:stop]) {
			id := e.byteIDs[b]
			if id < 0 {
				id = e.Model.UnkID
			}
			out = append(out, Token{ID: id, Start: start, End: end, Byte: true})
		}
	}
	return out, norm
}

func runeIndex(starts []int, off int) int {
	lo, hi := 0, len(starts)
	for lo < hi {
		mid := (lo + hi) / 2
		if starts[mid] < off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
