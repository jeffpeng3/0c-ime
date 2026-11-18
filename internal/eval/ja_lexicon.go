package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/score"
	"github.com/0c-biggo/0c-ime/core/segment"
	"github.com/0c-biggo/0c-ime/internal/jadic"
	"github.com/0c-biggo/0c-ime/internal/jadiclex"
)

// JALexSchema 是日文單語言題庫與報告的格式版本。
const JALexSchema = 1

// JALexPack 是題庫來源 pack 的識別。兩個雜湊都要記：pack 檔本身與建置
// manifest；只核對其中一個無法證明詞條內容與來源帳本一起被凍結。
type JALexPack struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	ManifestPath   string `json:"manifest_path,omitempty"`
	ManifestSHA256 string `json:"manifest_sha256,omitempty"`
	Entries        int    `json:"entries"`
}

// JALexCandidate 是題庫凍結的一個漢字詞面與 pack 的 signed i16 成本。
// 成本越小越優先；這裡保留整數，不用浮點數重現排序。
type JALexCandidate struct {
	Surface string `json:"surface"`
	Cost    int16  `json:"cost"`
}

// JALexItem 是一個讀音與它的多個漢字候選。Reference 依題庫規則挑選，
// 是「詞典偏好」而不是人工標註的標準答案。
type JALexItem struct {
	ID         string           `json:"id"`
	Reading    string           `json:"reading"`
	Reference  string           `json:"reference"`
	Candidates []JALexCandidate `json:"candidates"`
	Holdout    bool             `json:"holdout"`
}

// JALexCorpus 是 testdata/eval/ja-lexicon-ambiguous.json。
type JALexCorpus struct {
	Schema        int         `json:"schema"`
	Name          string      `json:"name"`
	License       string      `json:"license"`
	Source        string      `json:"source"`
	Notes         string      `json:"notes,omitempty"`
	Seed          int         `json:"seed"`
	SampleSize    int         `json:"sample_size"`
	HoldoutSize   int         `json:"holdout_size"`
	CandidateCap  int         `json:"candidate_cap"`
	ReadingMin    int         `json:"reading_min"`
	ReadingMax    int         `json:"reading_max"`
	ReferenceRule string      `json:"reference_rule"`
	Pack          JALexPack   `json:"pack"`
	Count         int         `json:"count"`
	Items         []JALexItem `json:"items"`
}

// JALexBuildOptions 是產生題庫的固定參數。種子與切分都寫進題庫，重跑時
// 不從時間或環境推導任何值。
type JALexBuildOptions struct {
	Seed         int
	SampleSize   int
	HoldoutSize  int
	CandidateCap int
	ReadingMin   int
	ReadingMax   int
}

// DefaultJALexBuildOptions 是 P2-60 凍結的取樣參數。
func DefaultJALexBuildOptions() JALexBuildOptions {
	return JALexBuildOptions{
		Seed:         60,
		SampleSize:   1200,
		HoldoutSize:  400,
		CandidateCap: 6,
		ReadingMin:   1,
		ReadingMax:   8,
	}
}

// BuildJALexCorpus 從真實詞典 pack 取樣。同一份 pack、同一組參數會產生
// 逐位元相同的題庫；測試以兩次建置比對。
func BuildJALexCorpus(packPath, manifestPath string, opts JALexBuildOptions) (*JALexCorpus, error) {
	if opts.SampleSize <= 0 || opts.HoldoutSize <= 0 || opts.HoldoutSize >= opts.SampleSize {
		return nil, fmt.Errorf("sample_size and holdout_size are invalid")
	}
	if opts.CandidateCap < 2 || opts.ReadingMin < 1 || opts.ReadingMax < opts.ReadingMin {
		return nil, fmt.Errorf("candidate_cap, reading_min or reading_max is invalid")
	}
	packSHA, err := shaFile(packPath)
	if err != nil {
		return nil, err
	}
	corpus := &JALexCorpus{
		Schema:        JALexSchema,
		Name:          "ja-lexicon-ambiguous",
		License:       "UniDic 2025.12 BSD-3-Clause; MeCab IPADic 2.7.0 NAIST/ICOT; 0C curated entries CC-BY-4.0. Derived surface/reading/cost metadata; see the pack manifest for attribution.",
		Source:        "ja-lexicon-public-core.pack",
		Notes:         "Readings with at least two distinct all-kanji surfaces. Reference is the lowest-cost surface (dictionary preference), not a human annotation. Only the holdout slice is the reported baseline; the dev slice exists for later calibration.",
		Seed:          opts.Seed,
		SampleSize:    opts.SampleSize,
		HoldoutSize:   opts.HoldoutSize,
		CandidateCap:  opts.CandidateCap,
		ReadingMin:    opts.ReadingMin,
		ReadingMax:    opts.ReadingMax,
		ReferenceRule: "lowest pack cost among distinct all-kanji surfaces; ties by surface code point order",
		Pack: JALexPack{
			Path:         filepath.Base(packPath),
			SHA256:       packSHA,
			ManifestPath: filepath.Base(manifestPath),
			Entries:      0,
		},
	}
	if manifestPath != "" {
		manifestSHA, err := shaFile(manifestPath)
		if err != nil {
			return nil, err
		}
		corpus.Pack.ManifestSHA256 = manifestSHA
	}
	p, err := jadic.Load(packPath)
	if err != nil {
		return nil, err
	}
	corpus.Pack.Entries = p.EntryCount()

	byReading := map[string]map[string]int16{}
	err = p.ForEach(func(e jadic.Entry) error {
		if !isKanjiSurface(e.Surface) {
			return nil
		}
		n := len([]rune(e.Reading))
		if n < opts.ReadingMin || n > opts.ReadingMax {
			return nil
		}
		surfaces := byReading[e.Reading]
		if surfaces == nil {
			surfaces = map[string]int16{}
			byReading[e.Reading] = surfaces
		}
		if old, ok := surfaces[e.Surface]; !ok || e.Cost < old {
			surfaces[e.Surface] = e.Cost
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var pool []string
	for reading, surfaces := range byReading {
		if len(surfaces) >= 2 {
			pool = append(pool, reading)
		}
	}
	if len(pool) < opts.SampleSize {
		return nil, fmt.Errorf("only %d ambiguous readings, need %d; lower sample_size deliberately", len(pool), opts.SampleSize)
	}
	sort.Slice(pool, func(i, j int) bool {
		return sampledKey(opts.Seed, "sample", pool[i]) < sampledKey(opts.Seed, "sample", pool[j])
	})
	selected := append([]string(nil), pool[:opts.SampleSize]...)

	holdoutOrder := append([]string(nil), selected...)
	sort.Slice(holdoutOrder, func(i, j int) bool {
		return sampledKey(opts.Seed, "holdout", holdoutOrder[i]) < sampledKey(opts.Seed, "holdout", holdoutOrder[j])
	})
	holdout := map[string]bool{}
	for _, r := range holdoutOrder[:opts.HoldoutSize] {
		holdout[r] = true
	}

	sort.Strings(selected)
	for i, r := range selected {
		surfaces := byReading[r]
		cands := make([]JALexCandidate, 0, len(surfaces))
		for surface, cost := range surfaces {
			cands = append(cands, JALexCandidate{Surface: surface, Cost: cost})
		}
		sort.Slice(cands, func(a, b int) bool {
			if cands[a].Cost != cands[b].Cost {
				return cands[a].Cost < cands[b].Cost
			}
			return cands[a].Surface < cands[b].Surface
		})
		if len(cands) > opts.CandidateCap {
			cands = cands[:opts.CandidateCap]
		}
		corpus.Items = append(corpus.Items, JALexItem{
			ID:         fmt.Sprintf("ja-%04d", i+1),
			Reading:    r,
			Reference:  cands[0].Surface,
			Candidates: cands,
			Holdout:    holdout[r],
		})
	}
	corpus.Count = len(corpus.Items)
	if err := corpus.validate(); err != nil {
		return nil, err
	}
	return corpus, nil
}

func sampledKey(seed int, salt, reading string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", seed, salt, reading)))
	return hex.EncodeToString(sum[:])
}

func isKanjiSurface(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			continue
		}
		// 々、〆、ヶ、ヵ、〇 是常見漢字詞面的一部分，但本身不是 Han。
		switch r {
		case '々', '〆', 'ヶ', 'ヵ', '〇':
			continue
		}
		return false
	}
	return true
}

// LoadJALexCorpus 讀取題庫並驗證內部一致性。
func LoadJALexCorpus(path string) (*JALexCorpus, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var c JALexCorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	return &c, hex.EncodeToString(sum[:]), nil
}

func (c *JALexCorpus) validate() error {
	if c.Schema != JALexSchema {
		return fmt.Errorf("schema %d, want %d", c.Schema, JALexSchema)
	}
	if c.Name == "" || c.License == "" || c.Source == "" {
		return fmt.Errorf("name, license and source are required")
	}
	if len(c.Pack.SHA256) != 64 || len(c.Pack.Path) == 0 {
		return fmt.Errorf("pack sha256 and path are required")
	}
	if c.Count != len(c.Items) || c.Count == 0 || c.Count != c.SampleSize {
		return fmt.Errorf("count %d, sample_size %d and %d items disagree", c.Count, c.SampleSize, len(c.Items))
	}
	if c.HoldoutSize <= 0 || c.HoldoutSize >= c.Count {
		return fmt.Errorf("holdout_size %d is invalid", c.HoldoutSize)
	}
	if c.CandidateCap < 2 {
		return fmt.Errorf("candidate_cap is too small")
	}
	seen := map[string]bool{}
	readings := map[string]bool{}
	holdout := 0
	for _, item := range c.Items {
		if item.ID == "" || item.Reading == "" || item.Reference == "" {
			return fmt.Errorf("item has an empty id, reading or reference")
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate item %q", item.ID)
		}
		seen[item.ID] = true
		if readings[item.Reading] {
			return fmt.Errorf("duplicate reading %q", item.Reading)
		}
		readings[item.Reading] = true
		if n := len([]rune(item.Reading)); n < c.ReadingMin || n > c.ReadingMax {
			return fmt.Errorf("%s: reading length %d outside [%d,%d]", item.ID, n, c.ReadingMin, c.ReadingMax)
		}
		if len(item.Candidates) < 2 || len(item.Candidates) > c.CandidateCap {
			return fmt.Errorf("%s: %d candidates, want 2..%d", item.ID, len(item.Candidates), c.CandidateCap)
		}
		best := item.Candidates[0]
		if !isKanjiSurface(best.Surface) {
			return fmt.Errorf("%s: candidates must be Han surfaces", item.ID)
		}
		if best.Surface != item.Reference {
			return fmt.Errorf("%s: reference %q is not the first candidate", item.ID, item.Reference)
		}
		for i, cand := range item.Candidates {
			if !isKanjiSurface(cand.Surface) {
				return fmt.Errorf("%s: candidate %q is not Han", item.ID, cand.Surface)
			}
			if i > 0 && (cand.Cost < best.Cost || cand.Cost == best.Cost && cand.Surface < best.Surface) {
				return fmt.Errorf("%s: candidates are not in dictionary order", item.ID)
			}
		}
		if item.Holdout {
			holdout++
		}
	}
	if holdout != c.HoldoutSize {
		return fmt.Errorf("%d holdout items, want %d", holdout, c.HoldoutSize)
	}
	return nil
}

// JALexConverter 是統計轉換器的最小介面；*decode.Decoder 直接符合。
type JALexConverter interface {
	NBest(seq reading.Sequence, ctx lang.Context, n int) ([]lang.Result, error)
}

// JALexArmMetrics 是單一排序策略在一個切分上的分數。
type JALexArmMetrics struct {
	RankMetrics
}

// JALexSplitReport 是一個固定切分（all、holdout、dev）的兩組分數。
type JALexSplitReport struct {
	Split          string          `json:"split"`
	N              int             `json:"n"`
	Dictionary     JALexArmMetrics `json:"dictionary"`
	Ranker         JALexArmMetrics `json:"ranker"`
	RankerOutcomes map[string]int  `json:"ranker_outcomes,omitempty"`
	RankerSkipped  bool            `json:"ranker_skipped,omitempty"`
}

// JALexReport 是日文單語言 benchmark 的完整結果。
type JALexReport struct {
	Schema            int                `json:"schema"`
	Corpus            string             `json:"corpus"`
	CorpusSHA256      string             `json:"corpus_sha256"`
	PackSHA256        string             `json:"pack_sha256"`
	ConnectionsSHA256 string             `json:"connections_sha256,omitempty"`
	TopN              int                `json:"top_n"`
	Splits            []JALexSplitReport `json:"splits"`
}

// RunJALex 對題庫的每個切分跑兩組排序：
//
//   - dictionary：只依凍結的詞典成本排序。這是「沒有模型時」的基準，參考
//     答案依定義就是成本最低的詞面，所以 top-1 恆為 1；它的用途是證明題庫
//     與分母正確，不是品質成就。
//   - ranker：統計轉換器產生 N-best，再由 score.Ranker 以沒有模型的特徵式
//     回退排序。這是目前產品的實際路徑；top-1/MRR 低於 dictionary 的差距，
//     就是統計轉換與仲裁吃掉「詞典偏好」的比例。
//
// conv 為 nil 時只跑 dictionary，報告會標記 ranker_skipped。兩組都不使用
// 任何未訓練的 placeholder 分數。
func RunJALex(conv JALexConverter, corpus *JALexCorpus, corpusSHA string, topN int) (JALexReport, error) {
	if topN <= 0 {
		topN = 8
	}
	rep := JALexReport{
		Schema:       JALexSchema,
		Corpus:       corpus.Name,
		CorpusSHA256: corpusSHA,
		PackSHA256:   corpus.Pack.SHA256,
		TopN:         topN,
	}
	splits := []string{"holdout", "dev", "all"}
	for _, name := range splits {
		out := JALexSplitReport{Split: name, RankerOutcomes: map[string]int{}}
		var ranker *score.Ranker
		if conv != nil {
			ranker = score.NewRanker(score.RankerConfig{})
		} else {
			out.RankerSkipped = true
		}
		for _, item := range corpus.Items {
			if name == "holdout" && !item.Holdout || name == "dev" && item.Holdout {
				continue
			}
			out.N++
			dictSurfaces := make([]string, len(item.Candidates))
			for i, cand := range item.Candidates {
				dictSurfaces[i] = cand.Surface
			}
			out.Dictionary.Add(Ranked(dictSurfaces, item.Reference))
			if ranker == nil {
				continue
			}
			res, err := runJALexRanker(conv, ranker, item, topN)
			if err != nil {
				return JALexReport{}, err
			}
			out.RankerOutcomes[res.outcome]++
			// 沒有候選也留在分母：漏掉參考答案要拉低 MRR，不能從分母消失。
			out.Ranker.Add(res.rank)
		}
		out.Dictionary.Finalize()
		out.Ranker.Finalize()
		rep.Splits = append(rep.Splits, out)
	}
	return rep, nil
}

type jaLexRank struct {
	rank    int
	outcome string
}

func runJALexRanker(conv JALexConverter, ranker *score.Ranker, item JALexItem, topN int) (jaLexRank, error) {
	seq := jaSequence(item.Reading)
	results, err := conv.NBest(seq, lang.Context{}, topN)
	if err != nil {
		return jaLexRank{}, err
	}
	cands := make([]lattice.Candidate, 0, len(results))
	for _, res := range results {
		surface := decode.ResultSurface(res.Clauses)
		if surface == "" {
			continue
		}
		cands = append(cands, lattice.Candidate{
			Segment: segment.Segment{
				Span:     segment.Span{Start: 0, End: len(seq)},
				Script:   segment.ScriptHan,
				Reading:  append(reading.Sequence(nil), seq...),
				Surface:  surface,
				Language: lang.Tag(lang.Ja),
			},
			Cost:   lattice.Cost{Dictionary: -res.Score},
			Source: lattice.SourceDictionary,
		})
	}
	ranking, err := ranker.RankResult(context.Background(), score.RankRequest{
		Privacy:          score.PublicPrivacy(),
		PrimaryLanguage:  lang.Ja,
		PreviousLanguage: lang.LanguageTag{},
		Candidates:       cands,
	})
	if err != nil {
		return jaLexRank{}, err
	}
	surfaces := make([]string, 0, len(ranking.Items))
	for _, it := range ranking.Items {
		surfaces = append(surfaces, cands[it.Index].Surface)
	}
	return jaLexRank{rank: Ranked(surfaces, item.Reference), outcome: ranking.Outcome.String()}, nil
}

// LoadJALexDecoder 用參考 pack 建立統計轉換器。連接成本沒有包含在 lexicon
// pack 裡，因此另外讀一份固定的 connection.tsv，並在報告與 baseline 記錄
// 它的雜湊；換連接表就必須重新凍結 baseline。
func LoadJALexDecoder(packPath, connectionPath string) (*decode.Decoder, string, string, error) {
	packSHA, err := shaFile(packPath)
	if err != nil {
		return nil, "", "", err
	}
	src, err := jadiclex.Load(packPath)
	if err != nil {
		return nil, "", "", err
	}
	var conn *lexicon.Matrix
	if connectionPath != "" {
		f, err := os.Open(connectionPath)
		if err != nil {
			return nil, "", "", err
		}
		defer f.Close()
		conn, err = lexicon.LoadConnections(f)
		if err != nil {
			return nil, "", "", err
		}
	}
	connSHA := ""
	if connectionPath != "" {
		connSHA, err = shaFile(connectionPath)
		if err != nil {
			return nil, "", "", err
		}
	}
	var c lexicon.Connection
	if conn != nil {
		c = conn
	}
	return &decode.Decoder{Lex: src, Conn: c}, packSHA, connSHA, nil
}

// splitByName 回傳指定切分的報告；找不到時回傳 false。
func splitByName(rep JALexReport, name string) (JALexSplitReport, bool) {
	for _, s := range rep.Splits {
		if s.Split == name {
			return s, true
		}
	}
	return JALexSplitReport{}, false
}
