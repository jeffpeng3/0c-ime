package mixed

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/ja/input"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Composer 是日英混打的組字器。它包住 core/ja/input 的かな組字器：
//
//   - 大寫、全角、數字、網址、email、識別字與英文詞照字面留在讀音序列裡。
//   - 其餘 ASCII 按鍵仍用原本的羅馬字表組成かな。
//   - かな與英文字可以直接相接；英文段是否結束由下一個非 ASCII 按鍵、
//     Flush、変換或標點決定。
//
// Composer 符合 lang.Composer，可直接註冊到語言 profile。英文字的自動補完
// 不是 lang.Composer 的一部分，宿主可以另外呼叫 Suggestion 與 AcceptSuggestion。
type Composer struct {
	method  string
	mode    input.Mode
	cfg     Config
	english *English

	jp   *input.Composer
	base reading.Sequence
	kind []bool // base 每一項是否為照字面的英數；false 是日文讀音

	run        string
	runUnits   reading.Sequence
	pending    string
	literal    bool
	kanaValid  bool
	forced     bool
	kanaForced bool // 使用者明確要求這一段英數按鍵組成かな（P2-52）
}

var _ lang.Composer = (*Composer)(nil)

// NewComposer 建立日英混打組字器。method.ID 為 "kana" 時使用直接かな輸入，
// 其他值（含空字串）使用羅馬字。
func NewComposer(method lang.InputMethod, cfg Config) *Composer {
	cfg = cfg.normalized()
	return newComposer(method, cfg)
}

func newComposer(method lang.InputMethod, cfg Config) *Composer {
	mode := input.ModeRomaji
	if method.ID == "kana" {
		mode = input.ModeKana
	}
	return &Composer{
		method:  method.ID,
		mode:    mode,
		cfg:     cfg,
		english: cfg.Vocabulary,
		jp:      input.NewComposer(mode),
	}
}

// Mode 回傳目前的輸入方式 ID。
func (c *Composer) Mode() string {
	if c == nil {
		return ""
	}
	return c.method
}

// English 回傳共用的英文查詢層。
func (c *Composer) English() *English {
	if c == nil {
		return nil
	}
	return c.english
}

// Insert 處理一個按鍵，符合 lang.Composer。
func (c *Composer) Insert(key string) (lang.ComposeResult, bool) {
	if c == nil || key == "" {
		return lang.ComposeResult{Pending: c.Pending()}, false
	}
	before := c.Units()
	r, size := utf8.DecodeRuneInString(key)
	single := size == len(key)

	switch {
	case single && isWordRune(r):
		c.startRun()
		c.appendRunKey(key)
		return c.result(before), true
	case single && r == ' ':
		if c.run != "" && c.literal {
			c.lockRun()
			c.appendLiteral(" ")
			return c.result(before), true
		}
		c.lockRun()
		return c.result(before), false
	case single && isRunPunctuation(r):
		if c.run == "" {
			if !strings.ContainsRune("@#$", r) {
				return c.result(before), false
			}
			c.startRun()
			c.run = key
			c.recompute()
			return c.result(before), true
		}
		candidate := c.run + key
		if c.literal {
			c.run = candidate
			c.recompute()
			return c.result(before), true
		}
		if Classify(candidate, c.english).Literal() && c.applySplit(candidate, c.run) {
			return c.result(before), true
		}
		c.lockRun()
		return c.result(before), false
	}

	// かな、全角標點或其他不能放進英數段的按鍵：先讓目前的英數段定案，
	// 再把按鍵交給原本的日文組字器。日文組字器不接受時，外殼照舊處理。
	return c.insertJapanese(before, key)
}

func (c *Composer) insertJapanese(before reading.Sequence, key string) (lang.ComposeResult, bool) {
	c.lockRun()
	c.flushJapanesePending()
	res, ok := c.jp.Insert(key)
	if ok {
		c.appendUnits(res.Completed, false)
	}
	return c.result(before), ok
}

// Backspace 先刪除未完成的英數段或羅馬字，再刪除最後一個已完成的讀音。
func (c *Composer) Backspace() bool {
	if c == nil {
		return false
	}
	if c.run != "" {
		wasLiteral := c.literal
		runes := []rune(c.run)
		c.run = string(runes[:len(runes)-1])
		if c.run == "" {
			c.runUnits = nil
			c.pending = ""
			c.literal = false
			c.kanaValid = false
		} else {
			if wasLiteral {
				c.forced = true // 刪改英文時不要突然變回かな
			}
			c.recompute()
		}
		return true
	}
	if len(c.base) > 0 {
		last := len(c.base) - 1
		literal := c.kind[last]
		c.base = c.base[:last]
		c.kind = c.kind[:last]
		if !literal && len(c.jp.Units()) > 0 {
			c.jp.Backspace()
		}
		return true
	}
	if c.jp.Pending() != "" {
		return c.jp.Backspace()
	}
	return false
}

// Reset 清空所有組字狀態。
func (c *Composer) Reset() {
	if c == nil {
		return
	}
	c.jp.Reset()
	c.base = nil
	c.kind = nil
	c.clearRun()
}

// Cancel 清空整段組字，對應 Escape；有東西被清掉時回傳 true。
func (c *Composer) Cancel() bool {
	if c == nil || len(c.base) == 0 && c.run == "" && c.jp.Pending() == "" {
		return false
	}
	c.Reset()
	return true
}

// Flush 結束目前輸入。英數段照目前的判定定案；かな段中單獨的 n 變成「ん」。
func (c *Composer) Flush() (reading.Sequence, bool) {
	if c == nil {
		return nil, false
	}
	before := c.Units()
	if c.run != "" {
		c.lockRun()
		return addedUnits(before, c.Units()), true
	}
	if done, ok := c.jp.Flush(); ok {
		c.appendUnits(done, false)
		return addedUnits(before, c.Units()), true
	}
	return nil, false
}

// Units 回傳已完成的讀音單位，包含照字面的英文字。
func (c *Composer) Units() reading.Sequence {
	if c == nil {
		return nil
	}
	out := make(reading.Sequence, 0, len(c.base)+len(c.runUnits))
	out = append(out, c.base...)
	out = append(out, c.runUnits...)
	return out
}

// Pending 回傳尚未完成的按鍵，例如羅馬字的 "k"；英文字照字面時為空字串。
func (c *Composer) Pending() string {
	if c == nil {
		return ""
	}
	if c.run != "" {
		return c.pending
	}
	return c.jp.Pending()
}

// Segments 回傳組字區的顯示分段：已完成的讀音一段，未完成按鍵一段。
func (c *Composer) Segments() []lang.DisplaySegment {
	if c == nil {
		return nil
	}
	var segments []lang.DisplaySegment
	units := c.Units()
	if len(units) > 0 {
		segments = append(segments, lang.DisplaySegment{
			Reading: units,
			Text:    sequenceText(units),
		})
	}
	if pending := c.Pending(); pending != "" {
		segments = append(segments, lang.DisplaySegment{Text: pending, Pending: true})
	}
	return segments
}

// DisplayText 回傳組字區目前顯示的所有文字。
func (c *Composer) DisplayText() string {
	if c == nil {
		return ""
	}
	return sequenceText(c.Units()) + c.Pending()
}

// Cursor 回傳游標位置，單位是顯示字元數。組字器只在尾端插入與刪除。
func (c *Composer) Cursor() int {
	return utf8.RuneCountInString(c.DisplayText())
}

// Run 回傳目前正在判定的英數段與是否已照字面定案。
func (c *Composer) Run() (text string, literal bool) {
	if c == nil {
		return "", false
	}
	return c.run, c.literal
}

// Suggestion 回傳目前英數段的英文自動補完。沒有較長的候選時 ok 為 false。
func (c *Composer) Suggestion() (string, bool) {
	if c == nil || c.run == "" || !asciiLetters(c.run) {
		return "", false
	}
	return c.english.Complete(c.run)
}

// AcceptSuggestion 接受目前的自動補完，並鎖定為英文。成功時回傳 true。
func (c *Composer) AcceptSuggestion() bool {
	if c == nil || c.run == "" {
		return false
	}
	word, ok := c.Suggestion()
	if !ok {
		return false
	}
	c.run = word
	c.forced = true
	c.recompute()
	return true
}

// AcceptEnglish 把目前英數段明確鎖定成英文，即使詞典沒有這個字。沒有英數
// 段時回傳 false。
func (c *Composer) AcceptEnglish() bool {
	if c == nil || c.run == "" {
		return false
	}
	c.forced = true
	c.kanaForced = false
	c.recompute()
	return true
}

// ForceKana 把目前的英數段明確改成かな（P2-52）：即使整段剛好是英文詞
// （kanji、sake），只要按鍵能完整組成合法羅馬字就組成かな，並在這一小段
// 結束前維持かな。打不成合法羅馬字（kubectl、hello）時不動，回傳 false。
func (c *Composer) ForceKana() bool {
	if c == nil || c.run == "" || hasASCIIUpper(c.run) {
		return false
	}
	if _, valid := c.replay(c.run); !valid {
		return false
	}
	c.forced = false
	c.kanaForced = true
	c.recompute()
	return !c.literal
}

// LearnCurrent 把目前英數段學進英文詞彙。只有照字面的英數段會被學習。
func (c *Composer) LearnCurrent() bool {
	if c == nil || c.run == "" || !c.literal {
		return false
	}
	return c.english.Learn(c.run)
}

// Candidates 回傳目前英數段的候選：原樣輸入、かな解釋、英文補完與字寬。
// limit 為 0 時使用 Config.MaxCandidates。
func (c *Composer) Candidates(limit int) []lang.Candidate {
	if c == nil || c.run == "" {
		return nil
	}
	if limit <= 0 {
		limit = c.cfg.MaxCandidates
	}
	limit = min(limit, MaxSuggestions)
	raw := c.run
	var out []lang.Candidate
	seen := map[string]bool{}
	add := func(surface, source, annotation string) {
		if surface == "" || seen[surface] || len(out) >= limit {
			return
		}
		seen[surface] = true
		out = append(out, lang.Candidate{
			Surface:    surface,
			Reading:    runeUnits(surface),
			Score:      float64(limit - len(out)),
			Source:     source,
			Annotation: annotation,
		})
	}
	add(raw, SourceLiteral, "")
	if !c.literal {
		kana := sequenceText(c.runUnits) + c.pending
		if kana != "" && kana != raw {
			add(kana, SourceKana, "")
		}
	}
	if asciiLetters(raw) {
		for _, cand := range c.english.Candidates(raw, limit-len(out)) {
			add(cand.Surface, cand.Source, cand.Annotation)
		}
	}
	if hasLatinAlnum(raw) && Classify(raw, c.english) != KindURL && Classify(raw, c.english) != KindEmail {
		if variant, ok := WidthVariant(raw); ok {
			add(variant, SourceLiteral, AnnotationWidth)
		}
	}
	return out
}

func (c *Composer) startRun() {
	if c.run == "" {
		c.flushJapanesePending()
		c.forced = false
		c.kanaForced = false
	}
}

// appendRunKey 追加一個英數字。若目前的かな段加上這個鍵會變成英數段，先找出
// 最長的かな前綴切開，讓「きょうはAPI」「きょうはhttps://…」可以一次打完，
// 不必先変換或送出日文段。
func (c *Composer) appendRunKey(key string) {
	if c.run != "" && !c.literal && !c.kanaForced {
		previous := c.run
		combined := previous + key
		if literal, _ := c.literalFor(combined); literal && c.applySplit(combined, previous) {
			return
		}
	}
	c.run += key
	c.recompute()
}

// applySplit 把 combined 切成かな前綴與英數後綴。成功時前綴已鎖進讀音，
// 目前英數段變成後綴。
func (c *Composer) applySplit(combined, previous string) bool {
	at := c.splitRun(combined, previous)
	if at <= 0 {
		return false
	}
	prefix, suffix := combined[:at], combined[at:]
	if !hasLatinAlnum(suffix) {
		return false
	}
	c.run = prefix
	c.recompute()
	c.lockRun()
	c.startRun()
	c.run = suffix
	c.recompute()
	return true
}

// splitRun 回傳 combined 裡「かな前綴 + 英數後綴」的切點；沒有時回傳 0。
//
// 前綴至少要有三個かな單位、不能停在未完成的羅馬字，這樣 hello、kube 這種
// 英文字才不會被拆成かな開頭。後綴則依「大寫、數字、已知英文詞、一般英文字」
// 評分；分數相同時取較長的かな前綴，因此 kyouhahttps 會在 https 前切開，
// kyouhafoo@example.com 則會停在已知的 foo，而不是後面的 o@。
func (c *Composer) splitRun(combined, previous string) int {
	runes := []rune(combined)
	maxAt := len([]rune(previous))
	if maxAt >= len(runes) {
		maxAt = len(runes) - 1
	}
	best, bestScore := 0, 0
	for at := maxAt; at >= 1; at-- {
		prefix, suffix := string(runes[:at]), string(runes[at:])
		shadow, valid := c.replay(prefix)
		if !valid || shadow.Pending() != "" || len(shadow.Units()) < 3 {
			continue
		}
		if c.decideLiteralText(prefix, true, false) {
			continue
		}
		if suffixLiteral, _ := c.literalFor(suffix); !suffixLiteral {
			continue
		}
		if !hasLatinAlnum(suffix) {
			continue
		}
		if score := c.splitScore(suffix); score > bestScore {
			best, bestScore = len(prefix), score
		}
	}
	return best
}

// splitScore 評估後綴有多像一個英數段的開頭。
func (c *Composer) splitScore(suffix string) int {
	if suffix == "" {
		return 0
	}
	first, _ := utf8.DecodeRuneInString(suffix)
	switch {
	case first >= 'A' && first <= 'Z' || first >= '0' && first <= '9' ||
		first >= 'Ａ' && first <= 'Ｚ' || first >= '０' && first <= '９' ||
		first == '@' || first == '#':
		return 4
	}
	leading := leadingAlnum(suffix)
	if leading == "" {
		return 0
	}
	if c.english.Has(leading) {
		return 3
	}
	first, _ = utf8.DecodeRuneInString(leading)
	if first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z' || IsFullWidthAlnum(first) {
		return 2
	}
	return 1
}

func leadingAlnum(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || IsFullWidthAlnum(r) {
			b.WriteRune(r)
			continue
		}
		break
	}
	return b.String()
}

func (c *Composer) recompute() {
	c.runUnits = nil
	c.pending = ""
	c.literal = false
	if c.run == "" {
		c.kanaValid = false
		return
	}
	shadow, valid := c.replay(c.run)
	c.kanaValid = valid
	c.literal = c.decideLiteral(valid)
	if c.literal {
		c.runUnits = runeUnits(c.run)
		return
	}
	c.runUnits = append(reading.Sequence(nil), shadow.Units()...)
	c.pending = shadow.Pending()
}

func (c *Composer) replay(raw string) (*input.Composer, bool) {
	shadow := input.NewComposer(c.mode)
	if c.mode != input.ModeRomaji {
		return shadow, false
	}
	for _, r := range raw {
		if r < 0x21 || r > 0x7e {
			return shadow, false
		}
		if _, ok := shadow.Insert(string(r)); !ok {
			return shadow, false
		}
	}
	return shadow, true
}

func (c *Composer) decideLiteral(kanaValid bool) bool {
	if c == nil {
		return false
	}
	// 明確要求かな時，只有打不成合法羅馬字的按鍵才照字面；大寫與識別字已在
	// ForceKana 之前擋掉，後續追加的按鍵也留在同一段かな。
	if c.kanaForced {
		return !kanaValid
	}
	return c.decideLiteralText(c.run, kanaValid, c.forced)
}

func (c *Composer) decideLiteralText(raw string, kanaValid, forced bool) bool {
	if raw == "" {
		return false
	}
	if forced || HasFullWidthAlnum(raw) || hasASCIIUpper(raw) {
		return true
	}
	switch Classify(raw, c.english) {
	case KindURL, KindEmail, KindIdentifier, KindFullWidthAlnum, KindNumber:
		return true
	case KindEnglish:
		if utf8.RuneCountInString(raw) >= c.cfg.MinEnglishLen && !c.cfg.PreferKana {
			return true
		}
	}
	if c.english.Learned(raw) {
		return true
	}
	// 打不成合法羅馬字（kubectl、github、user_name）時只能照字面。
	return !kanaValid
}

func (c *Composer) literalFor(raw string) (literal, kanaValid bool) {
	_, valid := c.replay(raw)
	return c.decideLiteralText(raw, valid, false), valid
}

func (c *Composer) lockRun() {
	if c.run == "" {
		return
	}
	raw, literal := c.run, c.literal
	c.clearRun()
	if literal {
		c.appendLiteral(raw)
		return
	}
	shadow, valid := c.replay(raw)
	if !valid {
		c.appendLiteral(raw)
		return
	}
	if _, ok := shadow.Flush(); !ok {
		// 未完成的羅馬字前綴接上日文邊界時無法再組かな，照字面留下。
		if pending := shadow.Pending(); pending != "" {
			c.appendLiteral(pending)
		}
	}
	c.appendUnits(shadow.Units(), false)
}

func (c *Composer) flushJapanesePending() {
	if c == nil || c.jp.Pending() == "" {
		return
	}
	if done, ok := c.jp.Flush(); ok {
		c.appendUnits(done, false)
		return
	}
	pending := c.jp.Pending()
	c.jp.Reset()
	c.appendLiteral(pending)
}

func (c *Composer) appendLiteral(text string) {
	for _, r := range text {
		c.base = append(c.base, reading.Unit(string(r)))
		c.kind = append(c.kind, true)
	}
}

func (c *Composer) appendUnits(units reading.Sequence, literal bool) {
	for _, u := range units {
		c.base = append(c.base, u)
		c.kind = append(c.kind, literal)
	}
}

func (c *Composer) clearRun() {
	c.run = ""
	c.runUnits = nil
	c.pending = ""
	c.literal = false
	c.kanaValid = false
	c.forced = false
	c.kanaForced = false
}

func (c *Composer) result(before reading.Sequence) lang.ComposeResult {
	return lang.ComposeResult{
		Completed: addedUnits(before, c.Units()),
		Pending:   c.Pending(),
	}
}

func addedUnits(before, after reading.Sequence) reading.Sequence {
	common := 0
	for common < len(before) && common < len(after) && before[common] == after[common] {
		common++
	}
	if common == len(after) {
		return nil
	}
	return append(reading.Sequence(nil), after[common:]...)
}

func runeUnits(text string) reading.Sequence {
	if text == "" {
		return nil
	}
	units := make(reading.Sequence, 0, len(text))
	for _, r := range text {
		units = append(units, reading.Unit(string(r)))
	}
	return units
}

func sequenceText(units reading.Sequence) string {
	var b strings.Builder
	for _, u := range units {
		b.WriteString(string(u))
	}
	return b.String()
}

func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || IsFullWidthAlnum(r)
}

func isRunPunctuation(r rune) bool {
	return r <= unicode.MaxASCII && strings.ContainsRune("._~:/?#[]@!$&'()*+,;=%\\-|{}", r)
}

func hasASCIIUpper(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}

func hasLatinAlnum(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || IsFullWidthAlnum(r) {
			return true
		}
	}
	return false
}
