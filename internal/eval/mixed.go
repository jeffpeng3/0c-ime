package eval

// 混合語言 benchmark（P2-16 格式、P2-61 接上真實引擎）。
//
// 這個檔案定義題庫格式、片段正確率、語言切換與功能 smoke 指標，不執行任何
// 語言轉換器。實際引擎由 P2-61 的 MixedSessionRunner 接上；同一個 runner
// 換掉就能比較不同版本，題庫與指標不需要改。
//
// 這裡刻意沒有準確度門檻。情境 A/B/C 的數字是觀察值與回歸比較的起點，不是
// 上線門檻。P2-61 的題庫另外用 tag 標出雜訊變體，讓「乾淨」與「雜訊」的
// 分數分開看。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

// MixedSchema 是目前題庫格式版本。
const MixedSchema = 1

// MixedScenario 是產品需求定義的使用者情境。
type MixedScenario string

const (
	// ScenarioA 是主要語言日文。
	ScenarioA MixedScenario = "A"
	// ScenarioB 是主要語言中文。
	ScenarioB MixedScenario = "B"
	// ScenarioC 是專有名詞。
	ScenarioC MixedScenario = "C"
)

// Valid 回報情境代號是否已定義。
func (s MixedScenario) Valid() bool {
	switch s {
	case ScenarioA, ScenarioB, ScenarioC:
		return true
	}
	return false
}

// MixedCorpus 是單一情境的混合語言題庫。
//
// 一個檔案只放一種情境，讓報告可以直接對應 A/B/C。primary_language 是
// 該情境宣稱的主要語言，只影響之後的 prior 與切換成本，不是模式開關。
//
// seed 是題庫的固定種子。P2-61 用它決定案例的報告順序（見 MixedSeedOrder），
// 不從時間、環境或 map 迭代推導任何順序。
type MixedCorpus struct {
	Schema          int           `json:"schema"`
	Name            string        `json:"name"`
	Scenario        MixedScenario `json:"scenario"`
	PrimaryLanguage lang.ID       `json:"primary_language"`
	Seed            int           `json:"seed,omitempty"`
	License         string        `json:"license"`
	Source          string        `json:"source"`
	Notes           string        `json:"notes,omitempty"`
	Count           int           `json:"count"`
	Cases           []MixedCase   `json:"cases"`
}

// MixedTidyTag 是乾淨案例的標籤。
const MixedTidyTag = "clean"

// MixedTags 是 P2-61 允許的案例維度標籤。未知標籤直接載入失敗，避免打錯字
// 的雜訊案例悄悄混進 clean 的統計。
var MixedTags = map[string]bool{
	MixedTidyTag:       true,
	"noisy:no-space":   true,
	"noisy:mixed-case": true,
	"noisy:digits":     true,
	"noisy:long-match": true,
}

// MixedCase 是一個混合輸入與逐片段參考答案。
//
// input 依使用者實際輸入順序排列；expect 必須一個 input 對一個 expect，
// 且 input_index 依序是 0..n-1。這樣片段正確率不會被任意對齊方式影響。
//
// tags 只描述案例維度（clean 或某一種雜訊），不能用來改變評分方式。
type MixedCase struct {
	ID     string               `json:"id"`
	Notes  string               `json:"notes,omitempty"`
	Tags   []string             `json:"tags,omitempty"`
	Input  []MixedInputSegment  `json:"input"`
	Expect []MixedExpectSegment `json:"expect"`
}

// MixedInputSegment 是一段原始輸入。
//
// text 是使用者看到的按鍵或文字；reading 是正規化後的讀音單位，羅馬字片段
// 可以先留空，由 converter 決定。language_hint 只是提示，實作不得拿它擋掉
// 其他語言候選。
//
// keys 是實際送進 session 的按鍵，只有無法從 text 直接推出按鍵時才需要：
// かな片段用羅馬字按鍵，注音片段由 reading 經標準排列推出，Latin／數字／
// 符號片段直接用 text。
//
// composition 把連續的 input 片段分到同一次組字：同一個非空名稱的片段會
// 一起打完才送出，讓跨片段的長詞有機會合併。空字串代表這一段自己送出。
type MixedInputSegment struct {
	Script       segment.Script   `json:"script"`
	Text         string           `json:"text"`
	Reading      reading.Sequence `json:"reading,omitempty"`
	LanguageHint lang.ID          `json:"language_hint,omitempty"`
	Keys         string           `json:"keys,omitempty"`
	Composition  string           `json:"composition,omitempty"`
}

// MixedExpectSegment 是一段參考輸出。
//
// accept 是詞面的其他可接受寫法；語言標籤與輸出變體仍然必須完全一致。
type MixedExpectSegment struct {
	InputIndex int                `json:"input_index"`
	Surface    string             `json:"surface"`
	Accept     []string           `json:"accept,omitempty"`
	Language   lang.ID            `json:"language"`
	Variant    lang.OutputVariant `json:"variant"`
}

// MixedPrediction 是受測系統對一個 case 的輸出。
//
// MergedSegments 與 UnorderedDirectKeys 是 runner 回報的結構觀察；Merged 記錄
// 哪些輸出片段跨過多個 input（長詞合併），評分時把這些 input 視為同一個片段，
// 不再把被合併的後段當成缺段。UnorderedDirectKeys 表示使用者在組字中打到
// 沒有被引擎接收的字元（由應用程式直接插入）。
type MixedPrediction struct {
	Text                string                  `json:"text"`
	Segments            []MixedPredictedSegment `json:"segments"`
	MergedSegments      int                     `json:"merged_segments,omitempty"`
	Merged              []MixedMergedSpan       `json:"merged,omitempty"`
	UnorderedDirectKeys int                     `json:"unordered_direct_keys,omitempty"`
}

// MixedMergedSpan 是一個輸出片段覆蓋的連續 input 範圍。
//
// First 是預測片段所在的 input 索引；Last 是同一片段實際覆蓋到的最後一個
// input。First == Last 表示沒有合併，通常不會出現在清單裡。
type MixedMergedSpan struct {
	First int `json:"first"`
	Last  int `json:"last"`
}

// MixedPredictedSegment 是預測輸出中的一段，以 input_index 對回題庫。
type MixedPredictedSegment struct {
	InputIndex int                `json:"input_index"`
	Surface    string             `json:"surface"`
	Language   lang.ID            `json:"language"`
	Variant    lang.OutputVariant `json:"variant"`
}

// MixedRunner 執行一個混合語言案例。實作可以包引擎、session 或假資料。
type MixedRunner interface {
	Predict(MixedCase) (MixedPrediction, error)
}

// MixedRunnerFunc 讓函式直接當 MixedRunner。
type MixedRunnerFunc func(MixedCase) (MixedPrediction, error)

// Predict 實作 MixedRunner。
func (f MixedRunnerFunc) Predict(c MixedCase) (MixedPrediction, error) { return f(c) }

// MixedCorpusEntry 是一個載入後的題庫與檔案雜湊。
type MixedCorpusEntry struct {
	Corpus *MixedCorpus
	SHA256 string
	Path   string
}

// MixedSegmentMetrics 是逐片段正確率。
//
// Surface 只比詞面；Language 與 Variant 分開計數，才看得出「字對但語言標錯」
// 與「語言對但字形慣例錯」這兩種失敗。Joint 是三項都對。
type MixedSegmentMetrics struct {
	N               int     `json:"n"`
	SurfaceCorrect  int     `json:"surface_correct"`
	SurfaceRate     float64 `json:"surface_rate"`
	LanguageCorrect int     `json:"language_correct"`
	LanguageRate    float64 `json:"language_rate"`
	VariantCorrect  int     `json:"variant_correct"`
	VariantRate     float64 `json:"variant_rate"`
	JointCorrect    int     `json:"joint_correct"`
	JointRate       float64 `json:"joint_rate"`
	// ByLanguage 只出現在報告彙總，逐題與單一語言條目留空。
	ByLanguage []MixedLanguageMetrics `json:"by_language,omitempty"`
}

// MixedLanguageMetrics 是單一參考語言的分段結果。
type MixedLanguageMetrics struct {
	Language lang.ID `json:"language"`
	MixedSegmentMetrics
}

// MixedSwitchMetrics 是語言切換邊界的 micro 指標。
//
// 邊界以 input 片段索引定義：第 i 個邊界在 input i-1 與 i 之間。只要相鄰
// 參考片段的語言不同就是預期切換。Precision/Recall/F1 用切換邊界計算，
// 不是用最終文字長度，因此缺段或重複段不會被位移掩蓋。
type MixedSwitchMetrics struct {
	Expected   int     `json:"expected"`
	Predicted  int     `json:"predicted"`
	TP         int     `json:"tp"`
	FP         int     `json:"fp"`
	FN         int     `json:"fn"`
	Precision  float64 `json:"precision"`
	Recall     float64 `json:"recall"`
	F1         float64 `json:"f1"`
	ExactCases int     `json:"exact_cases"`
}

// MixedCaseSmoke 是一個案例的功能檢查，不評分語言品質。
type MixedCaseSmoke struct {
	Run                 bool `json:"run"`
	Panic               bool `json:"panic,omitempty"`
	Complete            bool `json:"complete"`
	FinalTextNonEmpty   bool `json:"final_text_non_empty"`
	TextMatchesSegments bool `json:"text_matches_segments"`
	MissingSegments     int  `json:"missing_segments"`
	DuplicateSegments   int  `json:"duplicate_segments"`
	UnknownIndex        int  `json:"unknown_index_segments"`
	InvalidSegments     int  `json:"invalid_segments"`
	// MergedSegments 是一個輸出片段跨過多個 input 邊界；它通常正是
	// missing_segments 的來源，分開記才看得出「合併」與「掉段」不同。
	MergedSegments int `json:"merged_segments"`
	// UnorderedDirectKeys 是組字中沒被引擎接收、由應用程式直接插入的按鍵。
	UnorderedDirectKeys int `json:"unordered_direct_keys"`
}

// MixedSmoke 是所有案例的功能 smoke 彙總。
type MixedSmoke struct {
	Cases               int `json:"cases"`
	Runs                int `json:"runs"`
	FailedRuns          int `json:"failed_runs"`
	Panics              int `json:"panics"`
	CompleteCases       int `json:"complete_cases"`
	FinalTextNonEmpty   int `json:"final_text_non_empty"`
	TextMatchesSegments int `json:"text_matches_segments"`
	TextCorrect         int `json:"text_correct"`
	MissingSegments     int `json:"missing_segments"`
	DuplicateSegments   int `json:"duplicate_segments"`
	UnknownIndex        int `json:"unknown_index_segments"`
	InvalidSegments     int `json:"invalid_segments"`
	MergedSegments      int `json:"merged_segments"`
	UnorderedDirectKeys int `json:"unordered_direct_keys"`
}

// MixedCaseReport 保留逐題證據。
type MixedCaseReport struct {
	ID          string                  `json:"id"`
	Tags        []string                `json:"tags,omitempty"`
	Reference   string                  `json:"reference"`
	Output      string                  `json:"output"`
	Segments    []MixedPredictedSegment `json:"segments,omitempty"`
	TextCorrect bool                    `json:"text_correct"`
	Segment     MixedSegmentMetrics     `json:"segment"`
	Switch      MixedSwitchMetrics      `json:"switch"`
	SwitchExact bool                    `json:"switch_exact"`
	Smoke       MixedCaseSmoke          `json:"smoke"`
	Error       string                  `json:"error,omitempty"`
	Panic       bool                    `json:"panic,omitempty"`
}

// MixedCorpusInfo 記錄報告對應的題庫與來源。
type MixedCorpusInfo struct {
	Name            string        `json:"name"`
	Scenario        MixedScenario `json:"scenario"`
	PrimaryLanguage lang.ID       `json:"primary_language"`
	License         string        `json:"license"`
	Source          string        `json:"source"`
	Notes           string        `json:"notes,omitempty"`
	SHA256          string        `json:"sha256"`
	Count           int           `json:"count"`
}

// MixedReport 是一份完整且可重現的報告。
type MixedReport struct {
	Schema  int                 `json:"schema"`
	Corpus  MixedCorpusInfo     `json:"corpus"`
	Segment MixedSegmentMetrics `json:"segment"`
	Switch  MixedSwitchMetrics  `json:"switch"`
	Smoke   MixedSmoke          `json:"smoke"`
	Cases   []MixedCaseReport   `json:"cases"`
}

// LoadMixedCorpus 讀取並驗證一個題庫檔，回傳內容與 SHA-256。
func LoadMixedCorpus(path string) (*MixedCorpus, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var c MixedCorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	if err := ValidateMixedCorpus(&c); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	return &c, hex.EncodeToString(sum[:]), nil
}

// LoadMixedCorpusDir 讀取目錄下所有 .json 題庫，依檔名排序。
//
// README、manifest（*.manifest.json）與其他副檔名會被忽略；目錄內沒有題庫
// 時回傳錯誤，避免空跑得到全 0 的美麗報告。
func LoadMixedCorpusDir(dir string) ([]MixedCorpusEntry, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]MixedCorpusEntry, 0, len(paths))
	for _, path := range paths {
		if strings.HasSuffix(path, ".manifest.json") {
			continue
		}
		c, hash, err := LoadMixedCorpus(path)
		if err != nil {
			return nil, err
		}
		out = append(out, MixedCorpusEntry{Corpus: c, SHA256: hash, Path: path})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: 找不到混合語言題庫", dir)
	}
	return out, nil
}

// ValidateMixedCorpus 檢查題庫的結構與交叉參照。
func ValidateMixedCorpus(c *MixedCorpus) error {
	if c == nil {
		return fmt.Errorf("題庫是 nil")
	}
	if c.Schema != MixedSchema {
		return fmt.Errorf("schema %d，want %d", c.Schema, MixedSchema)
	}
	if c.Name == "" || c.License == "" || c.Source == "" {
		return fmt.Errorf("name、license、source 都是必填")
	}
	if !c.Scenario.Valid() {
		return fmt.Errorf("未知情境 %q", c.Scenario)
	}
	if !lang.Tag(c.PrimaryLanguage).Valid() {
		return fmt.Errorf("primary_language %q 無效", c.PrimaryLanguage)
	}
	if c.Seed < 0 {
		return fmt.Errorf("seed %d 無效", c.Seed)
	}
	if len(c.Cases) == 0 {
		return fmt.Errorf("沒有案例")
	}
	if c.Count != 0 && c.Count != len(c.Cases) {
		return fmt.Errorf("count %d 與案例數 %d 不符", c.Count, len(c.Cases))
	}
	seen := map[string]bool{}
	for i := range c.Cases {
		cs := &c.Cases[i]
		if cs.ID == "" {
			return fmt.Errorf("第 %d 個案例缺少 id", i+1)
		}
		if seen[cs.ID] {
			return fmt.Errorf("案例 id %q 重複", cs.ID)
		}
		seen[cs.ID] = true
		if err := validateMixedCase(cs); err != nil {
			return fmt.Errorf("%s: %w", cs.ID, err)
		}
	}
	return nil
}

func validateMixedCase(cs *MixedCase) error {
	if len(cs.Input) == 0 {
		return fmt.Errorf("沒有 input")
	}
	if len(cs.Tags) == 0 {
		return fmt.Errorf("至少需要一個 tag（乾淨案例用 %q）", MixedTidyTag)
	}
	seenTag := map[string]bool{}
	for _, tag := range cs.Tags {
		if !MixedTags[tag] {
			return fmt.Errorf("未知 tag %q", tag)
		}
		if seenTag[tag] {
			return fmt.Errorf("tag %q 重複", tag)
		}
		seenTag[tag] = true
	}
	if len(cs.Expect) != len(cs.Input) {
		return fmt.Errorf("input %d 段，expect %d 段；目前格式要求一對一", len(cs.Input), len(cs.Expect))
	}
	if err := validateMixedCompositions(cs.Input); err != nil {
		return err
	}
	for i, in := range cs.Input {
		if in.Script == segment.ScriptUnknown || in.Script > segment.ScriptMixed {
			return fmt.Errorf("input %d script %q 無效", i, in.Script)
		}
		if in.Text == "" {
			return fmt.Errorf("input %d text 是空的", i)
		}
		for _, u := range in.Reading {
			if !u.Valid() {
				return fmt.Errorf("input %d reading 單位 %q 無效", i, string(u))
			}
		}
		if in.LanguageHint != lang.Und && !lang.Tag(in.LanguageHint).Valid() {
			return fmt.Errorf("input %d language_hint %q 無效", i, in.LanguageHint)
		}
		switch in.Script {
		case segment.ScriptZhuyin:
			// 注音片段由 reading 經標準排列推出按鍵；沒有 reading 就無法
			// 重現使用者實際打的鍵。
			if len(in.Reading) == 0 {
				return fmt.Errorf("input %d 是注音片段，但沒有 reading", i)
			}
		case segment.ScriptKana:
			// かな沒有通用鍵盤按鍵，必須在題庫明確記錄羅馬字按鍵。
			if in.Keys == "" {
				return fmt.Errorf("input %d 是かな片段，但沒有 keys（羅馬字按鍵）", i)
			}
		}
	}
	for i, ex := range cs.Expect {
		if ex.InputIndex != i {
			return fmt.Errorf("expect %d input_index %d；必須依序對應", i, ex.InputIndex)
		}
		if ex.Surface == "" {
			return fmt.Errorf("expect %d surface 是空的", i)
		}
		if !lang.Tag(ex.Language).Valid() {
			return fmt.Errorf("expect %d language %q 無效", i, ex.Language)
		}
		if !ex.Variant.Valid() {
			return fmt.Errorf("expect %d variant %q 無效", i, ex.Variant)
		}
		if ex.Variant.Language() != ex.Language {
			return fmt.Errorf("expect %d language %q 與 variant %q 不相容", i, ex.Language, ex.Variant)
		}
		for _, a := range ex.Accept {
			if a == "" {
				return fmt.Errorf("expect %d accept 有空字串", i)
			}
		}
	}
	return nil
}

// validateMixedCompositions 要求同一個 composition 名稱的片段必須連續。
// 不連續代表使用者中間已經送出過一次，runner 無法用同一次組字重現。
func validateMixedCompositions(input []MixedInputSegment) error {
	closed := map[string]bool{}
	prev := ""
	for i, in := range input {
		name := in.Composition
		if name == prev {
			continue
		}
		if name != "" && closed[name] {
			return fmt.Errorf("input %d 的 composition %q 不連續", i, name)
		}
		if prev != "" {
			closed[prev] = true
		}
		if name != "" && len(name) > 64 {
			return fmt.Errorf("input %d 的 composition 名稱太長", i)
		}
		prev = name
	}
	return nil
}

// RunMixed 執行一份題庫並產生報告。runner 為 nil 或發生 panic 都會記成
// 失敗案例，不會讓整個 benchmark 中斷。
func RunMixed(c *MixedCorpus, corpusHash string, runner MixedRunner) MixedReport {
	return RunMixedCases(c, c.Cases, corpusHash, runner)
}

// RunMixedCases 以呼叫端指定的案例順序執行；P2-61 的固定種子順序用它。
// 報告記錄題庫的雜湊與實際執行的案例數，不因排序改變分母。
func RunMixedCases(c *MixedCorpus, cases []MixedCase, corpusHash string, runner MixedRunner) MixedReport {
	rep := MixedReport{
		Schema: MixedSchema,
		Corpus: MixedCorpusInfo{
			Name:            c.Name,
			Scenario:        c.Scenario,
			PrimaryLanguage: c.PrimaryLanguage,
			License:         c.License,
			Source:          c.Source,
			Notes:           c.Notes,
			SHA256:          corpusHash,
			Count:           len(cases),
		},
	}
	byLanguage := map[lang.ID]MixedSegmentMetrics{}
	for _, cs := range cases {
		cr := runMixedCase(cs, runner)
		rep.Cases = append(rep.Cases, cr.MixedCaseReport)
		addSegmentMetrics(&rep.Segment, cr.Segment)
		addSwitchMetrics(&rep.Switch, cr.Switch)
		if cr.SwitchExact {
			rep.Switch.ExactCases++
		}
		addSmoke(&rep.Smoke, cr)
		for _, lm := range cr.byLanguage {
			m := byLanguage[lm.Language]
			m.N += lm.N
			m.SurfaceCorrect += lm.SurfaceCorrect
			m.LanguageCorrect += lm.LanguageCorrect
			m.VariantCorrect += lm.VariantCorrect
			m.JointCorrect += lm.JointCorrect
			byLanguage[lm.Language] = m
		}
		if cr.TextCorrect {
			rep.Smoke.TextCorrect++
		}
	}
	finalizeSegment(&rep.Segment)
	finalizeSwitch(&rep.Switch)
	rep.Segment.ByLanguage = finalizeLanguages(byLanguage)
	return rep
}

// mixedCaseMetrics 是報告內部使用的逐題結果。
type mixedCaseMetrics struct {
	MixedCaseReport
	byLanguage []MixedLanguageMetrics
}

func runMixedCase(cs MixedCase, runner MixedRunner) mixedCaseMetrics {
	cr := mixedCaseMetrics{MixedCaseReport: MixedCaseReport{
		ID:        cs.ID,
		Tags:      append([]string(nil), cs.Tags...),
		Reference: expectedText(cs),
	}}
	pred, err, panicked := safeMixedPredict(runner, cs)
	cr.Panic = panicked
	cr.Error = ""
	if err != nil {
		cr.Error = err.Error()
	}
	if err != nil || panicked {
		pred = MixedPrediction{}
	} else {
		cr.Output = pred.Text
		cr.Segments = append([]MixedPredictedSegment(nil), pred.Segments...)
	}
	cr.Smoke.MergedSegments = pred.MergedSegments
	cr.Smoke.UnorderedDirectKeys = pred.UnorderedDirectKeys

	byIndex := map[int]MixedPredictedSegment{}
	for _, p := range pred.Segments {
		if p.InputIndex < 0 || p.InputIndex >= len(cs.Expect) {
			cr.Smoke.UnknownIndex++
			continue
		}
		if _, ok := byIndex[p.InputIndex]; ok {
			cr.Smoke.DuplicateSegments++
			continue
		}
		byIndex[p.InputIndex] = p
		if !validPredictionSegment(p) {
			cr.Smoke.InvalidSegments++
		}
	}
	cr.Smoke.Run = err == nil && !panicked
	units := mixedScoringUnits(cs, byIndex, pred.Merged)
	for _, u := range units {
		if !u.ok {
			cr.Smoke.MissingSegments++
		}
	}
	cr.Smoke.Complete = cr.Smoke.Run && cr.Smoke.MissingSegments == 0 && cr.Smoke.DuplicateSegments == 0 && cr.Smoke.UnknownIndex == 0
	cr.Smoke.FinalTextNonEmpty = cr.Output != ""
	cr.Smoke.TextMatchesSegments = cr.Smoke.Complete && joinedUnitSurfaces(units) == cr.Output

	langs := map[lang.ID]MixedSegmentMetrics{}
	for _, u := range units {
		surfaceOK := u.ok && mixedUnitSurfaceMatch(u.pred, cs.Expect[u.first:u.last+1])
		langOK, variantOK := false, false
		unitLang := cs.Expect[u.first].Language
		if u.ok {
			langOK, variantOK = true, true
			for i := u.first; i <= u.last; i++ {
				if u.pred.Language != cs.Expect[i].Language {
					langOK = false
				}
				if u.pred.Variant != cs.Expect[i].Variant {
					variantOK = false
				}
				if unitLang != cs.Expect[i].Language {
					unitLang = lang.Und
				}
			}
		}
		cr.Segment.N++
		if surfaceOK {
			cr.Segment.SurfaceCorrect++
		}
		if langOK {
			cr.Segment.LanguageCorrect++
		}
		if variantOK {
			cr.Segment.VariantCorrect++
		}
		if surfaceOK && langOK && variantOK {
			cr.Segment.JointCorrect++
		}
		lm := langs[unitLang]
		lm.N++
		if surfaceOK {
			lm.SurfaceCorrect++
		}
		if langOK {
			lm.LanguageCorrect++
		}
		if variantOK {
			lm.VariantCorrect++
		}
		if surfaceOK && langOK && variantOK {
			lm.JointCorrect++
		}
		langs[unitLang] = lm
	}
	finalizeSegment(&cr.Segment)
	cr.byLanguage = finalizeLanguages(langs)

	for i := 1; i < len(cs.Expect); i++ {
		want := cs.Expect[i-1].Language != cs.Expect[i].Language
		prev, prevOK := byIndex[i-1]
		cur, curOK := byIndex[i]
		got := prevOK && curOK && prev.Language != cur.Language
		if want {
			cr.Switch.Expected++
		}
		if got {
			cr.Switch.Predicted++
		}
		switch {
		case want && got:
			cr.Switch.TP++
		case got:
			cr.Switch.FP++
		case want:
			cr.Switch.FN++
		}
	}
	finalizeSwitch(&cr.Switch)
	cr.SwitchExact = cr.Switch.TP == cr.Switch.Expected && cr.Switch.FP == 0 && cr.Switch.FN == 0
	cr.TextCorrect = cr.Output == cr.Reference
	return cr
}

// mixedScoringUnit 是一個評分單位：單一 input，或被長詞合併的一組 input。
type mixedScoringUnit struct {
	first, last int
	pred        MixedPredictedSegment
	ok          bool
}

// mixedScoringUnits 依 runner 回報的合併範圍把 input 分組。合併片段只在
// 第一個 input 有預測；後段不再算缺段。沒有合併的 input 仍是一對一單位。
func mixedScoringUnits(cs MixedCase, byIndex map[int]MixedPredictedSegment, merged []MixedMergedSpan) []mixedScoringUnit {
	mergedEnd := map[int]int{}
	for _, m := range merged {
		if m.First < 0 || m.Last <= m.First || m.Last >= len(cs.Expect) {
			continue
		}
		if _, exists := mergedEnd[m.First]; exists {
			continue
		}
		mergedEnd[m.First] = m.Last
	}
	covered := make([]bool, len(cs.Expect))
	units := make([]mixedScoringUnit, 0, len(cs.Expect))
	for i := 0; i < len(cs.Expect); {
		if covered[i] {
			i++
			continue
		}
		last := i
		p, ok := byIndex[i]
		if end, has := mergedEnd[i]; has && ok {
			last = end
		}
		for j := i; j <= last; j++ {
			covered[j] = true
		}
		units = append(units, mixedScoringUnit{first: i, last: last, pred: p, ok: ok})
		i = last + 1
	}
	return units
}

// mixedUnitSurfaceMatch 比對一個單位。合併單位的參考詞面是各 input 詞面依序
// 串接；accept 也以同樣方式組合，維持「合併只算一個片段」的定義。
func mixedUnitSurfaceMatch(pred MixedPredictedSegment, expect []MixedExpectSegment) bool {
	target := ""
	for _, ex := range expect {
		target += ex.Surface
	}
	if pred.Surface == target {
		return true
	}
	variants := []string{""}
	for _, ex := range expect {
		next := make([]string, 0, len(variants)*(1+len(ex.Accept)))
		for _, prefix := range variants {
			next = append(next, prefix+ex.Surface)
			for _, alt := range ex.Accept {
				next = append(next, prefix+alt)
			}
		}
		if len(next) > 256 {
			next = next[:256]
		}
		variants = next
	}
	return slices.Contains(variants, pred.Surface)
}

func joinedUnitSurfaces(units []mixedScoringUnit) string {
	var b strings.Builder
	for _, u := range units {
		if u.ok {
			b.WriteString(u.pred.Surface)
		}
	}
	return b.String()
}

func safeMixedPredict(runner MixedRunner, cs MixedCase) (pred MixedPrediction, err error, panicked bool) {
	if runner == nil {
		return pred, fmt.Errorf("runner 是 nil"), false
	}
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	pred, err = runner.Predict(cs)
	return pred, err, false
}

func validPredictionSegment(p MixedPredictedSegment) bool {
	if p.Surface == "" {
		return false
	}
	if !lang.Tag(p.Language).Valid() {
		return false
	}
	if !p.Variant.Valid() || p.Variant.Language() != p.Language {
		return false
	}
	return true
}

func mixedSurfaceMatch(got string, ex MixedExpectSegment) bool {
	if got == ex.Surface {
		return true
	}
	for _, a := range ex.Accept {
		if got == a {
			return true
		}
	}
	return false
}

func expectedText(cs MixedCase) string {
	var b strings.Builder
	for _, ex := range cs.Expect {
		b.WriteString(ex.Surface)
	}
	return b.String()
}

func joinedSurfaces(cs MixedCase, byIndex map[int]MixedPredictedSegment) string {
	var b strings.Builder
	for i := range cs.Expect {
		p, ok := byIndex[i]
		if !ok {
			return ""
		}
		b.WriteString(p.Surface)
	}
	return b.String()
}

func addSegmentMetrics(dst *MixedSegmentMetrics, src MixedSegmentMetrics) {
	dst.N += src.N
	dst.SurfaceCorrect += src.SurfaceCorrect
	dst.LanguageCorrect += src.LanguageCorrect
	dst.VariantCorrect += src.VariantCorrect
	dst.JointCorrect += src.JointCorrect
}

func finalizeSegment(m *MixedSegmentMetrics) {
	m.SurfaceRate = ratio(m.SurfaceCorrect, m.N)
	m.LanguageRate = ratio(m.LanguageCorrect, m.N)
	m.VariantRate = ratio(m.VariantCorrect, m.N)
	m.JointRate = ratio(m.JointCorrect, m.N)
}

func finalizeLanguages(byLanguage map[lang.ID]MixedSegmentMetrics) []MixedLanguageMetrics {
	names := make([]string, 0, len(byLanguage))
	for name := range byLanguage {
		names = append(names, string(name))
	}
	sort.Strings(names)
	out := make([]MixedLanguageMetrics, 0, len(names))
	for _, name := range names {
		m := byLanguage[lang.ID(name)]
		finalizeSegment(&m)
		out = append(out, MixedLanguageMetrics{Language: lang.ID(name), MixedSegmentMetrics: m})
	}
	return out
}

func addSwitchMetrics(dst *MixedSwitchMetrics, src MixedSwitchMetrics) {
	dst.Expected += src.Expected
	dst.Predicted += src.Predicted
	dst.TP += src.TP
	dst.FP += src.FP
	dst.FN += src.FN
}

func finalizeSwitch(m *MixedSwitchMetrics) {
	m.Precision = ratio(m.TP, m.TP+m.FP)
	m.Recall = ratio(m.TP, m.TP+m.FN)
	m.F1 = harmonic(m.Precision, m.Recall)
}

func addSmoke(dst *MixedSmoke, cr mixedCaseMetrics) {
	dst.Cases++
	if cr.Smoke.Run {
		dst.Runs++
	} else {
		dst.FailedRuns++
	}
	if cr.Panic {
		dst.Panics++
	}
	if cr.Smoke.Complete {
		dst.CompleteCases++
	}
	if cr.Smoke.FinalTextNonEmpty {
		dst.FinalTextNonEmpty++
	}
	if cr.Smoke.TextMatchesSegments {
		dst.TextMatchesSegments++
	}
	dst.MissingSegments += cr.Smoke.MissingSegments
	dst.DuplicateSegments += cr.Smoke.DuplicateSegments
	dst.UnknownIndex += cr.Smoke.UnknownIndex
	dst.InvalidSegments += cr.Smoke.InvalidSegments
	dst.MergedSegments += cr.Smoke.MergedSegments
	dst.UnorderedDirectKeys += cr.Smoke.UnorderedDirectKeys
}

// MixedDeterminismSignature 回傳報告的 JSON。目前報告沒有延遲欄位，所以
// 同一份題庫與 runner 連跑兩次必須完全相同。
func MixedDeterminismSignature(rep MixedReport) (string, error) {
	b, err := json.Marshal(rep)
	return string(b), err
}
