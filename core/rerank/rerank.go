// Package rerank 定義可選神經評分器的純資料契約；模型載入、時間與取消由宿主負責。
package rerank

import (
	"context"
	"errors"
	"math"
	"time"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/decode"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// MaxContextRunes 是送給評分器的已確認前文上限，與序列模型訓練時的前文窗口相同。
const MaxContextRunes = 64

// DefaultAsyncTimeout 是候選視窗背景評分的上限。模型回來前照統計順序顯示；
// 超過這個時間就丟棄結果，不再改動已顯示的候選（WSL 的排程尖峰可能接近 1 秒）。
const DefaultAsyncTimeout = 250 * time.Millisecond

// DefaultAsyncTopK 是背景評分只送統計前幾名。top-3 gating 比整份候選更準也更快。
const DefaultAsyncTopK = 3

// ErrAsyncTimeout 表示候選視窗的背景評分沒有在時限內回來。呼叫端保留目前順序。
var ErrAsyncTimeout = errors.New("rerank async deadline exceeded")

// MaxCandidates 是一次評分的候選上限：打出讀音的 N-best 8 個，加上輕聲擴充最多 8 個。
const MaxCandidates = 16

// ModeText 是單音節文字評分：評分器只以前文續寫機率評候選，不看讀音，
// 讓句尾助詞（嗎／吧／啦…）在打成一聲或輕聲時仍由語境決定。必須有前文。
const ModeText = "text"

// Request 只含已輸入的完整音節與既有候選，沒有參考答案或任意文件文字。
// Context 是外殼確認已寫入文件的自身提交文字（最多 MaxContextRunes 字）；
// 只有宣告需要前文的評分器才會收到，私人／暫停／英數狀態一律為空。
type Request struct {
	AllowPartial bool                `json:"allow_partial,omitempty"`
	Mode         string              `json:"mode,omitempty"` // "" 音字評分；ModeText 只看前文（單音節聲調容錯）
	Revision     int                 `json:"revision"`
	Context      string              `json:"context,omitempty"`
	Reading      []zhuyin.Syllable   `json:"reading"`
	Candidates   []decode.Hypothesis `json:"candidates"`
}

type Scorer interface {
	Score(Request) ([]float64, error)
}

// ContextScorer 是可取消的非同步評分器。候選視窗的背景評分用它；模型載入、
// 排隊與逾時由宿主負責，取消後不得再回寫任何 session 狀態。
type ContextScorer interface {
	ScoreContext(context.Context, Request) ([]float64, error)
}

// AsyncOptions 是候選視窗的背景評分設定。零值停用；Scorer 為 nil 時也停用。
type AsyncOptions struct {
	Scorer  ContextScorer
	Timeout time.Duration // 0 用 DefaultAsyncTimeout
	TopK    int           // 0 用 DefaultAsyncTopK；最多只送 3 個
}

type Options struct {
	AllowPartial bool    // 僅限經獨立校準的等長前綴候選；預設仍整例回退。
	ContextRunes int     // 0 不送前文；序列模型套件宣告 MaxContextRunes。
	HintGap      float64 // >0 時，融合分數與首選差距小於此值的次佳候選以 Tab 替換提示呈現；0 停用
	Scorer       Scorer
	Async        AsyncOptions // 候選視窗的非同步評分；不影響 rerankPath 的同步路徑
	Weight       float64
	Margin       float64
	TextWeight   float64 // >0 時單音節段以 ModeText 評分；0 停用
	TextMargin   float64
	// NeutralExpand >0 時，段落最後一個音節的基底在 NeutralBases 內且不是輕聲，
	// 就另取「最後音節改輕聲」的 N-best，把新候選的引擎分數扣 NeutralExpand 後併入。
	NeutralExpand float64
	NeutralBases  map[zhuyin.Syllable]bool
}

// AsyncTopK 回傳有效的背景評分候選數：預設 3，最多 3。
func (o AsyncOptions) AsyncTopK() int {
	k := o.TopK
	if k <= 0 {
		k = DefaultAsyncTopK
	}
	return min(k, DefaultAsyncTopK)
}

// AsyncTimeout 回傳有效的背景評分時限。
func (o AsyncOptions) AsyncTimeout() time.Duration {
	if o.Timeout <= 0 {
		return DefaultAsyncTimeout
	}
	return o.Timeout
}

// Supported 採整例回退，不以刪除 Partial/Correction 路徑製造虛假的召回改善。
func Supported(r Request) bool {
	switch r.Mode {
	case "":
		if len(r.Reading) < 2 || len(r.Reading) > 32 {
			return false
		}
	case ModeText:
		if len(r.Reading) != 1 || r.Context == "" {
			return false
		}
	default:
		return false
	}
	if len(r.Candidates) < 2 || len(r.Candidates) > MaxCandidates {
		return false
	}
	if !utf8.ValidString(r.Context) || utf8.RuneCountInString(r.Context) > MaxContextRunes {
		return false
	}
	for _, y := range r.Reading {
		if !y.Valid() {
			return false
		}
	}
	seen := map[string]bool{}
	for _, c := range r.Candidates {
		if seen[c.Text] || utf8.RuneCountInString(c.Text) != len(r.Reading) || !finite(c.LogScore) {
			return false
		}
		seen[c.Text] = true
		text := []rune(c.Text)
		end := 0
		for _, g := range c.Segments {
			if g.Start != end || g.End <= end || g.End > len(text) || (g.Partial && !r.AllowPartial) || g.Correction || g.Word != string(text[end:g.End]) {
				return false
			}
			end = g.End
		}
		if end != len(text) {
			return false
		}
	}
	return true
}
func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// Choose 同分保留原首選；無效或缺失評分整例回退。
func Choose(rows []decode.Hypothesis, scores []float64, weight, margin float64) int {
	if len(rows) == 0 || len(rows) != len(scores) || !finite(weight) || weight <= 0 || !finite(margin) || margin < 0 {
		return 0
	}
	best := 0
	values := make([]float64, len(rows))
	for i, c := range rows {
		values[i] = c.LogScore + weight*scores[i]
		if !finite(c.LogScore) || !finite(scores[i]) || !finite(values[i]) {
			return 0
		}
		if values[i] > values[best] {
			best = i
		}
	}
	if values[best]-values[0] > margin {
		return best
	}
	return 0
}
