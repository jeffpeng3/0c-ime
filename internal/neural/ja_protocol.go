package neural

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// T14：日文神經驗證器的協定與套件配對。後端依 T03 選 llama.cpp（CPU）。
//
// 模型權重不進 Git，也不進資料包 manifest；models/ja/ 之下用 manifest.json
// 綁定 GGUF 雜湊與 llama.cpp commit。模型缺失或配對不符時回傳明確的
// JaModelError，呼叫端照統計轉換繼續，不影響輸入。
const (
	JaSchema         = "ja-zenz-verify-v1"
	JaBackend        = "llama.cpp"
	JaContextPolicy  = "confirmed_left_128"
	JaMaxReading     = 64
	JaMaxCandidates  = 8
	JaMaxLeftContext = 128
	JaMaxSurface     = 64
)

// T03 pin：zenz-v3.1-small Q5_K_M 與 llama.cpp v1.0.0。正式安裝把這兩個值
// 經 JaConfig 傳入，manifest 與實際模型檔都要對上；留空只給 mock／測試，
// 不代表可以載入未經驗證的權重。
const (
	JaZenzModelSHA256 = "4de930c06bef8c263aa1aa40684af206db4ce1b96375b3b8ed0ea508e0b14f6c"
	JaLlamaCPPCommit  = "2d8a32502909ca065c41c4cf5f2fb1c427c047fc"
)

// JaModelError 是模型或套件配對失敗的原因。Reason 是固定代碼，會出現在
// 狀態摘要；Cause 只給紀錄。
type JaModelError struct {
	Reason string
	Path   string
	err    error
}

func (e *JaModelError) Error() string {
	if e.Path != "" {
		return "japanese neural model " + e.Reason + ": " + e.Path
	}
	return "japanese neural model " + e.Reason
}

func (e *JaModelError) Unwrap() error { return e.err }

// JaModelError 的固定 Reason。
const (
	JaReasonManifestMissing = "model_manifest_missing"
	JaReasonManifestInvalid = "model_manifest_invalid"
	JaReasonModelMissing    = "model_missing"
	JaReasonHashMismatch    = "model_hash_mismatch"
	JaReasonBackendMismatch = "backend_mismatch"
	JaReasonContract        = "manifest_contract_mismatch"
)

// JaStartupError 是 worker 或 llama-server 起不來的原因。
type JaStartupError struct {
	Reason string
	Detail string
}

func (e *JaStartupError) Error() string {
	if e.Detail != "" {
		return "japanese neural runtime " + e.Reason + ": " + e.Detail
	}
	return "japanese neural runtime " + e.Reason
}

// JaStartupError 的固定 Reason。
const (
	JaReasonRuntimeMissing = "runtime_missing"
	JaReasonServerFailed   = "server_start_failed"
	JaReasonServerTimeout  = "server_timeout"
	JaReasonStartTimeout   = "startup_timeout"
	JaReasonStartFailed    = "startup_failed"
)

// JaCandidate 是一個文節或整句候選。Reading 必須與請求的 kana 完全相同，
// 驗證器不改變讀音對應。
type JaCandidate struct {
	Surface string     `json:"surface"`
	Reading []string   `json:"reading"`
	Clauses []JaClause `json:"clauses,omitempty"`
}

// JaClause 是 draft 的文節。前綴約束通過幾個完整文節會回報給 T30，
// 讓限制式重搜可以固定已確認的部分。
type JaClause struct {
	Reading []string `json:"reading"`
	Surface string   `json:"surface"`
}

// JaRequest 是一次 verify。Draft 是統計轉換的最佳結果，Candidates 是
// 要一起評分的候選；兩者的 Reading 都必須等於 Reading。Draft 可為 nil。
type JaRequest struct {
	Revision    int
	LeftContext string
	Reading     []string
	Draft       *JaCandidate
	Candidates  []JaCandidate
}

// JaVerdict 是單一表記的驗證結果。Score 是長度正規化後的 logprob（越高
// 越好）；LogProb 是總和，方便與其他模型比較。Prefix 是模型在該表記第一個
// 非 top-1 token 之前的高機率前綴；全部 token 都是 top-1 時等於 Surface。
type JaVerdict struct {
	Surface  string
	LogProb  float64
	Score    float64
	Prefix   string
	Passed   bool
	PrefixOK bool
}

// JaResult 是 worker 的完整回覆。Scores、Passed、PrefixOK 依 Candidates
// 順序攤平，方便 T30 直接接到 rerank；Verdicts 保留逐候選細節。
type JaResult struct {
	Prefix            string
	FixedClauses      int
	FixedReadingUnits int
	Draft             *JaVerdict
	Verdicts          []JaVerdict
	Scores            []float64
	Passed            []bool
	PrefixOK          []bool
	Elapsed           time.Duration
}

// JaManifest 是 models/ja/manifest.json 的形狀。權重檔名固定由 manifest
// 指定，但不得含路徑分隔，避免套件指向目錄外。
type JaManifest struct {
	Schema        string `json:"schema"`
	Backend       string `json:"backend"`
	BackendCommit string `json:"backend_commit"`
	ContextPolicy string `json:"context_policy"`
	Model         struct {
		File     string `json:"file"`
		SHA256   string `json:"sha256"`
		Bytes    int64  `json:"bytes"`
		Revision string `json:"revision"`
		License  string `json:"license"`
	} `json:"model"`
	Limits struct {
		MaxReadingUnits     int `json:"max_reading_units"`
		MaxCandidates       int `json:"max_candidates"`
		MaxLeftContextUnits int `json:"max_left_context_units"`
	} `json:"limits"`
	ContextSize int `json:"context_size"`
}

// JaConfig 是啟動驗證器所需的固定路徑與配對值。Timeout 上限 100 ms，
// 與 contracts.md 的神經驗證預算一致。
type JaConfig struct {
	Python        string
	Script        string
	Package       string
	Server        string
	ModelSHA256   string
	BackendCommit string
	Threads       int
	Timeout       time.Duration
	StartTimeout  time.Duration
	RetryCooldown time.Duration
}

func jaDefaults(c JaConfig) JaConfig {
	if c.Timeout == 0 {
		c.Timeout = 100 * time.Millisecond
	}
	if c.Threads == 0 {
		c.Threads = 6
	}
	if c.StartTimeout == 0 {
		c.StartTimeout = 30 * time.Second
	}
	if c.RetryCooldown == 0 {
		c.RetryCooldown = 3 * time.Second
	}
	return c
}

func jaModelError(reason, path string, err error) error {
	return &JaModelError{Reason: reason, Path: path, err: err}
}

// loadJaManifest 在啟動 worker 前核對契約、模型大小與雜湊。這一步不載入
// 模型，也不啟動 llama-server；缺模型時引擎仍可只用統計轉換。
func loadJaManifest(c JaConfig) (JaManifest, error) {
	var m JaManifest
	if c.Timeout <= 0 || c.Timeout > 100*time.Millisecond {
		return m, errors.New("japanese neural timeout must be in (0,100ms]")
	}
	if c.Threads < 1 || c.Threads > 8 {
		return m, errors.New("invalid japanese neural threads")
	}
	manifestPath := filepath.Join(c.Package, "manifest.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return m, jaModelError(JaReasonManifestMissing, manifestPath, err)
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return m, jaModelError(JaReasonManifestInvalid, manifestPath, err)
	}
	if m.Schema != JaSchema || m.ContextPolicy != JaContextPolicy {
		return m, jaModelError(JaReasonContract, manifestPath, nil)
	}
	if m.Backend != JaBackend {
		return m, jaModelError(JaReasonBackendMismatch, manifestPath, nil)
	}
	if m.BackendCommit == "" || c.BackendCommit != "" && m.BackendCommit != c.BackendCommit {
		return m, jaModelError(JaReasonBackendMismatch, manifestPath, nil)
	}
	if m.Model.File == "" || filepath.Base(m.Model.File) != m.Model.File || strings.ContainsAny(m.Model.File, `/\`) {
		return m, jaModelError(JaReasonManifestInvalid, manifestPath, nil)
	}
	if len(m.Model.SHA256) != 64 || m.Model.Bytes <= 0 {
		return m, jaModelError(JaReasonManifestInvalid, manifestPath, nil)
	}
	if c.ModelSHA256 != "" && m.Model.SHA256 != c.ModelSHA256 {
		return m, jaModelError(JaReasonHashMismatch, manifestPath, nil)
	}
	if m.Limits.MaxReadingUnits < 1 || m.Limits.MaxReadingUnits > JaMaxReading ||
		m.Limits.MaxCandidates < 1 || m.Limits.MaxCandidates > JaMaxCandidates ||
		m.Limits.MaxLeftContextUnits < 1 || m.Limits.MaxLeftContextUnits > JaMaxLeftContext {
		return m, jaModelError(JaReasonContract, manifestPath, nil)
	}
	if m.ContextSize == 0 {
		m.ContextSize = 512
	}
	if m.ContextSize < 256 || m.ContextSize > 2048 {
		return m, jaModelError(JaReasonContract, manifestPath, nil)
	}
	modelPath := filepath.Join(c.Package, m.Model.File)
	info, err := os.Stat(modelPath)
	if err != nil {
		return m, jaModelError(JaReasonModelMissing, modelPath, err)
	}
	if info.IsDir() || info.Size() != m.Model.Bytes {
		return m, jaModelError(JaReasonHashMismatch, modelPath, nil)
	}
	digest, err := fileSHA(modelPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return m, jaModelError(JaReasonModelMissing, modelPath, err)
		}
		return m, jaModelError(JaReasonManifestInvalid, modelPath, err)
	}
	if digest != m.Model.SHA256 {
		return m, jaModelError(JaReasonHashMismatch, modelPath, nil)
	}
	return m, nil
}

// validateJaRequest 在送出行程前擋掉不合協定的請求，避免把使用者文字送到
// 不會使用的 worker。Reading 必須完整、無空白，候選讀音必須完全相同。
func validateJaRequest(r JaRequest, m JaManifest) error {
	if len(r.Reading) < 1 || len(r.Reading) > m.Limits.MaxReadingUnits {
		return ErrJaUnsupported
	}
	for _, unit := range r.Reading {
		if unit == "" || strings.ContainsAny(unit, " \t\r\n") {
			return ErrJaUnsupported
		}
	}
	if len(r.Candidates) < 1 || len(r.Candidates) > m.Limits.MaxCandidates {
		return ErrJaUnsupported
	}
	if len([]rune(r.LeftContext)) > m.Limits.MaxLeftContextUnits {
		return ErrJaUnsupported
	}
	check := func(c JaCandidate) bool {
		if c.Surface == "" || len([]rune(c.Surface)) > JaMaxSurface || len(c.Reading) != len(r.Reading) {
			return false
		}
		for _, ch := range c.Surface {
			if unicode.IsControl(ch) || unicode.In(ch, unicode.Cf, unicode.Cs) {
				return false
			}
		}
		for i := range c.Reading {
			if c.Reading[i] != r.Reading[i] {
				return false
			}
		}
		for _, clause := range c.Clauses {
			if clause.Surface == "" || len(clause.Reading) == 0 {
				return false
			}
		}
		return true
	}
	for _, candidate := range r.Candidates {
		if !check(candidate) {
			return ErrJaUnsupported
		}
	}
	if r.Draft != nil && !check(*r.Draft) {
		return ErrJaUnsupported
	}
	return nil
}

// jaWireRequest／jaWireReply 是 Go 與 Python worker 的單行 JSON 介面。
type jaWireRequest struct {
	ID          uint64        `json:"id"`
	Revision    int           `json:"revision"`
	Schema      string        `json:"schema"`
	Model       string        `json:"model_sha256"`
	LeftContext string        `json:"left_context"`
	Reading     []string      `json:"reading"`
	Draft       *JaCandidate  `json:"draft,omitempty"`
	Candidates  []JaCandidate `json:"candidates"`
}

type jaWireVerdict struct {
	Surface  string  `json:"surface"`
	LogProb  float64 `json:"logprob"`
	Score    float64 `json:"score"`
	Prefix   string  `json:"prefix"`
	Passed   bool    `json:"passed"`
	PrefixOK bool    `json:"prefix_ok"`
}

type jaWireReply struct {
	ID                uint64          `json:"id"`
	Revision          int             `json:"revision"`
	Schema            string          `json:"schema"`
	Model             string          `json:"model_sha256"`
	Prefix            string          `json:"prefix"`
	FixedClauses      int             `json:"fixed_clauses"`
	FixedReadingUnits int             `json:"fixed_reading_units"`
	Draft             *jaWireVerdict  `json:"draft"`
	Candidates        []jaWireVerdict `json:"candidates"`
	Fallback          string          `json:"fallback"`
	Detail            string          `json:"detail"`
}

func (v jaWireVerdict) verdict(surface string) JaVerdict {
	if v.Surface == "" {
		v.Surface = surface
	}
	return JaVerdict{Surface: v.Surface, LogProb: v.LogProb, Score: v.Score, Prefix: v.Prefix, Passed: v.Passed, PrefixOK: v.PrefixOK}
}

// jaResult 把 worker 回覆轉成呼叫端用的結果，並核對每個候選都有對應分數。
func jaResult(reply jaWireReply, req JaRequest) (JaResult, error) {
	if len(reply.Candidates) != len(req.Candidates) {
		return JaResult{}, errors.New("japanese neural verdict count mismatch")
	}
	out := JaResult{
		Prefix:            reply.Prefix,
		FixedClauses:      reply.FixedClauses,
		FixedReadingUnits: reply.FixedReadingUnits,
		Verdicts:          make([]JaVerdict, 0, len(reply.Candidates)),
		Scores:            make([]float64, 0, len(reply.Candidates)),
		Passed:            make([]bool, 0, len(reply.Candidates)),
		PrefixOK:          make([]bool, 0, len(reply.Candidates)),
	}
	if reply.Draft != nil {
		surface := ""
		if req.Draft != nil {
			surface = req.Draft.Surface
		}
		draft := reply.Draft.verdict(surface)
		out.Draft = &draft
	}
	for i, candidate := range req.Candidates {
		if reply.Candidates[i].Surface != "" && reply.Candidates[i].Surface != candidate.Surface {
			return JaResult{}, errors.New("japanese neural candidate identity mismatch")
		}
		v := reply.Candidates[i].verdict(candidate.Surface)
		out.Verdicts = append(out.Verdicts, v)
		out.Scores = append(out.Scores, v.Score)
		out.Passed = append(out.Passed, v.Passed)
		out.PrefixOK = append(out.PrefixOK, v.PrefixOK)
	}
	return out, nil
}

// describeJaModelError 回傳穩定代碼；其他錯誤歸為 failed。
func describeJaModelError(err error) string {
	var model *JaModelError
	if errors.As(err, &model) {
		return model.Reason
	}
	var startup *JaStartupError
	if errors.As(err, &startup) {
		return startup.Reason
	}
	return JaReasonStartFailed
}

func (e *JaStartupError) wrap(err error) error {
	if err == nil {
		return e
	}
	return fmt.Errorf("%w: %v", e, err)
}
