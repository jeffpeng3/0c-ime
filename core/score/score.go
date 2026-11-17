// Package score 定義多語言 scorer 與特徵式仲裁的契約。
//
// 第一版：中文沿用既有 ONNX reranker，日文使用新訓模型，英文只用詞典。
// 模型不可用時由呼叫端退回統計轉換，因此 scorer 必須用錯誤回報不可用，
// 不得自行改變候選順序或阻塞輸入。實際權重與模型 wiring 由 P2-26、P2-31、
// P2-32 實作。
package score

import (
	"context"
	"errors"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
)

// ErrUnavailable 表示這個 scorer 目前不可用，例如模型缺失、載入失敗或
// 語言不支援。呼叫端應保留統計轉換結果，不要把它當成致命錯誤。
var ErrUnavailable = errors.New("score: model unavailable")

// Request 是一次 rerank 請求。
type Request struct {
	// Context 是已確定的左側文字。隱私欄位不得送進來。
	Context string
	// Candidates 是待評分的候選，可以混合多種語言。
	Candidates []lattice.Candidate
}

// Scorer 為候選打模型分數。
//
// Score 必須回傳與 req.Candidates 等長、同順序的分數，數值越高越優先。
// 實作不得修改 req 或候選。模型不可用時回傳 ErrUnavailable；逾時仍由呼叫端
// 的 context 決定，不應回傳部分結果假裝成功。
type Scorer interface {
	// Languages 回傳這個 scorer 支援的語言。一個 scorer 可以支援多語言，
	// 空切片表示由實作自行判斷。
	Languages() []lang.ID
	Score(ctx context.Context, req Request) ([]float64, error)
}

// Calibrator 把模型原始輸出轉成可比較的分數。
//
// 校準與訓練必須分離；校準器不得依賴測試題庫。沒有校準需求時實作可以
// 原樣回傳 raw。
type Calibrator interface {
	Calibrate(raw float64) float64
}

// Features 是仲裁使用的一項候選特徵。
//
// 前文語言、目前片段語言與主要語言都是偏好，不是硬性 gate；主要語言只讓
// 切換成本較低。DictionaryCost、FuzzyCost、SwitchCost 越低越好，ModelScore
// 與 UserHistory 越高越好。
type Features struct {
	// PrimaryLanguage 是使用者設定的主要語言；空值表示未設定。
	PrimaryLanguage lang.ID
	// PreviousLanguage 是左側片段的語言標籤；空值表示句首或未知。
	PreviousLanguage lang.LanguageTag
	// Language 是目前候選片段的語言標籤。
	Language lang.LanguageTag
	// ModelScore 是校準後的模型分數。
	ModelScore float64
	// DictionaryCost 是詞典成本。
	DictionaryCost float64
	// FuzzyCost 是模糊輸入的額外成本。
	FuzzyCost float64
	// SwitchCost 是從前文語言切到 Language 的成本。
	SwitchCost float64
	// UserHistory 是分語言學習與跨語言偏好的綜合分數。
	UserHistory float64
}

// Decision 是仲裁對單一候選的判定。
type Decision struct {
	// Score 是候選的仲裁分數，越高越優先。
	Score float64
	// Reject 表示硬性排除，例如隱私欄位或使用者鎖定不相容的片段。
	// 一般排序不應使用 Reject，讓候選留在清單裡供使用者覆寫。
	Reject bool
	// Reason 是診斷用的原因，不顯示給使用者，也不參與排序。
	Reason string
}

// Arbitrator 是特徵式仲裁介面。
//
// 實作對每個候選計算一次 Decision；Viterbi 在路徑上另外加總切換成本。
// 仲裁只依 Features 決定分數，不讀取全域 active language。
type Arbitrator interface {
	Arbitrate(Features) Decision
}

// Arbitration 是 Arbitrator 的別名，對應需求文件中的「arbitration 介面」。
type Arbitration = Arbitrator
