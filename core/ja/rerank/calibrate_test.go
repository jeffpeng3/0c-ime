package rerank

import "testing"

func TestCalibratePrefersInformativeSignal(t *testing.T) {
	groups := []SampleGroup{
		{Candidates: []Sample{
			{Stats: 10, Neural: -2, Draft: true, Correct: false},
			{Stats: 5, Neural: -0.1, Correct: true},
		}},
		{Candidates: []Sample{
			{Stats: 8, Neural: -3, Draft: true, Correct: false},
			{Stats: 6, Neural: -0.2, Correct: true},
		}},
	}
	got := Calibrate(groups, []Weights{
		{Neural: 0, Stats: 1, Draft: 0.5},
		{Neural: 1, Stats: 0, Draft: 0},
	})
	if got != (Weights{Neural: 1, Stats: 0, Draft: 0}) {
		t.Fatalf("Calibrate = %+v", got)
	}
}

func TestCalibrateEmptyUsesDefaults(t *testing.T) {
	if got := Calibrate(nil, nil); got != DefaultConfig().Weights {
		t.Fatalf("Calibrate(nil) = %+v", got)
	}
}
