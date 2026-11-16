package rerank

import (
	"math"
	"time"
)

// Weights 是統計分數、神經分數與統計 draft 的融合權重。
//
// Stats 作用在 [0,1] 正規化後的統計分數上，Draft 是統計第一名的固定加分。
// Neural 直接乘上驗證器的長度正規化 logprob。預設值讓神經分數主導，統計
// 只在神經分數接近時拆解平手，避免統計成本尺度較大時吃掉神經訊號。
type Weights struct {
	Neural float64
	Stats  float64
	Draft  float64
}

// Observation 是一次 rerank 的摘要，不含使用者文字。
type Observation struct {
	ReadingUnits int
	Drafts       int
	Candidates   int
	Rounds       int
	Verified     int
	Elapsed      time.Duration
	Fallback     string
	Passed       bool
	Selected     bool
	Changed      bool
}

// Observer 接收 rerank 摘要，供狀態列、紀錄或測試使用。
type Observer interface {
	Observe(Observation)
}

// ObserverFunc 讓函式可以直接當 Observer。
type ObserverFunc func(Observation)

// Observe 呼叫 f。
func (f ObserverFunc) Observe(o Observation) { f(o) }

// Stats 是 rerank 的累計統計。內容不含文字。
type Stats struct {
	Calls       int   `json:"calls"`
	Verified    int   `json:"verified"`
	Timeouts    int   `json:"timeouts"`
	Unavailable int   `json:"unavailable"`
	Errors      int   `json:"errors"`
	BudgetStops int   `json:"budget_stops"`
	Rounds      int   `json:"rounds"`
	TotalUS     int64 `json:"total_us"`
}

// Config 是 rerank 的開關、預算與權重。
//
// Disabled 是完整停用神經層的開關；停用時 Rerank 不呼叫驗證器，直接回傳
// 統計結果。Timeout 是單次轉換的總預算，預設 100 ms，與 contracts.md 的
// 神經驗證預算一致。
type Config struct {
	Disabled          bool
	Timeout           time.Duration
	PerRequestTimeout time.Duration
	MaxRounds         int
	MaxCandidates     int
	// ResearchCandidates 是每次重搜保留的路徑數。沒有可信前綴時，重搜
	// 退化成較寬的統計搜尋，用比 verify 上限更多的候選擴充覆蓋率。
	ResearchCandidates  int
	MaxReadingUnits     int
	MaxLeftContextUnits int
	Weights             Weights
	// MinGain 是換掉統計第一名所需的最小融合分數差距。0 表示只要融合
	// 分數較高就換；正的值可以抑制模型在雜訊上的翻盤。
	MinGain float64
	// PassThreshold 是通過候選仍須達到的最低神經分數。-Inf 表示不設限。
	PassThreshold float64
	Observer      Observer
}

// DefaultConfig 回傳正式路徑的預設值。
func DefaultConfig() Config {
	return Config{
		Timeout:             100 * time.Millisecond,
		PerRequestTimeout:   80 * time.Millisecond,
		MaxRounds:           2,
		MaxCandidates:       8,
		ResearchCandidates:  24,
		MaxReadingUnits:     64,
		MaxLeftContextUnits: 128,
		Weights:             Weights{Neural: 1, Stats: 0.2, Draft: 0.1},
		PassThreshold:       math.Inf(-1),
	}
}

// normalized 補上零值欄位。零值 Config 等同 DefaultConfig，除了 Disabled
// 保持 false，所以直接宣告 Config{} 也是啟用狀態。
func (c Config) normalized() Config {
	d := DefaultConfig()
	if c.Timeout <= 0 {
		c.Timeout = d.Timeout
	}
	if c.PerRequestTimeout <= 0 || c.PerRequestTimeout > c.Timeout {
		c.PerRequestTimeout = c.Timeout
	}
	if c.MaxRounds <= 0 {
		c.MaxRounds = d.MaxRounds
	}
	if c.MaxCandidates <= 0 {
		c.MaxCandidates = d.MaxCandidates
	}
	// T14 協定一次最多 8 條候選；rerank 不能超過驗證器上限。
	if c.MaxCandidates > 8 {
		c.MaxCandidates = 8
	}
	if c.ResearchCandidates <= 0 {
		c.ResearchCandidates = d.ResearchCandidates
	}
	if c.ResearchCandidates < c.MaxCandidates {
		c.ResearchCandidates = c.MaxCandidates
	}
	if c.MaxReadingUnits <= 0 || c.MaxReadingUnits > 64 {
		c.MaxReadingUnits = d.MaxReadingUnits
	}
	if c.MaxLeftContextUnits <= 0 || c.MaxLeftContextUnits > 128 {
		c.MaxLeftContextUnits = d.MaxLeftContextUnits
	}
	if c.Weights.Neural == 0 {
		c.Weights.Neural = d.Weights.Neural
	}
	if c.Weights.Stats == 0 {
		c.Weights.Stats = d.Weights.Stats
	}
	if c.Weights.Draft == 0 {
		c.Weights.Draft = d.Weights.Draft
	}
	if c.PassThreshold == 0 {
		c.PassThreshold = d.PassThreshold
	}
	return c
}
