package jadic

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

const (
	// DefaultOrigin 是匯入詞條寫進 words.tsv 的來源 ID。它不在 data/sources/ledger.tsv，
	// 因此 0c-compile -public 之前仍需補帳本與 NOTICE；匯入流程不擅自改 checked-in 資料。
	DefaultOrigin = "ja-jadic-public-core"
	// DefaultCostScale 把 pack 的 MeCab 成本換算成產品的相對成本，與 jaimport 的
	// Mozc 預設換算一致。
	DefaultCostScale = 1.0 / 1000.0
)

// CostRule 說明 production 的 pack 成本換算規則。P2-22F 依 P2-22E 的量測
// 採用 mapping D：保留 pack 的 signed i16 成本，只乘上 scale，不 clamp、不 offset。
// 負成本因此原樣寫進 words.tsv；產品詞典與編譯器接受 -32.767～32.767 的詞成本。
const CostRule = "cost * scale（signed，不 clamp、不 offset）；scale 預設 0.001"

// ConvertOptions 控制 pack → words.tsv 的轉換。
type ConvertOptions struct {
	// Origin 寫進第五欄；空白時使用 DefaultOrigin。
	Origin string
	// CostScale 是詞成本換算係數；0 使用 DefaultCostScale。
	CostScale float64
	// MaxReadingUnits 大於 0 時，超過上限的詞條視為錯誤；產品限制是 64。
	MaxReadingUnits int
}

// ConvertStats 是轉換結果統計。
type ConvertStats struct {
	Entries         int
	Surfaces        int
	NegativeCosts   int
	UnknownPOS      int
	UnknownConj     int
	POSCounts       map[string]int
	MaxReadingUnits int
}

// convertedRow 是一筆已換算成產品欄位的詞條，保留 pack 原值供同 surface 排序。
type convertedRow struct {
	source  Entry
	reading string
	pos     jaimport.POS
	conj    jaimport.Conjugation
	cost    float64
}

// Convert 把 pack 串流寫成產品 data/ja 的中介格式：
// 詞面、空白分隔讀音、產品成本、產品詞性、來源、活用型。
//
// 輸出順序是契約的一部分：surface 依 pack 的 code point 序，同一 surface 內依
// 成本（換算後）、讀音、詞性、活用型、活用形排序，五欄完全相同時保留 pack 內的
// 原始順序。同一份 pack 因此永遠產生相同位元組。
func Convert(w io.Writer, p *Pack, opt ConvertOptions) (ConvertStats, error) {
	stats := ConvertStats{POSCounts: map[string]int{}}
	if opt.Origin == "" {
		opt.Origin = DefaultOrigin
	}
	if strings.ContainsAny(opt.Origin, "\t\r\n") {
		return stats, fmt.Errorf("jadic: 來源 ID %q 含非法字元", opt.Origin)
	}
	if opt.CostScale == 0 {
		opt.CostScale = DefaultCostScale
	}
	if math.IsNaN(opt.CostScale) || math.IsInf(opt.CostScale, 0) || opt.CostScale < 0 {
		return stats, fmt.Errorf("jadic: 成本換算係數 %v 不合法", opt.CostScale)
	}

	bw := bufio.NewWriterSize(w, 1<<20)
	if _, err := fmt.Fprintln(bw, "# 由 0c-jadic pack 匯入；詞面\t讀音\t成本\t詞性\t來源\t活用型"); err != nil {
		return stats, err
	}
	rows := make([]convertedRow, 0, 128)
	currentSurface := ""
	flush := func() error {
		// 穩定排序：成本用實際寫出的產品成本；相同 tuple 保留 pack 內的先後順序。
		sort.SliceStable(rows, func(i, j int) bool {
			a := orderOf(rows[i].source, rows[i].cost)
			b := orderOf(rows[j].source, rows[j].cost)
			return a.Order(b) < 0
		})
		stats.Surfaces++
		for _, row := range rows {
			stats.Entries++
			stats.POSCounts[string(row.pos)]++
			if _, err := fmt.Fprintf(bw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				row.source.Surface, row.reading, formatCost(row.cost), row.pos, opt.Origin, row.conj); err != nil {
				return err
			}
		}
		rows = rows[:0]
		return nil
	}
	index := 0
	err := p.ForEach(func(e Entry) error {
		if len(rows) > 0 && e.Surface != currentSurface {
			if err := flush(); err != nil {
				return err
			}
		}
		currentSurface = e.Surface
		if err := convertCheckEntry(e, opt); err != nil {
			return fmt.Errorf("第 %d 筆 %q：%w", index, e.Surface, err)
		}
		index++
		reading, err := jaimport.NormalizeReading(e.Reading)
		if err != nil {
			return fmt.Errorf("第 %d 筆 %q 的讀音：%w", index, e.Surface, err)
		}
		pos := jaimport.MapPOS(e.POS)
		if pos == jaimport.POSOther {
			stats.UnknownPOS++
		}
		conj := jaimport.MapConjugation(e.ConjType)
		if conj == jaimport.ConjUnknown {
			stats.UnknownConj++
		}
		if e.Cost < 0 {
			stats.NegativeCosts++
		}
		// mapping D：signed 成本直接縮放，不飽和、不位移。
		cost := float64(e.Cost) * opt.CostScale
		if n := len(reading); n > stats.MaxReadingUnits {
			stats.MaxReadingUnits = n
		}
		rows = append(rows, convertedRow{
			source:  e,
			reading: readingTSV(reading),
			pos:     pos,
			conj:    conj,
			cost:    cost,
		})
		return nil
	})
	if err != nil {
		return stats, fmt.Errorf("jadic: %w", err)
	}
	if err := flush(); err != nil {
		return stats, fmt.Errorf("jadic: %w", err)
	}
	if stats.Entries != p.EntryCount() {
		return stats, fmt.Errorf("jadic: 轉換詞條數 %d 與 pack 的 %d 不符", stats.Entries, p.EntryCount())
	}
	if stats.Surfaces != p.SurfaceCount() {
		return stats, fmt.Errorf("jadic: 轉換 surface 數 %d 與 pack 的 %d 不符", stats.Surfaces, p.SurfaceCount())
	}
	if err := bw.Flush(); err != nil {
		return stats, err
	}
	return stats, nil
}

func convertCheckEntry(e Entry, opt ConvertOptions) error {
	if len([]rune(e.Reading)) == 0 {
		return fmt.Errorf("讀音不可為空")
	}
	if opt.MaxReadingUnits > 0 && len([]rune(e.Reading)) > opt.MaxReadingUnits {
		return fmt.Errorf("讀音 %d 單位超過上限 %d", len([]rune(e.Reading)), opt.MaxReadingUnits)
	}
	if strings.ContainsAny(e.Surface, "\t\r\n") {
		return fmt.Errorf("詞面含 tab 或換行")
	}
	return nil
}

func readingTSV(seq reading.Sequence) string {
	parts := make([]string, len(seq))
	for i, u := range seq {
		parts[i] = string(u)
	}
	return strings.Join(parts, " ")
}

func formatCost(c float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(c, 'f', 4, 64), "0"), ".")
}

// SortedPOS 回傳 POS 統計，依詞性名稱排序。
func (s ConvertStats) SortedPOS() []string {
	out := make([]string, 0, len(s.POSCounts))
	for p := range s.POSCounts {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
