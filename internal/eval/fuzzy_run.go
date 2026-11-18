package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0c-biggo/0c-ime/core/engine"
	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/layout"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/score"
	"github.com/0c-biggo/0c-ime/core/segment"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
	"github.com/0c-biggo/0c-ime/internal/datadir"
	"github.com/0c-biggo/0c-ime/internal/jadiclex"
)

// P2-62 runner：實際用產品 session 打模糊輸入，候選名次用引擎輸出，回退
// 名次用 score.Ranker 在沒有模型時的統計路徑。兩個角色的分母都是同一批
// 案例；打字錯誤、沒有候選、沒有讀音、沒有 pack 都不會把案例移出分母。

// FuzzyJADecoder 是日文統計轉換器的最小介面；*jadecode.Decoder 直接符合。
type FuzzyJADecoder interface {
	NBest(seq reading.Sequence, ctx lang.Context, n int) ([]lang.Result, error)
}

// FuzzyPackInfo 是日文參考 pack 的 identity。
type FuzzyPackInfo struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	ManifestPath   string `json:"manifest_path,omitempty"`
	ManifestSHA256 string `json:"manifest_sha256,omitempty"`
	Entries        int    `json:"entries,omitempty"`
}

// FuzzyEngineInfo 記錄一次執行實際使用的資料。Models 永遠是 none：P2-62
// 不載入任何模型，placeholder 也不得進入這條路徑。
type FuzzyEngineInfo struct {
	DataDir            string         `json:"data_dir"`
	LexiconSHA256      string         `json:"lexicon_sha256"`
	LMSHA256           string         `json:"lm_sha256"`
	JapanesePack       *FuzzyPackInfo `json:"japanese_pack,omitempty"`
	JapaneseConnSHA256 string         `json:"japanese_connection_sha256,omitempty"`
	Models             string         `json:"models"`
	ModelsUsed         bool           `json:"models_used"`
	Quality            string         `json:"quality"`
}

// FuzzyEnv 是模糊評測需要的已載入環境。沒有日文 pack 時 Decoder 與 JASource
// 可以是 nil；日文案例仍留在分母，只是候選指標標成不可用。
type FuzzyEnv struct {
	Engine   *engine.Engine
	Decoder  FuzzyJADecoder
	JASource jalexicon.Source
	JAConn   *jalexicon.Matrix
	Info     FuzzyEngineInfo
}

// LoadFuzzyEnv 從 repo 的注音資料與（可選的）日文 pack 建立環境。
func LoadFuzzyEnv(dataDir, packPath, connectionPath string) (*FuzzyEnv, FuzzyEngineInfo, error) {
	info := FuzzyEngineInfo{
		DataDir:    dataDir,
		Models:     "none: no model is loaded; placeholder int8 ONNX artifacts are never used",
		ModelsUsed: false,
		Quality:    FuzzyQuality,
	}
	eng, err := datadir.Open(dataDir)
	if err != nil {
		return nil, info, err
	}
	eng.MixedInput = true
	eng.Preview02 = true
	// 產品的模糊音預設是 select：開候選時合併近音，不改自動顯示。這裡套用
	// 同一組預設，不是把預設改成更寬鬆的 auto。
	eng.Fuzzy = fuzzySelectAll()
	info.LexiconSHA256, err = optionalFileSHA(filepath.Join(dataDir, "lexicon.tsv"))
	if err != nil {
		return nil, info, err
	}
	info.LMSHA256, err = optionalFileSHA(filepath.Join(dataDir, "lm.tsv"))
	if err != nil {
		return nil, info, err
	}
	env := &FuzzyEnv{Engine: eng, Info: info}
	if packPath == "" {
		return env, info, nil
	}
	src, err := jadiclex.Load(packPath)
	if err != nil {
		return nil, info, err
	}
	packSHA, err := optionalFileSHA(packPath)
	if err != nil {
		return nil, info, err
	}
	pack := &FuzzyPackInfo{Path: filepath.Base(packPath), SHA256: packSHA, Entries: src.Entries()}
	if manifestPath := filepath.Join(filepath.Dir(packPath), "manifest.json"); fileExists(manifestPath) {
		pack.ManifestPath = filepath.Base(manifestPath)
		pack.ManifestSHA256, err = optionalFileSHA(manifestPath)
		if err != nil {
			return nil, info, err
		}
	}
	var conn *jalexicon.Matrix
	if connectionPath != "" {
		f, err := os.Open(connectionPath)
		if err != nil {
			return nil, info, err
		}
		conn, err = jalexicon.LoadConnections(f)
		f.Close()
		if err != nil {
			return nil, info, err
		}
		info.JapaneseConnSHA256, err = optionalFileSHA(connectionPath)
		if err != nil {
			return nil, info, err
		}
	}
	env.Decoder = &jadecode.Decoder{Lex: src, Conn: conn}
	env.JASource = src
	env.JAConn = conn
	info.JapanesePack = pack
	return env, info, nil
}

// FuzzyArmMetrics 是一個角色（raw 或 fallback）的指標。N 是完整分母；
// missing、no_candidates 都只是 N 裡的原因，不會把案例移出分母。
type FuzzyArmMetrics struct {
	N            int     `json:"n"`
	Top1         int     `json:"top1"`
	Top1Rate     float64 `json:"top1_rate"`
	Top5         int     `json:"top5"`
	Top5Rate     float64 `json:"top5_rate"`
	Intended     int     `json:"intended"`
	IntendedRate float64 `json:"intended_rate"`
	Missing      int     `json:"missing"`
	NoCandidates int     `json:"no_candidates"`
	Recovered    int     `json:"recovered"`
	Harmed       int     `json:"harmed"`
}

func (m *FuzzyArmMetrics) add(rank int, intended, hasCandidates bool) {
	m.N++
	if rank == 1 {
		m.Top1++
	}
	if rank >= 1 && rank <= 5 {
		m.Top5++
	}
	if intended {
		m.Intended++
	}
	if rank == 0 {
		m.Missing++
	}
	if !hasCandidates {
		m.NoCandidates++
	}
}

func (m *FuzzyArmMetrics) finalize() {
	m.Top1Rate = ratio(m.Top1, m.N)
	m.Top5Rate = ratio(m.Top5, m.N)
	m.IntendedRate = ratio(m.Intended, m.N)
}

// FuzzyDegraded 統計留在分母裡的降級原因。Cases 是至少有一個原因的案例數。
type FuzzyDegraded struct {
	Cases        int `json:"cases"`
	TypingErrors int `json:"typing_errors"`
	NoReading    int `json:"no_reading"`
	NoDecoder    int `json:"no_decoder"`
	NoCandidates int `json:"no_candidates"`
	NoCommit     int `json:"no_commit"`
	Unhandled    int `json:"unhandled_keys"`
}

// FuzzyCaseResult 保留單題證據；DurationMS 不進 determinism signature。
type FuzzyCaseResult struct {
	ID               string   `json:"id"`
	Language         string   `json:"language"`
	Kind             string   `json:"kind"`
	Reference        string   `json:"reference"`
	Input            string   `json:"input"`
	NoisyReading     string   `json:"noisy_reading,omitempty"`
	Pending          string   `json:"pending,omitempty"`
	Output           string   `json:"output,omitempty"`
	Rank             int      `json:"rank"`
	RawTop5          []string `json:"raw_top5,omitempty"`
	Intended         bool     `json:"intended"`
	FallbackRank     int      `json:"fallback_rank"`
	FallbackTop5     []string `json:"fallback_top5,omitempty"`
	FallbackIntended bool     `json:"fallback_intended"`
	FallbackOutcome  string   `json:"fallback_outcome,omitempty"`
	Degraded         bool     `json:"degraded"`
	DegradedReason   string   `json:"degraded_reason,omitempty"`
	DurationMS       float64  `json:"duration_ms"`
}

// FuzzyGroupReport 是一個語言×擾動種類的彙總；Language 或 Kind 為空字串
// 時代表語言合計或全體合計。
type FuzzyGroupReport struct {
	Language string          `json:"language,omitempty"`
	Kind     string          `json:"kind,omitempty"`
	N        int             `json:"n"`
	Raw      FuzzyArmMetrics `json:"raw"`
	Fallback FuzzyArmMetrics `json:"fallback"`
	Outcomes map[string]int  `json:"fallback_outcomes"`
	Degraded FuzzyDegraded   `json:"degraded"`
}

// FuzzyRunConfig 是一次執行的設定與定位說明。
type FuzzyRunConfig struct {
	TopN    int    `json:"top_n"`
	Fuzzy   string `json:"fuzzy"`
	Ranker  string `json:"ranker"`
	Models  string `json:"models"`
	Quality string `json:"quality"`
}

// FuzzyReport 是完整報告。
type FuzzyReport struct {
	Schema       int                `json:"schema"`
	Corpus       string             `json:"corpus"`
	CorpusSHA256 string             `json:"corpus_sha256"`
	Seed         int                `json:"seed"`
	Engine       FuzzyEngineInfo    `json:"engine"`
	Config       FuzzyRunConfig     `json:"config"`
	Ranker       score.Counters     `json:"ranker_counters"`
	Groups       []FuzzyGroupReport `json:"groups"`
	Cases        []FuzzyCaseResult  `json:"cases"`
}

// FuzzyRunOptions 控制一次執行。
type FuzzyRunOptions struct {
	TopN  int
	Limit int // >0 時只跑排序後的前 Limit 題；測試用
}

// RunFuzzyCorpus 跑完整題庫。所有案例都進分母；單題錯誤只記錄原因，
// 不會中止或排除。
func RunFuzzyCorpus(env *FuzzyEnv, corpus *FuzzyCorpus, corpusSHA string, opts FuzzyRunOptions) (FuzzyReport, error) {
	if env == nil || env.Engine == nil {
		return FuzzyReport{}, fmt.Errorf("fuzzy runner: engine 為必填")
	}
	if opts.TopN <= 0 {
		opts.TopN = 8
	}
	if opts.TopN < 5 {
		opts.TopN = 5
	}
	items := corpus.Items
	if opts.Limit > 0 && opts.Limit < len(items) {
		items = items[:opts.Limit]
	}
	// 產品的模糊音預設是 select；runner 明確套用同一組預設，讓手動建立的
	// FuzzyEnv 與命令列路徑得到相同候選。
	env.Engine.Fuzzy = fuzzySelectAll()
	ranker := score.NewRanker(score.RankerConfig{})
	rep := FuzzyReport{
		Schema:       FuzzySchema,
		Corpus:       corpus.Name,
		CorpusSHA256: corpusSHA,
		Seed:         corpus.Seed,
		Engine:       env.Info,
		Config: FuzzyRunConfig{
			TopN:    opts.TopN,
			Fuzzy:   env.Engine.Fuzzy.Key(),
			Ranker:  "score.Ranker statistical fallback (no model)",
			Models:  "none",
			Quality: FuzzyQuality + "; fallback outcomes stay in every denominator",
		},
	}
	for _, item := range items {
		rep.Cases = append(rep.Cases, runFuzzyCase(env, item, opts.TopN, ranker))
	}
	rep.Ranker = ranker.Counters()
	rep.Groups = summarizeFuzzy(rep.Cases)
	return rep, nil
}

func runFuzzyCase(env *FuzzyEnv, item FuzzyItem, topN int, ranker *score.Ranker) FuzzyCaseResult {
	start := time.Now()
	res := FuzzyCaseResult{
		ID:           item.ID,
		Language:     item.Language,
		Kind:         item.Kind,
		Reference:    item.Surface,
		Input:        item.Input,
		NoisyReading: item.NoisyReading,
		Pending:      item.Pending,
	}
	var degraded []string
	id := env.Engine.OpenSession()
	defer env.Engine.CloseSession(id)
	sess := env.Engine.Session(id)
	if sess == nil {
		res.Degraded = true
		res.DegradedReason = "no_session"
		res.DurationMS = roundMS(time.Since(start))
		return res
	}
	_, _ = env.Engine.Do(id, proto.Request{Type: proto.Pause, On: true})

	if item.Language == FuzzyLangZh {
		runFuzzyZh(env, id, item, topN, ranker, &res, &degraded)
	} else {
		runFuzzyJA(env, id, item, topN, ranker, &res, &degraded, sess)
	}
	if len(degraded) > 0 {
		res.Degraded = true
		res.DegradedReason = strings.Join(uniqueStrings(degraded), ",")
	}
	res.DurationMS = roundMS(time.Since(start))
	return res
}

func runFuzzyZh(env *FuzzyEnv, id uint64, item FuzzyItem, topN int, ranker *score.Ranker, res *FuzzyCaseResult, degraded *[]string) {
	if err := typeZhFuzzy(env.Engine, id, item); err != nil {
		*degraded = append(*degraded, "typing_error")
		res.RawTop5 = nil
		return
	}
	n := len([]rune(item.Surface))
	snap, err := env.Engine.Do(id, proto.Request{Type: proto.Candidates, Length: n})
	if err != nil {
		*degraded = append(*degraded, "candidate_error")
		return
	}
	cands, err := collectCandidates(env.Engine, id, snap)
	if err != nil {
		*degraded = append(*degraded, "candidate_error")
		return
	}
	if len(cands) == 0 {
		*degraded = append(*degraded, "no_candidates")
	}
	res.Rank = Ranked(cands, item.Surface)
	res.RawTop5 = topNStrings(cands, topN)
	commit, err := env.Engine.Do(id, proto.Request{Type: proto.Commit})
	if err != nil {
		*degraded = append(*degraded, "commit_error")
	} else {
		res.Output = commit.Commit
		if res.Output == "" {
			*degraded = append(*degraded, "no_commit")
		}
	}
	res.Intended = res.Output == item.Surface
	applyFuzzyFallback(item, cands, res, ranker)
}

func runFuzzyJA(env *FuzzyEnv, id uint64, item FuzzyItem, topN int, ranker *score.Ranker, res *FuzzyCaseResult, degraded *[]string, sess interface {
	EnableJapanese(jalexicon.Source, *jalexicon.Matrix) bool
	SetLanguage(lang.ID) bool
	SetInputMethod(string) bool
}) {
	if !sess.EnableJapanese(env.JASource, env.JAConn) || !sess.SetLanguage(lang.Ja) || !sess.SetInputMethod("romaji") {
		*degraded = append(*degraded, "ja_session")
		return
	}
	unhandled := 0
	for _, key := range item.Input {
		snap, err := env.Engine.Do(id, proto.Request{Type: proto.Key, Key: string(key)})
		if err != nil {
			*degraded = append(*degraded, "typing_error")
			return
		}
		if !snap.Handled && key > 0x20 && key < 0x7f {
			unhandled++
		}
	}
	if unhandled > 0 || item.Pending != "" {
		*degraded = append(*degraded, "unhandled_keys")
	}
	if item.NoisyReading == "" {
		*degraded = append(*degraded, "no_reading")
	}
	if env.Decoder == nil {
		*degraded = append(*degraded, "no_decoder")
	} else if item.NoisyReading != "" {
		results, err := env.Decoder.NBest(jaSequence(item.NoisyReading), lang.Context{}, topN)
		if err != nil {
			*degraded = append(*degraded, "decode_error")
		} else {
			surfaces, readings, costs := fuzzyJAResults(results)
			res.Rank = Ranked(surfaces, item.Surface)
			res.RawTop5 = topNStrings(surfaces, topN)
			if len(surfaces) == 0 {
				*degraded = append(*degraded, "no_candidates")
			}
			applyFuzzyFallbackWithReadings(item, surfaces, readings, costs, res, ranker)
		}
	}
	// 実際の romaji session を変換して送出し、模糊入力が意図した文字まで
	// 届くかを測る。変換結果は decoder の top-1 と別でもよい。
	if _, err := env.Engine.Do(id, proto.Request{Type: proto.Key, Key: proto.KeyConvert}); err != nil {
		*degraded = append(*degraded, "convert_error")
	}
	commit, err := env.Engine.Do(id, proto.Request{Type: proto.Key, Key: proto.KeyEnter})
	if err != nil {
		*degraded = append(*degraded, "commit_error")
	} else {
		res.Output = commit.Commit
		if res.Output == "" {
			*degraded = append(*degraded, "no_commit")
		}
	}
	res.Intended = res.Output == item.Surface
}

func typeZhFuzzy(eng *engine.Engine, id uint64, item FuzzyItem) error {
	switch item.InputKind {
	case FuzzyInputZhuyinKeys:
		for _, key := range item.Input {
			sym, ok := layout.Standard.Symbol(key)
			if !ok {
				return fmt.Errorf("%s: 按鍵 %q 不是注音鍵", item.ID, key)
			}
			if _, err := eng.Do(id, proto.Request{Type: proto.Symbol, Symbol: string(sym)}); err != nil {
				return err
			}
		}
	case FuzzyInputZhuyinSyllables:
		for _, syl := range zhuyin.ParseReading(item.Input) {
			for _, sym := range string(syl) {
				if _, err := eng.Do(id, proto.Request{Type: proto.Symbol, Symbol: string(sym)}); err != nil {
					return err
				}
			}
			if syl.ToneOf() == 1 {
				if _, err := eng.Do(id, proto.Request{Type: proto.Symbol, Symbol: string(zhuyin.Tone1)}); err != nil {
					return err
				}
			}
		}
	default:
		return fmt.Errorf("%s: 未知的注音輸入種類 %q", item.ID, item.InputKind)
	}
	return nil
}

func collectCandidates(eng *engine.Engine, id uint64, snap proto.Snapshot) ([]string, error) {
	if snap.Candidates == nil {
		return nil, nil
	}
	var out []string
	for _, it := range snap.Candidates.Items {
		out = append(out, it.Text)
	}
	for page := 1; page < snap.Candidates.Pages; page++ {
		next, err := eng.Do(id, proto.Request{Type: proto.Page, Index: page})
		if err != nil {
			return nil, err
		}
		if next.Candidates == nil {
			continue
		}
		for _, it := range next.Candidates.Items {
			out = append(out, it.Text)
		}
	}
	return out, nil
}

func applyFuzzyFallback(item FuzzyItem, cands []string, res *FuzzyCaseResult, ranker *score.Ranker) {
	readings := make([]reading.Sequence, len(cands))
	costs := make([]float64, len(cands))
	for i := range cands {
		costs[i] = float64(i)
	}
	applyFuzzyFallbackWithReadings(item, cands, readings, costs, res, ranker)
}

func applyFuzzyFallbackWithReadings(item FuzzyItem, surfaces []string, readings []reading.Sequence, costs []float64, res *FuzzyCaseResult, ranker *score.Ranker) {
	rank, outcome, top, err := fuzzyFallbackRank(item.Language, item.Surface, surfaces, readings, costs, ranker)
	if err != nil {
		res.FallbackOutcome = "error"
		return
	}
	res.FallbackOutcome = outcome
	res.FallbackRank = rank
	res.FallbackTop5 = top
	res.FallbackIntended = rank == 1
}

func fuzzyFallbackRank(language, reference string, surfaces []string, readings []reading.Sequence, costs []float64, ranker *score.Ranker) (int, string, []string, error) {
	if len(surfaces) == 0 {
		return 0, "none", nil, nil
	}
	primary := lang.Zh
	if language == FuzzyLangJa {
		primary = lang.Ja
	}
	cands := make([]lattice.Candidate, 0, len(surfaces))
	for i, surface := range surfaces {
		readingSeq := reading.Sequence(nil)
		if i < len(readings) {
			readingSeq = readings[i]
		}
		cost := float64(i)
		if i < len(costs) {
			cost = costs[i]
		}
		languageID := lang.Zh
		if language == FuzzyLangJa {
			languageID = lang.Ja
		}
		cands = append(cands, lattice.Candidate{
			Segment: segment.Segment{
				Span:     segment.Span{Start: 0, End: len([]rune(surface))},
				Surface:  surface,
				Reading:  readingSeq,
				Language: lang.Tag(languageID),
			},
			Cost:   lattice.Cost{Dictionary: cost},
			Source: lattice.SourceDictionary,
		})
	}
	ranking, err := ranker.RankResult(context.Background(), score.RankRequest{
		Privacy:         score.PublicPrivacy(),
		PrimaryLanguage: primary,
		Candidates:      cands,
	})
	if err != nil {
		return 0, "error", nil, err
	}
	ordered := make([]string, 0, len(ranking.Items))
	for _, it := range ranking.Items {
		if it.Index >= 0 && it.Index < len(cands) {
			ordered = append(ordered, cands[it.Index].Surface)
		}
	}
	return Ranked(ordered, reference), ranking.Outcome.String(), topNStrings(ordered, 5), nil
}

func fuzzyJAResults(results []lang.Result) ([]string, []reading.Sequence, []float64) {
	surfaces := make([]string, 0, len(results))
	readings := make([]reading.Sequence, 0, len(results))
	costs := make([]float64, 0, len(results))
	for _, res := range results {
		surface := jadecode.ResultSurface(res.Clauses)
		if surface == "" {
			continue
		}
		surfaces = append(surfaces, surface)
		readings = append(readings, jaResultReading(res))
		costs = append(costs, -res.Score)
	}
	return surfaces, readings, costs
}

func jaResultReading(res lang.Result) reading.Sequence {
	var out reading.Sequence
	for _, clause := range res.Clauses {
		out = append(out, clause.Reading...)
	}
	return out
}

func topNStrings(xs []string, n int) []string {
	if n <= 0 || len(xs) == 0 {
		return nil
	}
	if len(xs) > n {
		xs = xs[:n]
	}
	return append([]string(nil), xs...)
}

func summarizeFuzzy(cases []FuzzyCaseResult) []FuzzyGroupReport {
	full := map[string]*FuzzyGroupReport{}
	langs := map[string]*FuzzyGroupReport{}
	all := &FuzzyGroupReport{Raw: FuzzyArmMetrics{}, Fallback: FuzzyArmMetrics{}, Outcomes: map[string]int{}}
	for _, c := range cases {
		keys := []string{c.Language + "\x00" + c.Kind, c.Language + "\x00"}
		for i, key := range keys {
			var g *FuzzyGroupReport
			if i == 0 {
				g = full[key]
			} else {
				g = langs[key]
			}
			if g == nil {
				g = &FuzzyGroupReport{Language: c.Language, Outcomes: map[string]int{}}
				if i == 0 {
					g.Kind = c.Kind
					full[key] = g
				} else {
					langs[key] = g
				}
			}
			addFuzzyCase(g, c)
		}
		addFuzzyCase(all, c)
	}
	var out []FuzzyGroupReport
	for _, g := range full {
		out = append(out, *g)
	}
	for _, g := range langs {
		out = append(out, *g)
	}
	out = append(out, *all)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Language != out[j].Language {
			return out[i].Language < out[j].Language
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return false
	})
	for i := range out {
		out[i].Raw.finalize()
		out[i].Fallback.finalize()
	}
	return out
}

func addFuzzyCase(g *FuzzyGroupReport, c FuzzyCaseResult) {
	g.N++
	g.Raw.add(c.Rank, c.Intended, len(c.RawTop5) > 0 || c.Rank > 0)
	g.Fallback.add(c.FallbackRank, c.FallbackIntended, len(c.FallbackTop5) > 0 || c.FallbackRank > 0)
	if c.FallbackOutcome != "" {
		g.Outcomes[c.FallbackOutcome]++
	}
	if c.Rank == 1 && c.FallbackRank != 1 {
		g.Fallback.Harmed++
	}
	if c.Rank != 1 && c.FallbackRank == 1 {
		g.Fallback.Recovered++
	}
	if c.Degraded {
		g.Degraded.Cases++
		for _, reason := range strings.Split(c.DegradedReason, ",") {
			switch reason {
			case "typing_error", "candidate_error", "decode_error", "convert_error", "commit_error", "ja_session", "no_session":
				g.Degraded.TypingErrors++
			case "no_reading":
				g.Degraded.NoReading++
			case "no_decoder":
				g.Degraded.NoDecoder++
			case "no_candidates":
				g.Degraded.NoCandidates++
			case "no_commit":
				g.Degraded.NoCommit++
			case "unhandled_keys":
				g.Degraded.Unhandled++
			}
		}
	}
}

func uniqueStrings(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// FuzzyDeterminismSignature 回傳跨兩次執行必須相同的部分；延遲全部歸零。
func FuzzyDeterminismSignature(rep FuzzyReport) (string, error) {
	copyRep := rep
	copyRep.Cases = make([]FuzzyCaseResult, len(rep.Cases))
	for i, c := range rep.Cases {
		c.DurationMS = 0
		copyRep.Cases[i] = c
	}
	b, err := json.Marshal(copyRep)
	return string(b), err
}
