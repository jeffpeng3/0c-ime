package lang

import "github.com/0c-biggo/0c-ime/core/reading"

// Converter 把讀音轉成詞面，並提供候選與預測。
//
// 注音 converter 包裝 core/decode；日文 converter 包裝 core/ja/decode。
// core/session 只依賴這個介面。
type Converter interface {
	// Convert 回傳整段讀音的最佳轉換結果。
	Convert(reading.Sequence, Context) (Result, error)
	// Candidates 回傳指定範圍的候選，依分數排序。
	Candidates(reading.Sequence, Span, Context) ([]Candidate, error)
	// Prefixes 回報可作為預測的詞面前綴，依分數排序；
	// 沒有預測能力時回傳 nil。
	Prefixes(reading.Sequence) []Candidate
}

// Context 是轉換時可用的上下文。
//
// Left 是已確定的左側文字，Right 是已存在的右側文字。轉換器不得修改傳入值。
type Context struct {
	Left  string
	Right string
}

// Span 是要列候選的讀音範圍，以 reading.Sequence 的索引表示，範圍是 [Start, Start+Length)。
type Span struct {
	Start  int
	Length int
}

// End 回傳範圍的結束索引。
func (s Span) End() int { return s.Start + s.Length }

// Result 是整段讀音的轉換結果。
type Result struct {
	Clauses []Clause
	Score   float64
}

// Clause 是轉換的最小單位：注音的一個詞段，或日文的一個文節。
type Clause struct {
	Reading  reading.Sequence
	Surface  string
	Segments []CandidateSegment
	Score    float64
}

// CandidateSegment 是候選詞面內部的一段，讓外殼能標示讀音與詞面的對應。
type CandidateSegment struct {
	Reading reading.Sequence
	Surface string
}

// Candidate 是候選視窗的一項。
type Candidate struct {
	Surface string
	Reading reading.Sequence
	// Clauses 是整句候選時的文節；只替換單一詞段時為空。
	Clauses    []Clause
	Score      float64
	Source     string
	Annotation string
}

// Source 的慣用值。實作可以回報其他字串，外殼不得假設只有這幾種。
const (
	SourceConvert  = "convert"
	SourcePredict  = "predict"
	SourcePersonal = "personal"
	SourceSymbol   = "symbol"
)
