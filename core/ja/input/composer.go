// Package input 實作日文輸入的組字層。
//
// 這一層只把實體按鍵變成かな讀音，不做かな漢字変換。羅馬字與直接かな輸入
// 都輸出 reading.Sequence，一個單位是一個かな字元，因此小書きかな與長音符
// 各自佔一個單位。
package input

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Mode 是日文組字層的輸入方式。
type Mode string

const (
	// ModeRomaji 是羅馬字輸入。
	ModeRomaji Mode = "romaji"
	// ModeKana 是直接かな輸入。
	ModeKana Mode = "kana"
)

// Composer 是日文組字狀態機。
//
// 羅馬字模式保留尚未完成的按鍵，例如 "k"；下一個按鍵完成かな後才放進
// Units。單獨的 "n" 會停在 Pending，等到邊界、Flush 或下一個按鍵才成為
// "ん"。直接かな模式每次 Insert 都是一個已完成的かな。
type Composer struct {
	mode    Mode
	table   *Table
	units   reading.Sequence
	pending string
	// carryN 表示剛打完 "nn" 並已輸出「ん」，第二個 n 仍可和下一個
	// 母音或 y 組成な行かな。下一個非母音按鍵會直接丟掉這個 n。
	carryN bool
}

var _ lang.Composer = (*Composer)(nil)

// NewComposer 建立指定模式的組字器，羅馬字模式使用內建對照表。
func NewComposer(mode Mode) *Composer {
	return NewComposerWithTable(mode, nil)
}

// NewRomajiComposer 建立羅馬字組字器。
func NewRomajiComposer() *Composer { return NewComposer(ModeRomaji) }

// NewKanaComposer 建立直接かな組字器。
func NewKanaComposer() *Composer { return NewComposer(ModeKana) }

// NewComposerWithTable 建立使用自訂羅馬字表的組字器。
// table 為 nil 時使用內建表；未知 mode 視為羅馬字模式。
func NewComposerWithTable(mode Mode, table *Table) *Composer {
	if mode != ModeKana {
		mode = ModeRomaji
	}
	if table == nil {
		table = DefaultRomajiTable()
	}
	return &Composer{mode: mode, table: table}
}

// Mode 回傳目前的輸入方式。
func (c *Composer) Mode() Mode { return c.mode }

// Insert 處理一個按鍵。
//
// 羅馬字模式只接受單一可列印 ASCII 字元；直接かな模式接受一個或多個
// かな字元。不合法或不能接在目前前綴後面的按鍵不會改變任何狀態。
func (c *Composer) Insert(key string) (lang.ComposeResult, bool) {
	if c.mode == ModeKana {
		return c.insertKana(key)
	}
	return c.insertRomaji(key)
}

func (c *Composer) insertKana(key string) (lang.ComposeResult, bool) {
	if key == "" {
		return lang.ComposeResult{Pending: c.pending}, false
	}
	runes := []rune(key)
	completed := make(reading.Sequence, 0, len(runes))
	for _, r := range runes {
		kana, ok := normalizeKana(r)
		if !ok {
			return lang.ComposeResult{Pending: c.pending}, false
		}
		completed = append(completed, reading.Unit(string(kana)))
	}
	c.units = append(c.units, completed...)
	return lang.ComposeResult{Completed: completed, Pending: c.pending}, true
}

func (c *Composer) insertRomaji(key string) (lang.ComposeResult, bool) {
	if key == "" {
		return lang.ComposeResult{Pending: c.pending}, false
	}
	r, size := utf8.DecodeRuneInString(key)
	if size != len(key) || r < 0x21 || r > 0x7e {
		return lang.ComposeResult{Pending: c.pending}, false
	}
	completed, ok := c.push(string(unicode.ToLower(r)))
	if !ok {
		return lang.ComposeResult{Pending: c.pending}, false
	}
	return lang.ComposeResult{Completed: completed, Pending: c.pending}, true
}

func (c *Composer) push(key string) (reading.Sequence, bool) {
	if c.carryN {
		if !c.knownStart(key) {
			return nil, false
		}
		c.carryN = false
		switch {
		case isVowelOrY(key):
			c.pending = "n"
			return c.pushGeneric(key)
		case key == "n":
			c.pending = "n"
			return nil, true
		case key == "'":
			return nil, true
		default:
			// 其他可開始新かな的按鍵直接處理，不會再產生第二個「ん」。
			return c.push(key)
		}
	}
	switch c.pending {
	case "n":
		return c.pushAfterN(key)
	}
	return c.pushGeneric(key)
}

// pushAfterN 處理前一個按鍵是單獨 n 的情況。
// na、ni、nya 走一般對照表；n 後面接子音、符號或長音符時先完成「ん」。
func (c *Composer) pushAfterN(key string) (reading.Sequence, bool) {
	switch {
	case key == "n":
		c.pending = ""
		c.carryN = true
		return c.emit("ん"), true
	case key == "'":
		c.pending = ""
		return c.emit("ん"), true
	case isVowelOrY(key):
		return c.pushGeneric(key)
	case c.knownStart(key):
		c.pending = ""
		return c.emitThen("ん", key)
	default:
		return nil, false
	}
}

func (c *Composer) pushGeneric(key string) (reading.Sequence, bool) {
	candidate := c.pending + key

	if kana, ok := c.table.Lookup(candidate); ok {
		c.pending = ""
		units := splitUnits(kana)
		c.units = append(c.units, units...)
		return units, true
	}

	if c.table.IsPrefix(candidate) {
		c.pending = candidate
		return nil, true
	}

	// 促音：相同子音連續出現時先完成「っ」，第二個子音留在 Pending，
	// 讓後面的母音或拗音組成下一個かな。n 由上面的專用規則處理。
	if len(candidate) == 2 && candidate[0] == candidate[1] && canSokuon(candidate[0]) {
		c.pending = candidate[1:]
		return c.emit("っ"), true
	}

	return nil, false
}

func (c *Composer) emit(unit reading.Unit) reading.Sequence {
	c.units = append(c.units, unit)
	return reading.Sequence{unit}
}

func (c *Composer) emitThen(unit reading.Unit, key string) (reading.Sequence, bool) {
	done := c.emit(unit)
	rest, ok := c.push(key)
	if !ok {
		return done, false
	}
	return append(done, rest...), true
}

func (c *Composer) knownStart(key string) bool {
	if key == "n" || key == "'" {
		return true
	}
	if _, ok := c.table.Lookup(key); ok {
		return true
	}
	return c.table.IsPrefix(key)
}

func isVowelOrY(key string) bool {
	switch key {
	case "a", "i", "u", "e", "o", "y":
		return true
	}
	return false
}

// Backspace 先移除未完成的按鍵；沒有未完成按鍵時移除最後一個已完成的かな。
// 兩者都沒有時回傳 false。
func (c *Composer) Backspace() bool {
	if c.carryN {
		c.carryN = false
		if len(c.units) > 0 {
			c.units = c.units[:len(c.units)-1]
		}
		return true
	}
	if c.pending != "" {
		runes := []rune(c.pending)
		c.pending = string(runes[:len(runes)-1])
		return true
	}
	if len(c.units) > 0 {
		c.units = c.units[:len(c.units)-1]
		return true
	}
	return false
}

// Reset 清空所有組字狀態。
func (c *Composer) Reset() {
	c.units = nil
	c.pending = ""
	c.carryN = false
}

// Cancel 清空整段組字，對應 Escape；有東西被清掉時回傳 true。
func (c *Composer) Cancel() bool {
	if len(c.units) == 0 && c.pending == "" && !c.carryN {
		return false
	}
	c.Reset()
	return true
}

// Flush 結束目前輸入，把可以獨立的未完成按鍵變成かな。
// 目前只有單獨的 n 會變成「ん」；其他未完成前綴保持原狀並回傳 false。
func (c *Composer) Flush() (reading.Sequence, bool) {
	switch c.pending {
	case "n":
		c.pending = ""
		return c.emit("ん"), true
	default:
		return nil, false
	}
}

// Units 回傳已完成的讀音單位。回傳的是副本，呼叫端不能改動組字器。
func (c *Composer) Units() reading.Sequence {
	if len(c.units) == 0 {
		return nil
	}
	out := make(reading.Sequence, len(c.units))
	copy(out, c.units)
	return out
}

// Pending 回傳尚未完成的按鍵，例如 "k"；沒有時為空字串。
func (c *Composer) Pending() string { return c.pending }

// Segments 回傳組字區的顯示分段：已完成的かな一段，未完成按鍵一段。
func (c *Composer) Segments() []lang.DisplaySegment {
	var segments []lang.DisplaySegment
	if len(c.units) > 0 {
		var text strings.Builder
		for _, unit := range c.units {
			text.WriteString(string(unit))
		}
		segments = append(segments, lang.DisplaySegment{
			Reading: c.Units(),
			Text:    text.String(),
		})
	}
	if c.pending != "" {
		segments = append(segments, lang.DisplaySegment{
			Text:    c.pending,
			Pending: true,
		})
	}
	return segments
}

// DisplayText 回傳組字區目前顯示的所有文字，包含未完成按鍵。
func (c *Composer) DisplayText() string {
	var text strings.Builder
	for _, unit := range c.units {
		text.WriteString(string(unit))
	}
	text.WriteString(c.pending)
	return text.String()
}

// Cursor 回傳游標位置，單位是顯示字元數。
// 組字層只在尾端插入與刪除，所以游標固定在組字內容之後。
func (c *Composer) Cursor() int {
	return utf8.RuneCountInString(c.DisplayText())
}

// normalizeKana 把直接かな輸入正規化成平假名。
// 片假名轉成對應平假名，長音符與踊り字保持原樣。
func normalizeKana(r rune) (rune, bool) {
	switch {
	case r >= 'ぁ' && r <= 'ゖ':
		return r, true
	case r == 'ー' || r == 'ゝ' || r == 'ゞ':
		return r, true
	case r >= 'ァ' && r <= 'ヶ':
		return r - 0x60, true
	case r == 'ヽ':
		return 'ゝ', true
	case r == 'ヾ':
		return 'ゞ', true
	default:
		return 0, false
	}
}
