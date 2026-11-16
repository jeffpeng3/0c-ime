package decode

import (
	"strings"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Segmenter 把 Viterbi 的詞序列合併成文節。
//
// 規則是固定的，因此同樣的輸入永遠得到同樣的文節邊界：
//   - 付属語（助詞、助動詞、接尾詞）依附前一個詞，不開新文節。
//   - 接頭詞與下一個詞黏在一起。
//   - 自立語開始新文節；只有兩個自立語的連接成本不超過正值的 MergeCost 時才視為複合詞合併。
//
// MergeCost 的零值或負值不合併複合詞，文節邊界只由詞性決定，最容易預測。
type Segmenter struct {
	MergeCost float64
}

// Break 回報 cur 是否要開始一個新文節。prev 為零值時代表這是第一個詞。
func (s Segmenter) Break(prev, cur lexicon.Entry, conn lexicon.Connection) bool {
	if prev.POS == "" || prev.POS == lexicon.POSBOS {
		return false
	}
	if prev.POS == lexicon.POSPrefix {
		return false
	}
	if cur.POS == lexicon.POSPrefix {
		return true
	}
	if cur.POS.Dependent() {
		return false
	}
	if prev.POS.Dependent() {
		return true
	}
	if !cur.POS.Independent() {
		return false
	}
	if s.MergeCost > 0 && connectionCost(conn, prev.POS, cur.POS) <= s.MergeCost {
		return false
	}
	return true
}

// Clauses 把詞序列轉成 lang.Clause。nodes 必須依序列順序排列且覆蓋完整讀音。
func Clauses(nodes []Node, conn lexicon.Connection, seg Segmenter) []lang.Clause {
	if len(nodes) == 0 {
		return nil
	}
	var out []lang.Clause
	var group []Node
	flush := func() {
		if len(group) == 0 {
			return
		}
		out = append(out, clause(group, conn))
		group = nil
	}
	for _, n := range nodes {
		if len(group) > 0 && seg.Break(group[len(group)-1].Entry, n.Entry, conn) {
			flush()
		}
		group = append(group, n)
	}
	flush()
	return out
}

func clause(nodes []Node, conn lexicon.Connection) lang.Clause {
	var surface string
	var units reading.Sequence
	segments := make([]lang.CandidateSegment, 0, len(nodes))
	for _, n := range nodes {
		surface += n.Entry.Surface
		units = append(units, n.Entry.Reading...)
		segments = append(segments, lang.CandidateSegment{
			Reading: append(reading.Sequence(nil), n.Entry.Reading...),
			Surface: n.Entry.Surface,
		})
	}
	return lang.Clause{
		Reading:  units,
		Surface:  surface,
		Segments: segments,
		Score:    -clauseCost(nodes, conn),
	}
}

func clauseCost(nodes []Node, conn lexicon.Connection) float64 {
	cost := 0.0
	prev := lexicon.POSBOS
	for _, n := range nodes {
		cost += n.Entry.Cost + connectionCost(conn, prev, n.Entry.POS)
		prev = n.Entry.POS
	}
	return cost
}

func connectionCost(conn lexicon.Connection, left, right lexicon.POS) float64 {
	if conn == nil {
		return lexicon.DefaultConnectionCost
	}
	return conn.Cost(left, right)
}

// ResultSurface 把文節串成完整詞面。
func ResultSurface(clauses []lang.Clause) string {
	var b strings.Builder
	for _, c := range clauses {
		b.WriteString(c.Surface)
	}
	return b.String()
}
