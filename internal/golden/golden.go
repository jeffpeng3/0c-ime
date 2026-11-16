// Package golden 凍結注音輸入的協定層級行為（T01）。
//
// 每個 case 用一串按鍵／請求驅動正式引擎（engine.Handle → session.Handle），把每一步的
// proto.Snapshot JSON 寫成一行；expected/ 底下的檔案就是 baseline。預設只比對，-update 才重寫。
//
// 詞庫與語言模型固定放在 testdata/golden/zh/data，與 build/data、data/ 的內容無關：資料包改版
// 不應該讓行為 baseline 失敗，反過來說，這份 baseline 只證明核心行為沒變，不證明選詞品質。
// 神經重排與補標點用套件內固定的替身評分器，確定性可重現；它們測的是呼叫路徑與契約，不是模型品質。
package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/0c-biggo/0c-ime/core/engine"
	"github.com/0c-biggo/0c-ime/core/english"
	"github.com/0c-biggo/0c-ime/core/fuzzy"
	"github.com/0c-biggo/0c-ime/core/layout"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/punct"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/core/suggest"
	"github.com/0c-biggo/0c-ime/core/vocab"
	"github.com/0c-biggo/0c-ime/core/zhconv"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
	"github.com/0c-biggo/0c-ime/internal/simplified"
)

// Format 是 cases.json 的格式版本；改變欄位語意時加一。
const Format = 1

// File 是 testdata/golden/zh/cases.json。
type File struct {
	Format int    `json:"format"`
	Cases  []Case `json:"cases"`
}

// Case 是一個 baseline 案例。
type Case struct {
	Name    string   `json:"name"`
	Note    string   `json:"note,omitempty"`
	Options Options  `json:"options,omitempty"`
	Seed    Seed     `json:"seed,omitempty"`
	Actions []Action `json:"actions"`
}

// Action 是驅動 session 的一步。五種欄位只能填一種。
type Action struct {
	Label     string         `json:"label,omitempty"`
	Syllables string         `json:"syllables,omitempty"` // 以標準排列的完整按鍵打出每個音節（含聲調）
	Bare      string         `json:"bare,omitempty"`      // 逐符號打；空白是空白鍵（聲調選填的一聲／分隔）
	Keys      string         `json:"keys,omitempty"`      // 逐字元送 ASCII 或具名鍵
	Key       string         `json:"key,omitempty"`       // 送一個具名鍵（Enter、Down、Ctrl+, …）
	Request   *proto.Request `json:"request,omitempty"`   // 直接送協定請求（select、page、forget、accept…）
}

// Options 是每個 case 的引擎設定，對應 host 在 OpenSession 前的設定。
type Options struct {
	Toneless            bool              `json:"toneless,omitempty"`
	MixedInput          bool              `json:"mixed_input,omitempty"`
	EnglishAutocomplete bool              `json:"english_autocomplete,omitempty"`
	Simplified          bool              `json:"simplified,omitempty"`
	CJKLatinSpace       bool              `json:"cjk_latin_space,omitempty"`
	CtrlPunct           bool              `json:"ctrl_punct,omitempty"`
	ZhuyinErrorHint     bool              `json:"zhuyin_error_hint,omitempty"`
	EscKeepComposition  bool              `json:"esc_keep_composition,omitempty"`
	SpaceInsertsSpace   bool              `json:"space_inserts_space,omitempty"`
	Preview02           *bool             `json:"preview02,omitempty"` // 預設 true，與 0.2.0 起的外殼一致
	SelectionKeys       string            `json:"selection_keys,omitempty"`
	Fuzzy               map[string]string `json:"fuzzy,omitempty"`   // 配對 id → select／auto
	Suggest             *Suggest          `json:"suggest,omitempty"` // Tab 補全門檻
	Rerank              *Rerank           `json:"rerank,omitempty"`  // 固定替身評分器
	Punct               *Punct            `json:"punct,omitempty"`   // 固定替身補標點
}

// Suggest 是 suggest.Options 的可選覆寫；nil 欄位保留預設值。
type Suggest struct {
	MinConf        *float64 `json:"min_conf,omitempty"`
	MinSyllables   *int     `json:"min_syllables,omitempty"`
	AttentionCost  *float64 `json:"attention_cost,omitempty"`
	RewritePrefix  *bool    `json:"rewrite_prefix,omitempty"`
	MinRewriteConf *float64 `json:"min_rewrite_conf,omitempty"`
}

// Rerank 開啟神經重排的呼叫路徑，換上固定替身評分器：候選文字含有 Prefer 任一項就加 PreferScore。
// 這不是模型，只用來證明重排開啟、關閉與否決規則這條路徑有被走過。
type Rerank struct {
	Weight      float64  `json:"weight"`
	Margin      float64  `json:"margin,omitempty"`
	HintGap     float64  `json:"hint_gap,omitempty"`
	Prefer      []string `json:"prefer"`
	PreferScore float64  `json:"prefer_score,omitempty"`
}

// Punct 開啟提交時補標點，換上固定替身：在最後一個漢字前插入 Mark。
type Punct struct {
	Mark string `json:"mark"`
}

// Seed 是 case 開始前的個人資料，讓反悔、忘記、學習排序有可重現的起點。
type Seed struct {
	Unigrams map[string]float64 `json:"unigrams,omitempty"`
	Bigrams  map[string]float64 `json:"bigrams,omitempty"` // "左詞\t詞"
	Total    float64            `json:"total,omitempty"`
	Vocab    []VocabEntry       `json:"vocab,omitempty"`
}

// VocabEntry 是自訂詞。
type VocabEntry struct {
	Text     string   `json:"text"`
	Readings []string `json:"readings"`
}

// Suite 是載入固定詞庫與案例後的 runner。
type Suite struct {
	dir   string
	cases []Case
	lex   lexicon.Source
	sys   lm.System
	conv  *zhconv.Converter
}

// Load 載入 dir 底下的 cases.json、data/lexicon.tsv 與 data/lm.tsv。
func Load(dir string) (*Suite, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		return nil, err
	}
	var f File
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("cases.json：%w", err)
	}
	if f.Format != Format {
		return nil, fmt.Errorf("cases.json：格式 %d，本版支援 %d", f.Format, Format)
	}
	if len(f.Cases) == 0 {
		return nil, fmt.Errorf("cases.json：沒有案例")
	}
	seen := map[string]bool{}
	for i, c := range f.Cases {
		if c.Name == "" || strings.ContainsAny(c.Name, `/\`) || c.Name == "." || c.Name == ".." {
			return nil, fmt.Errorf("第 %d 個案例名稱 %q 不能當檔名", i+1, c.Name)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("案例名稱重複：%s", c.Name)
		}
		seen[c.Name] = true
		if len(c.Actions) == 0 {
			return nil, fmt.Errorf("%s：沒有動作", c.Name)
		}
		for j, a := range c.Actions {
			if err := a.validate(); err != nil {
				return nil, fmt.Errorf("%s 第 %d 步：%w", c.Name, j+1, err)
			}
		}
	}
	lex, sys, err := loadData(filepath.Join(dir, "data"))
	if err != nil {
		return nil, err
	}
	return &Suite{dir: dir, cases: f.Cases, lex: lex, sys: sys}, nil
}

// Cases 回傳載入的案例（複本）。
func (s *Suite) Cases() []Case { return append([]Case(nil), s.cases...) }

// loadData 讀來源格式的 TSV，走與正式資料包相同的緊湊編碼／解碼路徑，避免 baseline 只測到
// 記憶體實作而漏掉打包格式的差異。
func loadData(dir string) (lexicon.Source, lm.System, error) {
	lf, err := os.Open(filepath.Join(dir, "lexicon.tsv"))
	if err != nil {
		return nil, nil, err
	}
	defer lf.Close()
	mem, err := lexicon.Load(lf)
	if err != nil {
		return nil, nil, fmt.Errorf("lexicon.tsv：%w", err)
	}
	mf, err := os.Open(filepath.Join(dir, "lm.tsv"))
	if err != nil {
		return nil, nil, err
	}
	defer mf.Close()
	sys, err := lm.Load(mf)
	if err != nil {
		return nil, nil, fmt.Errorf("lm.tsv：%w", err)
	}
	var entries []lexicon.Entry
	mem.Walk(func(e lexicon.Entry) { entries = append(entries, e) })
	built, err := lexicon.Build(entries)
	if err != nil {
		return nil, nil, err
	}
	lex, err := lexicon.Decode(built.Encode())
	if err != nil {
		return nil, nil, err
	}
	lmb, err := lm.BuildCompact(sys.Entries())
	if err != nil {
		return nil, nil, err
	}
	compact, err := lm.DecodeCompact(lmb.Encode())
	if err != nil {
		return nil, nil, err
	}
	return lex, compact, nil
}

// Output 跑完一個 case，回傳要寫進 expected/<name>.jsonl 的內容。同一個 case 連跑兩次，
// 輸出必須逐位元組相同，否則回傳錯誤（完成條件：同一組輸入連跑兩次輸出完全相同）。
func (s *Suite) Output(c Case) ([]byte, error) {
	first, err := s.run(c)
	if err != nil {
		return nil, err
	}
	second, err := s.run(c)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(first, second) {
		return nil, fmt.Errorf("%s：同一組輸入連跑兩次輸出不同", c.Name)
	}
	return first, nil
}

func (s *Suite) run(c Case) ([]byte, error) {
	eng, err := s.engine(c)
	if err != nil {
		return nil, err
	}
	id := eng.OpenSession()
	defer eng.CloseSession(id)
	if o := c.Options.Suggest; o != nil {
		sess := eng.Session(id)
		sess.Opt.Suggest = SuggestOptions(sess.Opt.Suggest, o)
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "# case: %s\n", c.Name)
	if c.Note != "" {
		fmt.Fprintf(&out, "# note: %s\n", c.Note)
	}
	for i, chunk := range c.steps() {
		fmt.Fprintf(&out, "# step %02d %s\n", i+1, chunk.label)
		var snap []byte
		for _, req := range chunk.requests {
			raw, err := json.Marshal(req)
			if err != nil {
				return nil, err
			}
			snap, err = eng.Handle(id, raw)
			if err != nil {
				return nil, fmt.Errorf("%s 第 %d 步（%s）：%w", c.Name, i+1, chunk.label, err)
			}
		}
		// 一個步驟是一段有意義的輸入（一個音節、一個英文詞），只留最後的快照，diff 才讀得懂。
		if snap != nil {
			out.Write(snap)
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

// engine 依 case 的設定建立引擎；每個 case 一個新引擎，學習狀態不互相污染。
func (s *Suite) engine(c Case) (*engine.Engine, error) {
	eng := engine.New(s.lex, s.sys)
	eng.English = english.Prototype()
	eng.Preview02 = true
	eng.Toneless = c.Options.Toneless
	eng.MixedInput = c.Options.MixedInput
	eng.EnglishAutocomplete = c.Options.EnglishAutocomplete
	eng.CJKLatinSpace = c.Options.CJKLatinSpace
	eng.CtrlPunct = c.Options.CtrlPunct
	eng.ZhuyinErrorHint = c.Options.ZhuyinErrorHint
	eng.EscKeepComposition = c.Options.EscKeepComposition
	eng.SpaceInsertsSpace = c.Options.SpaceInsertsSpace
	eng.SelectionKeys = c.Options.SelectionKeys
	if c.Options.Preview02 != nil {
		eng.Preview02 = *c.Options.Preview02
	}
	if c.Options.Fuzzy != nil {
		for id, mode := range c.Options.Fuzzy {
			p, ok := fuzzy.ParsePair(id)
			if !ok {
				return nil, fmt.Errorf("%s：未知的模糊音配對 %q", c.Name, id)
			}
			m, ok := fuzzy.ParseMode(mode)
			if !ok {
				return nil, fmt.Errorf("%s：未知的模糊音模式 %q", c.Name, mode)
			}
			eng.Fuzzy = eng.Fuzzy.With(p, m)
		}
	}
	if c.Options.Simplified {
		conv, err := s.simplify()
		if err != nil {
			return nil, err
		}
		eng.Simplified = conv
	}
	if r := c.Options.Rerank; r != nil {
		if r.Weight <= 0 {
			return nil, fmt.Errorf("%s：rerank.weight 須大於 0", c.Name)
		}
		score := r.PreferScore
		if score == 0 {
			score = 10
		}
		eng.Rerank = rerank.Options{Weight: r.Weight, Margin: r.Margin, HintGap: r.HintGap,
			Scorer: stubScorer{prefer: r.Prefer, score: score}}
	}
	if p := c.Options.Punct; p != nil {
		if p.Mark == "" {
			return nil, fmt.Errorf("%s：punct.mark 不能是空的", c.Name)
		}
		eng.Punct = stubPunctuator{mark: p.Mark}
	}
	if err := seed(eng.Model, c.Seed); err != nil {
		return nil, fmt.Errorf("%s：%w", c.Name, err)
	}
	return eng, nil
}

func (s *Suite) simplify() (*zhconv.Converter, error) {
	if s.conv == nil {
		conv, err := simplified.TW2S()
		if err != nil {
			return nil, err
		}
		s.conv = conv
	}
	return s.conv, nil
}

func seed(m *personal.Model, sd Seed) error {
	for w, p := range sd.Unigrams {
		m.Store.Uni[w] = p
	}
	for k, p := range sd.Bigrams {
		if !strings.Contains(k, "\t") {
			return fmt.Errorf("bigram 鍵 %q 須為「左詞\\t詞」", k)
		}
		m.Store.Bi[k] = p
	}
	if sd.Total != 0 {
		m.Store.Total = sd.Total
	}
	if len(sd.Vocab) > 0 {
		var entries []vocab.Entry
		for _, v := range sd.Vocab {
			if v.Text == "" || len(v.Readings) == 0 {
				return fmt.Errorf("自訂詞需要 text 與 readings")
			}
			entries = append(entries, vocab.Entry{Text: v.Text, Readings: v.Readings})
		}
		// 固定時鐘（model.Now 為 nil）讓批次時間可重現。
		if _, err := m.VocabAdd(entries, vocab.SourceManual); err != nil {
			return err
		}
	}
	return nil
}

// stubScorer 是 rerank.Scorer 的固定替身：候選文字含有偏好字串就加分。
type stubScorer struct {
	prefer []string
	score  float64
}

func (s stubScorer) Score(r rerank.Request) ([]float64, error) {
	out := make([]float64, len(r.Candidates))
	for i, c := range r.Candidates {
		for _, p := range s.prefer {
			if p != "" && strings.Contains(c.Text, p) {
				out[i] = s.score
				break
			}
		}
	}
	return out, nil
}

// stubPunctuator 是 punct.Punctuator 的固定替身：在最後一個漢字前插入設定的標點。
type stubPunctuator struct{ mark string }

func (p stubPunctuator) Punctuate(text string) ([]punct.Edit, error) {
	r := []rune(text)
	for i := len(r) - 1; i > 0; i-- {
		if isHan(r[i]) && isHan(r[i-1]) {
			return []punct.Edit{{Offset: i, Text: p.mark}}, nil
		}
	}
	return nil, nil
}

func isHan(r rune) bool {
	return r >= 0x4e00 && r <= 0x9fff || r >= 0x3400 && r <= 0x4dbf
}

// step 是一組請求，輸出只留最後一個快照。
type step struct {
	label    string
	requests []proto.Request
}

func (c Case) steps() []step {
	var out []step
	for _, a := range c.Actions {
		out = append(out, a.steps()...)
	}
	return out
}

func (a Action) validate() error {
	n := 0
	for _, set := range []bool{a.Syllables != "", a.Bare != "", a.Keys != "", a.Key != "", a.Request != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("syllables、bare、keys、key、request 必須恰好填一個")
	}
	if a.Request != nil && a.Request.Type == "" {
		return fmt.Errorf("request.type 不能是空的")
	}
	return nil
}

func (a Action) steps() []step {
	switch {
	case a.Syllables != "":
		var out []step
		for _, syl := range strings.Fields(a.Syllables) {
			s := step{label: a.label("syllables " + syl)}
			for _, k := range layoutKeys(syl) {
				s.requests = append(s.requests, keyRequest(k))
			}
			out = append(out, s)
		}
		return out
	case a.Bare != "":
		var out []step
		chunks := strings.Split(a.Bare, " ")
		for i, chunk := range chunks {
			s := step{label: a.label("bare " + chunk)}
			for _, r := range chunk {
				s.requests = append(s.requests, keyRequest(bareKey(r)))
			}
			if i < len(chunks)-1 {
				s.requests = append(s.requests, keyRequest(" "))
				s.label += " + space"
			}
			out = append(out, s)
		}
		return out
	case a.Keys != "":
		s := step{label: a.label("keys " + a.Keys)}
		for _, r := range a.Keys {
			s.requests = append(s.requests, keyRequest(string(r)))
		}
		return []step{s}
	case a.Key != "":
		return []step{{label: a.label("key " + a.Key), requests: []proto.Request{keyRequest(a.Key)}}}
	default:
		return []step{{label: a.label("request " + a.Request.Type), requests: []proto.Request{*a.Request}}}
	}
}

func (a Action) label(fallback string) string {
	if a.Label != "" {
		return a.Label
	}
	return fallback
}

func keyRequest(k string) proto.Request {
	return proto.Request{Type: proto.Key, Key: k}
}

// layoutKeys 回傳標準排列打出一個音節（音節字串可含聲調）的按鍵；沒有聲調時 layout.Keys
// 會補一聲的空白，與外殼實際送出的按鍵相同。
func layoutKeys(syl string) []string {
	var out []string
	for _, k := range layout.Standard.Keys(zhuyin.Syllable(syl)) {
		out = append(out, string(k))
	}
	return out
}

// firstKey 是標準排列打出一個注音符號或聲調的按鍵。
func firstKey(r rune) string {
	keys := []rune(layout.Standard.Keys(zhuyin.Syllable(string(r))))
	if len(keys) == 0 {
		return string(r)
	}
	return string(keys[0])
}

// bareKey：ASCII 字元照原樣送（聲調選填的分隔鍵 '、方向鍵以外的符號），注音符號與聲調走標準排列。
func bareKey(r rune) string {
	if r < 0x80 {
		return string(r)
	}
	return firstKey(r)
}

// Filenames 回傳所有案例的 expected 檔名（依名稱排序）。
func (s *Suite) Filenames() []string {
	names := make([]string, len(s.cases))
	for i, c := range s.cases {
		names[i] = c.Name + ".jsonl"
	}
	sort.Strings(names)
	return names
}

// Compare 比對 dir/expected 下的 baseline；回傳的 error 列出第一個不同的地方。
func (s *Suite) Compare() error {
	var failures []string
	for _, c := range s.cases {
		want, err := os.ReadFile(filepath.Join(s.dir, "expected", c.Name+".jsonl"))
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s：%v（以 -update 產生）", c.Name, err))
			continue
		}
		got, err := s.Output(c)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s：%v", c.Name, err))
			continue
		}
		if !bytes.Equal(got, want) {
			failures = append(failures, fmt.Sprintf("%s：與 baseline 不符\n%s\n確認是預期的行為改變後以 -update 重寫", c.Name, Diff(want, got)))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "\n\n"))
	}
	return nil
}

// Update 以目前行為重寫 dir/expected；回傳寫入與刪除的檔案。
func (s *Suite) Update() (written, removed []string, err error) {
	dir := filepath.Join(s.dir, "expected")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	keep := map[string]bool{}
	for _, c := range s.cases {
		out, err := s.Output(c)
		if err != nil {
			return written, removed, err
		}
		name := c.Name + ".jsonl"
		keep[name] = true
		if err := os.WriteFile(filepath.Join(dir, name), out, 0o644); err != nil {
			return written, removed, err
		}
		written = append(written, name)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return written, removed, err
	}
	for _, e := range entries {
		if e.IsDir() || keep[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return written, removed, err
		}
		removed = append(removed, e.Name())
	}
	sort.Strings(written)
	sort.Strings(removed)
	return written, removed, nil
}

// Diff 顯示第一處不同的行與前後文；沒有 diff 套件也能讀懂 CI 的失敗訊息。
func Diff(want, got []byte) string {
	w := strings.Split(string(want), "\n")
	g := strings.Split(string(got), "\n")
	at := 0
	for at < len(w) && at < len(g) && w[at] == g[at] {
		at++
	}
	if at == len(w) && at == len(g) {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "第 %d 行不同\n", at+1)
	for i := max(at-3, 0); i < at; i++ {
		fmt.Fprintf(&b, "  %s\n", w[i])
	}
	if at < len(w) {
		fmt.Fprintf(&b, "- %s\n", w[at])
	}
	if at < len(g) {
		fmt.Fprintf(&b, "+ %s\n", g[at])
	}
	for i := at + 1; i < min(at+3, len(w)); i++ {
		fmt.Fprintf(&b, "  %s\n", w[i])
	}
	return strings.TrimRight(b.String(), "\n")
}

// SuggestOptions 把 case 的覆寫套到 suggest 預設值；給 session 層使用。
func SuggestOptions(base suggest.Options, o *Suggest) suggest.Options {
	if o == nil {
		return base
	}
	if o.MinConf != nil {
		base.MinConf = *o.MinConf
	}
	if o.MinSyllables != nil {
		base.MinSyllables = *o.MinSyllables
	}
	if o.AttentionCost != nil {
		base.AttentionCost = *o.AttentionCost
	}
	if o.RewritePrefix != nil {
		base.RewritePrefix = *o.RewritePrefix
	}
	if o.MinRewriteConf != nil {
		base.MinRewriteConf = *o.MinRewriteConf
	}
	return base
}
