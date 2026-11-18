package jadic

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

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
// 一般詞條的負成本因此原樣寫進 words.tsv；產品詞典與編譯器接受 -32.767～32.767
// 的詞成本。POS 為記号的詞條不進 words.tsv，負成本記号直接丟棄（P2-45E）。
const CostRule = "cost * scale（signed，不 clamp、不 offset）；scale 預設 0.001；負成本記号丟棄"

// ConvertOptions 控制 pack → words.tsv 的轉換。
type ConvertOptions struct {
	// Origin 寫進第五欄；空白時使用 DefaultOrigin。
	Origin string
	// CostScale 是詞成本換算係數；0 使用 DefaultCostScale。
	CostScale float64
	// MaxReadingUnits 大於 0 時，超過上限的詞條視為錯誤；產品限制是 64。
	MaxReadingUnits int
	// Symbols 是選填的記号辭典輸出。POS 為記号的詞條不會寫進 words.tsv；
	// 有提供時改寫成「讀音 TAB 詞面 TAB 描述 TAB 分類」四欄。nil 時只計數不輸出。
	Symbols io.Writer
}

// SymbolCategory 是匯出的記号辭典分類，與 data/ja/symbols/*.tsv 的契約一致。
const SymbolCategory = "記号"

// symbolSampleLimit 是片假名記号樣本的上限；只供人決定是否放寬過濾規則。
const symbolSampleLimit = 8

// nonJapaneseSampleLimit 是非日文詞面的樣本上限；只供人檢查過濾結果。
const nonJapaneseSampleLimit = 8

// ConvertStats 是轉換結果統計。Entries 與 Surfaces 只算實際寫進 words.tsv 的詞條。
type ConvertStats struct {
	// Entries 是寫進 words.tsv 的詞條數。
	Entries int
	// Surfaces 是 words.tsv 實際出現的 surface 數；只有記号的 surface 不算。
	Surfaces int
	// FilteredSymbols 是因 POS 為記号而被移出 words.tsv 的詞條數；含負成本丟棄者。
	FilteredSymbols int
	// FilteredNonJapanese 是詞面不含平假名、片假名、漢字或長音符而被移出
	// words.tsv 的詞條數。POS 為記号的詞條優先計入 FilteredSymbols，不重複計算。
	FilteredNonJapanese int
	// NonJapaneseSurfaces 是上述規則丟棄的詞面樣本（最多 nonJapaneseSampleLimit 筆、去重）。
	NonJapaneseSurfaces []string
	// SymbolEntries 是實際寫進記号辭典的詞條數。
	SymbolEntries int
	// SymbolNegativeCosts 是成本為負而直接丟棄的記号詞條數。
	SymbolNegativeCosts int
	// KatakanaSymbols 是被過濾的記号中，詞面含片假名的樣本（最多 symbolSampleLimit 筆）。
	KatakanaSymbols []string
	// NegativeCosts 是實際寫進 words.tsv 且成本為負的詞條數。
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

// symbolRow 是一筆要寫進記号辭典的詞條。成本不進產品格式。
type symbolRow struct {
	reading     string
	surface     string
	description string
}

// Convert 把 pack 串流寫成產品 data/ja 的中介格式：
// 詞面、空白分隔讀音、產品成本、產品詞性、來源、活用型。
//
// POS 為記号的詞條不寫進 words.tsv，改寫進 opt.Symbols 的四欄記号辭典；
// 成本為負的記号詞條直接丟棄並計入 SymbolNegativeCosts。其餘詞條若詞面不含
// 平假名、片假名、漢字或長音符，也丟棄並計入 FilteredNonJapanese；emoji、
// 拉丁字母、數字與符號因此不進 IME 詞典。片假名與漢字詞即使成本為負仍保留。
//
// 輸出順序是契約的一部分：surface 依 pack 的 code point 序，同一 surface 內依
// 成本（換算後）、讀音、詞性、活用型、活用形排序，五欄完全相同時保留 pack 內的
// 原始順序。記号辭典依讀音、詞面、描述排序，完全相同的一列只寫一次。
// 同一份 pack 因此永遠產生相同位元組。
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
	var sbw *bufio.Writer
	if opt.Symbols != nil {
		sbw = bufio.NewWriterSize(opt.Symbols, 1<<16)
		if _, err := fmt.Fprintln(sbw, "# 由 0c-jadic pack 匯入；讀音\t詞面\t描述\t分類"); err != nil {
			return stats, err
		}
	}
	rows := make([]convertedRow, 0, 128)
	symbols := make([]symbolRow, 0, 64)
	seenSymbols := map[string]bool{}
	seenNonJapanese := map[string]bool{}
	currentSurface := ""
	visitedSurfaces := 0
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
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
	flushSymbols := func() error {
		if sbw == nil {
			return nil
		}
		sort.SliceStable(symbols, func(i, j int) bool {
			a, b := symbols[i], symbols[j]
			if a.reading != b.reading {
				return a.reading < b.reading
			}
			if a.surface != b.surface {
				return a.surface < b.surface
			}
			return a.description < b.description
		})
		for _, row := range symbols {
			if _, err := fmt.Fprintf(sbw, "%s\t%s\t%s\t%s\n",
				row.reading, row.surface, row.description, SymbolCategory); err != nil {
				return err
			}
		}
		return nil
	}
	index := 0
	err := p.ForEach(func(e Entry) error {
		if e.Surface != currentSurface {
			if err := flush(); err != nil {
				return err
			}
			currentSurface = e.Surface
			visitedSurfaces++
		}
		if err := convertCheckEntry(e, opt); err != nil {
			return fmt.Errorf("第 %d 筆 %q：%w", index, e.Surface, err)
		}
		index++
		reading, err := jaimport.NormalizeReading(e.Reading)
		if err != nil {
			return fmt.Errorf("第 %d 筆 %q 的讀音：%w", index, e.Surface, err)
		}
		pos := jaimport.MapPOS(e.POS)
		if pos == jaimport.POSSymbol {
			stats.FilteredSymbols++
			if e.Cost < 0 {
				// 記号不進候選排序；負成本只會讓記号壓過真詞，直接丟棄。
				stats.SymbolNegativeCosts++
				return nil
			}
			desc, err := symbolDescription(e.POS)
			if err != nil {
				return fmt.Errorf("第 %d 筆 %q：%w", index, e.Surface, err)
			}
			key := readingString(reading) + "\t" + e.Surface + "\t" + desc
			if !seenSymbols[key] {
				seenSymbols[key] = true
				stats.SymbolEntries++
				symbols = append(symbols, symbolRow{
					reading:     readingString(reading),
					surface:     e.Surface,
					description: desc,
				})
			}
			if len(stats.KatakanaSymbols) < symbolSampleLimit && containsKatakana(e.Surface) {
				stats.KatakanaSymbols = append(stats.KatakanaSymbols,
					fmt.Sprintf("%s（%s）", e.Surface, readingString(reading)))
			}
			return nil
		}
		if !containsJapanese(e.Surface) {
			// emoji、拉丁字母、數字與符號不是 IME 的詞；不論成本正負都丟棄，
			// 避免負成本垃圾詞壓過真正的日文候選。
			stats.FilteredNonJapanese++
			if len(stats.NonJapaneseSurfaces) < nonJapaneseSampleLimit && !seenNonJapanese[e.Surface] {
				seenNonJapanese[e.Surface] = true
				stats.NonJapaneseSurfaces = append(stats.NonJapaneseSurfaces, e.Surface)
			}
			return nil
		}
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
	if err := flushSymbols(); err != nil {
		return stats, fmt.Errorf("jadic: %w", err)
	}
	if total := stats.Entries + stats.FilteredSymbols + stats.FilteredNonJapanese; total != p.EntryCount() {
		return stats, fmt.Errorf("jadic: 轉換詞條數 %d 與 pack 的 %d 不符（words %d、filtered symbols %d、filtered non-Japanese %d）",
			total, p.EntryCount(), stats.Entries, stats.FilteredSymbols, stats.FilteredNonJapanese)
	}
	if visitedSurfaces != p.SurfaceCount() {
		return stats, fmt.Errorf("jadic: 轉換 surface 數 %d 與 pack 的 %d 不符", visitedSurfaces, p.SurfaceCount())
	}
	if err := bw.Flush(); err != nil {
		return stats, err
	}
	if sbw != nil {
		if err := sbw.Flush(); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// symbolDescription 產生記号辭典的穩定描述：取 POS 分類去掉萬用字元，
// 若還有細分類就省略開頭的「記号」。例如「記号,文字,*,*」→「文字」。
func symbolDescription(pos string) (string, error) {
	parts := strings.Split(pos, ",")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "*" {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) > 1 && kept[0] == "記号" {
		kept = kept[1:]
	}
	desc := strings.Join(kept, "・")
	if desc == "" {
		desc = SymbolCategory
	}
	if strings.ContainsAny(desc, "\t\r\n") {
		return "", fmt.Errorf("記号描述含 tab 或換行")
	}
	return desc, nil
}

// containsKatakana 回報詞面是否含片假名或半形片假名。
func containsKatakana(s string) bool {
	for _, r := range s {
		if (r >= 0x30A0 && r <= 0x30FF) || (r >= 0xFF66 && r <= 0xFF9F) {
			return true
		}
	}
	return false
}

// containsJapanese 回報詞面是否含平假名、片假名、漢字或日文長音符。
// 長音符 U+30FC 的 script 是 Common，不在 unicode.Katakana，因此另外檢查。
func containsJapanese(s string) bool {
	for _, r := range s {
		if r == 'ー' || unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			return true
		}
	}
	return false
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

// readingString 是記号辭典用的讀音：單元直接相連，不留空白。
func readingString(seq reading.Sequence) string {
	parts := make([]string, len(seq))
	for i, u := range seq {
		parts[i] = string(u)
	}
	return strings.Join(parts, "")
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
