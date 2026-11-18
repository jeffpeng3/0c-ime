package decode

import (
	"unicode"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// 平假名讀音的片假名詞面容忍度。
//
// 片假名詞面只有在明顯比同讀音的非片假名詞面便宜時才保留；差距在容忍度
// 以內時視為雜訊（ワタ 對 私、ワタシ 對 私），不讓它形成錯誤路徑。真正以
// 片假名書寫的外來語（メール、パン、バス、カメ）成本優勢通常大於這些值，
// 仍然保留。詞性 unknown 的詞條多來自 pack 的未分類項，容忍度放寬，避免
// 只用成本就把可用的外來語移掉。
const (
	katakanaMargin           = 0.5
	katakanaUnknownPOSMargin = 2.0
)

// applyScriptPolicy 依輸入讀音的書寫系統整理同一組候選詞。
//
// 平假名讀音代表使用者用羅馬字或かな輸入一般日文，不是直接輸入片假名。此時：
//   - 同一個讀音若已有平假名付属語（助詞、助動詞、接尾詞），片假名與拉丁
//     字母詞面都是雜訊，直接排除（です 不應變成 デス 或 ＤＥＡＴＨ）。
//   - 片假名自立語若沒有明顯比同讀音的非片假名詞面便宜，也排除，避免成本
//     幾乎相同的片假名拼寫勝出（私 不應變成 ワタ）。
func applyScriptPolicy(nodes []Node, seq reading.Sequence, start int) []Node {
	if len(nodes) == 0 {
		return nodes
	}
	dependent := make(map[int]bool)
	bestPlain := 0.0
	havePlain := false
	for _, n := range nodes {
		if !hiraganaUnits(seq[start:n.End]) {
			continue
		}
		if !katakanaSurface(n.Entry.Surface) && !latinSurface(n.Entry.Surface) {
			if !havePlain || n.Entry.Cost < bestPlain {
				bestPlain = n.Entry.Cost
				havePlain = true
			}
		}
		if n.Entry.POS.Dependent() && hiraganaSurface(n.Entry.Surface) {
			dependent[n.End] = true
		}
	}
	out := nodes[:0]
	for _, n := range nodes {
		if !hiraganaUnits(seq[start:n.End]) {
			out = append(out, n)
			continue
		}
		katakana := katakanaSurface(n.Entry.Surface)
		if dependent[n.End] && (katakana || latinSurface(n.Entry.Surface)) {
			continue
		}
		if katakana && havePlain {
			margin := katakanaMargin
			if n.Entry.POS == lexicon.POSOther {
				margin = katakanaUnknownPOSMargin
			}
			if bestPlain-n.Entry.Cost <= margin {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// hiraganaUnits 回報這段讀音是否全部由平假名（含長音符、踊り字）組成。
func hiraganaUnits(units reading.Sequence) bool {
	if len(units) == 0 {
		return false
	}
	for _, u := range units {
		for _, r := range string(u) {
			if !unicode.Is(unicode.Hiragana, r) && r != 'ー' && r != 'ゝ' && r != 'ゞ' {
				return false
			}
		}
	}
	return true
}

// hiraganaSurface 回報詞面是否至少有一個平假名，且其餘字元也都是平假名或長音符。
func hiraganaSurface(surface string) bool {
	seen := false
	for _, r := range surface {
		switch {
		case unicode.Is(unicode.Hiragana, r):
			seen = true
		case r == 'ー' || r == 'ゝ' || r == 'ゞ':
		default:
			return false
		}
	}
	return seen
}

// katakanaSurface 回報詞面是否至少有一個片假名，且其餘字元也都是かな或長音符。
func katakanaSurface(surface string) bool {
	seen := false
	for _, r := range surface {
		switch {
		case unicode.Is(unicode.Katakana, r):
			seen = true
		case unicode.Is(unicode.Hiragana, r), r == 'ー', r == 'ゝ', r == 'ゞ':
		default:
			return false
		}
	}
	return seen
}

// latinSurface 回報詞面是否含拉丁字母（半形或全形）。
func latinSurface(surface string) bool {
	for _, r := range surface {
		if unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}
