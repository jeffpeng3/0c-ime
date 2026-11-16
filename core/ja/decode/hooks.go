package decode

import (
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Learning 是個人學習的鉤子。T12 只定義介面與空實作，T21 接上真正的學習檔。
//
// Cost 回傳指定詞條在該位置的額外成本；0 表示不調整。回傳負值可以提高優先度。
type Learning interface {
	Cost(seq reading.Sequence, start int, e lexicon.Entry, ctx lang.Context) float64
}

// NoLearning 是沒有學習資料時的空實作。
type NoLearning struct{}

// Cost 永遠回傳 0。
func (NoLearning) Cost(reading.Sequence, int, lexicon.Entry, lang.Context) float64 { return 0 }

// Verifier 是神經驗證的鉤子。T12 只定義介面與空實作，T30 接上 draft、verify 與重搜。
//
// Score 回傳整句結果的額外加分；越高越好。驗證器不得改變讀音對應。
type Verifier interface {
	Score(seq reading.Sequence, res lang.Result, ctx lang.Context) float64
}

// NoVerifier 是沒有神經模型時的空實作。
type NoVerifier struct{}

// Score 永遠回傳 0。
func (NoVerifier) Score(reading.Sequence, lang.Result, lang.Context) float64 { return 0 }

// Reranker 是 T30 的 draft、verify 與限制式重搜鉤子。
//
// Rerank 收到統計轉換已排序的候選，回傳完整排序結果；實作必須保留讀音
// 對應，且在任何錯誤或逾時時原樣回傳 drafts。沒有這個鉤子時行為完全不變。
type Reranker interface {
	Rerank(seq reading.Sequence, drafts []lang.Result, ctx lang.Context) []lang.Result
}
