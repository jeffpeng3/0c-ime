package lexicon

import (
	"strconv"
	"strings"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

// Query 是一次候選查詢，對應 lattice 的一條邊。
//
// Span 是查詢涵蓋的原始輸入範圍；Reading 是這段輸入的讀音。Language 與 Variant
// 是分段器已知的提示，可以留空：留空時由 Query.Candidate 依讀音推斷。Limit
// 是候選數量上限，0 表示不限制。
//
// Query 不是「目前語言」：同一個 session 可以同時查詢多個語言標籤，實作不得
// 因為 Language 只標了一種語言就丟掉其他語言的詞條。
type Query struct {
	Span     segment.Span       `json:"span"`
	Script   segment.Script     `json:"script,omitempty"`
	Reading  reading.Sequence   `json:"reading,omitempty"`
	Language lang.LanguageTag   `json:"language"`
	Variant  lang.OutputVariant `json:"variant,omitempty"`
	Limit    int                `json:"limit,omitempty"`
}

// Valid 回報查詢欄位是否一致。空語言標籤與空變體是合法的「尚未判定」。
func (q Query) Valid() bool {
	if !q.Span.Valid() || q.Limit < 0 {
		return false
	}
	if !q.Language.IsZero() && !q.Language.Valid() {
		return false
	}
	if q.Variant != "" && !q.Variant.Valid() {
		return false
	}
	for _, u := range q.Reading {
		if !u.Valid() {
			return false
		}
	}
	return true
}

// Key 回傳查詢的穩定鍵，供快取、除重與測試使用。
// 鍵不含 Limit 與可信度：兩者只影響要取幾筆與排序，不改變查詢身分。
func (q Query) Key() string {
	return strings.Join([]string{
		"[" + strconv.Itoa(q.Span.Start) + "," + strconv.Itoa(q.Span.End) + ")",
		q.Script.String(),
		reading.Key(q.Reading),
		string(q.Language.Language),
		string(q.Variant),
	}, "|")
}

// Candidate 由詞條 e 組出候選。
//
// 查詢沒有語言標籤時依讀音推斷：注音是中文、かな是日文；無法判定時保持未指定，
// 由呼叫端或後續的語言判定補上。查詢沒有輸出變體時套用該語言的預設變體。
// 成本與來源由呼叫端提供，這裡不加總也不排序。
func (q Query) Candidate(e Entry, src lattice.Source, cost lattice.Cost) lattice.Candidate {
	tag := q.Language
	if tag.IsZero() {
		if id := InferLanguage(e.Reading); id != lang.Und {
			tag = lang.Tag(id)
		}
	}
	variant := q.Variant
	if variant == "" {
		variant = lang.DefaultVariant(tag.Language)
	}
	return lattice.Candidate{
		Segment: segment.Segment{
			Span:     q.Span,
			Script:   q.Script,
			Reading:  e.Reading,
			Surface:  e.Surface,
			Language: tag,
			Variant:  variant,
		},
		Cost:   cost,
		Source: src,
	}
}

// InferLanguage 依讀音推斷語言。注音與かな是強訊號；其他讀音回傳 lang.Und。
// 拉丁字母同時可能是英文或羅馬字，不在這裡推斷。
func InferLanguage(seq reading.Sequence) lang.ID {
	switch {
	case hasZhuyin(seq):
		return lang.Zh
	case hasKana(seq):
		return lang.Ja
	}
	return lang.Und
}

// CandidateSource 依查詢回傳帶語言標籤、輸出變體與成本的候選。
//
// 回傳的候選只描述「如果選這條邊會輸出什麼」，不代表已被選中；實作可以回傳
// 多語言候選，並自行決定排序。空查詢或查無資料時回傳 nil。
type CandidateSource interface {
	Candidates(Query) []lattice.Candidate
}
