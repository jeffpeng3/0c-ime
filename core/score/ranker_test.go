package score

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/segment"
)

func scenarioCandidate(surface string, id lang.ID, variant lang.OutputVariant, dictionary, fuzzy float64, source lattice.Source) lattice.Candidate {
	return lattice.Candidate{
		Segment: segment.Segment{
			Span:     segment.Span{Start: 0, End: utf8.RuneCountInString(surface)},
			Surface:  surface,
			Language: lang.Tag(id).WithConfidence(0.9),
			Variant:  variant,
		},
		Cost:   lattice.Cost{Dictionary: dictionary, Fuzzy: fuzzy},
		Source: source,
	}
}

func scenarioRomaji() []lattice.Candidate {
	return []lattice.Candidate{
		scenarioCandidate("私わ", lang.Ja, lang.VariantJa, 0.10, 0, lattice.SourceDictionary),
		scenarioCandidate("わたしわ", lang.Ja, lang.VariantJa, 0.25, 0, lattice.SourceDictionary),
		scenarioCandidate("watashiwa", lang.En, lang.VariantEn, 0.55, 0, lattice.SourceLiteral),
		scenarioCandidate("我わ", lang.Zh, lang.VariantZhHant, 0.90, 0, lattice.SourceDictionary),
	}
}

func scenarioZhuyin() []lattice.Candidate {
	return []lattice.Candidate{
		scenarioCandidate("大雞巴", lang.Zh, lang.VariantZhHant, 0.10, 0, lattice.SourceDictionary),
		scenarioCandidate("大几巴", lang.Zh, lang.VariantZhHant, 0.45, 0.10, lattice.SourceFuzzy),
		scenarioCandidate("だいきば", lang.Ja, lang.VariantJa, 1.20, 0, lattice.SourceDictionary),
		scenarioCandidate("dajiba", lang.En, lang.VariantEn, 0.95, 0, lattice.SourceLiteral),
	}
}

func scenarioDesu() []lattice.Candidate {
	return []lattice.Candidate{
		scenarioCandidate("です", lang.Ja, lang.VariantJa, 0.10, 0, lattice.SourceDictionary),
		scenarioCandidate("desu", lang.En, lang.VariantEn, 0.55, 0, lattice.SourceLiteral),
		scenarioCandidate("得思", lang.Zh, lang.VariantZhHant, 0.90, 0, lattice.SourceDictionary),
	}
}

func scenarioEnglishTail() []lattice.Candidate {
	return []lattice.Candidate{
		scenarioCandidate("is a good sentence", lang.En, lang.VariantEn, 0.05, 0, lattice.SourceDictionary),
		scenarioCandidate("イズ・ア・グッド・センテンス", lang.Ja, lang.VariantJa, 1.50, 0, lattice.SourceDictionary),
		scenarioCandidate("是好句子", lang.Zh, lang.VariantZhHant, 1.20, 0, lattice.SourceDictionary),
	}
}

// TestFeatureRankerScenariosAB 驗證同一串輸入在主要語言日文與中文下都得到
// 相同結果：所有語言留在同一份候選清單，主要語言只改變分數，不是 gate。
func TestFeatureRankerScenariosAB(t *testing.T) {
	steps := []struct {
		name       string
		previous   lang.ID
		candidates []lattice.Candidate
		want       string
	}{
		{name: "romaji", previous: lang.Und, candidates: scenarioRomaji(), want: "私わ"},
		{name: "zhuyin", previous: lang.Ja, candidates: scenarioZhuyin(), want: "大雞巴"},
		{name: "desu", previous: lang.Zh, candidates: scenarioDesu(), want: "です"},
		{name: "english", previous: lang.Ja, candidates: scenarioEnglishTail(), want: "is a good sentence"},
	}
	for _, scenario := range []struct {
		name    string
		primary lang.ID
	}{
		{name: "scenario-a-primary-ja", primary: lang.Ja},
		{name: "scenario-b-primary-zh", primary: lang.Zh},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ranker := NewRanker(RankerConfig{}) // 沒有模型：走統計回退
			for _, step := range steps {
				t.Run(step.name, func(t *testing.T) {
					got, err := ranker.Rank(context.Background(), RankRequest{
						Context:          "前文",
						PreviousLanguage: lang.Tag(step.previous),
						PrimaryLanguage:  scenario.primary,
						Candidates:       step.candidates,
					})
					if err != nil {
						t.Fatal(err)
					}
					if len(got) != len(step.candidates) {
						t.Fatalf("ranked %d candidates, want %d", len(got), len(step.candidates))
					}
					if winner := step.candidates[got[0].Index].Surface; winner != step.want {
						t.Errorf("winner = %q, want %q; ranking = %#v", winner, step.want, got)
					}
					seen := make([]bool, len(step.candidates))
					for _, item := range got {
						if item.Index < 0 || item.Index >= len(step.candidates) {
							t.Fatalf("index %d out of range", item.Index)
						}
						if seen[item.Index] {
							t.Fatalf("index %d appeared twice", item.Index)
						}
						seen[item.Index] = true
						if item.Reject {
							t.Errorf("candidate %d rejected; primary language must not gate", item.Index)
						}
					}
				})
			}
		})
	}
}

// TestFeatureRankerScenarioC 驗證專有名詞直接保留原文，且 core/personal 的
// 使用者歷史能讓學過的詞面在下次優先。
func TestFeatureRankerScenarioC(t *testing.T) {
	model := personal.NewModel(nil, nil)
	model.LearnLanguage(personal.LanguageSelection{Surface: "GitHub", Language: lang.En, At: 1})
	history := HistoryFunc(func(surface string, language, previous lang.ID) float64 {
		return model.LanguageCandidateScore(personal.LanguageCandidate{
			Surface:  surface,
			Language: language,
		}, previous)
	})
	ranker := NewRanker(RankerConfig{History: history})

	proper := []struct {
		name       string
		candidates []lattice.Candidate
		want       string
	}{
		{
			name: "kubectl",
			candidates: []lattice.Candidate{
				scenarioCandidate("kubectl", lang.En, lang.VariantEn, 0.10, 0, lattice.SourceLiteral),
				scenarioCandidate("くべctl", lang.Ja, lang.VariantJa, 0.90, 0, lattice.SourceDictionary),
				scenarioCandidate("库ectl", lang.Zh, lang.VariantZhHant, 0.80, 0, lattice.SourceDictionary),
			},
			want: "kubectl",
		},
		{
			name: "pCloud",
			candidates: []lattice.Candidate{
				scenarioCandidate("pCloud", lang.En, lang.VariantEn, 0.12, 0, lattice.SourceLiteral),
				scenarioCandidate("ピーグラウド", lang.Ja, lang.VariantJa, 0.95, 0, lattice.SourceDictionary),
				scenarioCandidate("p云", lang.Zh, lang.VariantZhHant, 0.85, 0, lattice.SourceDictionary),
			},
			want: "pCloud",
		},
	}
	for _, tc := range proper {
		t.Run(tc.name, func(t *testing.T) {
			got := mustRank(t, ranker, RankRequest{
				PrimaryLanguage: lang.Zh,
				Candidates:      tc.candidates,
			})
			if winner := tc.candidates[got[0].Index].Surface; winner != tc.want {
				t.Errorf("winner = %q, want %q; ranking = %#v", winner, tc.want, got)
			}
			for _, item := range got {
				if item.Reject {
					t.Errorf("candidate %d rejected", item.Index)
				}
			}
		})
	}

	t.Run("github-history", func(t *testing.T) {
		candidates := []lattice.Candidate{
			scenarioCandidate("GitHub", lang.En, lang.VariantEn, 0.50, 0, lattice.SourcePersonal),
			scenarioCandidate("GitHub", lang.En, lang.VariantEn, 0.50, 0, lattice.SourceLiteral),
			scenarioCandidate("github", lang.En, lang.VariantEn, 0.20, 0, lattice.SourceLiteral),
		}
		req := RankRequest{PrimaryLanguage: lang.Zh, Candidates: candidates}

		withoutHistory := mustRank(t, NewRanker(RankerConfig{}), req)
		if winner := candidates[withoutHistory[0].Index].Surface; winner != "github" {
			t.Fatalf("without history winner = %q, want github", winner)
		}

		got := mustRank(t, ranker, req)
		if winner := candidates[got[0].Index].Surface; winner != "GitHub" {
			t.Fatalf("with history winner = %q, want GitHub", winner)
		}
		if got[0].Index != 0 || candidates[got[0].Index].Source != lattice.SourcePersonal {
			t.Fatalf("winner = %#v, want personal GitHub", got[0])
		}
		if len(got) != len(candidates) {
			t.Fatalf("ranked %d candidates, want %d", len(got), len(candidates))
		}
	})
}

type stubScorer struct {
	langs []lang.ID
	raw   []float64
	err   error
}

func (s stubScorer) Languages() []lang.ID { return s.langs }

func (s stubScorer) Score(context.Context, Request) ([]float64, error) {
	return s.raw, s.err
}

// TestFeatureRankerMissingModelFallsBack 驗證模型不可用、回錯、長度不符或
// 非有限分數時都只跳過模型，排序仍等於完全沒有模型的統計結果。
func TestFeatureRankerMissingModelFallsBack(t *testing.T) {
	req := RankRequest{
		Context:          "前文",
		PreviousLanguage: lang.Tag(lang.Ja),
		PrimaryLanguage:  lang.Ja,
		Candidates:       scenarioZhuyin(),
	}
	want := mustRank(t, NewRanker(RankerConfig{}), req)

	models := []struct {
		name  string
		model Scorer
	}{
		{name: "nil model", model: nil},
		{name: "unavailable", model: stubScorer{langs: []lang.ID{lang.Zh, lang.Ja, lang.En}, err: ErrUnavailable}},
		{name: "worker error", model: stubScorer{langs: []lang.ID{lang.Zh}, err: errors.New("worker down")}},
		{name: "short scores", model: stubScorer{langs: []lang.ID{lang.Zh}, raw: []float64{1}}},
		{name: "non-finite score", model: stubScorer{langs: []lang.ID{lang.Zh}, raw: []float64{math.NaN(), 1, 2, 3}}},
	}
	for _, tc := range models {
		t.Run(tc.name, func(t *testing.T) {
			got := mustRank(t, NewRanker(RankerConfig{Models: []Scorer{tc.model}}), req)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("fallback ranking = %#v, want %#v", got, want)
			}
		})
	}
}

func TestFeatureRankerUsesAvailableModel(t *testing.T) {
	candidates := []lattice.Candidate{
		scenarioCandidate("模型加分", lang.En, lang.VariantEn, 0.50, 0, lattice.SourceModel),
		scenarioCandidate("統計優先", lang.En, lang.VariantEn, 0.05, 0, lattice.SourceDictionary),
	}
	req := RankRequest{Candidates: candidates}

	without := mustRank(t, NewRanker(RankerConfig{}), req)
	if winner := candidates[without[0].Index].Surface; winner != "統計優先" {
		t.Fatalf("without model winner = %q, want 統計優先", winner)
	}

	scorer := stubScorer{langs: []lang.ID{lang.En}, raw: []float64{1, 0}}
	with := mustRank(t, NewRanker(RankerConfig{Models: []Scorer{scorer}}), req)
	if winner := candidates[with[0].Index].Surface; winner != "模型加分" {
		t.Fatalf("with model winner = %q, want 模型加分", winner)
	}
}

func TestFeatureRankerStableTieBreak(t *testing.T) {
	first := scenarioCandidate("b", lang.En, lang.VariantEn, 0.20, 0, lattice.SourceLiteral)
	second := scenarioCandidate("a", lang.En, lang.VariantEn, 0.20, 0, lattice.SourceLiteral)
	ranker := NewRanker(RankerConfig{})

	got := mustRank(t, ranker, RankRequest{Candidates: []lattice.Candidate{first, second}})
	if got[0].Index != 0 || got[1].Index != 1 {
		t.Fatalf("ranking = %#v, want original order for equal scores", got)
	}

	reversed := mustRank(t, ranker, RankRequest{Candidates: []lattice.Candidate{second, first}})
	if reversed[0].Index != 0 || reversed[1].Index != 1 {
		t.Fatalf("reversed ranking = %#v, want original order for equal scores", reversed)
	}
	if second.Surface != "a" || first.Surface != "b" {
		t.Fatal("candidate order changed")
	}
}

func TestFeatureRankerRankedHasNoLanguageLabel(t *testing.T) {
	typ := reflect.TypeOf(Ranked{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		if strings.Contains(name, "lang") {
			t.Fatalf("Ranked field %q exposes a language label", typ.Field(i).Name)
		}
	}
	got := mustRank(t, NewRanker(RankerConfig{}), RankRequest{Candidates: scenarioDesu()})
	for _, item := range got {
		if strings.Contains(strings.ToLower(item.Reason), "zh") ||
			strings.Contains(strings.ToLower(item.Reason), "ja") ||
			strings.Contains(strings.ToLower(item.Reason), "en") {
			t.Fatalf("Reason = %q exposes a language label", item.Reason)
		}
	}
}

func TestFeatureRankerArbitrateFormula(t *testing.T) {
	ranker := NewRanker(RankerConfig{})
	got := ranker.Arbitrate(Features{
		PrimaryLanguage:  lang.Ja,
		PreviousLanguage: lang.Tag(lang.Ja),
		Language:         lang.Tag(lang.Ja).WithConfidence(0.5),
		ModelScore:       0.8,
		DictionaryCost:   1.2,
		FuzzyCost:        0.1,
		SwitchCost:       0,
		UserHistory:      0.4,
	})
	want := DefaultModelWeight*0.8 -
		DefaultDictionaryWeight*1.2 -
		DefaultFuzzyWeight*0.1 +
		DefaultHistoryWeight*0.4 +
		DefaultPreviousLanguageWeight +
		DefaultPrimaryLanguageWeight +
		DefaultLanguageConfidenceWeight*0.5
	if got.Reject || math.Abs(got.Score-want) > 1e-9 {
		t.Fatalf("Decision = %#v, want score %v", got, want)
	}
}

func TestFeatureRankerCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewRanker(RankerConfig{}).Rank(ctx, RankRequest{Candidates: scenarioDesu()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func mustRank(t *testing.T, ranker *Ranker, req RankRequest) []Ranked {
	t.Helper()
	got, err := ranker.Rank(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
