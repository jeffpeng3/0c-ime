package eval

import (
	"encoding/json"
	"fmt"
	"os"
)

// SingleBaselineSchema 是 testdata/eval/baseline.json 的格式版本。
const SingleBaselineSchema = 1

// SingleGate 是一個語言的門檻。Gated 為 false 時只記錄 baseline，不因
// 指標變動讓 CI 失敗；雜湊不符仍然失敗，因為那代表跑到的不是同一份題庫。
type SingleGate struct {
	Gated                bool    `json:"gated"`
	MaxTop1Drop          float64 `json:"max_top1_drop"`
	MaxMRRDrop           float64 `json:"max_mrr_drop"`
	MaxSentenceExactDrop float64 `json:"max_whole_sentence_drop"`
	Note                 string  `json:"note,omitempty"`
}

// ZhGoldenBaseline 是注音必須維持的數字。P2-60 的門檻是「完全不能退步」。
type ZhGoldenBaseline struct {
	CorpusSHA256      string       `json:"corpus_sha256"`
	Engine            ZhEngineInfo `json:"engine"`
	SegmentTop1Rate   float64      `json:"segment_top1_rate"`
	SegmentMRR        float64      `json:"segment_mrr"`
	Sentences         int          `json:"sentences"`
	SentenceExactRate float64      `json:"sentence_exact_rate"`
	Gate              SingleGate   `json:"gate"`
}

// JALexBaseline 是日文的 baseline 數字，不是目標。題庫的參考答案是詞典
// 偏好而非人工標註，所以這裡只凍結「現在是多少」，等有標註資料再談目標。
type JALexBaseline struct {
	CorpusSHA256       string     `json:"corpus_sha256"`
	PackSHA256         string     `json:"pack_sha256"`
	ConnectionsSHA256  string     `json:"connections_sha256,omitempty"`
	Split              string     `json:"split"`
	N                  int        `json:"n"`
	DictionaryTop1Rate float64    `json:"dictionary_top1_rate"`
	DictionaryMRR      float64    `json:"dictionary_mrr"`
	RankerTop1Rate     float64    `json:"ranker_top1_rate"`
	RankerMRR          float64    `json:"ranker_mrr"`
	Gate               SingleGate `json:"gate"`
}

// SingleBaseline 是 testdata/eval/baseline.json。
type SingleBaseline struct {
	Schema   int               `json:"schema"`
	Zhuyin   *ZhGoldenBaseline `json:"zhuyin,omitempty"`
	Japanese *JALexBaseline    `json:"japanese,omitempty"`
}

// LoadSingleBaseline 讀取 baseline。
func LoadSingleBaseline(path string) (*SingleBaseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b SingleBaseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if b.Schema != SingleBaselineSchema {
		return nil, fmt.Errorf("%s: schema %d, want %d", path, b.Schema, SingleBaselineSchema)
	}
	if b.Zhuyin == nil && b.Japanese == nil {
		return nil, fmt.Errorf("%s: no baselines", path)
	}
	return &b, nil
}

// ZhGoldenProblems 比對注音報告與 baseline。任何品質下降、題庫或資料雜湊
// 變動都是問題；注音沒有「容許誤差」。
func ZhGoldenProblems(rep ZhGoldenReport, base *ZhGoldenBaseline) []string {
	if base == nil {
		return []string{"zhuyin: no baseline entry"}
	}
	var problems []string
	if base.CorpusSHA256 != rep.CorpusSHA256 {
		problems = append(problems, fmt.Sprintf("zhuyin: corpus hash %s does not match baseline %s", rep.CorpusSHA256, base.CorpusSHA256))
	}
	if base.Engine.LexiconSHA256 != rep.Engine.LexiconSHA256 {
		problems = append(problems, fmt.Sprintf("zhuyin: lexicon hash %s does not match baseline %s", rep.Engine.LexiconSHA256, base.Engine.LexiconSHA256))
	}
	if base.Engine.LMSHA256 != rep.Engine.LMSHA256 {
		problems = append(problems, fmt.Sprintf("zhuyin: lm hash %s does not match baseline %s", rep.Engine.LMSHA256, base.Engine.LMSHA256))
	}
	check := func(name string, got, want, tolerance float64) {
		if got+tolerance < want {
			problems = append(problems, fmt.Sprintf("zhuyin: %s %.6f < baseline %.6f", name, got, want))
		}
	}
	check("segment_top1_rate", rep.Metrics.Segments.Top1Rate, base.SegmentTop1Rate, base.Gate.MaxTop1Drop)
	check("segment_mrr", rep.Metrics.Segments.MRR, base.SegmentMRR, base.Gate.MaxMRRDrop)
	check("sentence_exact_rate", rep.Metrics.SentenceExactRate, base.SentenceExactRate, base.Gate.MaxSentenceExactDrop)
	if base.Sentences != rep.Metrics.Sentences {
		problems = append(problems, fmt.Sprintf("zhuyin: %d sentences, baseline %d", rep.Metrics.Sentences, base.Sentences))
	}
	return problems
}

// JALexProblems 比對日文報告與 baseline。未設定 Gated 時只回報雜湊問題；
// 指標的增減由 JALexDelta 印出，不讓 baseline 變成未經標註的品質目標。
func JALexProblems(rep JALexReport, base *JALexBaseline) []string {
	if base == nil {
		return []string{"japanese: no baseline entry"}
	}
	var problems []string
	if base.CorpusSHA256 != rep.CorpusSHA256 {
		problems = append(problems, fmt.Sprintf("japanese: corpus hash %s does not match baseline %s", rep.CorpusSHA256, base.CorpusSHA256))
	}
	if base.PackSHA256 != rep.PackSHA256 {
		problems = append(problems, fmt.Sprintf("japanese: pack hash %s does not match baseline %s", rep.PackSHA256, base.PackSHA256))
	}
	if base.ConnectionsSHA256 != "" && rep.ConnectionsSHA256 != "" && base.ConnectionsSHA256 != rep.ConnectionsSHA256 {
		problems = append(problems, fmt.Sprintf("japanese: connections hash %s does not match baseline %s", rep.ConnectionsSHA256, base.ConnectionsSHA256))
	}
	split, ok := splitByName(rep, base.Split)
	if !ok {
		problems = append(problems, fmt.Sprintf("japanese: split %q is missing from report", base.Split))
		return problems
	}
	if split.RankerSkipped {
		problems = append(problems, "japanese: ranker arm skipped; set OC_JADIC_REFERENCE_PACK to reproduce the baseline")
		return problems
	}
	if !base.Gate.Gated {
		return problems
	}
	if base.N != split.N {
		problems = append(problems, fmt.Sprintf("japanese: n=%d, baseline %d", split.N, base.N))
	}
	check := func(name string, got, want, tolerance float64) {
		if got+tolerance < want {
			problems = append(problems, fmt.Sprintf("japanese: %s %.6f < baseline %.6f", name, got, want))
		}
	}
	check("dictionary_top1_rate", split.Dictionary.Top1Rate, base.DictionaryTop1Rate, base.Gate.MaxTop1Drop)
	check("dictionary_mrr", split.Dictionary.MRR, base.DictionaryMRR, base.Gate.MaxMRRDrop)
	check("ranker_top1_rate", split.Ranker.Top1Rate, base.RankerTop1Rate, base.Gate.MaxTop1Drop)
	check("ranker_mrr", split.Ranker.MRR, base.RankerMRR, base.Gate.MaxMRRDrop)
	return problems
}

// JALexDelta 回傳一行相對於 baseline 的變化，供未 gated 的日文 baseline
// 印在報告裡；不是問題清單，也不會讓 CI 失敗。
func JALexDelta(rep JALexReport, base *JALexBaseline) string {
	if base == nil {
		return "no japanese baseline"
	}
	split, ok := splitByName(rep, base.Split)
	if !ok {
		return fmt.Sprintf("split %q missing", base.Split)
	}
	return fmt.Sprintf(
		"split=%s n=%d dictionary top1 %+.4f mrr %+.4f | ranker top1 %+.4f mrr %+.4f",
		base.Split, split.N,
		split.Dictionary.Top1Rate-base.DictionaryTop1Rate, split.Dictionary.MRR-base.DictionaryMRR,
		split.Ranker.Top1Rate-base.RankerTop1Rate, split.Ranker.MRR-base.RankerMRR,
	)
}

// CorpusManifest 是 testdata/eval/*.manifest.json。它記錄產生題庫的來源
// 雜湊，讓「題庫換了」與「程式退步了」在報告裡可以分開。
type CorpusManifest struct {
	Schema         int        `json:"schema"`
	Corpus         string     `json:"corpus"`
	Path           string     `json:"path"`
	SHA256         string     `json:"sha256"`
	Count          int        `json:"count"`
	Holdout        int        `json:"holdout,omitempty"`
	Seed           int        `json:"seed,omitempty"`
	SampleSize     int        `json:"sample_size,omitempty"`
	Pack           *JALexPack `json:"pack,omitempty"`
	CasesSHA256    string     `json:"cases_sha256,omitempty"`
	ExpectedSHA256 string     `json:"expected_sha256,omitempty"`
}

// LoadCorpusManifest 讀取並檢查 sidecar manifest。
func LoadCorpusManifest(path string) (*CorpusManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m CorpusManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.Schema != SingleBaselineSchema || m.Corpus == "" || m.Path == "" || len(m.SHA256) != 64 {
		return nil, fmt.Errorf("%s: invalid manifest", path)
	}
	return &m, nil
}
