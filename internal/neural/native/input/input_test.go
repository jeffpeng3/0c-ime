package input

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type goldenFile struct {
	Bases      []string `json:"bases"`
	Characters []string `json:"characters"`
	Cases      []struct {
		Name    string `json:"name"`
		Request struct {
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
		} `json:"request"`
		Arrays struct {
			IDs        []int64   `json:"ids"`
			Bases      []int64   `json:"bases"`
			Tones      []int64   `json:"tones"`
			Types      []int64   `json:"types"`
			Positions  []int64   `json:"positions"`
			Mask       [][]bool  `json:"mask"`
			ProjPos    []int64   `json:"proj_pos"`
			TargetNode [][]int64 `json:"target_node"`
			TargetIDs  [][]int64 `json:"target_ids"`
		} `json:"arrays"`
	} `json:"cases"`
}

// TestPythonGolden compares the Go port with arrays produced by
// sequence_runtime.prepare(). Regenerate with the parity oracle when the
// Python assembly changes.
func TestPythonGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden goldenFile
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	maps := Maps{Bases: map[string]int64{}, Characters: map[string]int64{}}
	for i, b := range golden.Bases {
		maps.Bases[b] = int64(i + 2)
	}
	for i, c := range golden.Characters {
		maps.Characters[c] = int64(i + 8)
	}
	for _, tc := range golden.Cases {
		req := Request{Context: tc.Request.Context, Reading: tc.Request.Reading,
			AllowPartial: tc.Request.AllowPartial, TextOnly: tc.Request.TextOnly,
			NeutralLast: tc.Request.NeutralLast, ToneKnown: tc.Request.ToneKnown,
			ToneKnownSet: tc.Request.ToneKnownSet, AllowUnknownTones: tc.Request.AllowUnknownTones}
		for _, c := range tc.Request.Candidates {
			candidate := Candidate{Text: c.Text, LogScore: c.LogScore}
			for _, s := range c.Segments {
				candidate.Segments = append(candidate.Segments, Segment{Start: s.Start, End: s.End,
					Word: s.Word, Partial: s.Partial, Correction: s.Correction})
			}
			req.Candidates = append(req.Candidates, candidate)
		}
		got, err := Prepare(req, maps)
		if err != nil {
			t.Fatalf("%s: %v", tc.Name, err)
		}
		if !reflect.DeepEqual(got.IDs, tc.Arrays.IDs) {
			t.Errorf("%s: ids = %v, want %v", tc.Name, got.IDs, tc.Arrays.IDs)
		}
		if !reflect.DeepEqual(got.Bases, tc.Arrays.Bases) {
			t.Errorf("%s: bases = %v, want %v", tc.Name, got.Bases, tc.Arrays.Bases)
		}
		if !reflect.DeepEqual(got.Tones, tc.Arrays.Tones) {
			t.Errorf("%s: tones = %v, want %v", tc.Name, got.Tones, tc.Arrays.Tones)
		}
		if !reflect.DeepEqual(got.Types, tc.Arrays.Types) {
			t.Errorf("%s: types = %v, want %v", tc.Name, got.Types, tc.Arrays.Types)
		}
		if !reflect.DeepEqual(got.Positions, tc.Arrays.Positions) {
			t.Errorf("%s: positions = %v, want %v", tc.Name, got.Positions, tc.Arrays.Positions)
		}
		if !reflect.DeepEqual(got.Mask, tc.Arrays.Mask) {
			t.Errorf("%s: mask mismatch", tc.Name)
		}
		if !reflect.DeepEqual(got.ProjPos, tc.Arrays.ProjPos) {
			t.Errorf("%s: proj_pos = %v, want %v", tc.Name, got.ProjPos, tc.Arrays.ProjPos)
		}
		if !reflect.DeepEqual(got.TargetNode, tc.Arrays.TargetNode) {
			t.Errorf("%s: target_node = %v, want %v", tc.Name, got.TargetNode, tc.Arrays.TargetNode)
		}
		if !reflect.DeepEqual(got.TargetIDs, tc.Arrays.TargetIDs) {
			t.Errorf("%s: target_ids = %v, want %v", tc.Name, got.TargetIDs, tc.Arrays.TargetIDs)
		}
	}
}

func TestRejectsUnknownCharacter(t *testing.T) {
	maps := Maps{Bases: map[string]int64{"a": 2}, Characters: map[string]int64{"一": 8}}
	req := Request{Reading: []string{"a", "a"}, Candidates: []Candidate{
		{Text: "一い", Segments: []Segment{{Start: 0, End: 2, Word: "一い"}}},
		{Text: "一一", Segments: []Segment{{Start: 0, End: 2, Word: "一一"}}},
	}}
	if _, err := Prepare(req, maps); err != ErrUnknownCharacter {
		t.Fatalf("err = %v, want %v", err, ErrUnknownCharacter)
	}
}

func TestRejectsDuplicateCandidates(t *testing.T) {
	maps := Maps{Bases: map[string]int64{"a": 2}, Characters: map[string]int64{"一": 8}}
	req := Request{Reading: []string{"a", "a"}, Candidates: []Candidate{
		{Text: "一一", Segments: []Segment{{Start: 0, End: 2, Word: "一一"}}},
		{Text: "一一", Segments: []Segment{{Start: 0, End: 2, Word: "一一"}}},
	}}
	if _, err := Prepare(req, maps); err != ErrDuplicateCandidates {
		t.Fatalf("err = %v, want %v", err, ErrDuplicateCandidates)
	}
}
