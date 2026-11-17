// Package spm is a pure-Go SentencePiece unigram encoder.
//
// It implements only what the 0c-ja public-core tokenizer actually uses:
// a unigram model with an identity normalizer and byte fallback. Models with a
// precompiled charsmap are rejected instead of being encoded incorrectly.
//
// The package has no CGO or external dependencies, so CGO_ENABLED=0 builds
// still compile. It is used by the opt-in native ONNX backend and by the
// P2-30S measurement probe.
package spm

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
)

// PieceType mirrors sentencepiece_model.proto SentencePiece.Type.
type PieceType int32

const (
	PieceNormal      PieceType = 1
	PieceUnknown     PieceType = 2
	PieceControl     PieceType = 3
	PieceUserDefined PieceType = 4
	PieceUnused      PieceType = 5
	PieceByte        PieceType = 6
)

// Piece is one entry of the model proto.
type Piece struct {
	Text  string
	Score float32
	Type  PieceType
}

// Model is the subset of sentencepiece_model.proto that the encoder reads.
type Model struct {
	Pieces []Piece

	ModelType              int32
	VocabSize              int32
	UnkID                  int32
	BosID                  int32
	EosID                  int32
	PadID                  int32
	ByteFallback           bool
	UnkSurface             string
	AddDummyPrefix         bool
	RemoveExtraWhitespaces bool
	EscapeWhitespaces      bool
	NormalizerName         string
	CharsMapLen            int
}

// ParseModel reads a serialized sentencepiece ModelProto. Normalizers that
// need a precompiled charsmap are rejected: this spike only supports the
// identity normalizer used by the public-core Japanese tokenizer.
func ParseModel(path string) (*Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseModel(data)
}

func parseModel(data []byte) (*Model, error) {
	m := &Model{
		ModelType:              1, // UNIGRAM
		UnkID:                  0,
		BosID:                  1,
		EosID:                  2,
		PadID:                  -1,
		AddDummyPrefix:         true,
		RemoveExtraWhitespaces: true,
		EscapeWhitespaces:      true,
	}
	err := walk(data, func(num, wire int, u uint64, s []byte) error {
		switch num {
		case 1: // repeated SentencePiece pieces
			p, err := parsePiece(s)
			if err != nil {
				return err
			}
			m.Pieces = append(m.Pieces, p)
		case 2: // TrainerSpec
			return walk(s, func(num, wire int, u uint64, s []byte) error {
				switch num {
				case 3:
					m.ModelType = int32(u)
				case 4:
					m.VocabSize = int32(u)
				case 35:
					m.ByteFallback = u != 0
				case 40:
					m.UnkID = int32(u)
				case 41:
					m.BosID = int32(u)
				case 42:
					m.EosID = int32(u)
				case 43:
					m.PadID = int32(u)
				case 44:
					m.UnkSurface = string(s)
				}
				return nil
			})
		case 3: // NormalizerSpec
			return walk(s, func(num, wire int, u uint64, s []byte) error {
				switch num {
				case 1:
					m.NormalizerName = string(s)
				case 2:
					m.CharsMapLen = len(s)
				case 3:
					m.AddDummyPrefix = u != 0
				case 4:
					m.RemoveExtraWhitespaces = u != 0
				case 5:
					m.EscapeWhitespaces = u != 0
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(m.Pieces) == 0 {
		return nil, fmt.Errorf("spm: model has no pieces")
	}
	if m.ModelType != 1 {
		return nil, fmt.Errorf("spm: model type %d is not unigram", m.ModelType)
	}
	if m.CharsMapLen != 0 {
		return nil, fmt.Errorf("spm: precompiled charsmap is not supported")
	}
	return m, nil
}

func parsePiece(data []byte) (Piece, error) {
	p := Piece{Type: PieceNormal}
	err := walk(data, func(num, wire int, u uint64, s []byte) error {
		switch num {
		case 1:
			p.Text = string(s)
		case 2:
			if len(s) != 4 {
				return fmt.Errorf("spm: bad score field")
			}
			p.Score = math.Float32frombits(binary.LittleEndian.Uint32(s))
		case 3:
			p.Type = PieceType(u)
		}
		return nil
	})
	return p, err
}

func uvarint(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i := 0; i < len(b) && i < 10; i++ {
		c := b[i]
		if c < 0x80 {
			return x | uint64(c)<<s, i + 1
		}
		x |= uint64(c&0x7f) << s
		s += 7
	}
	return 0, 0
}

// walk iterates protobuf fields without needing a generated schema.
func walk(b []byte, fn func(num, wire int, u uint64, s []byte) error) error {
	for len(b) > 0 {
		key, n := uvarint(b)
		if n == 0 {
			return fmt.Errorf("spm: truncated field key")
		}
		b = b[n:]
		num, wire := int(key>>3), int(key&7)
		if num == 0 {
			return fmt.Errorf("spm: invalid field number")
		}
		switch wire {
		case 0:
			v, n := uvarint(b)
			if n == 0 {
				return fmt.Errorf("spm: truncated varint")
			}
			b = b[n:]
			if err := fn(num, wire, v, nil); err != nil {
				return err
			}
		case 1:
			if len(b) < 8 {
				return fmt.Errorf("spm: truncated fixed64")
			}
			if err := fn(num, wire, 0, b[:8]); err != nil {
				return err
			}
			b = b[8:]
		case 2:
			l, n := uvarint(b)
			if n == 0 || l > uint64(len(b)-n) {
				return fmt.Errorf("spm: truncated length-delimited field")
			}
			b = b[n:]
			if err := fn(num, wire, 0, b[:l]); err != nil {
				return err
			}
			b = b[l:]
		case 5:
			if len(b) < 4 {
				return fmt.Errorf("spm: truncated fixed32")
			}
			if err := fn(num, wire, 0, b[:4]); err != nil {
				return err
			}
			b = b[4:]
		default:
			return fmt.Errorf("spm: unsupported wire type %d", wire)
		}
	}
	return nil
}

// trie is a byte trie over matchable pieces.
type trie struct {
	nodes []trieNode
}

type trieNode struct {
	children []trieChild
	id       int32 // -1 when no piece ends here
	score    float32
}

type trieChild struct {
	b    byte
	node int32
}

func newTrie() *trie {
	return &trie{nodes: []trieNode{{id: -1}}}
}

func (t *trie) insert(s string, id int32, score float32) {
	n := int32(0)
	for i := 0; i < len(s); i++ {
		ch := &t.nodes[n]
		idx := sort.Search(len(ch.children), func(j int) bool { return ch.children[j].b >= s[i] })
		if idx < len(ch.children) && ch.children[idx].b == s[i] {
			n = ch.children[idx].node
			continue
		}
		next := int32(len(t.nodes))
		t.nodes = append(t.nodes, trieNode{id: -1})
		ch = &t.nodes[n]
		ch.children = append(ch.children, trieChild{})
		copy(ch.children[idx+1:], ch.children[idx:])
		ch.children[idx] = trieChild{b: s[i], node: next}
		n = next
	}
	t.nodes[n].id = id
	t.nodes[n].score = score
}

// match appends every piece that is a prefix of s to dst.
func (t *trie) match(s string, dst []match) []match {
	n := int32(0)
	for i := 0; i < len(s); i++ {
		ch := &t.nodes[n]
		idx := sort.Search(len(ch.children), func(j int) bool { return ch.children[j].b >= s[i] })
		if idx >= len(ch.children) || ch.children[idx].b != s[i] {
			return dst
		}
		n = ch.children[idx].node
		if id := t.nodes[n].id; id >= 0 {
			dst = append(dst, match{length: i + 1, id: id, score: t.nodes[n].score})
		}
	}
	return dst
}

type match struct {
	length int // bytes
	id     int32
	score  float32
}
