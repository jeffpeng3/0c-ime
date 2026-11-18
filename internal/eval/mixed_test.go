package eval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

const mixedDir = "../../testdata/mixed"

func loadMixedFixtures(t *testing.T) []MixedCorpusEntry {
	t.Helper()
	entries, err := LoadMixedCorpusDir(mixedDir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// perfectRunner 直接照參考答案輸出，用來驗證 harness 自己在無雜訊時是全對。
func perfectRunner(cs MixedCase) (MixedPrediction, error) {
	p := MixedPrediction{Text: expectedText(cs)}
	for _, ex := range cs.Expect {
		p.Segments = append(p.Segments, MixedPredictedSegment{
			InputIndex: ex.InputIndex,
			Surface:    ex.Surface,
			Language:   ex.Language,
			Variant:    ex.Variant,
		})
	}
	return p, nil
}

func TestMixedFixturesLoad(t *testing.T) {
	entries := loadMixedFixtures(t)
	if len(entries) != 3 {
		t.Fatalf("題庫數 = %d，want 3", len(entries))
	}
	wantScenario := map[MixedScenario]bool{ScenarioA: false, ScenarioB: false, ScenarioC: false}
	total := 0
	for _, entry := range entries {
		c := entry.Corpus
		seen, known := wantScenario[c.Scenario]
		if !known {
			t.Errorf("%s: 未知情境 %q", c.Name, c.Scenario)
		} else if seen {
			t.Errorf("%s: 情境 %q 重複", c.Name, c.Scenario)
		}
		wantScenario[c.Scenario] = true
		wantCases := 4
		if c.Scenario == ScenarioA {
			wantCases = 5
		}
		if len(c.Cases) != wantCases {
			t.Errorf("%s: 案例數 = %d，want %d", c.Name, len(c.Cases), wantCases)
		}
		if len(entry.SHA256) != 64 {
			t.Errorf("%s: SHA-256 = %q", c.Name, entry.SHA256)
		}
		if c.Seed != MixedBenchSeed {
			t.Errorf("%s: seed = %d，want %d", c.Name, c.Seed, MixedBenchSeed)
		}
		total += len(c.Cases)
	}
	if total != 13 {
		t.Fatalf("總案例數 = %d，want 13", total)
	}
	for scenario, seen := range wantScenario {
		if !seen {
			t.Errorf("缺少情境 %s", scenario)
		}
	}
}

func TestMixedPerfectRun(t *testing.T) {
	for _, entry := range loadMixedFixtures(t) {
		rep := RunMixed(entry.Corpus, entry.SHA256, MixedRunnerFunc(perfectRunner))
		if rep.Schema != MixedSchema {
			t.Errorf("%s: schema = %d", entry.Corpus.Name, rep.Schema)
		}
		if rep.Corpus.SHA256 != entry.SHA256 {
			t.Errorf("%s: 報告雜湊 = %q，want %q", entry.Corpus.Name, rep.Corpus.SHA256, entry.SHA256)
		}
		if rep.Segment.N == 0 || rep.Segment.JointCorrect != rep.Segment.N {
			t.Errorf("%s: joint = %d/%d，want 全對", entry.Corpus.Name, rep.Segment.JointCorrect, rep.Segment.N)
		}
		if rep.Segment.JointRate != 1 || rep.Segment.SurfaceRate != 1 || rep.Segment.LanguageRate != 1 || rep.Segment.VariantRate != 1 {
			t.Errorf("%s: 正確率不是 1：%#v", entry.Corpus.Name, rep.Segment)
		}
		if rep.Switch.TP != rep.Switch.Expected || rep.Switch.FP != 0 || rep.Switch.FN != 0 {
			t.Errorf("%s: 切換 = %#v", entry.Corpus.Name, rep.Switch)
		}
		if rep.Switch.ExactCases != len(entry.Corpus.Cases) {
			t.Errorf("%s: switch exact = %d/%d", entry.Corpus.Name, rep.Switch.ExactCases, len(entry.Corpus.Cases))
		}
		if rep.Smoke.Cases != len(entry.Corpus.Cases) || rep.Smoke.Runs != len(entry.Corpus.Cases) {
			t.Errorf("%s: smoke 沒跑完：%#v", entry.Corpus.Name, rep.Smoke)
		}
		if rep.Smoke.CompleteCases != len(entry.Corpus.Cases) || rep.Smoke.TextCorrect != len(entry.Corpus.Cases) {
			t.Errorf("%s: smoke 完整度或文字正確數不足：%#v", entry.Corpus.Name, rep.Smoke)
		}
		if rep.Smoke.FailedRuns != 0 || rep.Smoke.MissingSegments != 0 || rep.Smoke.InvalidSegments != 0 {
			t.Errorf("%s: smoke 有失敗：%#v", entry.Corpus.Name, rep.Smoke)
		}
		if rep.Smoke.TextMatchesSegments != len(entry.Corpus.Cases) || rep.Smoke.FinalTextNonEmpty != len(entry.Corpus.Cases) {
			t.Errorf("%s: 最終文字與片段不一致：%#v", entry.Corpus.Name, rep.Smoke)
		}
		byLang := rep.Segment.ByLanguage
		if len(byLang) == 0 {
			t.Errorf("%s: 缺少分語言統計", entry.Corpus.Name)
		}
		var sum int
		for _, lm := range byLang {
			sum += lm.N
			if lm.JointCorrect != lm.N {
				t.Errorf("%s/%s: joint = %d/%d", entry.Corpus.Name, lm.Language, lm.JointCorrect, lm.N)
			}
		}
		if sum != rep.Segment.N {
			t.Errorf("%s: 分語言 n 合計 = %d，總 n = %d", entry.Corpus.Name, sum, rep.Segment.N)
		}
	}
}

func TestMixedFaultsAreVisible(t *testing.T) {
	entries := loadMixedFixtures(t)
	var a *MixedCorpus
	for _, entry := range entries {
		if entry.Corpus.Scenario == ScenarioA {
			a = entry.Corpus
		}
	}
	if a == nil {
		t.Fatal("缺少情境 A")
	}

	t.Run("wrong-surface", func(t *testing.T) {
		rep := RunMixed(a, "fixture", MixedRunnerFunc(wrongSurfaceRunner))
		got := rep.Cases[0]
		if got.TextCorrect {
			t.Error("錯字不應算文字正確")
		}
		if got.Segment.SurfaceCorrect == got.Segment.N || got.Segment.JointCorrect == got.Segment.N {
			t.Errorf("錯字沒有反映在片段指標：%#v", got.Segment)
		}
		if !got.Smoke.Complete || !got.Smoke.TextMatchesSegments {
			t.Errorf("只改詞面不應破壞 smoke：%#v", got.Smoke)
		}
		if !got.SwitchExact {
			t.Errorf("只改詞面不應影響語言切換：%#v", got.Switch)
		}
	})

	t.Run("wrong-language", func(t *testing.T) {
		rep := RunMixed(a, "fixture", MixedRunnerFunc(func(c MixedCase) (MixedPrediction, error) {
			p, _ := perfectRunner(c)
			p.Segments[2].Language = "zh" // variant 仍是 ja，形成不相容片段
			return p, nil
		}))
		got := rep.Cases[0]
		if !got.TextCorrect {
			t.Error("只改語言標籤不應改變最終文字")
		}
		if got.Segment.LanguageCorrect == got.Segment.N {
			t.Errorf("語言錯誤沒有反映：%#v", got.Segment)
		}
		if got.Smoke.InvalidSegments != 1 {
			t.Errorf("invalid segments = %d，want 1", got.Smoke.InvalidSegments)
		}
		if got.SwitchExact {
			t.Errorf("語言標錯應改變切換邊界：%#v", got.Switch)
		}
	})

	t.Run("missing-segment", func(t *testing.T) {
		rep := RunMixed(a, "fixture", MixedRunnerFunc(func(c MixedCase) (MixedPrediction, error) {
			p, _ := perfectRunner(c)
			p.Segments = p.Segments[:len(p.Segments)-1]
			p.Text = strings.TrimSuffix(p.Text, " hello")
			return p, nil
		}))
		got := rep.Cases[0]
		if got.Smoke.MissingSegments != 1 || got.Smoke.Complete {
			t.Errorf("缺段沒有被記錄：%#v", got.Smoke)
		}
		if got.TextCorrect {
			t.Error("缺段不應文字正確")
		}
		if got.Switch.FN == 0 {
			t.Errorf("缺段應造成切換漏報：%#v", got.Switch)
		}
	})

	t.Run("unknown-index", func(t *testing.T) {
		rep := RunMixed(a, "fixture", MixedRunnerFunc(func(c MixedCase) (MixedPrediction, error) {
			p, _ := perfectRunner(c)
			p.Segments = append(p.Segments, MixedPredictedSegment{
				InputIndex: 99, Surface: "?", Language: "en", Variant: "en",
			})
			return p, nil
		}))
		got := rep.Cases[0]
		if got.Smoke.UnknownIndex != 1 || got.Smoke.Complete {
			t.Errorf("未知 input_index 沒有被記錄：%#v", got.Smoke)
		}
		if got.Smoke.InvalidSegments != 0 {
			t.Errorf("未知 index 應計在 unknown_index，不是 invalid：%#v", got.Smoke)
		}
	})

	t.Run("duplicate-index", func(t *testing.T) {
		rep := RunMixed(a, "fixture", MixedRunnerFunc(func(c MixedCase) (MixedPrediction, error) {
			p, _ := perfectRunner(c)
			p.Segments = append(p.Segments, p.Segments[0])
			return p, nil
		}))
		got := rep.Cases[0]
		if got.Smoke.DuplicateSegments != 1 || got.Smoke.Complete {
			t.Errorf("重複 index 沒有被記錄：%#v", got.Smoke)
		}
	})

	t.Run("panic", func(t *testing.T) {
		rep := RunMixed(a, "fixture", MixedRunnerFunc(func(MixedCase) (MixedPrediction, error) {
			panic("boom")
		}))
		if rep.Smoke.Panics != len(a.Cases) || rep.Smoke.FailedRuns != len(a.Cases) {
			t.Errorf("panic 沒有被記成失敗：%#v", rep.Smoke)
		}
		if !strings.Contains(rep.Cases[0].Error, "panic") {
			t.Errorf("panic 錯誤訊息 = %q", rep.Cases[0].Error)
		}
	})

	t.Run("nil-runner", func(t *testing.T) {
		rep := RunMixed(a, "fixture", nil)
		if rep.Smoke.FailedRuns != len(a.Cases) || rep.Smoke.Runs != 0 {
			t.Errorf("nil runner 沒有被記成失敗：%#v", rep.Smoke)
		}
	})
}

func wrongSurfaceRunner(cs MixedCase) (MixedPrediction, error) {
	p, _ := perfectRunner(cs)
	for i := range p.Segments {
		if p.Segments[i].InputIndex == 1 {
			p.Segments[i].Surface = "今添"
			p.Text = strings.Replace(p.Text, "今天", "今添", 1)
		}
	}
	return p, nil
}

func TestMixedAcceptAlternatives(t *testing.T) {
	entries := loadMixedFixtures(t)
	var a *MixedCorpus
	for _, entry := range entries {
		if entry.Corpus.Scenario == ScenarioA {
			a = entry.Corpus
		}
	}
	if a == nil {
		t.Fatal("缺少情境 A")
	}
	// 題庫本體已不再用 accept 放寬 デス；這個測試臨時加上 accept，專門驗證
	// accept 詞面會算片段正確，但不會讓 text_correct 變成 true。
	for i := range a.Cases {
		if a.Cases[i].ID == "a-canonical-mixed" {
			a.Cases[i].Expect[2].Accept = []string{"デス"}
		}
	}
	rep := RunMixed(a, "fixture", MixedRunnerFunc(func(cs MixedCase) (MixedPrediction, error) {
		p, _ := perfectRunner(cs)
		if cs.ID == "a-canonical-mixed" {
			for i := range p.Segments {
				if p.Segments[i].InputIndex == 2 {
					p.Segments[i].Surface = "デス"
					p.Text = strings.Replace(p.Text, "です", "デス", 1)
				}
			}
		}
		return p, nil
	}))
	var got MixedCaseReport
	for _, c := range rep.Cases {
		if c.ID == "a-canonical-mixed" {
			got = c
		}
	}
	if got.Segment.SurfaceCorrect != got.Segment.N || got.Segment.JointCorrect != got.Segment.N {
		t.Errorf("accept 詞面應算片段正確：%#v", got.Segment)
	}
	if got.TextCorrect {
		t.Error("accept 詞面不是主要參考文字，text_correct 應為 false")
	}
}

func TestMixedDeterministic(t *testing.T) {
	for _, entry := range loadMixedFixtures(t) {
		first := RunMixed(entry.Corpus, entry.SHA256, MixedRunnerFunc(perfectRunner))
		second := RunMixed(entry.Corpus, entry.SHA256, MixedRunnerFunc(perfectRunner))
		a, err := MixedDeterminismSignature(first)
		if err != nil {
			t.Fatal(err)
		}
		b, err := MixedDeterminismSignature(second)
		if err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Errorf("%s: 同一份題庫連跑兩次結果不同", entry.Corpus.Name)
		}
	}
}

func TestMixedValidationRejectsBrokenCorpus(t *testing.T) {
	base := loadMixedFixtures(t)[0].Corpus
	clone := func(t *testing.T) MixedCorpus {
		raw, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		var c MixedCorpus
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	tests := map[string]func(*MixedCorpus){
		"schema":           func(c *MixedCorpus) { c.Schema = 2 },
		"scenario":         func(c *MixedCorpus) { c.Scenario = "D" },
		"primary-language": func(c *MixedCorpus) { c.PrimaryLanguage = "" },
		"count":            func(c *MixedCorpus) { c.Count = 99 },
		"duplicate-id":     func(c *MixedCorpus) { c.Cases[1].ID = c.Cases[0].ID },
		"input-index":      func(c *MixedCorpus) { c.Cases[0].Expect[0].InputIndex = 1 },
		"variant-language": func(c *MixedCorpus) { c.Cases[0].Expect[1].Variant = "ja" },
		"empty-surface":    func(c *MixedCorpus) { c.Cases[0].Expect[0].Surface = "" },
		"invalid-reading":  func(c *MixedCorpus) { c.Cases[0].Input[0].Reading = reading.Sequence{reading.Unit("x")} },
		"unknown-script":   func(c *MixedCorpus) { c.Cases[0].Input[0].Script = 99 },
		"unknown-tag":      func(c *MixedCorpus) { c.Cases[0].Tags = []string{"noisy:unknown"} },
		"missing-tags":     func(c *MixedCorpus) { c.Cases[0].Tags = nil },
		"duplicate-tag":    func(c *MixedCorpus) { c.Cases[0].Tags = []string{MixedTidyTag, MixedTidyTag} },
		"negative-seed":    func(c *MixedCorpus) { c.Seed = -1 },
		"kana-without-keys": func(c *MixedCorpus) {
			c.Cases[0].Input[1].Script = segment.ScriptKana
			c.Cases[0].Input[1].Keys = ""
		},
		"zhuyin-no-reading": func(c *MixedCorpus) {
			c.Cases[0].Input[0].Script = segment.ScriptZhuyin
			c.Cases[0].Input[0].Reading = nil
		},
		"composition-split": func(c *MixedCorpus) {
			c.Cases[0].Input[0].Composition = "a"
			c.Cases[0].Input[1].Composition = "b"
			c.Cases[0].Input[2].Composition = "a"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := clone(t)
			mutate(&c)
			if err := ValidateMixedCorpus(&c); err == nil {
				t.Fatal("壞題庫沒有被拒絕")
			}
		})
	}
}

func TestMixedLoaderRejectsEmptyDir(t *testing.T) {
	if _, err := LoadMixedCorpusDir(t.TempDir()); err == nil {
		t.Fatal("空目錄應回傳錯誤")
	}
}

// 長詞合併是正確產品行為：一個輸出片段覆蓋兩個 input 時，只算一個評分片段，
// 後段不是缺段；整句文字仍要完全正確。
func TestMergedPredictionCountsAsOneSegment(t *testing.T) {
	cs := MixedCase{
		ID:   "merged",
		Tags: []string{MixedTidyTag},
		Input: []MixedInputSegment{
			{Script: segment.ScriptLatin, Text: "toukyou", Reading: reading.Sequence{"と", "う", "き", "ょ", "う"}},
			{Script: segment.ScriptLatin, Text: "daigaku", Reading: reading.Sequence{"だ", "い", "が", "く"}},
		},
		Expect: []MixedExpectSegment{
			{InputIndex: 0, Surface: "東京", Language: "ja", Variant: "ja"},
			{InputIndex: 1, Surface: "大学", Language: "ja", Variant: "ja"},
		},
	}
	runner := MixedRunnerFunc(func(MixedCase) (MixedPrediction, error) {
		return MixedPrediction{
			Text:           "東京大学",
			MergedSegments: 1,
			Merged:         []MixedMergedSpan{{First: 0, Last: 1}},
			Segments: []MixedPredictedSegment{{
				InputIndex: 0,
				Surface:    "東京大学",
				Language:   "ja",
				Variant:    "ja",
			}},
		}, nil
	})
	got := runMixedCase(cs, runner)
	if got.Smoke.MissingSegments != 0 || !got.Smoke.Complete {
		t.Fatalf("合併不應算缺段：%+v", got.Smoke)
	}
	if got.Segment.N != 1 || got.Segment.JointCorrect != 1 {
		t.Fatalf("合併應只算一個片段：%+v", got.Segment)
	}
	if !got.Smoke.TextMatchesSegments || !got.TextCorrect {
		t.Fatalf("合併文字或片段串接不正確：smoke=%+v text=%q", got.Smoke, got.Output)
	}
}
