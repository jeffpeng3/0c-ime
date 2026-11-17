package score_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/score"
	"github.com/0c-biggo/0c-ime/core/segment"
)

var (
	_ score.Scorer      = fakeScorer{}
	_ score.Calibrator  = fakeCalibrator{}
	_ score.Arbitrator  = fakeArbitrator{}
	_ score.Arbitration = fakeArbitrator{}
)

func TestScorerContract(t *testing.T) {
	req := score.Request{
		Context: "前文",
		Candidates: []lattice.Candidate{
			{Segment: segment.Segment{Surface: "今日", Language: lang.Tag(lang.Ja)}},
			{Segment: segment.Segment{Surface: "京", Language: lang.Tag(lang.Ja)}},
		},
	}
	got, err := fakeScorer{}.Score(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scores = %v, want %v", got, want)
	}
	if langs := (fakeScorer{}).Languages(); len(langs) != 2 || langs[0] != lang.Zh || langs[1] != lang.Ja {
		t.Fatalf("Languages = %v", langs)
	}

	_, err = fakeScorer{unavailable: true}.Score(context.Background(), req)
	if !errors.Is(err, score.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestCalibratorContract(t *testing.T) {
	c := fakeCalibrator{}
	if got := c.Calibrate(2); got != 2 {
		t.Errorf("Calibrate(2) = %v, want 2", got)
	}
}

func TestArbitratorContract(t *testing.T) {
	features := score.Features{
		PrimaryLanguage:  lang.Ja,
		PreviousLanguage: lang.Tag(lang.Zh),
		Language:         lang.Tag(lang.Ja).WithConfidence(0.9),
		ModelScore:       0.8,
		DictionaryCost:   1.2,
		FuzzyCost:        0.1,
		SwitchCost:       0.3,
		UserHistory:      0.4,
	}
	got := fakeArbitrator{}.Arbitrate(features)
	want := 0.8 + 0.4 - 1.2 - 0.1 - 0.3
	if got.Reject || got.Reason != "ok" || math.Abs(got.Score-want) > 1e-9 {
		t.Fatalf("Decision = %#v, want score %v", got, want)
	}
}

type fakeScorer struct{ unavailable bool }

func (fakeScorer) Languages() []lang.ID { return []lang.ID{lang.Zh, lang.Ja} }

func (s fakeScorer) Score(_ context.Context, req score.Request) ([]float64, error) {
	if s.unavailable {
		return nil, score.ErrUnavailable
	}
	out := make([]float64, len(req.Candidates))
	for i := range out {
		out[i] = float64(i)
	}
	return out, nil
}

type fakeCalibrator struct{}

func (fakeCalibrator) Calibrate(raw float64) float64 { return raw }

type fakeArbitrator struct{}

func (fakeArbitrator) Arbitrate(f score.Features) score.Decision {
	return score.Decision{
		Score:  f.ModelScore + f.UserHistory - f.DictionaryCost - f.FuzzyCost - f.SwitchCost,
		Reason: "ok",
	}
}
