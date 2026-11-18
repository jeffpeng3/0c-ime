package jaeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SnapshotSchema is the version of the committed snapshot file.
const SnapshotSchema = 1

// Snapshot is a per-case output record. SHA256 covers the payload below but not
// itself, so it is stable across file formatting and contains no timestamps.
type Snapshot struct {
	Schema       int            `json:"schema"`
	Corpus       string         `json:"corpus"`
	CorpusSHA256 string         `json:"corpus_sha256"`
	Cases        []SnapshotCase `json:"cases"`
	SHA256       string         `json:"sha256"`
}

// SnapshotCase is the recorded conversion for one case.
type SnapshotCase struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Input      string   `json:"input"`
	Expect     string   `json:"expect"`
	Output     string   `json:"output"`
	Candidates []string `json:"candidates"`
	Rank       int      `json:"rank"`
}

type snapshotPayload struct {
	Schema       int            `json:"schema"`
	Corpus       string         `json:"corpus"`
	CorpusSHA256 string         `json:"corpus_sha256"`
	Cases        []SnapshotCase `json:"cases"`
}

// BuildSnapshot turns live results into a snapshot and fills its checksum.
func BuildSnapshot(corpusName, corpusSHA string, results []CaseResult) *Snapshot {
	s := &Snapshot{
		Schema:       SnapshotSchema,
		Corpus:       corpusName,
		CorpusSHA256: corpusSHA,
		Cases:        make([]SnapshotCase, 0, len(results)),
	}
	for _, r := range results {
		s.Cases = append(s.Cases, SnapshotCase{
			ID:         r.ID,
			Category:   r.Category,
			Input:      r.Input,
			Expect:     r.Expect,
			Output:     r.Output,
			Candidates: append([]string(nil), r.Candidates...),
			Rank:       r.Rank,
		})
	}
	s.SHA256 = SnapshotSHA(s)
	return s
}

// SnapshotSHA returns the stable SHA-256 of the canonical snapshot payload.
func SnapshotSHA(s *Snapshot) string {
	if s == nil {
		return ""
	}
	payload := snapshotPayload{
		Schema:       s.Schema,
		Corpus:       s.Corpus,
		CorpusSHA256: s.CorpusSHA256,
		Cases:        s.Cases,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// WriteSnapshot writes a snapshot with indentation and verifies its checksum.
func WriteSnapshot(path string, s *Snapshot) error {
	s.SHA256 = SnapshotSHA(s)
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, raw, 0644)
}

// LoadSnapshot reads a snapshot and fails closed if its checksum no longer
// matches the canonical payload.
func LoadSnapshot(path string) (*Snapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Schema != SnapshotSchema {
		return nil, fmt.Errorf("%s: schema %d, want %d", path, s.Schema, SnapshotSchema)
	}
	if got := SnapshotSHA(&s); got != s.SHA256 {
		return nil, fmt.Errorf("%s: snapshot sha256 %s does not match payload %s", path, s.SHA256, got)
	}
	return &s, nil
}

// Diff is one changed snapshot field. ID is empty for whole-snapshot fields.
type Diff struct {
	ID    string
	Field string
	Want  string
	Got   string
}

// CompareSnapshots returns changes sorted by case ID and then field. A checksum
// or corpus change is reported even when all cases still match.
func CompareSnapshots(want, got *Snapshot) []Diff {
	var diffs []Diff
	if want.Schema != got.Schema {
		diffs = append(diffs, Diff{Field: "schema", Want: fmt.Sprint(want.Schema), Got: fmt.Sprint(got.Schema)})
	}
	if want.CorpusSHA256 != got.CorpusSHA256 {
		diffs = append(diffs, Diff{Field: "corpus_sha256", Want: want.CorpusSHA256, Got: got.CorpusSHA256})
	}
	if want.Corpus != got.Corpus {
		diffs = append(diffs, Diff{Field: "corpus", Want: want.Corpus, Got: got.Corpus})
	}

	byWant := make(map[string]SnapshotCase, len(want.Cases))
	byGot := make(map[string]SnapshotCase, len(got.Cases))
	for _, c := range want.Cases {
		byWant[c.ID] = c
	}
	for _, c := range got.Cases {
		byGot[c.ID] = c
	}
	ids := make([]string, 0, len(byWant)+len(byGot))
	seen := map[string]bool{}
	for id := range byWant {
		ids = append(ids, id)
		seen[id] = true
	}
	for id := range byGot {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		w, okW := byWant[id]
		g, okG := byGot[id]
		if !okW || !okG {
			diffs = append(diffs, Diff{ID: id, Field: "case", Want: present(okW), Got: present(okG)})
			continue
		}
		add := func(field, a, b string) {
			if a != b {
				diffs = append(diffs, Diff{ID: id, Field: field, Want: a, Got: b})
			}
		}
		add("category", w.Category, g.Category)
		add("input", w.Input, g.Input)
		add("expect", w.Expect, g.Expect)
		add("output", w.Output, g.Output)
		add("candidates", strings.Join(w.Candidates, " | "), strings.Join(g.Candidates, " | "))
		add("rank", fmt.Sprint(w.Rank), fmt.Sprint(g.Rank))
	}
	return diffs
}

func present(ok bool) string {
	if ok {
		return "present"
	}
	return "missing"
}

// FormatDiffs is a readable, deterministic diff for the command line.
func FormatDiffs(diffs []Diff) string {
	if len(diffs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, d := range diffs {
		switch {
		case d.ID == "":
			fmt.Fprintf(&b, "snapshot %s\n  - %s\n  + %s\n", d.Field, d.Want, d.Got)
		case d.Field == "case":
			fmt.Fprintf(&b, "case %s: %s (snapshot=%s, current=%s)\n", d.ID, d.Field, d.Want, d.Got)
		default:
			fmt.Fprintf(&b, "case %s: %s\n  - %s\n  + %s\n", d.ID, d.Field, d.Want, d.Got)
		}
	}
	return b.String()
}
