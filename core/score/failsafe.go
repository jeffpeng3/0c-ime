package score

import (
	"sync/atomic"
)

// Privacy 說明一次排序請求的來源欄位敏感度。
//
// 這個型別刻意「失敗時關閉」：零值 PrivacyUnspecified 與所有非 Public 的值都
// 視為私人。呼叫端必須明確標成 PublicPrivacy 才會把任何文字送進模型。忘記
// 標記時最壞的結果是少一次模型加分，不會把密碼或私人前文送出。
type Privacy uint8

const (
	// PrivacyUnspecified 是零值：來源未宣告，當成私人處理。
	PrivacyUnspecified Privacy = iota
	// PrivacyPublic 是一般可送模型、可學習的欄位。
	PrivacyPublic
	// PrivacyPrivate 是宿主標示的私人欄位（proto.Request.Private）。
	PrivacyPrivate
	// PrivacyPassword 是密碼欄位；即使內容看起來像一般文字也不送模型。
	PrivacyPassword
	// PrivacySecure 是安全輸入環境（系統密碼框、憑證輸入等）。
	PrivacySecure
)

// PublicPrivacy 回傳一般公開欄位。這個建構子是唯一會開啟模型路徑的值；
// 其他值都留在統計排序。
func PublicPrivacy() Privacy { return PrivacyPublic }

// AllowsModel 回報這個隱私等級是否允許呼叫模型。只有明確的公開欄位可以。
func (p Privacy) AllowsModel() bool { return p == PrivacyPublic }

// IsPrivate 回報是否為私人等級；零值也算私人。
func (p Privacy) IsPrivate() bool { return !p.AllowsModel() }

// String 是診斷用的固定代碼，不含輸入文字。
func (p Privacy) String() string {
	switch p {
	case PrivacyPublic:
		return "public"
	case PrivacyPrivate:
		return "private"
	case PrivacyPassword:
		return "password"
	case PrivacySecure:
		return "secure"
	default:
		return "unspecified"
	}
}

// Outcome 是一次排序請求最後由誰服務。
//
// 這是評測分母的單位：逾時、隱私跳過與統計回退都仍然是 Outcome，不會從
// 分母消失。OutcomeNone 只出現在沒有候選的請求，該請求不進分母。
type Outcome uint8

const (
	// OutcomeNone 是沒有候選、沒有模型工作的請求，不進評測分母。
	OutcomeNone Outcome = iota
	// OutcomeModel 表示至少一個模型成功評分，且分數被採用。
	OutcomeModel
	// OutcomeFallback 表示模型缺失、不可用、錯誤或回傳無效分數，改用統計排序。
	OutcomeFallback
	// OutcomeTimeout 表示模型超過本次請求的時間預算，改用統計排序。
	OutcomeTimeout
	// OutcomePrivacy 表示請求被隱私規則擋下，模型完全沒有被呼叫。
	OutcomePrivacy
)

// String 是診斷用的固定代碼。
func (o Outcome) String() string {
	switch o {
	case OutcomeModel:
		return "model"
	case OutcomeFallback:
		return "fallback"
	case OutcomeTimeout:
		return "timeout"
	case OutcomePrivacy:
		return "privacy"
	default:
		return "none"
	}
}

// Ranking 是一次排序的完整結果：候選順序與服務來源。
type Ranking struct {
	// Items 是排序後的候選；不因回退或隱私而少任何一項。
	Items []Ranked
	// Outcome 是這次請求由模型、統計回退、逾時或隱私規則服務。
	Outcome Outcome
}

// Counters 是 Ranker 的累計服務來源。
//
// 不變量：Requests == Model + Fallback + Timeout + Privacy。也就是回退與
// 隱私跳過的請求都留在分母裡，不會讓模型命中率看起來變好。
type Counters struct {
	// Requests 是已完成的排序請求數，也是評測分母。
	Requests uint64
	// Model 是模型分數被採用的請求數。
	Model uint64
	// Fallback 是模型缺失、不可用、錯誤或分數無效而改用統計排序的請求數。
	Fallback uint64
	// Timeout 是模型超過時間預算的請求數。
	Timeout uint64
	// Privacy 是被隱私規則擋下、模型完全沒被呼叫的請求數。
	Privacy uint64
}

// Degraded 回傳沒有模型貢獻的請求數，仍然算在分母裡。
func (c Counters) Degraded() uint64 { return c.Fallback + c.Timeout + c.Privacy }

// Covered 回傳四個來源的總和；正常情況下等於 Requests。
func (c Counters) Covered() uint64 { return c.Model + c.Fallback + c.Timeout + c.Privacy }

type counterSet struct {
	requests atomic.Uint64
	model    atomic.Uint64
	fallback atomic.Uint64
	timeout  atomic.Uint64
	privacy  atomic.Uint64
}

func (c *counterSet) record(outcome Outcome) {
	c.requests.Add(1)
	switch outcome {
	case OutcomeModel:
		c.model.Add(1)
	case OutcomeFallback:
		c.fallback.Add(1)
	case OutcomeTimeout:
		c.timeout.Add(1)
	case OutcomePrivacy:
		c.privacy.Add(1)
	}
}

func (c *counterSet) snapshot() Counters {
	if c == nil {
		return Counters{}
	}
	return Counters{
		Requests: c.requests.Load(),
		Model:    c.model.Load(),
		Fallback: c.fallback.Load(),
		Timeout:  c.timeout.Load(),
		Privacy:  c.privacy.Load(),
	}
}
