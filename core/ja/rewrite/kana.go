package rewrite

import "strings"

// ToHiragana 把かな字串轉成平假名。
//
// 片假名、半角カナ與濁點／半濁點都會一併正規化；非かな字元原樣保留。
func ToHiragana(s string) string {
	var b strings.Builder
	for _, r := range toKatakanaRunes(s) {
		switch {
		case r >= 'ァ' && r <= 'ヶ':
			b.WriteRune(r - 0x60)
		case r == 'ヽ':
			b.WriteRune('ゝ')
		case r == 'ヾ':
			b.WriteRune('ゞ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ToKatakana 把かな字串轉成片假名。
//
// 平假名與半角カナ都會一併正規化；非かな字元原樣保留。
func ToKatakana(s string) string {
	var b strings.Builder
	for _, r := range toKatakanaRunes(s) {
		switch {
		case r >= 'ぁ' && r <= 'ゖ':
			b.WriteRune(r + 0x60)
		case r == 'ゝ':
			b.WriteRune('ヽ')
		case r == 'ゞ':
			b.WriteRune('ヾ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ToHalfwidthKana 把かな字串轉成半角カナ。
//
// 濁音與半濁音使用半角的濁點／半濁點表示，例如「がっこう」→「ｶﾞｯｺｳ」。
// 非かな字元原樣保留。
func ToHalfwidthKana(s string) string {
	var b strings.Builder
	for _, r := range ToKatakana(s) {
		if half, ok := katakanaHalfwidth[r]; ok {
			b.WriteString(half)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ToFullwidthLatin 把 ASCII 英數與標點轉成全角。空白轉成全角空格。
func ToFullwidthLatin(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteRune('\u3000')
		case r >= '!' && r <= '~':
			b.WriteRune(r + 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ToHalfwidthLatin 把全角英數與標點轉成 ASCII。全角空格轉成半角空白。
func ToHalfwidthLatin(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\u3000':
			b.WriteRune(' ')
		case r >= '！' && r <= '～':
			b.WriteRune(r - 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// toKatakanaRunes 先把半角カナ展開成片假名，其他字元不動。
func toKatakanaRunes(s string) []rune {
	src := []rune(s)
	out := make([]rune, 0, len(src))
	for i := 0; i < len(src); i++ {
		r := src[i]
		switch r {
		case 'ﾞ':
			if len(out) > 0 {
				if voiced, ok := katakanaDakuten[out[len(out)-1]]; ok {
					out[len(out)-1] = voiced
					continue
				}
			}
			out = append(out, '゛')
			continue
		case 'ﾟ':
			if len(out) > 0 {
				if semi, ok := katakanaHandakuten[out[len(out)-1]]; ok {
					out[len(out)-1] = semi
					continue
				}
			}
			out = append(out, '゜')
			continue
		}
		if full, ok := halfwidthToKatakana[r]; ok {
			out = append(out, full)
			continue
		}
		out = append(out, r)
	}
	return out
}

// halfwidthToKatakana 只放單一字元的半角カナ；濁點由 toKatakanaRunes 組合。
var halfwidthToKatakana = map[rune]rune{
	'｡': '。', '｢': '「', '｣': '」', '､': '、', '･': '・',
	'ｦ': 'ヲ', 'ｧ': 'ァ', 'ｨ': 'ィ', 'ｩ': 'ゥ', 'ｪ': 'ェ', 'ｫ': 'ォ',
	'ｬ': 'ャ', 'ｭ': 'ュ', 'ｮ': 'ョ', 'ｯ': 'ッ', 'ｰ': 'ー',
	'ｱ': 'ア', 'ｲ': 'イ', 'ｳ': 'ウ', 'ｴ': 'エ', 'ｵ': 'オ',
	'ｶ': 'カ', 'ｷ': 'キ', 'ｸ': 'ク', 'ｹ': 'ケ', 'ｺ': 'コ',
	'ｻ': 'サ', 'ｼ': 'シ', 'ｽ': 'ス', 'ｾ': 'セ', 'ｿ': 'ソ',
	'ﾀ': 'タ', 'ﾁ': 'チ', 'ﾂ': 'ツ', 'ﾃ': 'テ', 'ﾄ': 'ト',
	'ﾅ': 'ナ', 'ﾆ': 'ニ', 'ﾇ': 'ヌ', 'ﾈ': 'ネ', 'ﾉ': 'ノ',
	'ﾊ': 'ハ', 'ﾋ': 'ヒ', 'ﾌ': 'フ', 'ﾍ': 'ヘ', 'ﾎ': 'ホ',
	'ﾏ': 'マ', 'ﾐ': 'ミ', 'ﾑ': 'ム', 'ﾒ': 'メ', 'ﾓ': 'モ',
	'ﾔ': 'ヤ', 'ﾕ': 'ユ', 'ﾖ': 'ヨ',
	'ﾗ': 'ラ', 'ﾘ': 'リ', 'ﾙ': 'ル', 'ﾚ': 'レ', 'ﾛ': 'ロ',
	'ﾜ': 'ワ', 'ﾝ': 'ン',
	'ﾞ': '゛', 'ﾟ': '゜',
}

var katakanaDakuten = map[rune]rune{
	'ウ': 'ヴ', 'カ': 'ガ', 'キ': 'ギ', 'ク': 'グ', 'ケ': 'ゲ', 'コ': 'ゴ',
	'サ': 'ザ', 'シ': 'ジ', 'ス': 'ズ', 'セ': 'ゼ', 'ソ': 'ゾ',
	'タ': 'ダ', 'チ': 'ヂ', 'ツ': 'ヅ', 'テ': 'デ', 'ト': 'ド',
	'ハ': 'バ', 'ヒ': 'ビ', 'フ': 'ブ', 'ヘ': 'ベ', 'ホ': 'ボ',
	'ワ': 'ヷ', 'ヰ': 'ヸ', 'ヱ': 'ヹ', 'ヲ': 'ヺ',
}

var katakanaHandakuten = map[rune]rune{
	'ハ': 'パ', 'ヒ': 'ピ', 'フ': 'プ', 'ヘ': 'ペ', 'ホ': 'ポ',
}

var katakanaHalfwidth = map[rune]string{
	'。': "｡", '「': "｢", '」': "｣", '、': "､", '・': "･",
	'ヲ': "ｦ", 'ァ': "ｧ", 'ィ': "ｨ", 'ゥ': "ｩ", 'ェ': "ｪ", 'ォ': "ｫ",
	'ャ': "ｬ", 'ュ': "ｭ", 'ョ': "ｮ", 'ッ': "ｯ", 'ー': "ｰ",
	'ア': "ｱ", 'イ': "ｲ", 'ウ': "ｳ", 'エ': "ｴ", 'オ': "ｵ",
	'カ': "ｶ", 'キ': "ｷ", 'ク': "ｸ", 'ケ': "ｹ", 'コ': "ｺ",
	'サ': "ｻ", 'シ': "ｼ", 'ス': "ｽ", 'セ': "ｾ", 'ソ': "ｿ",
	'タ': "ﾀ", 'チ': "ﾁ", 'ツ': "ﾂ", 'テ': "ﾃ", 'ト': "ﾄ",
	'ナ': "ﾅ", 'ニ': "ﾆ", 'ヌ': "ﾇ", 'ネ': "ﾈ", 'ノ': "ﾉ",
	'ハ': "ﾊ", 'ヒ': "ﾋ", 'フ': "ﾌ", 'ヘ': "ﾍ", 'ホ': "ﾎ",
	'マ': "ﾏ", 'ミ': "ﾐ", 'ム': "ﾑ", 'メ': "ﾒ", 'モ': "ﾓ",
	'ヤ': "ﾔ", 'ユ': "ﾕ", 'ヨ': "ﾖ",
	'ラ': "ﾗ", 'リ': "ﾘ", 'ル': "ﾙ", 'レ': "ﾚ", 'ロ': "ﾛ",
	'ワ': "ﾜ", 'ヰ': "ｲ", 'ヱ': "ｴ", 'ン': "ﾝ",
	'ガ': "ｶﾞ", 'ギ': "ｷﾞ", 'グ': "ｸﾞ", 'ゲ': "ｹﾞ", 'ゴ': "ｺﾞ",
	'ザ': "ｻﾞ", 'ジ': "ｼﾞ", 'ズ': "ｽﾞ", 'ゼ': "ｾﾞ", 'ゾ': "ｿﾞ",
	'ダ': "ﾀﾞ", 'ヂ': "ﾁﾞ", 'ヅ': "ﾂﾞ", 'デ': "ﾃﾞ", 'ド': "ﾄﾞ",
	'バ': "ﾊﾞ", 'ビ': "ﾋﾞ", 'ブ': "ﾌﾞ", 'ベ': "ﾍﾞ", 'ボ': "ﾎﾞ",
	'パ': "ﾊﾟ", 'ピ': "ﾋﾟ", 'プ': "ﾌﾟ", 'ペ': "ﾍﾟ", 'ポ': "ﾎﾟ",
	'ヴ': "ｳﾞ", 'ヷ': "ﾜﾞ", 'ヸ': "ｲﾞ", 'ヹ': "ｴﾞ", 'ヺ': "ｦﾞ",
	'ヵ': "ｶ", 'ヶ': "ｹ", 'ヮ': "ﾜ",
	'゛': "ﾞ", '゜': "ﾟ", '　': " ",
}
