package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/engine"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// ZhGoldenSchema 是凍結注音題庫與報告的格式版本。
const ZhGoldenSchema = 1

// ZhGoldenSegment 是題庫中的一個詞。Reading 的音節數等於 Surface 的字數；
// 兩個以上音節的詞一起排名，才能把「打完整個詞才選對」和「逐字猜對」分開。
type ZhGoldenSegment struct {
	Reading string `json:"reading"`
	Surface string `json:"surface"`
}

// ZhGoldenItem 是一個 golden 案例。Reading 與 Segments 都是標準注音，
// 音節之間以單一空格分隔。
type ZhGoldenItem struct {
	ID       string            `json:"id"`
	Case     string            `json:"case"`
	Reading  string            `json:"reading"`
	Surface  string            `json:"surface"`
	Segments []ZhGoldenSegment `json:"segments"`
}

// ZhGoldenSourcePin 記錄題庫是從哪一份 golden 凍結而來。
type ZhGoldenSourcePin struct {
	CasesSHA256    string   `json:"cases_sha256"`
	ExpectedSHA256 string   `json:"expected_sha256"`
	CaseIDs        []string `json:"case_ids"`
}

// ZhGoldenCorpus 是 testdata/eval/zhuyin-golden.json。
type ZhGoldenCorpus struct {
	Schema    int               `json:"schema"`
	Name      string            `json:"name"`
	License   string            `json:"license"`
	Source    string            `json:"source"`
	Notes     string            `json:"notes,omitempty"`
	SourcePin ZhGoldenSourcePin `json:"source_pin"`
	Count     int               `json:"count"`
	Items     []ZhGoldenItem    `json:"items"`
}

// zhGoldenCaseIDs 是刻意凍結的清單，不是「所有 golden 案例」。
// 只收一種輸入：全部由完整注音音節與 Enter 組成、沒有選項、只有一次提交、
// 最後一次組字就是提交文字。混合輸入、簡繁、模糊音與學習案例另有題庫。
var zhGoldenCaseIDs = []string{
	"single-char",
	"word",
	"long-word",
	"sentence",
	"long-sentence-1",
	"long-sentence-2",
	"long-sentence-3",
	"long-sentence-4",
	"long-sentence-5",
	"long-sentence-6",
	"long-sentence-7",
	"long-sentence-8",
	"long-sentence-9",
	"long-sentence-10",
	"heteronym-chong",
	"heteronym-chang",
	"heteronym-hao",
}

type zhGoldenCase struct {
	Name    string          `json:"name"`
	Options json.RawMessage `json:"options,omitempty"`
	Actions []struct {
		Syllables string          `json:"syllables,omitempty"`
		Bare      string          `json:"bare,omitempty"`
		Keys      string          `json:"keys,omitempty"`
		Key       string          `json:"key,omitempty"`
		Request   json.RawMessage `json:"request,omitempty"`
	} `json:"actions"`
}

// BuildZhGoldenCorpus 從 testdata/golden/zh 重新產生題庫。輸出與提交進
// repo 的檔案必須逐位元相同；測試會比對，避免有人手改題庫卻沒有留下來源。
func BuildZhGoldenCorpus(goldenDir string) (*ZhGoldenCorpus, error) {
	casesPath := filepath.Join(goldenDir, "cases.json")
	rawCases, err := os.ReadFile(casesPath)
	if err != nil {
		return nil, err
	}
	var file struct {
		Cases []zhGoldenCase `json:"cases"`
	}
	if err := json.Unmarshal(rawCases, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", casesPath, err)
	}
	byName := map[string]zhGoldenCase{}
	for _, c := range file.Cases {
		byName[c.Name] = c
	}

	corpus := &ZhGoldenCorpus{
		Schema:  ZhGoldenSchema,
		Name:    "zhuyin-golden",
		License: "CC0-1.0. Derived from the project's own T01 zhuyin golden fixtures.",
		Source:  "testdata/golden/zh/cases.json and testdata/golden/zh/expected",
		Notes: "Frozen regression corpus, not a human-annotated accuracy benchmark. " +
			"Segments are the engine's own word chunks from the golden preedit, so a change in either " +
			"candidate ranking or segmentation lowers a metric instead of silently redefining the reference.",
		SourcePin: ZhGoldenSourcePin{CaseIDs: append([]string(nil), zhGoldenCaseIDs...)},
	}
	corpus.SourcePin.CasesSHA256 = shaBytes(rawCases)

	expectedHash := sha256.New()
	for _, id := range zhGoldenCaseIDs {
		c, ok := byName[id]
		if !ok {
			return nil, fmt.Errorf("%s: golden case %q is missing", casesPath, id)
		}
		if len(c.Options) > 0 && string(c.Options) != "null" && string(c.Options) != "{}" {
			return nil, fmt.Errorf("%s: case %q has options; the zhuyin corpus only accepts defaults", casesPath, id)
		}
		reading, err := zhCaseReading(c)
		if err != nil {
			return nil, fmt.Errorf("%s: case %q: %w", casesPath, id, err)
		}
		commits, preedit, err := readZhExpected(filepath.Join(goldenDir, "expected", id+".jsonl"))
		if err != nil {
			return nil, err
		}
		if len(commits) != 1 {
			return nil, fmt.Errorf("case %q: %d commits, want exactly one", id, len(commits))
		}
		surface := commits[0]
		if err := allHan(surface); err != nil {
			return nil, fmt.Errorf("case %q: %w", id, err)
		}
		if err := allHan(strings.Join(preedit, "")); err != nil {
			return nil, fmt.Errorf("case %q final preedit: %w", id, err)
		}
		if joined := strings.Join(preedit, ""); joined != surface {
			return nil, fmt.Errorf("case %q: final preedit %q does not join to commit %q", id, joined, surface)
		}
		syllables := strings.Fields(reading)
		if len(syllables) != len([]rune(surface)) {
			return nil, fmt.Errorf("case %q: %d syllables for %d characters; segment mapping is ambiguous", id, len(syllables), len([]rune(surface)))
		}
		item := ZhGoldenItem{ID: id, Case: id, Reading: reading, Surface: surface}
		at := 0
		for _, word := range preedit {
			n := len([]rune(word))
			if at+n > len(syllables) {
				return nil, fmt.Errorf("case %q: segment %q exceeds the reading", id, word)
			}
			item.Segments = append(item.Segments, ZhGoldenSegment{
				Reading: strings.Join(syllables[at:at+n], " "),
				Surface: word,
			})
			at += n
		}
		if at != len(syllables) {
			return nil, fmt.Errorf("case %q: segments cover %d of %d syllables", id, at, len(syllables))
		}
		corpus.Items = append(corpus.Items, item)
		fmt.Fprintf(expectedHash, "%s\x00", id)
		rawExpected, err := os.ReadFile(filepath.Join(goldenDir, "expected", id+".jsonl"))
		if err != nil {
			return nil, err
		}
		expectedHash.Write(rawExpected)
	}
	corpus.SourcePin.ExpectedSHA256 = hex.EncodeToString(expectedHash.Sum(nil))
	corpus.Count = len(corpus.Items)
	if err := corpus.validate(); err != nil {
		return nil, err
	}
	return corpus, nil
}

func zhCaseReading(c zhGoldenCase) (string, error) {
	var syllables []string
	for _, a := range c.Actions {
		set := 0
		if a.Syllables != "" {
			set++
		}
		if a.Bare != "" {
			set++
		}
		if a.Keys != "" {
			set++
		}
		if a.Key != "" {
			set++
		}
		if len(a.Request) > 0 && string(a.Request) != "null" {
			set++
		}
		if set != 1 {
			return "", fmt.Errorf("action must set exactly one field")
		}
		switch {
		case a.Syllables != "":
			syllables = append(syllables, strings.Fields(a.Syllables)...)
		case a.Key == proto.KeyEnter:
			// The final Enter only commits; it does not add reading.
		default:
			return "", fmt.Errorf("unsupported golden action")
		}
	}
	if len(syllables) == 0 {
		return "", fmt.Errorf("no syllables")
	}
	return strings.Join(syllables, " "), nil
}

// readZhExpected 回傳所有 commit 與最後一次 preedit 的詞塊。expected 檔的
// 註解行以 # 起始；每個 JSON 行都是一個完整快照。
func readZhExpected(path string) (commits, preedit []string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var snap struct {
			Commit  string `json:"commit"`
			Preedit []struct {
				Text string `json:"text"`
			} `json:"preedit"`
		}
		if err := json.Unmarshal([]byte(line), &snap); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		if snap.Commit != "" {
			commits = append(commits, snap.Commit)
		}
		if len(snap.Preedit) > 0 {
			preedit = preedit[:0]
			for _, p := range snap.Preedit {
				preedit = append(preedit, p.Text)
			}
		}
	}
	if len(preedit) == 0 {
		return nil, nil, fmt.Errorf("%s: no composition preedit before commit", path)
	}
	return commits, preedit, nil
}

func allHan(s string) error {
	if s == "" {
		return fmt.Errorf("empty text")
	}
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) {
			return fmt.Errorf("%q contains non-Han rune %q", s, r)
		}
	}
	return nil
}

// LoadZhGoldenCorpus 讀取題庫並驗證內部一致性。
func LoadZhGoldenCorpus(path string) (*ZhGoldenCorpus, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var c ZhGoldenCorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	return &c, hex.EncodeToString(sum[:]), nil
}

func (c *ZhGoldenCorpus) validate() error {
	if c.Schema != ZhGoldenSchema {
		return fmt.Errorf("schema %d, want %d", c.Schema, ZhGoldenSchema)
	}
	if c.Name == "" || c.License == "" || c.Source == "" {
		return fmt.Errorf("name, license and source are required")
	}
	if c.Count != len(c.Items) || c.Count == 0 {
		return fmt.Errorf("count %d does not match %d items", c.Count, len(c.Items))
	}
	seen := map[string]bool{}
	for _, item := range c.Items {
		if item.ID == "" || item.Surface == "" || item.Reading == "" {
			return fmt.Errorf("item has an empty id, reading or surface")
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate item %q", item.ID)
		}
		seen[item.ID] = true
		if err := allHan(item.Surface); err != nil {
			return fmt.Errorf("%s: %w", item.ID, err)
		}
		var surfaces, readings []string
		for _, seg := range item.Segments {
			if seg.Surface == "" || seg.Reading == "" {
				return fmt.Errorf("%s: empty segment", item.ID)
			}
			if err := allHan(seg.Surface); err != nil {
				return fmt.Errorf("%s: %w", item.ID, err)
			}
			if got, want := len(strings.Fields(seg.Reading)), len([]rune(seg.Surface)); got != want {
				return fmt.Errorf("%s: segment %q has %d syllables for %d characters", item.ID, seg.Surface, got, want)
			}
			surfaces = append(surfaces, seg.Surface)
			readings = append(readings, strings.Fields(seg.Reading)...)
		}
		if strings.Join(surfaces, "") != item.Surface {
			return fmt.Errorf("%s: segments do not join to the surface", item.ID)
		}
		if strings.Join(readings, " ") != item.Reading {
			return fmt.Errorf("%s: segment readings do not join to the item reading", item.ID)
		}
	}
	return nil
}

// ZhEngineInfo 識別這次評測用的固定注音資料。
type ZhEngineInfo struct {
	DataDir       string `json:"data"`
	LexiconSHA256 string `json:"lexicon_sha256"`
	LMSHA256      string `json:"lm_sha256"`
}

// ZhSegmentResult 保留名次與前幾個候選，失敗時不必重跑整個題庫。
type ZhSegmentResult struct {
	Reading    string   `json:"reading"`
	Reference  string   `json:"reference"`
	Rank       int      `json:"rank"`
	Top        []string `json:"top,omitempty"`
	DurationMS float64  `json:"duration_ms"`
}

// ZhGoldenItemResult 是一個案例的完整結果。
type ZhGoldenItemResult struct {
	ID         string            `json:"id"`
	Reading    string            `json:"reading"`
	Reference  string            `json:"reference"`
	Output     string            `json:"output"`
	Exact      bool              `json:"exact"`
	Segments   []ZhSegmentResult `json:"segments"`
	DurationMS float64           `json:"duration_ms"`
}

// ZhGoldenMetrics 是 P2-60 要求的注音指標。
type ZhGoldenMetrics struct {
	Segments          RankMetrics `json:"segments"`
	Sentences         int         `json:"sentences"`
	SentencesExact    int         `json:"sentences_exact"`
	SentenceExactRate float64     `json:"sentence_exact_rate"`
}

// ZhGoldenReport 是一次注音評測的完整結果。
type ZhGoldenReport struct {
	Schema       int                  `json:"schema"`
	Corpus       string               `json:"corpus"`
	CorpusSHA256 string               `json:"corpus_sha256"`
	Engine       ZhEngineInfo         `json:"engine"`
	Metrics      ZhGoldenMetrics      `json:"metrics"`
	Items        []ZhGoldenItemResult `json:"items"`
}

// RunZhGolden 用正式引擎跑完整題庫。每個案例依序打完讀音，逐詞開候選找
// 參考詞的名次，最後再提交一次比對整句。學習保持暫停，提交不會改變之後的
// 結果；候選查詢與翻頁只讀排序，不會選字。
func RunZhGolden(eng *engine.Engine, corpus *ZhGoldenCorpus, corpusSHA string, info ZhEngineInfo) ZhGoldenReport {
	rep := ZhGoldenReport{
		Schema:       ZhGoldenSchema,
		Corpus:       corpus.Name,
		CorpusSHA256: corpusSHA,
		Engine:       info,
	}
	ty := NewTypist(eng)
	for _, item := range corpus.Items {
		start := time.Now()
		syllables := zhuyin.ParseReading(item.Reading)
		for _, s := range syllables {
			ty.Type(s)
		}
		res := ZhGoldenItemResult{
			ID:        item.ID,
			Reading:   item.Reading,
			Reference: item.Surface,
		}
		pos := 0
		for _, seg := range item.Segments {
			segStart := time.Now()
			n := len([]rune(seg.Surface))
			ty.do(proto.Request{Type: proto.Cursor, To: pos})
			snap := ty.do(proto.Request{Type: proto.Candidates, Length: n})
			cands := collectZhCandidates(ty, snap)
			ty.do(proto.Request{Type: proto.Close})
			rank := Ranked(cands, seg.Surface)
			top := cands
			if len(top) > 5 {
				top = top[:5]
			}
			res.Segments = append(res.Segments, ZhSegmentResult{
				Reading:    seg.Reading,
				Reference:  seg.Surface,
				Rank:       rank,
				Top:        top,
				DurationMS: roundMS(time.Since(segStart)),
			})
			rep.Metrics.Segments.Add(rank)
			pos += n
		}
		res.Output = ty.Commit()
		res.Exact = res.Output == item.Surface
		res.DurationMS = roundMS(time.Since(start))
		rep.Metrics.Sentences++
		if res.Exact {
			rep.Metrics.SentencesExact++
		}
		rep.Items = append(rep.Items, res)
	}
	rep.Metrics.Segments.Finalize()
	rep.Metrics.SentenceExactRate = ratio(rep.Metrics.SentencesExact, rep.Metrics.Sentences)
	return rep
}

func collectZhCandidates(t *Typist, snap proto.Snapshot) []string {
	if snap.Candidates == nil {
		return nil
	}
	var out []string
	for _, it := range snap.Candidates.Items {
		out = append(out, it.Text)
	}
	for page := 1; page < snap.Candidates.Pages; page++ {
		next := t.do(proto.Request{Type: proto.Page, Index: page})
		if next.Candidates == nil {
			continue
		}
		for _, it := range next.Candidates.Items {
			out = append(out, it.Text)
		}
	}
	return out
}

func shaBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
