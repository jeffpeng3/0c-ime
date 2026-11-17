// Package lattice 定義多語言 lattice 的邊與候選資料形狀。
//
// lattice 的邊一律帶語言標籤與輸出變體，路徑可以跨語言；這裡不安裝任何
// 「目前語言」狀態，也不決定 Viterbi 權重。實際 path search 由 P2-12 實作。
package lattice

import (
	"strconv"
	"strings"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

// Cost 是候選的各項成本。
//
// Dictionary 與 Fuzzy 越低越好；Model 是模型分數，越高越好。三者單位不同，
// 因此不在這裡相加，仲裁與 Viterbi 要各自套權重。
type Cost struct {
	// Dictionary 是詞典成本，0 表示沒有詞典成本或未提供。
	Dictionary float64 `json:"dictionary_cost,omitempty"`
	// Fuzzy 是模糊輸入的額外成本。
	Fuzzy float64 `json:"fuzzy_cost,omitempty"`
	// Model 是校準後的模型分數，越高越優先。
	Model float64 `json:"model_score,omitempty"`
}

// Source 是候選來源。實作可以回報其他字串，外殼不得假設只有這幾種。
type Source string

const (
	// SourceDictionary 是一般詞典。
	SourceDictionary Source = "dictionary"
	// SourcePersonal 是使用者個人詞彙或學習結果。
	SourcePersonal Source = "personal"
	// SourceFuzzy 是模糊音或錯鍵展開。
	SourceFuzzy Source = "fuzzy"
	// SourcePrediction 是前綴預測或自動補完。
	SourcePrediction Source = "prediction"
	// SourceModel 是模型產生或模型重排後的候選。
	SourceModel Source = "model"
	// SourceLiteral 是保留原文的候選，例如英文專有名詞。
	SourceLiteral Source = "literal"
	// SourceSymbol 是符號選單。
	SourceSymbol Source = "symbol"
)

// Candidate 是一個帶語言標籤的候選，也是 lattice 的一條邊。
//
// Segment 以匿名欄位內嵌，所以 JSON 是平坦的 input span、reading、surface、
// language、variant 欄位，呼叫端也可直接用 Candidate.Span、Candidate.Surface
// 存取。Cost 同樣內嵌。候選只描述「如果選這條邊會輸出什麼」，不表示它已被選中。
type Candidate struct {
	segment.Segment
	Cost
	Source Source `json:"source,omitempty"`
}

// Valid 回報候選的基本欄位是否一致。
func (c Candidate) Valid() bool { return c.Segment.Valid() }

// Key 回傳候選的穩定鍵，供除重、快取與測試使用。
// 鍵包含輸入範圍、讀音、詞面、語言、變體與來源。
func (c Candidate) Key() string {
	return strings.Join([]string{
		spanKey(c.Span),
		reading.Key(c.Reading),
		c.Surface,
		string(c.Language.Language),
		string(c.Variant),
		string(c.Source),
	}, "|")
}

func spanKey(s segment.Span) string {
	return "[" + strconv.Itoa(s.Start) + "," + strconv.Itoa(s.End) + ")"
}
