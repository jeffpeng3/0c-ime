package score

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
)

// 特徵式仲裁（P2-31）。
//
// 這個檔案只做排序：把不同語言的候選放進同一個清單，依固定公式混合前文
// 語言、片段語言、模型分數、詞典與模糊成本、切換成本、主要語言先驗與
// 使用者歷史。它不讀取全域 active language，也不假設呼叫端先選語言。
//
// 第一版不線上學習權重。下面的數字全部是 placeholder，等 P2-60、P2-61、
// P2-62 有評測資料後再離線校準；調整前先補對應的 scenario 測試。

// Weights 是候選特徵的混合權重。分數越高越優先。
//
// 成本類特徵（Dictionary、Fuzzy、Switch）會從分數中扣除；模型分數、
// 使用者歷史與語言偏好會加分。
type Weights struct {
	// Model 是校準後模型分數的權重。
	Model float64
	// Dictionary 是詞典成本的權重。
	Dictionary float64
	// Fuzzy 是模糊輸入額外成本的權重。
	Fuzzy float64
	// Switch 是語言切換成本的權重。
	Switch float64
	// PreviousLanguage 是候選語言與前文語言相同時的加分。
	PreviousLanguage float64
	// PrimaryLanguage 是候選語言等於主要語言時的先驗加分。
	PrimaryLanguage float64
	// History 是 core/personal 使用者歷史的權重。
	History float64
	// LanguageConfidence 是目前片段語言標籤可信度的權重。
	LanguageConfidence float64
}

// 第一版固定常數。全部是 placeholder，不是訓練結果。
const (
	// DefaultModelWeight 暫時與成本同權，讓模型只在校準後才有影響。
	DefaultModelWeight = 1.0
	// DefaultDictionaryWeight 讓詞典證據可以抵銷一次語言切換。
	DefaultDictionaryWeight = 1.0
	// DefaultFuzzyWeight 與詞典同權；模糊輸入只在其他證據接近時才輸。
	DefaultFuzzyWeight = 1.0
	// DefaultSwitchWeight 與 lattice 的預設切換成本同量級。
	DefaultSwitchWeight = 1.0
	// DefaultPreviousLanguageWeight 是延續前文語言的弱偏好，不足以蓋過詞典。
	DefaultPreviousLanguageWeight = 0.35
	// DefaultPrimaryLanguageWeight 是主要語言先驗，必須小於常見詞典差距。
	DefaultPrimaryLanguageWeight = 0.25
	// DefaultHistoryWeight 是使用者歷史的弱權重，單次學習不會翻轉強詞典證據。
	DefaultHistoryWeight = 0.5
	// DefaultLanguageConfidenceWeight 獎勵分段器較有把握的語言標籤。
	DefaultLanguageConfidenceWeight = 0.2
	// DefaultSwitchCost 與 lattice.DefaultConfig 相同，方便兩層結果一致。
	DefaultSwitchCost = 0.5
	// DefaultPrimarySwitchCost 是涉及主要語言時的較低切換成本。
	DefaultPrimarySwitchCost = 0.1
	// DefaultModelTimeout 是一次請求的模型評分時間預算。
	//
	// 這是 placeholder：輸入法每鍵都要在數十毫秒內回應，模型只能分到一小段。
	// 真正的數值等 P2-60 的延遲評測後再校準；調大之前先確認最慢按鍵仍在預算內。
	DefaultModelTimeout = 25 * time.Millisecond
)

// DefaultWeights 回傳第一版固定權重。
func DefaultWeights() Weights {
	return Weights{
		Model:              DefaultModelWeight,
		Dictionary:         DefaultDictionaryWeight,
		Fuzzy:              DefaultFuzzyWeight,
		Switch:             DefaultSwitchWeight,
		PreviousLanguage:   DefaultPreviousLanguageWeight,
		PrimaryLanguage:    DefaultPrimaryLanguageWeight,
		History:            DefaultHistoryWeight,
		LanguageConfidence: DefaultLanguageConfidenceWeight,
	}
}

// History 是使用者歷史的特徵來源。
//
// LanguageScore 回傳詞面在 language 的弱偏好，previous 是左側片段語言。
// 分數越高越優先；0 表示沒有證據。core/personal 的
// Model.LanguageCandidateScore 可以直接用 HistoryFunc 轉接：
//
//	history := score.HistoryFunc(func(surface string, language, previous lang.ID) float64 {
//		return model.LanguageCandidateScore(personal.LanguageCandidate{
//			Surface: surface, Language: language,
//		}, previous)
//	})
type History interface {
	LanguageScore(surface string, language, previous lang.ID) float64
}

// HistoryFunc 讓一般函式直接當 History 使用。
type HistoryFunc func(surface string, language, previous lang.ID) float64

// LanguageScore 實作 History。
func (f HistoryFunc) LanguageScore(surface string, language, previous lang.ID) float64 {
	if f == nil {
		return 0
	}
	return f(surface, language, previous)
}

// Ranker 是單一特徵式仲裁器，同時實作 Arbitrator 與多候選排序。
//
// Models 可以為空。空清單或所有模型都回報 ErrUnavailable 時，Rank 仍會用
// 統計特徵排序，這是「沒有模型也能跑」的正式路徑，不是錯誤。
type Ranker struct {
	// Models 是選配的語言模型 scorer。實作必須依介面契約回傳等長分數，
	// 非自己語言的候選保留中性值；Ranker 仍會依 Languages 過濾。
	Models []Scorer
	// Calibrator 是選配的模型分數校準器；nil 表示原樣使用。
	Calibrator Calibrator
	// History 是選配的使用者歷史；nil 表示沒有個人化證據。
	History History
	// Weights 是混合權重。零值表示使用 DefaultWeights。
	Weights Weights
	// SwitchCost 是一般語言切換成本。零值且 PrimarySwitchCost 也為零時
	// 使用 DefaultSwitchCost。
	SwitchCost float64
	// PrimarySwitchCost 是涉及主要語言時的切換成本。零值規則同上。
	PrimarySwitchCost float64
	// Timeout 是一次請求的模型評分時間預算。零值使用 DefaultModelTimeout；
	// 負值也使用預設值，沒有「關閉逾時」的設定。
	Timeout time.Duration

	counters counterSet
}

// RankerConfig 是建立 Ranker 的參數。零值代表沒有模型、沒有歷史、使用
// 第一版固定權重。
type RankerConfig struct {
	Models            []Scorer
	Calibrator        Calibrator
	History           History
	Weights           Weights
	SwitchCost        float64
	PrimarySwitchCost float64
	// Timeout 是模型評分時間預算；零值使用 DefaultModelTimeout。
	Timeout time.Duration
}

// NewRanker 建立特徵式仲裁器。Models 可以是 nil，表示只用統計特徵。
func NewRanker(cfg RankerConfig) *Ranker {
	return &Ranker{
		Models:            append([]Scorer(nil), cfg.Models...),
		Calibrator:        cfg.Calibrator,
		History:           cfg.History,
		Weights:           cfg.Weights,
		SwitchCost:        cfg.SwitchCost,
		PrimarySwitchCost: cfg.PrimarySwitchCost,
		Timeout:           cfg.Timeout,
	}
}

// Counters 回傳目前累計的服務來源。回退、逾時與隱私跳過都留在 Requests
// 分母裡；這個快照可以安全地被多個 goroutine 同時讀取。
func (r *Ranker) Counters() Counters {
	if r == nil {
		return Counters{}
	}
	return r.counters.snapshot()
}

var _ Arbitrator = (*Ranker)(nil)

// RankRequest 是一次多語言候選排序請求。
//
// PreviousLanguage 只描述左側片段，不是模式；PrimaryLanguage 只是先驗。
// Candidates 可以混合任何語言，Rank 不會依語言分組或過濾。
type RankRequest struct {
	// Context 是已確定的左側文字，只轉交給模型；隱私欄位不得送進來。
	Context string
	// Privacy 是這次請求的欄位敏感度。零值 PrivacyUnspecified 視為私人，
	// 模型完全不會被呼叫；一般欄位必須明確標成 PublicPrivacy()。
	Privacy Privacy
	// PreviousLanguage 是前文語言；零值表示句首或未知。
	PreviousLanguage lang.LanguageTag
	// PrimaryLanguage 是使用者主要語言；零值表示未設定。
	PrimaryLanguage lang.ID
	// Candidates 是待排序候選，順序就是同分時的穩定順序。
	Candidates []lattice.Candidate
}

// Ranked 是排序後的一項。它刻意不含語言標籤，讓 UI 只看到順序與分數。
//
// 需要語言標籤的內部呼叫端可用 Index 回頭查 RankRequest.Candidates；UI
// 不應把那個欄位顯示出來。
type Ranked struct {
	// Index 是原始 Candidates 的索引。
	Index int
	// Score 是仲裁分數，越高越優先。
	Score float64
	// Reject 保留給未來的硬性排除；目前恆為 false。
	Reject bool
	// Reason 是診斷文字，不顯示給使用者，也不含語言標籤。
	Reason string
}

const featureReason = "feature"

// Arbitrate 依 Features 計算單一候選分數。
//
// 公式（所有權重都是 placeholder）：
//
//	score = Model*ModelScore
//	      - Dictionary*DictionaryCost
//	      - Fuzzy*FuzzyCost
//	      - Switch*SwitchCost
//	      + History*UserHistory
//	      + PreviousLanguage（語言與前文相同）
//	      + PrimaryLanguage（語言等於主要語言）
//	      + LanguageConfidence*片段語言可信度
//
// 沒有任何一項是硬性 gate；主要語言與前文語言只改變分數。
func (r *Ranker) Arbitrate(f Features) Decision {
	w := r.weights()
	model := finiteOrZero(f.ModelScore)
	dictionary := finiteOrZero(f.DictionaryCost)
	fuzzy := finiteOrZero(f.FuzzyCost)
	switchCost := finiteOrZero(f.SwitchCost)
	history := finiteOrZero(f.UserHistory)

	score := w.Model*model -
		w.Dictionary*dictionary -
		w.Fuzzy*fuzzy -
		w.Switch*switchCost +
		w.History*history

	if !f.PreviousLanguage.IsZero() && !f.Language.IsZero() &&
		f.Language.Language == f.PreviousLanguage.Language {
		score += w.PreviousLanguage
	}
	if f.PrimaryLanguage != lang.Und && !f.Language.IsZero() &&
		f.Language.Language == f.PrimaryLanguage {
		score += w.PrimaryLanguage
	}
	if confidence := f.Language.Confidence; confidence > 0 {
		score += w.LanguageConfidence * clamp01(confidence)
	}
	if !isFinite(score) {
		// 非有限值不應排序；回 0 讓候選仍留在清單裡，由統計特徵決定。
		score = 0
	}
	return Decision{Score: score, Reason: featureReason}
}

// Rank 排序全部候選並回傳單一清單，是 RankResult 的便利包裝。
//
// 決定性規則：分數由高到低；分數完全相同時保留 Candidates 的原始索引，
// 也就是先出現的候選優先。這個規則讓沒有模型時的結果與統計順序一致，也
// 讓同一份輸入每次都得到相同順序。Rank 不會修改 req 或候選。
//
// 模型錯誤的處理：隱私等級不是公開、模型缺失、ErrUnavailable、回傳長度
// 不符、非有限分數或超過 Timeout，都會讓整筆請求改用統計排序；只有呼叫端
// 自己的 context 取消會回傳錯誤。回退不會刪除任何候選。
func (r *Ranker) Rank(ctx context.Context, req RankRequest) ([]Ranked, error) {
	ranking, err := r.RankResult(ctx, req)
	return ranking.Items, err
}

// RankResult 與 Rank 相同，另外回報這次請求的服務來源，讓評測把回退、
// 逾時與隱私跳過留在分母裡。
func (r *Ranker) RankResult(ctx context.Context, req RankRequest) (Ranking, error) {
	if err := ctx.Err(); err != nil {
		return Ranking{}, err
	}
	out := make([]Ranked, len(req.Candidates))
	if len(out) == 0 {
		return Ranking{Items: out, Outcome: OutcomeNone}, nil
	}

	modelScores, outcome, err := r.scoreModels(ctx, req)
	if err != nil {
		return Ranking{}, err
	}
	if err := ctx.Err(); err != nil {
		return Ranking{}, err
	}

	for i, candidate := range req.Candidates {
		tag := candidateLanguage(candidate)
		history := 0.0
		if r != nil && r.History != nil {
			history = finiteOrZero(r.History.LanguageScore(
				candidate.Surface,
				tag.Language,
				req.PreviousLanguage.Language,
			))
		}
		decision := r.Arbitrate(Features{
			PrimaryLanguage:  req.PrimaryLanguage,
			PreviousLanguage: req.PreviousLanguage,
			Language:         tag,
			ModelScore:       finiteOrZero(candidate.Cost.Model) + modelScores[i],
			DictionaryCost:   candidate.Cost.Dictionary,
			FuzzyCost:        candidate.Cost.Fuzzy,
			SwitchCost:       r.switchCost(req.PreviousLanguage, tag, req.PrimaryLanguage),
			UserHistory:      history,
		})
		out[i] = Ranked{
			Index:  i,
			Score:  decision.Score,
			Reject: decision.Reject,
			Reason: decision.Reason,
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Index < out[j].Index
	})
	if r != nil {
		r.counters.record(outcome)
	}
	return Ranking{Items: out, Outcome: outcome}, nil
}

// scorerReply 是背景模型呼叫的結果；緩衝通道讓逾時後才完成的呼叫不會卡住。
type scorerReply struct {
	raw []float64
	err error
}

// scoreModels 呼叫所有可用模型，回傳每個候選的模型分數合計與服務來源。
//
// 一次請求只有一份時間預算（Ranker.Timeout）。任何模型逾時、回錯、長度
// 不符或分數無效，整筆請求都不採用模型分數：已算好的模型分數也會丟棄，
// 讓回退結果與「完全沒有模型」逐位元相同。模型只對 Languages 宣告的語言
// 貢獻分數；其他候選保持 0。
func (r *Ranker) scoreModels(ctx context.Context, req RankRequest) ([]float64, Outcome, error) {
	out := make([]float64, len(req.Candidates))
	// 隱私檢查必須在任何模型、任何前文複製之前。零值也走這條路。
	if !req.Privacy.AllowsModel() {
		return out, OutcomePrivacy, nil
	}
	if r == nil || len(r.Models) == 0 {
		return out, OutcomeFallback, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, OutcomeNone, err
	}

	var relevant []Scorer
	for _, scorer := range r.Models {
		if scorer == nil || !scorerRelevant(scorer, req.Candidates) {
			continue
		}
		relevant = append(relevant, scorer)
	}
	if len(relevant) == 0 {
		return out, OutcomeFallback, nil
	}

	deadline := time.Now().Add(r.timeout())
	for _, scorer := range relevant {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return out, OutcomeTimeout, nil
		}
		raw, timedOut, err := callScorer(ctx, scorer, Request{
			Privacy:    req.Privacy,
			Context:    req.Context,
			Candidates: req.Candidates,
		}, remaining)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, OutcomeNone, ctxErr
			}
			if timedOut || errors.Is(err, context.DeadlineExceeded) {
				return out, OutcomeTimeout, nil
			}
			return out, OutcomeFallback, nil
		}
		if timedOut {
			return out, OutcomeTimeout, nil
		}
		if len(raw) != len(out) {
			return out, OutcomeFallback, nil
		}

		add := make([]float64, len(raw))
		for i, value := range raw {
			if !scorerSupports(scorer, candidateLanguage(req.Candidates[i])) {
				continue
			}
			if !isFinite(value) {
				return out, OutcomeFallback, nil
			}
			if r.Calibrator != nil {
				value = r.Calibrator.Calibrate(value)
				if !isFinite(value) {
					return out, OutcomeFallback, nil
				}
			}
			add[i] = value
		}
		// 只有整筆驗證通過才併入；失敗的請求不會留下部分模型分數。
		for i := range out {
			out[i] += add[i]
		}
	}
	return out, OutcomeModel, nil
}

// callScorer 在背景 goroutine 呼叫模型，最多等 wait。
//
// 模型實作即使不理會 context，也無法讓呼叫端等超過預算；背景 goroutine
// 之後完成時把結果寫進緩衝通道就結束，不會洩漏阻塞。回傳 timedOut 表示
// 是這層的預算用完，呼叫端自己的 context 取消另有回傳值。
func callScorer(ctx context.Context, scorer Scorer, req Request, wait time.Duration) (raw []float64, timedOut bool, err error) {
	runCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	done := make(chan scorerReply, 1)
	go func() {
		scores, scoreErr := scorer.Score(runCtx, req)
		done <- scorerReply{raw: scores, err: scoreErr}
	}()
	select {
	case reply := <-done:
		if reply.err != nil && ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return reply.raw, false, reply.err
	case <-runCtx.Done():
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		return nil, true, runCtx.Err()
	}
}

func (r *Ranker) timeout() time.Duration {
	if r != nil && r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultModelTimeout
}

func (r *Ranker) weights() Weights {
	if r == nil || r.Weights == (Weights{}) {
		return DefaultWeights()
	}
	return r.Weights
}

func (r *Ranker) switchCosts() (normal, primary float64) {
	if r == nil || (r.SwitchCost == 0 && r.PrimarySwitchCost == 0) {
		return DefaultSwitchCost, DefaultPrimarySwitchCost
	}
	return r.SwitchCost, r.PrimarySwitchCost
}

// switchCost 回傳從前文語言接到候選語言的成本。同語言或任一側未知時是
// 0；涉及主要語言時使用較低的 PrimarySwitchCost，但不會排除其他語言。
func (r *Ranker) switchCost(from, to lang.LanguageTag, primary lang.ID) float64 {
	if from.IsZero() || to.IsZero() || from.Language == to.Language {
		return 0
	}
	normal, primaryCost := r.switchCosts()
	if primary != lang.Und && (from.Language == primary || to.Language == primary) {
		return primaryCost
	}
	return normal
}

// candidateLanguage 取得候選的語言標籤。沒有標籤時先看輸出變體；仍然
// 未知就保留零值，讓排序把它當成「未知」而不是猜測語言。
func candidateLanguage(candidate lattice.Candidate) lang.LanguageTag {
	if !candidate.Language.IsZero() {
		return candidate.Language
	}
	if candidate.Variant != "" {
		if id := candidate.Variant.Language(); id != lang.Und {
			return lang.Tag(id)
		}
	}
	return lang.LanguageTag{}
}

// scorerRelevant 回報 scorer 是否至少支援一個候選語言。Languages 空切片
// 表示由實作自行判斷，一律視為可能相關。
func scorerRelevant(scorer Scorer, candidates []lattice.Candidate) bool {
	langs := scorer.Languages()
	if len(langs) == 0 {
		return true
	}
	for _, candidate := range candidates {
		if scorerSupports(scorer, candidateLanguage(candidate)) {
			return true
		}
	}
	return false
}

// scorerSupports 回報 scorer 是否宣告支援這個語言。零值語言標籤沿用中文
// adapter 的相容規則，視為中文；明確的其他語言標籤不會被誤送。
func scorerSupports(scorer Scorer, tag lang.LanguageTag) bool {
	langs := scorer.Languages()
	if len(langs) == 0 {
		return true
	}
	id := tag.Language
	if id == lang.Und {
		id = lang.Zh
	}
	for _, supported := range langs {
		if supported == id {
			return true
		}
	}
	return false
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func finiteOrZero(value float64) float64 {
	if isFinite(value) {
		return value
	}
	return 0
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
