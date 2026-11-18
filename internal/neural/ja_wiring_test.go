package neural

import (
	"context"
	"errors"
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/rerank"
	"github.com/0c-biggo/0c-ime/core/reading"
)

func TestJAVerifierScoresCandidatesThroughManager(t *testing.T) {
	loaded := &Loaded{
		ScoreTexts: func(_ context.Context, left string, candidates []string) ([]float64, error) {
			if left != "前" {
				t.Fatalf("left context = %q", left)
			}
			out := make([]float64, len(candidates))
			for i := range candidates {
				out[i] = float64(i)
			}
			return out, nil
		},
		Quality: "trained",
		Close:   func() error { return nil },
		State:   func() string { return "ready" },
	}
	m, err := OpenManager(ManagerConfig{Mode: ModeAll, Loader: func(context.Context, ModelSpec, *Prepared) (*Loaded, error) {
		return loaded, nil
	}}, ModelSpec{Name: "ja-rerank", Language: "ja", Role: RoleRerank})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitFor(t, "ja rerank load", func() bool { return m.Residency().Loaded == 1 })

	v := NewJAVerifier(m.Handle("ja-rerank"))
	if v.Handle.Quality() != "trained" {
		t.Fatalf("quality = %q", v.Handle.Quality())
	}
	seq := reading.Sequence{reading.Unit("き"), reading.Unit("ょ"), reading.Unit("う")}
	result, err := v.Verify(context.Background(), rerank.Request{
		LeftContext: "前",
		Reading:     seq,
		Candidates: []rerank.Candidate{
			{Surface: "今日", Reading: seq},
			{Surface: "京", Reading: seq},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Verdicts) != 2 || result.Verdicts[0].Score != 0 || result.Verdicts[1].Score != 1 {
		t.Fatalf("verdicts = %+v", result.Verdicts)
	}
}

func TestJAVerifierMapsUnavailableAndTimeout(t *testing.T) {
	if _, err := NewJAVerifier(nil).Verify(context.Background(), rerank.Request{}); !errors.Is(err, rerank.ErrUnavailable) {
		t.Fatalf("nil handle: %v", err)
	}
	loaded := &Loaded{ScoreTexts: func(context.Context, string, []string) ([]float64, error) {
		return nil, context.Canceled
	}, Close: func() error { return nil }, State: func() string { return "ready" }}
	m, err := OpenManager(ManagerConfig{Mode: ModeAll, Loader: func(context.Context, ModelSpec, *Prepared) (*Loaded, error) {
		return loaded, nil
	}}, ModelSpec{Name: "ja-rerank", Language: "ja", Role: RoleRerank})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitFor(t, "ja rerank load", func() bool { return m.Residency().Loaded == 1 })
	if _, err := NewJAVerifier(m.Handle("ja-rerank")).Verify(context.Background(), rerank.Request{}); !errors.Is(err, rerank.ErrTimeout) {
		t.Fatalf("canceled backend: %v", err)
	}
	if _, err := NewJAVerifier(m.Handle("missing")).Verify(context.Background(), rerank.Request{}); !errors.Is(err, rerank.ErrUnavailable) {
		t.Fatalf("missing handle: %v", err)
	}
}
