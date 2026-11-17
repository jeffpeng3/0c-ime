// Package jaimport 把日文詞典來源轉成 0C 的日文詞條，並編譯成可載入的資料包格式。
//
// 這個套件只依賴 core/reading 與標準函式庫：不 import core/ja/*，避免與 T12 的
// 執行期詞典套件互相依賴。編譯出的 lexicon.bin 由 core/ja/lexicon 的解碼器載入，
// 格式在 docs 與 README 中固定；測試同時輸出與 core/ja/lexicon.Load 相同的 TSV。
package jaimport

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/reading"
)

// POS 是日文詞性，值與 core/ja/lexicon.POS 相同。
type POS string

// 詞性常數。BOS 與 EOS 只用在連接成本矩陣。
const (
	POSOther          POS = "other"
	POSNoun           POS = "noun"
	POSVerb           POS = "verb"
	POSAdjective      POS = "adjective"
	POSAdjectivalNoun POS = "adjectival_noun"
	POSAdverb         POS = "adverb"
	POSParticle       POS = "particle"
	POSAuxiliary      POS = "auxiliary"
	POSConjunction    POS = "conjunction"
	POSPrefix         POS = "prefix"
	POSSuffix         POS = "suffix"
	POSDeterminer     POS = "determiner"
	POSInterjection   POS = "interjection"
	POSNumber         POS = "number"
	POSSymbol         POS = "symbol"

	POSBOS POS = "<s>"
	POSEOS POS = "</s>"
)

var posNames = map[string]POS{
	string(POSOther):          POSOther,
	string(POSNoun):           POSNoun,
	string(POSVerb):           POSVerb,
	string(POSAdjective):      POSAdjective,
	string(POSAdjectivalNoun): POSAdjectivalNoun,
	string(POSAdverb):         POSAdverb,
	string(POSParticle):       POSParticle,
	string(POSAuxiliary):      POSAuxiliary,
	string(POSConjunction):    POSConjunction,
	string(POSPrefix):         POSPrefix,
	string(POSSuffix):         POSSuffix,
	string(POSDeterminer):     POSDeterminer,
	string(POSInterjection):   POSInterjection,
	string(POSNumber):         POSNumber,
	string(POSSymbol):         POSSymbol,
}

// posList 是二進位格式的固定詞性順序；只能往後追加，不可重排。
var posList = []POS{
	POSOther, POSNoun, POSVerb, POSAdjective, POSAdjectivalNoun, POSAdverb,
	POSParticle, POSAuxiliary, POSConjunction, POSPrefix, POSSuffix,
	POSDeterminer, POSInterjection, POSNumber, POSSymbol,
}

// connPOSList 是連接成本矩陣可用的詞性，額外包含句首與句尾。
var connPOSList = append(append([]POS{}, posList...), POSBOS, POSEOS)

func posIndex(p POS) (int, bool) {
	for i, x := range posList {
		if x == p {
			return i, true
		}
	}
	return 0, false
}

// ParsePOS 把詞性名稱轉成 POS。不接受 BOS 與 EOS。
func ParsePOS(s string) (POS, error) {
	p, ok := posNames[s]
	if !ok {
		return "", fmt.Errorf("未知日文詞性 %q", s)
	}
	return p, nil
}

// Valid 回報 p 是否可寫進詞條。
func (p POS) Valid() bool {
	_, ok := posNames[string(p)]
	return ok
}

// MapPOS 把 Mozc／MeCab／UniDic 的日文詞性名稱（可含逗號分隔的細分類）轉成 POS。
// 只取第一層，例如「名詞,固有名詞,地域,一般」→ noun。
func MapPOS(s string) POS {
	first, _, _ := strings.Cut(strings.TrimSpace(s), ",")
	first = strings.TrimSpace(first)
	switch first {
	case "名詞", "名詞句":
		return POSNoun
	case "動詞":
		return POSVerb
	case "形容詞":
		return POSAdjective
	case "形容動詞":
		return POSAdjectivalNoun
	case "副詞":
		return POSAdverb
	case "助詞":
		return POSParticle
	case "助動詞":
		return POSAuxiliary
	case "接続詞":
		return POSConjunction
	case "接頭詞", "接頭辞", "接頭語":
		return POSPrefix
	case "接尾", "接尾辞", "接尾語":
		return POSSuffix
	case "連体詞":
		return POSDeterminer
	case "感動詞", "間投詞":
		return POSInterjection
	case "数詞", "数字":
		return POSNumber
	case "記号", "補助記号":
		return POSSymbol
	}
	// 相容英文詞性名稱與 Sudachi 的 POS 英文別名。
	if p, ok := posNames[strings.ToLower(first)]; ok {
		return p
	}
	return POSOther
}

// Conjugation 是活用型的穩定分類。空字串表示不適用或未標注。
type Conjugation string

const (
	ConjNone         Conjugation = ""
	ConjGodan        Conjugation = "godan"
	ConjIchidan      Conjugation = "ichidan"
	ConjSuru         Conjugation = "suru"
	ConjKuru         Conjugation = "kuru"
	ConjIAdjective   Conjugation = "i-adjective"
	ConjNaAdjective  Conjugation = "na-adjective"
	ConjUnknown      Conjugation = "unknown"
	conjRulePrefix               = "rule:conjugation"
	generatedCostAdd             = 1.0
)

var conjList = []Conjugation{
	ConjNone, ConjGodan, ConjIchidan, ConjSuru, ConjKuru,
	ConjIAdjective, ConjNaAdjective, ConjUnknown,
}

func conjIndex(c Conjugation) uint8 {
	for i, x := range conjList {
		if x == c {
			return uint8(i)
		}
	}
	return 0
}

// MapConjugation 把 Mozc／UniDic 的活用型名稱轉成穩定分類。
func MapConjugation(s string) Conjugation {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "*" {
		return ConjNone
	}
	switch {
	case strings.Contains(s, "五段"):
		return ConjGodan
	case strings.Contains(s, "一段"):
		return ConjIchidan
	case strings.Contains(s, "サ変"), strings.Contains(s, "サ行変格"):
		return ConjSuru
	case strings.Contains(s, "カ変"), strings.Contains(s, "カ行変格"):
		return ConjKuru
	case strings.Contains(s, "形容詞"):
		return ConjIAdjective
	case strings.Contains(s, "形容動詞"):
		return ConjNaAdjective
	case strings.Contains(s, "不変化"), strings.Contains(s, "無活用"):
		return ConjNone
	}
	switch strings.ToLower(s) {
	case string(ConjGodan), string(ConjIchidan), string(ConjSuru), string(ConjKuru),
		string(ConjIAdjective), string(ConjNaAdjective), string(ConjUnknown):
		return Conjugation(strings.ToLower(s))
	}
	return ConjUnknown
}

// Entry 是一筆日文詞條。讀音是かな單位序列，長度可與詞面字數不同。
type Entry struct {
	Surface     string
	Reading     reading.Sequence
	Cost        float64
	POS         POS
	Origins     []string
	Conjugation Conjugation
	// Okurigana 是詞面尾端かな的數量，供送り仮名索引與顯示使用。
	Okurigana int
}

// 產品詞成本容許的範圍，與 core/ja/lexicon 的 MinWordCost/MaxWordCost 相同。
// 0c-jadic 把 pack 的 signed i16 成本乘上 0.001，所以負成本是合法輸入；
// jaimport 與 core/ja/lexicon 刻意不互相 import，兩邊各自持有同一組常數。
const (
	MinWordCost = -32.767
	MaxWordCost = 32.767
)

// validCost 回報 c 是否為文件化的詞成本；NaN、Inf 與超出範圍都不合法。
func validCost(c float64) bool {
	return !math.IsNaN(c) && !math.IsInf(c, 0) && c >= MinWordCost && c <= MaxWordCost
}

// Key 是詞條的穩定鍵，供除重使用。
func (e Entry) Key() string {
	return e.Surface + "|" + reading.Key(e.Reading) + "|" + string(e.POS)
}

// Validate 檢查詞條是否可以寫進資料包。
func (e Entry) Validate() error {
	if e.Surface == "" {
		return fmt.Errorf("詞面不可為空")
	}
	if len(e.Reading) == 0 {
		return fmt.Errorf("%q 的讀音不可為空", e.Surface)
	}
	for _, u := range e.Reading {
		if !u.Valid() {
			return fmt.Errorf("%q 的讀音單位 %q 不合法", e.Surface, u)
		}
	}
	if !validCost(e.Cost) {
		return fmt.Errorf("%q 的詞成本 %v 超出容許範圍 %v～%v", e.Surface, e.Cost, MinWordCost, MaxWordCost)
	}
	if !e.POS.Valid() {
		return fmt.Errorf("%q 的詞性 %q 不合法", e.Surface, e.POS)
	}
	if e.Okurigana < 0 || e.Okurigana > len(e.Reading) {
		return fmt.Errorf("%q 的送り仮名 %d 超出讀音長度 %d", e.Surface, e.Okurigana, len(e.Reading))
	}
	return nil
}

// NormalizeReading 把來源讀音正規化成かな單位序列：片假名轉平假名、去除空白。
// 長音符、踊り字與小寫かな都保留，因此讀音可以可変長。
func NormalizeReading(s string) (reading.Sequence, error) {
	var seq reading.Sequence
	for _, r := range s {
		if unicode.IsSpace(r) {
			if r != ' ' {
				return nil, fmt.Errorf("讀音含有不合法的空白字元 %q", r)
			}
			continue
		}
		switch {
		case r >= 'ァ' && r <= 'ヶ':
			r -= 0x60 // 片假名 → 平假名（含ヵ、ヶ、ヴ）
		case r >= 'ヽ' && r <= 'ヾ':
			r -= 0x60 // 片假名踊り字 → 平假名踊り字
		}
		if !isKana(r) {
			return nil, fmt.Errorf("讀音含有非かな字元 %q；讀音只能使用かな，記号與標點屬於 T22 的記号辭典", r)
		}
		unit := reading.Unit(r)
		if !unit.Valid() {
			return nil, fmt.Errorf("讀音單位 %q 不合法：未通過 reading.Unit.Valid", r)
		}
		seq = append(seq, unit)
	}
	if len(seq) == 0 {
		return nil, fmt.Errorf("讀音不可為空")
	}
	return seq, nil
}

// OkuriganaLength 回傳詞面尾端かな的數量，最多不超過讀音單位數。
// 例：「食べる／たべる」2、「書く／かく」1、「明日／あした」0。
func OkuriganaLength(surface string, reading reading.Sequence) int {
	n := 0
	for _, r := range reverseRunes(surface) {
		if !isKana(r) {
			break
		}
		n++
	}
	if n > len(reading) {
		n = len(reading)
	}
	return n
}

func reverseRunes(s string) []rune {
	rs := []rune(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return rs
}

func isKana(r rune) bool {
	switch {
	case r >= 'ぁ' && r <= 'ゖ':
		return true
	case r >= 'ァ' && r <= 'ヺ':
		return true
	case r == 'ー' || r == 'ゝ' || r == 'ゞ' || r == 'ヽ' || r == 'ヾ':
		return true
	}
	return false
}

// MakeEntry 建立詞條並補上送り仮名長度。
func MakeEntry(surface string, reading reading.Sequence, cost float64, pos POS, origins []string, conj Conjugation) Entry {
	origins = append([]string(nil), origins...)
	sort.Strings(origins)
	return Entry{
		Surface:     surface,
		Reading:     reading,
		Cost:        cost,
		POS:         pos,
		Origins:     origins,
		Conjugation: conj,
		Okurigana:   OkuriganaLength(surface, reading),
	}
}

// Merge 依 Key 合併重複詞條：成本取最小，來源取聯集，活用型只在一致時保留。
func Merge(entries []Entry) []Entry {
	byKey := make(map[string]Entry, len(entries))
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Okurigana == 0 {
			e.Okurigana = OkuriganaLength(e.Surface, e.Reading)
		}
		k := e.Key()
		old, ok := byKey[k]
		if !ok {
			origins := append([]string(nil), e.Origins...)
			sort.Strings(origins)
			e.Origins = origins
			byKey[k] = e
			order = append(order, k)
			continue
		}
		if e.Cost < old.Cost {
			old.Cost = e.Cost
		}
		if old.Conjugation != e.Conjugation {
			if old.Conjugation == ConjNone {
				old.Conjugation = e.Conjugation
			} else if e.Conjugation != ConjNone {
				old.Conjugation = ConjUnknown
			}
		}
		if len(e.Origins) > 0 {
			seen := map[string]bool{}
			for _, o := range old.Origins {
				seen[o] = true
			}
			for _, o := range e.Origins {
				if !seen[o] {
					old.Origins = append(old.Origins, o)
					seen[o] = true
				}
			}
			sort.Strings(old.Origins)
		}
		byKey[k] = old
	}
	out := make([]Entry, 0, len(byKey))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	SortEntries(out)
	return out
}

// SortEntries 以穩定順序排序，確保同一份輸入產生相同位元組。
func SortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if ka, kb := reading.Key(a.Reading), reading.Key(b.Reading); ka != kb {
			return ka < kb
		}
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		if a.POS != b.POS {
			return a.POS < b.POS
		}
		return a.Cost < b.Cost
	})
}
