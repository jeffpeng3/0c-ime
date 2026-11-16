package jaimport

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/reading"
)

// SudachiOptions 控制 SudachiDict CSV 匯入。
type SudachiOptions struct {
	// Origin 寫進詞條來源，必須是 data/sources/ledger.tsv 的 ID。
	Origin string
	// Cost 估算 SudachiDict 沒有的 IME 詞成本；nil 時使用 EstimatedCost。
	Cost func(surface string, pos POS, readingLength int) float64
}

// DefaultSudachiOptions 回傳以 ja-sudachidict 為來源的匯入選項。
func DefaultSudachiOptions() SudachiOptions {
	return SudachiOptions{Origin: "ja-sudachidict"}
}

// EstimatedCost 估算 SudachiDict 詞條的成本。Sudachi 只有詞條與詞性，
// 沒有 Mozc 的 IME 詞成本，所以用詞性與長度給一個保守值；有 Wikipedia
// 詞頻時，編譯器會用詞頻覆寫。數值只要求相對排序合理。
func EstimatedCost(surface string, pos POS, readingLength int) float64 {
	base := 1.0
	switch pos {
	case POSParticle, POSAuxiliary:
		base = 0.2
	case POSSymbol:
		base = 0.3
	case POSPrefix, POSSuffix:
		base = 0.5
	case POSConjunction, POSDeterminer, POSInterjection:
		base = 0.8
	case POSVerb, POSAdjective, POSAdjectivalNoun:
		base = 1.2
	}
	extra := 0.1 * float64(max(0, utf8.RuneCountInString(surface)-1))
	if readingLength > 0 && readingLength != utf8.RuneCountInString(surface) {
		extra += 0.1
	}
	return base + extra
}

// ParseSudachi 讀取 SudachiDict Core 的 CSV（18 欄）。必要欄位：
// 0 詞面、1～4 詞性、5 活用型、6 活用形、7 讀音。
// 沒有讀音的列會被視為錯誤；SudachiDict Core 所有列都有讀音。
func ParseSudachi(r io.Reader, opt SudachiOptions) ([]Entry, error) {
	if opt.Origin == "" {
		return nil, fmt.Errorf("Sudachi 匯入缺少來源 ID")
	}
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	var out []Entry
	for ln := 1; ; ln++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", ln, err)
		}
		if len(rec) == 0 {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(rec[0]), "#") {
			continue
		}
		if len(rec) < 8 {
			return nil, fmt.Errorf("第 %d 行：Sudachi CSV 至少需要 8 欄，實際 %d 欄", ln, len(rec))
		}
		surface := rec[0]
		if surface == "" {
			return nil, fmt.Errorf("第 %d 行：詞面不可為空", ln)
		}
		reading, err := NormalizeReading(rec[7])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：讀音 %q：%w", ln, rec[7], err)
		}
		pos := MapPOS(strings.Join(rec[1:5], ","))
		conj := MapConjugation(rec[5])
		// 形容動詞在 Sudachi 的活用型欄是 *，只有「語幹」等活用形；
		// 由詞性補回分類，讓活用展開與索引知道這是な形容詞。
		if conj == ConjNone && pos == POSAdjectivalNoun {
			conj = ConjNaAdjective
		}
		cost := EstimatedCost(surface, pos, len(reading))
		if opt.Cost != nil {
			cost = opt.Cost(surface, pos, len(reading))
		}
		out = append(out, MakeEntry(surface, reading, cost, pos, []string{opt.Origin}, conj))
	}
	return Merge(out), nil
}

// ParseSudachiFile 讀取一份 SudachiDict CSV。
func ParseSudachiFile(path string, opt SudachiOptions) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseSudachi(f, opt)
}

// TSV 把詞條輸出成 core/ja/lexicon.Load 可讀的 TSV：詞面、讀音、成本、詞性。
// 這是資料包之外的中介格式，讓 T12 的記憶體詞典可以直接載入同一份資料。
func TSV(entries []Entry) []byte {
	var b strings.Builder
	b.WriteString("# 0c 日文詞典中介格式：詞面\t讀音\t成本\t詞性\n")
	sorted := append([]Entry(nil), entries...)
	SortEntries(sorted)
	for _, e := range sorted {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", e.Surface, readingTSV(e.Reading), formatCost(e.Cost), e.POS)
	}
	return []byte(b.String())
}

func readingTSV(seq reading.Sequence) string {
	parts := make([]string, len(seq))
	for i, u := range seq {
		parts[i] = string(u)
	}
	return strings.Join(parts, " ")
}

func formatCost(c float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", c), "0"), ".")
}
