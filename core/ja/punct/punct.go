// Package punct 產生日文的標點補全規則。
//
// 規則只插入標點，不改、不刪、不重排使用者打的任何字。回傳的 Edit 以原始
// 字串的 rune 位移表示，套用後的新字串一定可以把插入的標點拿掉還原成原文。
// 這個套件不載入模型、不碰時間與網路，也不修改注音的標點（core/punct 與
// core/session 的注音流程完全沒有被引用）。
//
// 預設關閉：Options.Enabled 為 false 時 Edits 與 Complete 都是原樣不動。
package punct

import (
	"sort"
	"strings"
	"unicode"
)

// 支援插入的標點。
const (
	Period      = "。"
	Comma       = "、"
	Question    = "？"
	Exclamation = "！"
)

// Options 是標點補全的開關與規則選擇。零值全部關閉。
type Options struct {
	// Enabled 是總開關，預設 false。
	Enabled bool
	// Period 允許在句尾補「。」。
	Period bool
	// Comma 允許在連接詞與接續助詞後補「、」。
	Comma bool
	// Brackets 允許在句尾補上未閉合的括號。
	Brackets bool
	// Question 允許疑問句尾補「？」。
	Question bool
	// Exclamation 允許感嘆句尾補「！」。
	Exclamation bool
	// MinRunes 是開始補標點的最短長度；0 視為 1。
	MinRunes int
}

// DefaultOptions 回傳建議規則，但總開關保持關閉。
func DefaultOptions() Options {
	return Options{
		Enabled:     false,
		Period:      true,
		Comma:       true,
		Brackets:    true,
		Question:    true,
		Exclamation: true,
		MinRunes:    1,
	}
}

// Edit 在原始字串第 Offset 個 rune 之前插入 Text。
//
// 多個 Edit 可以有相同 Offset，依切片順序插入；例如句尾的「。」要排在
// 收尾括號之前。
type Edit struct {
	Offset int
	Text   string
}

// Completer 依固定規則補標點。
type Completer struct {
	opts Options
}

// NewCompleter 建立補標點器。
func NewCompleter(opts Options) *Completer { return &Completer{opts: opts} }

// Options 回傳目前設定。
func (c *Completer) Options() Options {
	if c == nil {
		return Options{}
	}
	return c.opts
}

// Complete 回傳補完標點的文字。沒有插入時回傳原文。
func (c *Completer) Complete(text string) string {
	out, _ := Apply(text, c.Edits(text))
	return out
}

// Edits 回傳要插入的標點。回傳切片已依 Offset 穩定排序，可重複插入相同
// 位移（順序有意義）。功能關閉、文字太短或沒有可用規則時回傳 nil。
func (c *Completer) Edits(text string) []Edit {
	if c == nil || !c.opts.Enabled || text == "" {
		return nil
	}
	r := []rune(text)
	if len(r) < max(1, c.opts.MinRunes) {
		return nil
	}
	// 沒有任何かな的文字（例如純中文或英文）不補日文標點，避免碰觸
	// 注音／中文既有的標點行為。
	if !containsKana(r) {
		return nil
	}

	var edits []Edit
	if c.opts.Comma {
		edits = append(edits, commaEdits(r)...)
	}
	if !endsWithPunctuation(r[len(r)-1]) {
		if mark := c.terminalMark(r); mark != "" {
			edits = append(edits, Edit{Offset: len(r), Text: mark})
		}
	}
	if c.opts.Brackets {
		edits = append(edits, bracketEdits(r)...)
	}
	return normalize(edits)
}

// Apply 依 Edit 插入標點。位置必須遞增（可相同）且在原文範圍內，插入的
// 必須是支援的標點；任一項不合規就整批不套用，回傳原文與 false。
func Apply(text string, edits []Edit) (string, bool) {
	if len(edits) == 0 {
		return text, false
	}
	r := []rune(text)
	last := 0
	for i, e := range edits {
		if e.Offset < 0 || e.Offset > len(r) {
			return text, false
		}
		if i > 0 && e.Offset < last {
			return text, false
		}
		if !allowedMark(e.Text) {
			return text, false
		}
		last = e.Offset
	}
	var b strings.Builder
	b.Grow(len(text) + len(edits))
	at := 0
	for i := 0; i <= len(r); i++ {
		for at < len(edits) && edits[at].Offset == i {
			b.WriteString(edits[at].Text)
			at++
		}
		if i < len(r) {
			b.WriteRune(r[i])
		}
	}
	return b.String(), true
}

func (c *Completer) terminalMark(r []rune) string {
	if !isSentenceTail(r[len(r)-1]) {
		return ""
	}
	body := string(r)
	if c.opts.Question && hasSuffix(body, questionSuffixes) {
		return Question
	}
	if c.opts.Exclamation && hasSuffix(body, exclamationSuffixes) {
		return Exclamation
	}
	if c.opts.Period {
		return Period
	}
	return ""
}

// 連接詞：出現在句首或標點之後時，在詞後補「、」。
var connectives = []string{
	"しかし", "そして", "だから", "ところで", "つまり", "例えば",
	"ですが", "でも", "なお", "ただし", "それで", "したがって", "また",
}

// 接續助詞：後面還有內容時，在助詞後補「、」。
var conjunctiveParticles = []string{
	"けれども", "けれど", "けど", "ので", "のに", "から", "が",
}

var questionSuffixes = []string{
	"かしら", "でしょうか", "ますか", "ですか", "だろうか", "かな", "か",
}

var exclamationSuffixes = []string{
	"ましょう", "だろう", "よね", "よ", "ね", "わ", "ぞ", "ぜ", "な",
}

func commaEdits(r []rune) []Edit {
	var edits []Edit
	for _, word := range connectives {
		w := []rune(word)
		for i := 0; i+len(w) <= len(r); i++ {
			if !runesEqual(r[i:i+len(w)], w) {
				continue
			}
			after := i + len(w)
			if after >= len(r) || isPunctuation(r[after]) || isSpace(r[after]) {
				continue
			}
			if i > 0 && !isClauseStart(r[i-1]) {
				continue
			}
			edits = append(edits, Edit{Offset: after, Text: Comma})
		}
	}
	for _, word := range conjunctiveParticles {
		w := []rune(word)
		for i := 0; i+len(w) <= len(r); i++ {
			if !runesEqual(r[i:i+len(w)], w) {
				continue
			}
			after := i + len(w)
			if i == 0 || after >= len(r) || isPunctuation(r[after]) || isSpace(r[after]) {
				continue
			}
			if !isKana(r[i-1]) || isKana(r[after]) {
				continue
			}
			edits = append(edits, Edit{Offset: after, Text: Comma})
		}
	}
	return edits
}

// bracketEdits 在句尾補上沒有閉合的括號。開頭括號後面至少要有一個字，
// 才補對應的收尾括號；由內而外關閉。
func bracketEdits(r []rune) []Edit {
	var open []rune
	for _, ch := range r {
		if _, ok := openerCloser[ch]; ok {
			open = append(open, ch)
			continue
		}
		if len(open) > 0 && closerOf(open[len(open)-1]) == ch {
			open = open[:len(open)-1]
		}
	}
	if len(open) == 0 || openerCloser[open[len(open)-1]] == 0 {
		return nil
	}
	// 最後一個開頭括號就是句尾，代表裡面還沒有內容。
	if r[len(r)-1] == open[len(open)-1] {
		return nil
	}
	edits := make([]Edit, 0, len(open))
	for i := len(open) - 1; i >= 0; i-- {
		edits = append(edits, Edit{Offset: len(r), Text: string(openerCloser[open[i]])})
	}
	return edits
}

var openerCloser = map[rune]rune{
	'「': '」',
	'『': '』',
	'（': '）',
	'【': '】',
	'〈': '〉',
	'《': '》',
}

var allowedMarks = map[rune]bool{
	'。': true, '、': true, '？': true, '！': true,
	'」': true, '』': true, '）': true, '】': true, '〉': true, '》': true,
}

func closerOf(open rune) rune { return openerCloser[open] }

func allowedMark(s string) bool {
	r := []rune(s)
	return len(r) == 1 && allowedMarks[r[0]]
}

func normalize(edits []Edit) []Edit {
	if len(edits) == 0 {
		return nil
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].Offset < edits[j].Offset })
	out := edits[:0]
	for _, e := range edits {
		if len(out) > 0 && out[len(out)-1] == e {
			continue
		}
		out = append(out, e)
	}
	return out
}

func containsKana(r []rune) bool {
	for _, ch := range r {
		if isKana(ch) {
			return true
		}
	}
	return false
}

func isKana(ch rune) bool {
	return unicode.In(ch, unicode.Hiragana, unicode.Katakana)
}

func isSentenceTail(ch rune) bool {
	if isKana(ch) || unicode.Is(unicode.Han, ch) {
		return true
	}
	switch ch {
	case 'ー', '々', 'ゝ', 'ゞ', 'ヽ', 'ヾ':
		return true
	}
	return unicode.IsLetter(ch) || unicode.IsDigit(ch)
}

func endsWithPunctuation(ch rune) bool {
	switch ch {
	case '。', '、', '．', '，', '！', '？', '!', '?', '.', ',', '；', ';', '：', ':',
		'」', '』', '）', '】', '〉', '》', '…', '‥', '・', '～', '〜', '\n', '\r':
		return true
	}
	return false
}

func isPunctuation(ch rune) bool {
	return endsWithPunctuation(ch) || isSpace(ch)
}

func isSpace(ch rune) bool { return unicode.IsSpace(ch) }

func isClauseStart(ch rune) bool {
	switch ch {
	case '。', '、', '！', '？', '!', '?', '.', ',', '\n', '\r':
		return true
	}
	return isSpace(ch)
}

func hasSuffix(s string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
