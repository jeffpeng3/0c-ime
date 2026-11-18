package eval

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

// P2-61 混合語言 benchmark：固定種子的題庫順序、可重現的 manifest、跨情境
// 彙總與雜訊分組。
//
// 這一層不產生任何模型分數。參考資料夾的 int8 ONNX 檔是 placeholder；若有
// 呼叫端把模型接進來，manifest 的 models.used 必須是 true，報告會標成
// wiring_only，不得當作品質數字引用。

const (
	// MixedBenchSchema 是 manifest 與彙總報告的格式版本。
	MixedBenchSchema = 1
	// MixedBenchSeed 是 P2-61 題庫的固定種子。
	MixedBenchSeed = 61
	// MixedBenchGenerator 記錄寫出 manifest 的工具。
	MixedBenchGenerator = "cmd/0c-eval-mixed build"
	// MixedBenchQuality 說明這份報告的數字定位。
	MixedBenchQuality = "observational baseline, not a release gate; references are lexicon preference and hand-written fixtures"
	// MixedBenchModelNote 是 placeholder 模型的固定說明。
	MixedBenchModelNote = "no model loaded: reference int8 ONNX files are placeholders; any run that loads them is wiring-only and must not be quoted as quality"
)

// MixedFixturePin 是一份題庫檔的 identity。
type MixedFixturePin struct {
	File     string        `json:"file"`
	Name     string        `json:"name"`
	Scenario MixedScenario `json:"scenario"`
	Count    int           `json:"count"`
	SHA256   string        `json:"sha256"`
}

// MixedPackPin 是日文詞典 pack 的 identity；即使執行時沒有載入也要留下，
// 讓「換了 pack」與「程式退步」在報告裡分得開。
type MixedPackPin struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	ManifestPath   string `json:"manifest_path,omitempty"`
	ManifestSHA256 string `json:"manifest_sha256,omitempty"`
	Entries        int    `json:"entries,omitempty"`
}

// MixedDataPin 是注音／中文 fixture 資料的 identity。
type MixedDataPin struct {
	Dir           string `json:"dir"`
	LexiconSHA256 string `json:"lexicon_sha256"`
	LMSHA256      string `json:"lm_sha256"`
}

// MixedModelPin 記錄模型是否參與。Used=false 時 report 的 wiring_only 也是
// false；Used=true 只能產生 wiring-only 觀察，不能寫 baseline。
type MixedModelPin struct {
	Used bool   `json:"used"`
	Note string `json:"note"`
}

// MixedBenchManifest 是 testdata/mixed/bench.manifest.json。
type MixedBenchManifest struct {
	Schema          int               `json:"schema"`
	Seed            int               `json:"seed"`
	Generator       string            `json:"generator"`
	Fixtures        []MixedFixturePin `json:"fixtures"`
	CaseOrderSHA256 string            `json:"case_order_sha256"`
	Pack            *MixedPackPin     `json:"pack,omitempty"`
	Data            *MixedDataPin     `json:"data,omitempty"`
	Models          MixedModelPin     `json:"models"`
	Quality         string            `json:"quality"`
}

// MixedSeedOrder 回傳題庫案例的固定順序：依 seed 與 case id 的 SHA-256 排序。
// 沒有任何時間、map 迭代或 Go 版本因素；同一份題庫與 seed 永遠相同。
func MixedSeedOrder(seed int, cases []MixedCase) []MixedCase {
	out := append([]MixedCase(nil), cases...)
	sort.Slice(out, func(i, j int) bool {
		a := sampledKey(seed, "mixed-order", out[i].ID)
		b := sampledKey(seed, "mixed-order", out[j].ID)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// BuildMixedBenchManifest 從題庫目錄與已載入的資料 identity 建立 manifest。
// pack 可以為 nil（只跑かな的 wiring 檢查）；data 可以為 nil。
func BuildMixedBenchManifest(dir string, seed int, pack *MixedPackPin, data *MixedDataPin, models MixedModelPin) (*MixedBenchManifest, error) {
	if seed <= 0 {
		return nil, fmt.Errorf("mixed bench: seed 必須大於 0")
	}
	entries, err := LoadMixedCorpusDir(dir)
	if err != nil {
		return nil, err
	}
	m := &MixedBenchManifest{
		Schema:    MixedBenchSchema,
		Seed:      seed,
		Generator: MixedBenchGenerator,
		Pack:      pack,
		Data:      data,
		Models:    models,
		Quality:   MixedBenchQuality,
	}
	if m.Models.Note == "" {
		m.Models.Note = MixedBenchModelNote
	}
	var order strings.Builder
	for _, entry := range entries {
		if entry.Corpus.Seed != 0 && entry.Corpus.Seed != seed {
			return nil, fmt.Errorf("%s: 題庫 seed %d 與 manifest seed %d 不符", entry.Path, entry.Corpus.Seed, seed)
		}
		m.Fixtures = append(m.Fixtures, MixedFixturePin{
			File:     filepath.Base(entry.Path),
			Name:     entry.Corpus.Name,
			Scenario: entry.Corpus.Scenario,
			Count:    len(entry.Corpus.Cases),
			SHA256:   entry.SHA256,
		})
		order.WriteString(mixedCaseOrderKey(seed, entry))
	}
	sum := sha256.Sum256([]byte(order.String()))
	m.CaseOrderSHA256 = hex.EncodeToString(sum[:])
	if err := validateMixedBenchManifest(m); err != nil {
		return nil, err
	}
	return m, nil
}

// mixedCaseOrderKey 是 manifest 的 case_order_sha256 輸入：每份題庫依固定
// 種子排序後的情境、名稱與案例 ID。
func mixedCaseOrderKey(seed int, entry MixedCorpusEntry) string {
	var b strings.Builder
	for _, cs := range MixedSeedOrder(seed, entry.Corpus.Cases) {
		b.WriteString(string(entry.Corpus.Scenario))
		b.WriteByte('\x00')
		b.WriteString(entry.Corpus.Name)
		b.WriteByte('\x00')
		b.WriteString(cs.ID)
		b.WriteByte('\n')
	}
	return b.String()
}

func mixedCaseOrderSHA(seed int, entries []MixedCorpusEntry) string {
	var order strings.Builder
	for _, entry := range entries {
		order.WriteString(mixedCaseOrderKey(seed, entry))
	}
	sum := sha256.Sum256([]byte(order.String()))
	return hex.EncodeToString(sum[:])
}

// LoadMixedBenchManifest 讀取並檢查 manifest。
func LoadMixedBenchManifest(path string) (*MixedBenchManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m MixedBenchManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateMixedBenchManifest(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &m, nil
}

func validateMixedBenchManifest(m *MixedBenchManifest) error {
	if m.Schema != MixedBenchSchema {
		return fmt.Errorf("schema %d, want %d", m.Schema, MixedBenchSchema)
	}
	if m.Seed <= 0 {
		return fmt.Errorf("seed 必須大於 0")
	}
	if len(m.Fixtures) == 0 {
		return fmt.Errorf("沒有 fixtures")
	}
	seen := map[string]bool{}
	for _, f := range m.Fixtures {
		if f.File == "" || f.Name == "" || !f.Scenario.Valid() || f.Count <= 0 || len(f.SHA256) != 64 {
			return fmt.Errorf("fixture %+v 不完整", f)
		}
		if seen[f.File] {
			return fmt.Errorf("fixture %q 重複", f.File)
		}
		seen[f.File] = true
	}
	if len(m.CaseOrderSHA256) != 64 {
		return fmt.Errorf("case_order_sha256 無效")
	}
	if m.Pack != nil && (m.Pack.Path == "" || len(m.Pack.SHA256) != 64) {
		return fmt.Errorf("pack pin 不完整")
	}
	if m.Data != nil && (m.Data.Dir == "" || len(m.Data.LexiconSHA256) != 64 || len(m.Data.LMSHA256) != 64) {
		return fmt.Errorf("data pin 不完整")
	}
	if m.Models.Note == "" {
		return fmt.Errorf("models.note 不能是空的")
	}
	if m.Quality == "" {
		return fmt.Errorf("quality 不能是空的")
	}
	return nil
}

// MixedBenchTotals 是所有情境加總。SegmentTop1 是引擎實際顯示／送出的那條
// 路徑的詞面正確率，也就是這個 runner 的 top-1；Joint 再加上語言與變體。
type MixedBenchTotals struct {
	Cases             int                 `json:"cases"`
	Segment           MixedSegmentMetrics `json:"segment"`
	SegmentTop1       int                 `json:"segment_top1"`
	SegmentTop1Rate   float64             `json:"segment_top1_rate"`
	SegmentJoint      int                 `json:"segment_joint"`
	SegmentJointRate  float64             `json:"segment_joint_rate"`
	Switch            MixedSwitchMetrics  `json:"switch"`
	Smoke             MixedSmoke          `json:"smoke"`
	SentencesExact    int                 `json:"sentences_exact"`
	SentenceExactRate float64             `json:"sentence_exact_rate"`
}

// MixedTagMetrics 是同一個 tag 的案例彙總。clean 與每種雜訊分開看，才不會
// 讓長詞合併或大小寫雜訊的平均掩蓋乾淨案例。
type MixedTagMetrics struct {
	Tag               string              `json:"tag"`
	Cases             int                 `json:"cases"`
	Segment           MixedSegmentMetrics `json:"segment"`
	Switch            MixedSwitchMetrics  `json:"switch"`
	Smoke             MixedSmoke          `json:"smoke"`
	SentencesExact    int                 `json:"sentences_exact"`
	SentenceExactRate float64             `json:"sentence_exact_rate"`
}

// MixedBenchReport 是一份完整的混合語言 benchmark 報告。
type MixedBenchReport struct {
	Schema         int               `json:"schema"`
	Seed           int               `json:"seed"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	Engine         MixedEngineInfo   `json:"engine"`
	Models         MixedModelPin     `json:"models"`
	Quality        string            `json:"quality"`
	WiringOnly     bool              `json:"wiring_only"`
	Notes          []string          `json:"notes,omitempty"`
	Corpora        []MixedReport     `json:"corpora"`
	Totals         MixedBenchTotals  `json:"totals"`
	ByTag          []MixedTagMetrics `json:"by_tag"`
}

// RunMixedBench 依 manifest 的固定種子順序跑所有情境並彙總。runner 可以是
// 真引擎或替身；報告不含延遲，所以同一份輸入連跑兩次必須逐位元相同。
func RunMixedBench(entries []MixedCorpusEntry, m *MixedBenchManifest, manifestSHA string, engineInfo MixedEngineInfo, runner MixedRunner) (MixedBenchReport, error) {
	if m == nil {
		return MixedBenchReport{}, fmt.Errorf("manifest 是 nil")
	}
	rep := MixedBenchReport{
		Schema:         MixedBenchSchema,
		Seed:           m.Seed,
		ManifestSHA256: manifestSHA,
		Engine:         engineInfo,
		Models:         m.Models,
		Quality:        m.Quality,
		WiringOnly:     m.Models.Used,
	}
	if rep.WiringOnly {
		rep.Notes = append(rep.Notes, "wiring-only: 模型參與本次執行；分數只證明呼叫路徑，不是品質")
	}
	if engineInfo.JapanesePack == nil {
		rep.Notes = append(rep.Notes, "japanese pack 未載入：日文片段只會組かな，沒有漢字轉換")
	}
	byTag := map[string]*MixedTagMetrics{}
	for _, entry := range entries {
		ordered := MixedSeedOrder(m.Seed, entry.Corpus.Cases)
		cr := RunMixedCases(entry.Corpus, ordered, entry.SHA256, runner)
		rep.Corpora = append(rep.Corpora, cr)
		addBenchTotals(&rep.Totals, cr)
		for _, c := range cr.Cases {
			for _, tag := range c.Tags {
				tm := byTag[tag]
				if tm == nil {
					tm = &MixedTagMetrics{Tag: tag}
					byTag[tag] = tm
				}
				tm.Cases++
				tm.Segment.N += c.Segment.N
				tm.Segment.SurfaceCorrect += c.Segment.SurfaceCorrect
				tm.Segment.LanguageCorrect += c.Segment.LanguageCorrect
				tm.Segment.VariantCorrect += c.Segment.VariantCorrect
				tm.Segment.JointCorrect += c.Segment.JointCorrect
				addSwitchMetrics(&tm.Switch, c.Switch)
				if c.SwitchExact {
					tm.Switch.ExactCases++
				}
				addSmoke(&tm.Smoke, mixedCaseMetrics{MixedCaseReport: c})
				if c.TextCorrect {
					tm.SentencesExact++
				}
			}
		}
	}
	finalizeBenchTotals(&rep.Totals)
	tags := make([]string, 0, len(byTag))
	for tag := range byTag {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	for _, tag := range tags {
		tm := byTag[tag]
		finalizeSegment(&tm.Segment)
		finalizeSwitch(&tm.Switch)
		tm.SentenceExactRate = ratio(tm.SentencesExact, tm.Cases)
		rep.ByTag = append(rep.ByTag, *tm)
	}
	return rep, nil
}

func addBenchTotals(dst *MixedBenchTotals, cr MixedReport) {
	dst.Cases += len(cr.Cases)
	dst.Segment.N += cr.Segment.N
	dst.Segment.SurfaceCorrect += cr.Segment.SurfaceCorrect
	dst.Segment.LanguageCorrect += cr.Segment.LanguageCorrect
	dst.Segment.VariantCorrect += cr.Segment.VariantCorrect
	dst.Segment.JointCorrect += cr.Segment.JointCorrect
	addSwitchMetrics(&dst.Switch, cr.Switch)
	dst.Switch.ExactCases += cr.Switch.ExactCases
	dst.Smoke = mergeSmoke(dst.Smoke, cr.Smoke)
	for _, c := range cr.Cases {
		if c.TextCorrect {
			dst.SentencesExact++
		}
	}
}

func mergeSmoke(dst, src MixedSmoke) MixedSmoke {
	dst.Cases += src.Cases
	dst.Runs += src.Runs
	dst.FailedRuns += src.FailedRuns
	dst.Panics += src.Panics
	dst.CompleteCases += src.CompleteCases
	dst.FinalTextNonEmpty += src.FinalTextNonEmpty
	dst.TextMatchesSegments += src.TextMatchesSegments
	dst.TextCorrect += src.TextCorrect
	dst.MissingSegments += src.MissingSegments
	dst.DuplicateSegments += src.DuplicateSegments
	dst.UnknownIndex += src.UnknownIndex
	dst.InvalidSegments += src.InvalidSegments
	dst.MergedSegments += src.MergedSegments
	dst.UnorderedDirectKeys += src.UnorderedDirectKeys
	return dst
}

func finalizeBenchTotals(t *MixedBenchTotals) {
	finalizeSegment(&t.Segment)
	finalizeSwitch(&t.Switch)
	t.SegmentTop1 = t.Segment.SurfaceCorrect
	t.SegmentTop1Rate = t.Segment.SurfaceRate
	t.SegmentJoint = t.Segment.JointCorrect
	t.SegmentJointRate = t.Segment.JointRate
	t.SentenceExactRate = ratio(t.SentencesExact, t.Cases)
}

// MixedBenchProblems 比對 manifest 與實際載入的題庫／資料。它只回報
// identity 不一致；準確度不是門檻（P2-61 明確不設硬性上線門檻）。
func MixedBenchProblems(entries []MixedCorpusEntry, m *MixedBenchManifest, manifestSHA string, report MixedBenchReport) []string {
	var problems []string
	if m == nil {
		return []string{"manifest 是 nil"}
	}
	if report.Seed != m.Seed {
		problems = append(problems, fmt.Sprintf("seed %d != manifest %d", report.Seed, m.Seed))
	}
	if report.ManifestSHA256 != manifestSHA {
		problems = append(problems, "報告的 manifest 雜湊與檔案不符")
	}
	if len(entries) != len(m.Fixtures) {
		problems = append(problems, fmt.Sprintf("%d 份題庫, manifest 釘選 %d 份", len(entries), len(m.Fixtures)))
	}
	if got := mixedCaseOrderSHA(m.Seed, entries); got != m.CaseOrderSHA256 {
		problems = append(problems, "case_order_sha256 與固定種子排序不符")
	}
	pins := map[string]MixedFixturePin{}
	for _, f := range m.Fixtures {
		pins[f.File] = f
	}
	for _, entry := range entries {
		file := filepath.Base(entry.Path)
		pin, ok := pins[file]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: manifest 沒有釘選", file))
			continue
		}
		if pin.SHA256 != entry.SHA256 {
			problems = append(problems, fmt.Sprintf("%s: sha256 %s != manifest %s", file, entry.SHA256, pin.SHA256))
		}
		if pin.Name != entry.Corpus.Name || pin.Scenario != entry.Corpus.Scenario || pin.Count != len(entry.Corpus.Cases) {
			problems = append(problems, fmt.Sprintf("%s: name/scenario/count 與 manifest 不符", file))
		}
		if entry.Corpus.Seed != 0 && entry.Corpus.Seed != m.Seed {
			problems = append(problems, fmt.Sprintf("%s: 題庫 seed %d != manifest %d", file, entry.Corpus.Seed, m.Seed))
		}
	}
	if m.Pack != nil && report.Engine.JapanesePack != nil {
		if m.Pack.SHA256 != report.Engine.JapanesePack.SHA256 {
			problems = append(problems, fmt.Sprintf("pack sha256 %s != manifest %s", report.Engine.JapanesePack.SHA256, m.Pack.SHA256))
		}
	}
	if m.Data != nil {
		if report.Engine.LexiconSHA256 != "" && report.Engine.LexiconSHA256 != m.Data.LexiconSHA256 {
			problems = append(problems, "lexicon sha256 與 manifest 不符")
		}
		if report.Engine.LMSHA256 != "" && report.Engine.LMSHA256 != m.Data.LMSHA256 {
			problems = append(problems, "lm sha256 與 manifest 不符")
		}
	}
	if report.WiringOnly != m.Models.Used {
		problems = append(problems, "wiring_only 與 manifest models.used 不一致")
	}
	return problems
}

// MixedBenchDeterminismSignature 回傳報告的 JSON。報告沒有延遲欄位，同一份
// 題庫與 runner 連跑兩次必須完全相同。
func MixedBenchDeterminismSignature(rep MixedBenchReport) (string, error) {
	b, err := json.Marshal(rep)
	return string(b), err
}

// FormatMixedBenchTable 產生人看的固定欄寬表格。數字順序固定，適合貼進
// 報告或 diff。沒有預期切換時 precision／recall 顯示 n/a，不假裝是 0。
func FormatMixedBenchTable(rep MixedBenchReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mixed-language benchmark  seed=%d  pack=%s  data=%s\n",
		rep.Seed, benchPackLabel(rep.Engine), benchDataLabel(rep.Engine))
	fmt.Fprintf(&b, "%-4s %-28s %5s %5s %6s %6s %6s %6s %7s %7s %7s %6s %5s %5s %5s %5s\n",
		"sc", "corpus", "cases", "segs", "top1", "lang", "var", "joint",
		"swP", "swR", "swF1", "swEx", "sent", "runs", "done", "miss")
	row := func(sc, name string, cases, n int, m MixedSegmentMetrics, sw MixedSwitchMetrics, swEx, exact, runs, complete, missing int) {
		fmt.Fprintf(&b, "%-4s %-28s %5d %5d %6.4f %6.4f %6.4f %6.4f %7s %7s %7s %4d/%-3d %2d/%-3d %4d/%-4d %4d/%-4d %5d\n",
			sc, name, cases, n,
			m.SurfaceRate, m.LanguageRate, m.VariantRate, m.JointRate,
			switchPrecision(sw), switchRecall(sw), switchF1(sw), swEx, cases,
			exact, cases, runs, cases, complete, cases, missing)
	}
	for _, c := range rep.Corpora {
		row(string(c.Corpus.Scenario), clipName(c.Corpus.Name), len(c.Cases), c.Segment.N,
			c.Segment, c.Switch, c.Switch.ExactCases, c.Smoke.TextCorrect,
			c.Smoke.Runs, c.Smoke.CompleteCases, c.Smoke.MissingSegments)
	}
	t := rep.Totals
	row("all", "total", t.Cases, t.Segment.N, t.Segment, t.Switch, t.Switch.ExactCases,
		t.SentencesExact, t.Smoke.Runs, t.Smoke.CompleteCases, t.Smoke.MissingSegments)
	b.WriteString("\nby tag\n")
	fmt.Fprintf(&b, "%-22s %5s %5s %6s %6s %7s %7s %7s %6s\n",
		"tag", "cases", "segs", "top1", "joint", "swP", "swR", "swF1", "sent")
	for _, tag := range rep.ByTag {
		fmt.Fprintf(&b, "%-22s %5d %5d %6.4f %6.4f %7s %7s %7s %6.4f\n",
			tag.Tag, tag.Cases, tag.Segment.N, tag.Segment.SurfaceRate, tag.Segment.JointRate,
			switchPrecision(tag.Switch), switchRecall(tag.Switch), switchF1(tag.Switch), tag.SentenceExactRate)
	}
	fmt.Fprintf(&b, "\nswitch: expected=%d predicted=%d tp=%d fp=%d fn=%d exact_cases=%d/%d\n",
		t.Switch.Expected, t.Switch.Predicted, t.Switch.TP, t.Switch.FP, t.Switch.FN,
		t.Switch.ExactCases, t.Cases)
	fmt.Fprintf(&b, "smoke: runs=%d failed=%d panics=%d complete=%d missing=%d merged=%d duplicate=%d unknown=%d invalid=%d unordered_direct=%d\n",
		t.Smoke.Runs, t.Smoke.FailedRuns, t.Smoke.Panics, t.Smoke.CompleteCases,
		t.Smoke.MissingSegments, t.Smoke.MergedSegments, t.Smoke.DuplicateSegments,
		t.Smoke.UnknownIndex, t.Smoke.InvalidSegments, t.Smoke.UnorderedDirectKeys)
	fmt.Fprintf(&b, "models=%s wiring_only=%v\n", rep.Models.Note, rep.WiringOnly)
	return b.String()
}

func switchPrecision(m MixedSwitchMetrics) string { return rateLabel(m.TP, m.TP+m.FP) }
func switchRecall(m MixedSwitchMetrics) string    { return rateLabel(m.TP, m.TP+m.FN) }
func switchF1(m MixedSwitchMetrics) string {
	p, r := m.TP+m.FP, m.TP+m.FN
	if p == 0 || r == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.4f", harmonic(float64(m.TP)/float64(p), float64(m.TP)/float64(r)))
}

func rateLabel(num, den int) string {
	if den == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.4f", float64(num)/float64(den))
}

func benchPackLabel(info MixedEngineInfo) string {
	if info.JapanesePack == nil {
		return "none"
	}
	return sha12(info.JapanesePack.SHA256)
}

func benchDataLabel(info MixedEngineInfo) string {
	if info.LexiconSHA256 == "" {
		return info.DataDir
	}
	return sha12(info.LexiconSHA256)
}

func sha12(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func clipName(s string) string {
	r := []rune(s)
	if len(r) <= 28 {
		return s
	}
	return string(r[:28])
}
