package score

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/decode"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	rd "github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/core/segment"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

type recordingZhScorer struct {
	calls []rerank.Request
	fn    func(rerank.Request) ([]float64, error)
}

func (s *recordingZhScorer) Score(request rerank.Request) ([]float64, error) {
	s.calls = append(s.calls, request)
	if s.fn != nil {
		return s.fn(request)
	}
	out := make([]float64, len(request.Candidates))
	for i := range out {
		out[i] = float64(i) - 10
	}
	return out, nil
}

func zhCandidate(surface, reading string, id lang.ID) lattice.Candidate {
	var units rd.Sequence
	for _, unit := range strings.Fields(reading) {
		units = append(units, rd.Unit(unit))
	}
	return lattice.Candidate{
		Segment: segment.Segment{
			Reading:  units,
			Surface:  surface,
			Language: lang.Tag(id),
			Variant:  lang.VariantZhHant,
		},
	}
}

func TestZhAdapterLanguages(t *testing.T) {
	adapter := NewZhAdapter(&recordingZhScorer{})
	if got := adapter.Languages(); !reflect.DeepEqual(got, []lang.ID{lang.Zh}) {
		t.Fatalf("Languages = %v, want [zh]", got)
	}
}

func TestZhAdapterConvertsAndScores(t *testing.T) {
	scorer := &recordingZhScorer{fn: func(rerank.Request) ([]float64, error) {
		return []float64{-1, -2, -3}, nil
	}}
	adapter := NewZhAdapter(scorer)
	adapter.AllowPartial = true
	contextText := strings.Repeat("文", 70)
	req := Request{
		Context: contextText,
		Candidates: []lattice.Candidate{
			zhCandidate("今天", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
			zhCandidate("金田", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
			zhCandidate("巾田", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
		},
	}

	got, err := adapter.Score(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{-1, -2, -3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scores = %v, want %v", got, want)
	}
	if len(scorer.calls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(scorer.calls))
	}
	call := scorer.calls[0]
	if !call.AllowPartial {
		t.Error("AllowPartial was not forwarded")
	}
	if want := []zhuyin.Syllable{"ㄐㄧㄣ", "ㄊㄧㄢ"}; !reflect.DeepEqual(call.Reading, want) {
		t.Errorf("Reading = %v, want %v", call.Reading, want)
	}
	if want := []rune(contextText)[len([]rune(contextText))-rerank.MaxContextRunes:]; !reflect.DeepEqual([]rune(call.Context), want) {
		t.Errorf("Context was not truncated to the last %d runes", rerank.MaxContextRunes)
	}
	for i, row := range call.Candidates {
		if row.Text != req.Candidates[i].Surface || row.LogScore != 0 {
			t.Errorf("candidate %d = %#v", i, row)
		}
		if len(row.Segments) != 1 || row.Segments[0] != (decode.Seg{Word: row.Text, Start: 0, End: len([]rune(row.Text))}) {
			t.Errorf("candidate %d segments = %#v", i, row.Segments)
		}
	}
}

func TestZhAdapterMixedLanguagesKeepsOrder(t *testing.T) {
	scorer := &recordingZhScorer{fn: func(rerank.Request) ([]float64, error) {
		return []float64{1, 2}, nil
	}}
	adapter := NewZhAdapter(scorer)
	candidates := []lattice.Candidate{
		zhCandidate("今天", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
		{Segment: segment.Segment{Surface: "きょう", Reading: rd.Sequence{"き", "ょ", "う"}, Language: lang.Tag(lang.Ja), Variant: lang.VariantJa}},
		zhCandidate("金田", "ㄐㄧㄣ ㄊㄧㄢ", lang.Und),
		{Segment: segment.Segment{Surface: "kubectl", Language: lang.Tag(lang.En), Variant: lang.VariantEn}},
	}

	got, err := adapter.Score(context.Background(), Request{Candidates: candidates})
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{1, ZhNeutralScore, 2, ZhNeutralScore}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scores = %v, want %v", got, want)
	}
	if len(scorer.calls) != 1 || len(scorer.calls[0].Candidates) != 2 {
		t.Fatalf("model calls = %#v, want one call with two Chinese candidates", scorer.calls)
	}
}

func TestZhAdapterSeparateReadings(t *testing.T) {
	scorer := &recordingZhScorer{fn: func(request rerank.Request) ([]float64, error) {
		switch zhuyin.Key(request.Reading) {
		case "ㄐㄧㄣ ㄊㄧㄢ":
			return []float64{1, 2}, nil
		case "ㄇㄧㄥˊ ㄊㄧㄢ":
			return []float64{3, 4}, nil
		}
		return nil, errors.New("unexpected reading")
	}}
	adapter := NewZhAdapter(scorer)
	req := Request{Candidates: []lattice.Candidate{
		zhCandidate("今天", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
		zhCandidate("明天", "ㄇㄧㄥˊ ㄊㄧㄢ", lang.Zh),
		zhCandidate("金田", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
		zhCandidate("名天", "ㄇㄧㄥˊ ㄊㄧㄢ", lang.Zh),
	}}

	got, err := adapter.Score(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{1, 3, 2, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scores = %v, want %v", got, want)
	}
	if len(scorer.calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(scorer.calls))
	}
}

func TestZhAdapterDeduplicatesSurface(t *testing.T) {
	scorer := &recordingZhScorer{fn: func(request rerank.Request) ([]float64, error) {
		if len(request.Candidates) != 2 {
			t.Fatalf("model candidates = %d, want 2 unique", len(request.Candidates))
		}
		return []float64{7, 8}, nil
	}}
	adapter := NewZhAdapter(scorer)
	first := zhCandidate("今天", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh)
	first.Source = lattice.SourceDictionary
	again := first
	again.Source = lattice.SourcePersonal
	req := Request{Candidates: []lattice.Candidate{
		first,
		zhCandidate("金田", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
		again,
	}}

	got, err := adapter.Score(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{7, 8, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scores = %v, want %v", got, want)
	}
}

func TestZhAdapterSingleSyllableTextMode(t *testing.T) {
	scorer := &recordingZhScorer{}
	adapter := NewZhAdapter(scorer)
	req := Request{
		Context: "前文",
		Candidates: []lattice.Candidate{
			zhCandidate("今", "ㄐㄧㄣ", lang.Zh),
			zhCandidate("金", "ㄐㄧㄣ", lang.Zh),
		},
	}
	if _, err := adapter.Score(context.Background(), req); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable without text mode", err)
	}
	if len(scorer.calls) != 0 {
		t.Fatalf("model calls = %d, want 0", len(scorer.calls))
	}

	adapter.TextMode = true
	if _, err := adapter.Score(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(scorer.calls) != 1 || scorer.calls[0].Mode != rerank.ModeText {
		t.Fatalf("mode = %q, want text", scorer.calls[0].Mode)
	}
}

func TestZhAdapterContextModes(t *testing.T) {
	scorer := &recordingZhScorer{}
	adapter := NewZhAdapter(scorer)
	req := Request{
		Context: "abcdef",
		Candidates: []lattice.Candidate{
			zhCandidate("今天", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
			zhCandidate("金田", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
		},
	}
	for _, tc := range []struct {
		limit int
		want  string
	}{
		{limit: 3, want: "def"},
		{limit: -1, want: ""},
		{limit: 0, want: "abcdef"},
	} {
		adapter.ContextRunes = tc.limit
		if _, err := adapter.Score(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		if got := scorer.calls[len(scorer.calls)-1].Context; got != tc.want {
			t.Errorf("limit %d context = %q, want %q", tc.limit, got, tc.want)
		}
	}
}

func TestZhAdapterFallback(t *testing.T) {
	valid := []lattice.Candidate{
		zhCandidate("今天", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
		zhCandidate("金田", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
	}
	nonZh := []lattice.Candidate{
		{Segment: segment.Segment{Surface: "きょう", Reading: rd.Sequence{"き", "ょ", "う"}, Language: lang.Tag(lang.Ja), Variant: lang.VariantJa}},
		{Segment: segment.Segment{Surface: "kubectl", Language: lang.Tag(lang.En), Variant: lang.VariantEn}},
	}
	badSyllable := []lattice.Candidate{
		zhCandidate("きょう", "き ょ う", lang.Und),
		zhCandidate("きよう", "き よ う", lang.Und),
	}
	wrongLength := []lattice.Candidate{
		zhCandidate("今天", "ㄐㄧㄣ", lang.Zh),
		zhCandidate("金田", "ㄐㄧㄣ", lang.Zh),
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tc := range []struct {
		name    string
		adapter *ZhAdapter
		ctx     context.Context
		req     Request
	}{
		{name: "nil scorer", adapter: NewZhAdapter(nil), ctx: context.Background(), req: Request{Candidates: valid}},
		{name: "scorer error", adapter: NewZhAdapter(&recordingZhScorer{fn: func(rerank.Request) ([]float64, error) {
			return nil, errors.New("worker down")
		}}), ctx: context.Background(), req: Request{Candidates: valid}},
		{name: "no chinese candidates", adapter: NewZhAdapter(&recordingZhScorer{}), ctx: context.Background(), req: Request{Candidates: nonZh}},
		{name: "kana reading", adapter: NewZhAdapter(&recordingZhScorer{}), ctx: context.Background(), req: Request{Candidates: badSyllable}},
		{name: "surface reading mismatch", adapter: NewZhAdapter(&recordingZhScorer{}), ctx: context.Background(), req: Request{Candidates: wrongLength}},
		{name: "single candidate", adapter: NewZhAdapter(&recordingZhScorer{}), ctx: context.Background(), req: Request{Candidates: valid[:1]}},
		{name: "score count mismatch", adapter: NewZhAdapter(&recordingZhScorer{fn: func(rerank.Request) ([]float64, error) {
			return []float64{1}, nil
		}}), ctx: context.Background(), req: Request{Candidates: valid}},
		{name: "non-finite score", adapter: NewZhAdapter(&recordingZhScorer{fn: func(rerank.Request) ([]float64, error) {
			return []float64{math.NaN(), 1}, nil
		}}), ctx: context.Background(), req: Request{Candidates: valid}},
		{name: "canceled context", adapter: NewZhAdapter(&recordingZhScorer{}), ctx: canceled, req: Request{Candidates: valid}},
		{name: "invalid context", adapter: NewZhAdapter(&recordingZhScorer{}), ctx: context.Background(), req: Request{Context: string([]byte{0xff}), Candidates: valid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.adapter.Score(tc.ctx, tc.req)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestZhAdapterDoesNotMutateRequest(t *testing.T) {
	req := Request{
		Context: "前文",
		Candidates: []lattice.Candidate{
			zhCandidate("今天", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
			zhCandidate("金田", "ㄐㄧㄣ ㄊㄧㄢ", lang.Zh),
		},
	}
	before := Request{
		Context:    req.Context,
		Candidates: append([]lattice.Candidate(nil), req.Candidates...),
	}
	if _, err := NewZhAdapter(&recordingZhScorer{}).Score(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req, before) {
		t.Fatalf("request mutated:\n got %#v\nwant %#v", req, before)
	}
}

func TestZhAdapterEmptyCandidates(t *testing.T) {
	got, err := NewZhAdapter(&recordingZhScorer{}).Score(context.Background(), Request{})
	if err != nil || len(got) != 0 {
		t.Fatalf("Score = %v, %v; want empty and nil", got, err)
	}
}
