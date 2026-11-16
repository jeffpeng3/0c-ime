package rewrite

import (
	"fmt"
	"strconv"
	"strings"
)

// 各種數字表記的候選註解。字串與 Mozc 的候選描述一致，供外殼直接顯示。
const (
	AnnotationKanji   = "漢数字"
	AnnotationDaiji   = "大字"
	AnnotationCircled = "丸数字"
	AnnotationRoman   = "ローマ数字(大文字)"
	AnnotationRomanS  = "ローマ数字(小文字)"
	AnnotationHex     = "16進数"
	AnnotationOct     = "8進数"
	AnnotationBin     = "2進数"
)

var (
	kanjiDigits = [...]string{"〇", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	daijiDigits = [...]string{"", "壱", "弐", "参", "四", "五", "六", "七", "八", "九"}

	kanjiBigUnits = [...]string{"", "万", "億", "兆", "京"}
	daijiBigUnits = [...]string{"", "萬", "億", "兆", "京"}

	romanCapital = [...]string{
		"", "Ⅰ", "Ⅱ", "Ⅲ", "Ⅳ", "Ⅴ", "Ⅵ", "Ⅶ", "Ⅷ", "Ⅸ", "Ⅹ", "Ⅺ", "Ⅻ",
	}
	romanSmall = [...]string{
		"", "ⅰ", "ⅱ", "ⅲ", "ⅳ", "ⅴ", "ⅵ", "ⅶ", "ⅷ", "ⅸ", "ⅹ", "ⅺ", "ⅻ",
	}
	circledNumbers = [...]string{
		"⓪", "①", "②", "③", "④", "⑤", "⑥", "⑦", "⑧", "⑨", "⑩",
		"⑪", "⑫", "⑬", "⑭", "⑮", "⑯", "⑰", "⑱", "⑲", "⑳", "㉑",
		"㉒", "㉓", "㉔", "㉕", "㉖", "㉗", "㉘", "㉙", "㉚", "㉛", "㉜",
		"㉝", "㉞", "㉟", "㊱", "㊲", "㊳", "㊴", "㊵", "㊶", "㊷", "㊸",
		"㊹", "㊺", "㊻", "㊼", "㊽", "㊾", "㊿",
	}
)

// numberCandidates 回傳詞面的數字表記候選。詞面不是純阿拉伯數字時回傳 nil。
//
// 支援半角與全角阿拉伯數字。輸出順序與 Mozc 的 NumberRewriter 相近：
// 漢数字、大字、丸数字、羅馬數字、16／8／2 進位。
func numberCandidates(surface string) []langCandidate {
	n, ok := parseArabic(surface)
	if !ok {
		return nil
	}
	var out []langCandidate
	add := func(text, annotation string) {
		out = append(out, langCandidate{surface: text, annotation: annotation})
	}

	add(kanjiNumber(n, false), AnnotationKanji)
	add(kanjiNumber(n, true), AnnotationDaiji)

	if n < uint64(len(circledNumbers)) {
		add(circledNumbers[n], AnnotationCircled)
	}
	if n > 0 && n < uint64(len(romanCapital)) {
		add(romanCapital[n], AnnotationRoman)
		add(romanSmall[n], AnnotationRomanS)
	}

	// Mozc 只在數字大到需要另一種進位才有意義時才列進位候選。
	if n > 9 {
		add(fmt.Sprintf("0x%x", n), AnnotationHex)
	}
	if n > 7 {
		add(fmt.Sprintf("0%o", n), AnnotationOct)
	}
	if n > 1 {
		add("0b"+strconv.FormatUint(n, 2), AnnotationBin)
	}
	return out
}

// parseArabic 解析純阿拉伯數字；全角數字先轉成半角。
func parseArabic(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= '０' && r <= '９':
			b.WriteRune('0' + r - '０')
		default:
			return 0, false
		}
	}
	n, err := strconv.ParseUint(b.String(), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// kanjiNumber 把整數轉成漢數字或大字。
//
// 一般漢數字省略十、百、千前面的「一」；大字則保留「壱」，
// 並使用阡、萬等舊字，例如 123 → 壱百弐拾参。
func kanjiNumber(n uint64, daiji bool) string {
	if n == 0 {
		return "零"
	}
	var groups [5]uint64
	for i := range groups {
		groups[i] = n % 10000
		n /= 10000
	}

	var b strings.Builder
	for i := len(groups) - 1; i >= 0; i-- {
		if groups[i] == 0 {
			continue
		}
		b.WriteString(formatGroup(groups[i], daiji))
		if daiji {
			b.WriteString(daijiBigUnits[i])
		} else {
			b.WriteString(kanjiBigUnits[i])
		}
	}
	return b.String()
}

func formatGroup(n uint64, daiji bool) string {
	type unit struct {
		value int
		rank  string
		old   string
	}
	units := [...]unit{
		{1000, "千", "阡"},
		{100, "百", "百"},
		{10, "十", "拾"},
		{1, "", ""},
	}
	var b strings.Builder
	for _, u := range units {
		d := int(n) / u.value
		n -= uint64(d * u.value)
		if d == 0 {
			continue
		}
		if daiji {
			b.WriteString(daijiDigits[d])
			b.WriteString(u.old)
			continue
		}
		if d != 1 || u.value == 1 {
			b.WriteString(kanjiDigits[d])
		}
		b.WriteString(u.rank)
	}
	return b.String()
}
