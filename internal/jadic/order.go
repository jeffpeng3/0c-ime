package jadic

import "sort"

// EntryOrder 是同一 surface 內詞條的排序鍵，順序固定為：
// 成本升冪、讀音、詞性、活用型、活用形。
//
// Cost 是實際寫進 words.tsv 的產品成本，因此同一份 pack 換了成本換算規則，
// 同分集合可能改變，排序也跟著換。字串比較是 UTF-8 位元組序，對合法 UTF-8
// 等同 Unicode code point 序，跨平台穩定。
type EntryOrder struct {
	Cost     float64
	Reading  string
	POS      string
	ConjType string
	ConjForm string
}

// Order 依成本、讀音、詞性、活用型、活用形比較兩個排序鍵。
// 回傳 -1、0、1；0 表示五個欄位完全相同。
func (a EntryOrder) Order(b EntryOrder) int {
	if a.Cost != b.Cost {
		if a.Cost < b.Cost {
			return -1
		}
		return 1
	}
	if a.Reading != b.Reading {
		if a.Reading < b.Reading {
			return -1
		}
		return 1
	}
	if a.POS != b.POS {
		if a.POS < b.POS {
			return -1
		}
		return 1
	}
	if a.ConjType != b.ConjType {
		if a.ConjType < b.ConjType {
			return -1
		}
		return 1
	}
	if a.ConjForm != b.ConjForm {
		if a.ConjForm < b.ConjForm {
			return -1
		}
		return 1
	}
	return 0
}

// orderOf 以 pack 原值建立排序鍵；cost 由呼叫端指定，換算流程傳產品成本。
func orderOf(e Entry, cost float64) EntryOrder {
	return EntryOrder{
		Cost:     cost,
		Reading:  e.Reading,
		POS:      e.POS,
		ConjType: e.ConjType,
		ConjForm: e.ConjForm,
	}
}

// CompareEntries 以 pack 原始 i16 成本比較兩筆詞條，順序與 EntryOrder 相同。
// 供不經成本換算的場合使用；相同 tuple 回傳 0。
func CompareEntries(a, b Entry) int {
	return orderOf(a, float64(a.Cost)).Order(orderOf(b, float64(b.Cost)))
}

// SortSurfaceEntries 以 pack 原始成本穩定排序同一 surface 的詞條。
// 相同 tuple 的詞條保留呼叫端給的順序。
func SortSurfaceEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return CompareEntries(entries[i], entries[j]) < 0
	})
}
