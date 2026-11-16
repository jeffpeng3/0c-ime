package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/input"
	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/ja/rerank"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/session"
	"github.com/0c-biggo/0c-ime/internal/neural"
)

// 日文神經重排的宿主接線（T30b）。core 不能 import internal/neural，所以
// 由 0c-server 建立 T14 worker、把它接到 T30 rerank，再把同一個 reranker
// 掛到日文 decoder。注音 decoder 與注音 session 完全不經過這裡。

const (
	jaNeuralDefaultTimeout = 100 * time.Millisecond
	jaNeuralStateMissing   = "not_installed"
	jaNeuralStateFallback  = "fallback"
)

var errJaDataMissing = errors.New("japanese conversion data is not installed")

// jaNeuralOptions 是啟動日文神經層所需的固定路徑與預算。Timeout 同時是
// T14 單次 verify 的上限與 T30 一次轉換的總預算；兩者都預設 100 ms。
type jaNeuralOptions struct {
	Enabled  bool
	Package  string
	Python   string
	Script   string
	Server   string
	Threads  int
	Timeout  time.Duration
	Disabled bool
}

// jaNeuralWiring 是 0c-server 內共用的一份日文資源：一個延遲啟動的 T14
// worker、一個 T30 reranker，以及可註冊到多個 session 的語言 profile。
type jaNeuralWiring struct {
	host        *neural.JaNeural
	profile     lang.Profile
	reranker    *rerank.Reranker
	packagePath string
	reason      string
}

// newJaNeuralWiring 建立接線。模型檔存在才建立共享 worker；模型缺失時
// host 為 nil，profile 仍使用純統計 decoder，日文輸入照常可用。日文詞典
// 不在資料包內時回傳 errJaDataMissing，呼叫端不需要視為錯誤。
func newJaNeuralWiring(dataDir string, o jaNeuralOptions) (*jaNeuralWiring, error) {
	if o.Threads <= 0 {
		o.Threads = neural.DefaultThreads()
	}
	if o.Threads > 8 {
		o.Threads = 8
	}
	if o.Timeout <= 0 || o.Timeout > jaNeuralDefaultTimeout {
		o.Timeout = jaNeuralDefaultTimeout
	}
	w := &jaNeuralWiring{packagePath: o.Package}
	dec, err := loadJaDecoder(dataDir)
	switch {
	case errors.Is(err, errJaDataMissing):
		// 沒有日文資料包時只保留 worker 設定，之後由 T90 的日文 profile 使用。
	case err != nil:
		return nil, err
	default:
		w.profile = jaProfile{dec: dec}
	}

	present, reason := jaNeuralAvailability(o.Package)
	w.reason = reason
	if !o.Enabled || o.Disabled || !present {
		return w, nil
	}
	cfg := neural.JaConfig{
		Python: o.Python, Script: o.Script, Package: o.Package, Server: o.Server,
		Threads: o.Threads, Timeout: o.Timeout,
	}
	host := neural.NewJaNeural(neural.JaNeuralConfig{
		Open: func() (*neural.JaRuntime, error) { return neural.OpenJa(cfg) },
		// 缺模型或執行環境時不要每次按鍵都重新雜湊模型；使用者補檔後重啟引擎。
		RetryCooldown: 10 * time.Minute,
	})
	w.host = host
	if dec != nil {
		// rerank 需要 decoder 才能做限制式重搜；worker 在第一次轉換才啟動。
		w.reranker = rerank.New(dec, jaVerifier{rt: host}, rerank.Config{
			Timeout:           o.Timeout,
			PerRequestTimeout: o.Timeout,
		})
		dec.Reranker = w.reranker
	}
	return w, nil
}

// Attach 把日文 profile 註冊到 session 並切換過去。回傳 false 表示沒有
// 日文資料或 session 不接受這個 profile；注音 session 不受影響。
func (w *jaNeuralWiring) Attach(s *session.Session) bool {
	if w == nil || w.profile == nil || s == nil {
		return false
	}
	if !s.AddLanguageProfile(w.profile) {
		return false
	}
	return s.SetLanguage(lang.Ja)
}

// State 回報 worker 狀態。沒有模型時是 not_installed，模型檔存在但
// worker 載入失敗或逾時則是 fallback；日文仍使用統計轉換。
func (w *jaNeuralWiring) State() string {
	if w == nil {
		return jaNeuralStateMissing
	}
	if w.host != nil {
		return w.host.State()
	}
	if w.reason != "" {
		return jaNeuralStateFallback
	}
	return jaNeuralStateMissing
}

// Reason 回傳模型不可用的固定代碼，不含使用者文字。
func (w *jaNeuralWiring) Reason() string {
	if w == nil {
		return ""
	}
	if w.host != nil {
		return w.host.Reason()
	}
	return w.reason
}

// Stats 回傳 worker 的累計統計；沒有模型時為零值。
func (w *jaNeuralWiring) Stats() neural.JaStats {
	if w == nil || w.host == nil {
		return neural.JaStats{}
	}
	return w.host.Stats()
}

// RerankerStats 回傳 T30 的累計統計，供健康摘要與測試確認真的走了
// draft、verify、重搜，而不是只建立物件。
func (w *jaNeuralWiring) RerankerStats() rerank.Stats {
	if w == nil || w.reranker == nil {
		return rerank.Stats{}
	}
	return w.reranker.Stats()
}

// Close 停止共享 worker。可重複呼叫。
func (w *jaNeuralWiring) Close() {
	if w != nil && w.host != nil {
		w.host.Close()
	}
}

// jaVerifier 是 T14 → T30 的 production adapter。core/ja/rerank 不能 import
// internal/neural，而 internal/neural 也不能 import core/ja/rerank（T30 的
// neural_bridge_test 會造成測試 import cycle），所以轉接放在外殼這一層。
type jaVerifier struct {
	rt interface {
		VerifyContext(context.Context, neural.JaRequest) (neural.JaResult, error)
	}
}

var _ rerank.Verifier = jaVerifier{}

func (v jaVerifier) Verify(ctx context.Context, req rerank.Request) (rerank.Result, error) {
	if v.rt == nil {
		return rerank.Result{}, fmt.Errorf("%w: japanese neural runtime is not configured", rerank.ErrUnavailable)
	}
	nres, err := v.rt.VerifyContext(ctx, jaRequestForRerank(req))
	if err != nil {
		return rerank.Result{}, mapJaError(err)
	}
	out, err := jaRerankResult(nres, len(req.Candidates))
	if err != nil {
		return rerank.Result{}, fmt.Errorf("%w: %v", rerank.ErrUnsupported, err)
	}
	return out, nil
}

// jaRequestForRerank 把 T30 的請求轉成 T14 的協定形狀。讀音逐單位轉成
// 字串；draft 與候選的文節一起帶過去，讓 worker 回報已固定的文節數。
func jaRequestForRerank(req rerank.Request) neural.JaRequest {
	out := neural.JaRequest{Revision: req.Revision, LeftContext: req.LeftContext}
	out.Reading = make([]string, len(req.Reading))
	for i, u := range req.Reading {
		out.Reading[i] = string(u)
	}
	if req.Draft != nil {
		draft := jaCandidateForRerank(*req.Draft)
		out.Draft = &draft
	}
	if len(req.Candidates) > 0 {
		out.Candidates = make([]neural.JaCandidate, len(req.Candidates))
		for i, c := range req.Candidates {
			out.Candidates[i] = jaCandidateForRerank(c)
		}
	}
	return out
}

func jaCandidateForRerank(c rerank.Candidate) neural.JaCandidate {
	out := neural.JaCandidate{Surface: c.Surface}
	out.Reading = make([]string, len(c.Reading))
	for i, u := range c.Reading {
		out.Reading[i] = string(u)
	}
	if len(c.Clauses) > 0 {
		out.Clauses = make([]neural.JaClause, len(c.Clauses))
		for i, clause := range c.Clauses {
			reading := make([]string, len(clause.Reading))
			for j, u := range clause.Reading {
				reading[j] = string(u)
			}
			out.Clauses[i] = neural.JaClause{Surface: clause.Surface, Reading: reading}
		}
	}
	return out
}

// jaRerankResult 轉換 worker 回覆。Scores、Passed、PrefixOK 是 T14 為了
// T30 攤平的欄位；缺少時從 Verdicts 補，避免舊 worker 讓整次轉換失敗。
func jaRerankResult(n neural.JaResult, want int) (rerank.Result, error) {
	if len(n.Verdicts) != want {
		return rerank.Result{}, fmt.Errorf("verdict count %d, want %d", len(n.Verdicts), want)
	}
	out := rerank.Result{
		Prefix:            n.Prefix,
		FixedClauses:      n.FixedClauses,
		FixedReadingUnits: n.FixedReadingUnits,
		Elapsed:           n.Elapsed,
		Verdicts:          make([]rerank.Verdict, len(n.Verdicts)),
		Scores:            make([]float64, len(n.Verdicts)),
		Passed:            make([]bool, len(n.Verdicts)),
		PrefixOK:          make([]bool, len(n.Verdicts)),
	}
	if n.Draft != nil {
		out.Draft = &rerank.Verdict{
			Surface: n.Draft.Surface, LogProb: n.Draft.LogProb, Score: n.Draft.Score,
			Prefix: n.Draft.Prefix, Passed: n.Draft.Passed, PrefixOK: n.Draft.PrefixOK,
		}
	}
	for i, v := range n.Verdicts {
		out.Verdicts[i] = rerank.Verdict{
			Surface: v.Surface, LogProb: v.LogProb, Score: v.Score,
			Prefix: v.Prefix, Passed: v.Passed, PrefixOK: v.PrefixOK,
		}
		out.Scores[i] = v.Score
		out.Passed[i] = v.Passed
		out.PrefixOK[i] = v.PrefixOK
	}
	if len(n.Scores) == len(out.Scores) {
		copy(out.Scores, n.Scores)
	}
	if len(n.Passed) == len(out.Passed) {
		copy(out.Passed, n.Passed)
	}
	if len(n.PrefixOK) == len(out.PrefixOK) {
		copy(out.PrefixOK, n.PrefixOK)
	}
	return out, nil
}

// mapJaError 把 T14 的 sentinel 包成 core/ja/rerank 認得的類別。其他錯誤
// 原樣回傳，T30 會把它算成協議錯誤並保留統計結果。
func mapJaError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, neural.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", rerank.ErrTimeout, err)
	case errors.Is(err, neural.ErrUnavailable), errors.Is(err, context.Canceled):
		return fmt.Errorf("%w: %v", rerank.ErrUnavailable, err)
	case errors.Is(err, neural.ErrJaUnsupported):
		return fmt.Errorf("%w: %v", rerank.ErrUnsupported, err)
	default:
		return err
	}
}

// jaProfile 是 host 的日文語言 profile：composer 用 T11、converter 用 T12
// decoder。同一個 decoder 不保存輸入狀態，可同時服務多個 session；reranker
// 與 worker 也共用，因此多個輸入框不會各開一份模型。
type jaProfile struct {
	dec *jadecode.Decoder
}

var _ lang.Profile = jaProfile{}

func (p jaProfile) ID() lang.ID { return lang.Ja }

func (p jaProfile) InputMethods() []lang.InputMethod {
	return []lang.InputMethod{{ID: "romaji", Name: "ローマ字"}, {ID: "kana", Name: "かな"}}
}

func (p jaProfile) Limits() lang.Limits { return lang.DefaultLimits() }

func (p jaProfile) KeyPolicy() lang.KeyPolicy { return session.JapaneseKeyPolicy{} }

func (p jaProfile) NewComposer(method lang.InputMethod) lang.Composer {
	if method.ID == "kana" {
		return input.NewKanaComposer()
	}
	return input.NewRomajiComposer()
}

func (p jaProfile) NewConverter() lang.Converter { return p.dec }

func (p jaProfile) NewPunctuator() lang.Punctuator { return nil }

// jaNeuralAvailability 只做便宜的檔案檢查：manifest 可讀、模型檔存在而且
// 大小符合。完整雜湊與後端配對仍由 OpenJa 在背景啟動時核對。
func jaNeuralAvailability(pkg string) (bool, string) {
	if pkg == "" {
		return false, ""
	}
	b, err := os.ReadFile(filepath.Join(pkg, "manifest.json"))
	if err != nil {
		return false, ""
	}
	var m struct {
		Model struct {
			File  string `json:"file"`
			Bytes int64  `json:"bytes"`
		} `json:"model"`
	}
	if err := json.Unmarshal(b, &m); err != nil || m.Model.File == "" || filepath.Base(m.Model.File) != m.Model.File {
		return false, "model_manifest_invalid"
	}
	info, err := os.Stat(filepath.Join(pkg, m.Model.File))
	if err != nil {
		return false, "model_missing"
	}
	if info.IsDir() || m.Model.Bytes > 0 && info.Size() != m.Model.Bytes {
		return false, "model_hash_mismatch"
	}
	return true, ""
}

// findJaNeuralPackage 找日文模型套件。明確指定的路徑優先；否則看資料包
// 旁的 models/ja（原始碼樹與安裝包共用這個配置）以及資料包內的 neural/ja。
func findJaNeuralPackage(dataDir, explicit string) string {
	if explicit != "" {
		return explicit
	}
	parent := filepath.Dir(filepath.Clean(dataDir))
	for _, dir := range []string{
		filepath.Join(parent, "models", "ja"),
		filepath.Join(dataDir, "ja", "neural"),
		filepath.Join(dataDir, "neural", "ja"),
	} {
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err == nil {
			return dir
		}
	}
	return ""
}

// findJaNeuralScript 找 T14 Python worker。原始碼樹放在 platforms/neural/ja；
// 安裝包可把同一個檔案放在模型或資料包旁的 neural/ja。
func findJaNeuralScript(dataDir, pkg, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if pkg != "" {
		if path := filepath.Join(pkg, "ja_runtime.py"); fileExists(path) {
			return path
		}
	}
	parent := filepath.Dir(filepath.Clean(dataDir))
	for _, path := range []string{
		filepath.Join(parent, "platforms", "neural", "ja", "ja_runtime.py"),
		filepath.Join(parent, "neural", "ja", "ja_runtime.py"),
		filepath.Join(dataDir, "neural", "ja", "ja_runtime.py"),
	} {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

// findJaNeuralServer 找 llama-server。明確指定的路徑優先，其次 PATH；找到
// 才能啟動真模型，找不到時 OpenJa 會回 runtime_missing 並退回統計轉換。
func findJaNeuralServer(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if path, err := exec.LookPath("llama-server"); err == nil {
		return path
	}
	return ""
}

func loadJaDecoder(dataDir string) (*jadecode.Decoder, error) {
	words, conns, ok := jaDataFiles(dataDir)
	if !ok {
		return nil, errJaDataMissing
	}
	wf, err := os.Open(words)
	if err != nil {
		return nil, err
	}
	defer wf.Close()
	lex, err := jalexicon.Load(wf)
	if err != nil {
		return nil, err
	}
	cf, err := os.Open(conns)
	if err != nil {
		return nil, err
	}
	defer cf.Close()
	conn, err := jalexicon.LoadConnections(cf)
	if err != nil {
		return nil, err
	}
	return &jadecode.Decoder{Lex: lex, Conn: conn}, nil
}

func jaDataFiles(dataDir string) (words, conns string, ok bool) {
	for _, dir := range []string{filepath.Join(dataDir, "ja"), dataDir} {
		words = filepath.Join(dir, "lexicon", "words.tsv")
		conns = filepath.Join(dir, "connection.tsv")
		if fileExists(words) && fileExists(conns) {
			return words, conns, true
		}
	}
	return "", "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
