package input

import "github.com/0c-biggo/0c-ime/core/reading"

// Table 是一份羅馬字對照表。
//
// 鍵是羅馬字序列，值是對應的かな。一個值可以包含多個かな字元，
// 例如 "kya" 對應 "きゃ"，這會拆成兩個 reading.Unit。
// 表建立後不再修改，可以安全共用。
type Table struct {
	entries  map[string]string
	prefixes map[string]struct{}
}

// NewTable 建立對照表。空鍵或空值會被忽略。
func NewTable(entries map[string]string) *Table {
	t := &Table{
		entries:  make(map[string]string, len(entries)),
		prefixes: make(map[string]struct{}, len(entries)),
	}
	for keys, kana := range entries {
		if keys == "" || kana == "" {
			continue
		}
		t.entries[keys] = kana
		for i := 1; i < len(keys); i++ {
			t.prefixes[keys[:i]] = struct{}{}
		}
	}
	return t
}

// DefaultRomajiTable 回傳內建的羅馬字對照表。
func DefaultRomajiTable() *Table { return defaultRomajiTable }

// Lookup 查詢完整羅馬字序列對應的かな。
func (t *Table) Lookup(keys string) (string, bool) {
	if t == nil {
		return "", false
	}
	kana, ok := t.entries[keys]
	return kana, ok
}

// IsPrefix 回報序列是否為某個較長對照項的前綴。
func (t *Table) IsPrefix(keys string) bool {
	if t == nil {
		return false
	}
	_, ok := t.prefixes[keys]
	return ok
}

// splitUnits 把かな字串拆成一個字元一個讀音單位。
func splitUnits(kana string) reading.Sequence {
	if kana == "" {
		return nil
	}
	units := make(reading.Sequence, 0, len(kana))
	for _, r := range kana {
		units = append(units, reading.Unit(string(r)))
	}
	return units
}

// canSokuon 回報重複這個子音時是否形成促音。
// n 由專用規則處理，y、w、x、l 不使用促音寫法。
func canSokuon(c byte) bool {
	switch c {
	case 'b', 'c', 'd', 'f', 'g', 'h', 'j', 'k', 'm',
		'p', 'q', 'r', 's', 't', 'v', 'z':
		return true
	}
	return false
}

var defaultRomajiTable = NewTable(map[string]string{
	// 母音
	"a": "あ", "i": "い", "u": "う", "e": "え", "o": "お",

	// か行
	"ka": "か", "ki": "き", "ku": "く", "ke": "け", "ko": "こ",
	"kya": "きゃ", "kyi": "きぃ", "kyu": "きゅ", "kye": "きぇ", "kyo": "きょ",

	// が行
	"ga": "が", "gi": "ぎ", "gu": "ぐ", "ge": "げ", "go": "ご",
	"gya": "ぎゃ", "gyi": "ぎぃ", "gyu": "ぎゅ", "gye": "ぎぇ", "gyo": "ぎょ",

	// さ行
	"sa": "さ", "si": "し", "shi": "し", "su": "す", "se": "せ", "so": "そ",
	"sha": "しゃ", "shu": "しゅ", "she": "しぇ", "sho": "しょ",
	"sya": "しゃ", "syu": "しゅ", "syo": "しょ",

	// ざ行
	"za": "ざ", "zi": "じ", "ji": "じ", "zu": "ず", "ze": "ぜ", "zo": "ぞ",
	"ja": "じゃ", "ju": "じゅ", "je": "じぇ", "jo": "じょ",
	"jya": "じゃ", "jyu": "じゅ", "jyo": "じょ",
	"zya": "じゃ", "zyu": "じゅ", "zyo": "じょ",

	// た行
	"ta": "た", "ti": "ち", "chi": "ち", "tsu": "つ", "tu": "つ", "te": "て", "to": "と",
	"cha": "ちゃ", "chu": "ちゅ", "che": "ちぇ", "cho": "ちょ",
	"tya": "ちゃ", "tyu": "ちゅ", "tyo": "ちょ",
	"cya": "ちゃ", "cyu": "ちゅ", "cyo": "ちょ",

	// だ行
	"da": "だ", "di": "ぢ", "du": "づ", "de": "で", "do": "ど",
	"dya": "ぢゃ", "dyu": "ぢゅ", "dyo": "ぢょ",

	// な行
	"na": "な", "ni": "に", "nu": "ぬ", "ne": "ね", "no": "の",
	"nya": "にゃ", "nyu": "にゅ", "nyo": "にょ",

	// は行
	"ha": "は", "hi": "ひ", "hu": "ふ", "fu": "ふ", "he": "へ", "ho": "ほ",
	"hya": "ひゃ", "hyu": "ひゅ", "hyo": "ひょ",
	"fa": "ふぁ", "fi": "ふぃ", "fe": "ふぇ", "fo": "ふぉ",
	"fya": "ふゃ", "fyu": "ふゅ", "fyo": "ふょ",

	// ば行
	"ba": "ば", "bi": "び", "bu": "ぶ", "be": "べ", "bo": "ぼ",
	"bya": "びゃ", "byu": "びゅ", "byo": "びょ",

	// ぱ行
	"pa": "ぱ", "pi": "ぴ", "pu": "ぷ", "pe": "ぺ", "po": "ぽ",
	"pya": "ぴゃ", "pyu": "ぴゅ", "pyo": "ぴょ",

	// ま行
	"ma": "ま", "mi": "み", "mu": "む", "me": "め", "mo": "も",
	"mya": "みゃ", "myu": "みゅ", "myo": "みょ",

	// や行
	"ya": "や", "yi": "い", "yu": "ゆ", "ye": "いぇ", "yo": "よ",

	// ら行
	"ra": "ら", "ri": "り", "ru": "る", "re": "れ", "ro": "ろ",
	"rya": "りゃ", "ryu": "りゅ", "ryo": "りょ",

	// わ行
	"wa": "わ", "wi": "うぃ", "wu": "う", "we": "うぇ", "wo": "を",
	"wyi": "ゐ", "wye": "ゑ",

	// ゔ行
	"va": "ゔぁ", "vi": "ゔぃ", "vu": "ゔ", "ve": "ゔぇ", "vo": "ゔぉ",
	"vya": "ゔゃ", "vyu": "ゔゅ", "vyo": "ゔょ",

	// くぁ行
	"qa": "くぁ", "qi": "くぃ", "qu": "く", "qe": "くぇ", "qo": "くぉ",
	"qya": "くゃ", "qyu": "くゅ", "qyo": "くょ",

	// c で始まる別表記
	"ca": "か", "ci": "し", "cu": "く", "ce": "せ", "co": "こ",

	// 小書きかな（x）
	"xa": "ぁ", "xi": "ぃ", "xu": "ぅ", "xe": "ぇ", "xo": "ぉ",
	"xya": "ゃ", "xyu": "ゅ", "xyo": "ょ",
	"xtu": "っ", "xtsu": "っ",
	"xwa": "ゎ", "xka": "ゕ", "xke": "ゖ",

	// 小書きかな（l）
	"la": "ぁ", "li": "ぃ", "lu": "ぅ", "le": "ぇ", "lo": "ぉ",
	"lya": "ゃ", "lyu": "ゅ", "lyo": "ょ",
	"ltu": "っ", "ltsu": "っ",
	"lwa": "ゎ", "lka": "ゕ", "lke": "ゖ",

	// 長音符
	"-": "ー",
})
