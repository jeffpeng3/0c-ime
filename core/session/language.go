package session

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/proto"
	rd "github.com/0c-biggo/0c-ime/core/reading"
)

// LanguageEvents 讓宿主把學習與補完事件送到語言專屬的儲存層（T20）。
//
// 事件一定帶著語言 ID；注音仍由既有的 personal.Model 處理，這裡只是額外通知。
// 宿主沒有設定時什麼都不做，注音行為不變。
type LanguageEvents struct {
	Learn    func(id lang.ID, text string)
	Complete func(id lang.ID, text string)
}

// languageRuntime 是某一個非注音語言在 session 內的完整狀態。
//
// composer 與 converter 依 profile 建立，切換語言時保留；因此切回同一語言可以
// 接續未完成的組字。切換語言不會重啟 session。
type languageRuntime struct {
	profile   lang.Profile
	method    lang.InputMethod
	policy    lang.KeyPolicy
	composer  lang.Composer
	converter lang.Converter

	converting bool
	clauses    []lang.Clause
	clause     int // 目前的文節索引（0 起算）

	candidates   []lang.Candidate
	candidate    int // candidates 的索引
	candidateFor int // 候選是為哪一個文節列出（-1 表示沒有）

	// surface 是直接かな輸入時使用者用 F6–F10 指定的顯示字串；下一次輸入或
	// 変換時捨棄。組字讀音仍由 composer 保存，取消不會丟掉讀音。
	surface string
}

func (r *languageRuntime) reset() {
	if r.composer != nil {
		r.composer.Reset()
	}
	r.endConversion()
	r.surface = ""
}

func (r *languageRuntime) composing() bool {
	if r == nil {
		return false
	}
	if r.converting || r.surface != "" {
		return true
	}
	if r.composer == nil {
		return false
	}
	return len(r.composer.Units()) > 0 || r.composer.Pending() != ""
}

func (r *languageRuntime) endConversion() {
	r.converting = false
	r.clauses = nil
	r.clause = 0
	r.candidates = nil
	r.candidate = 0
	r.candidateFor = -1
}

// AddLanguageProfile 註冊一個語言 profile。之後可以用 SetLanguage 切換。
func (s *Session) AddLanguageProfile(p lang.Profile) bool {
	if p == nil || p.ID() == "" {
		return false
	}
	if s.languages == nil {
		s.languages = map[lang.ID]lang.Profile{}
	}
	s.languages[p.ID()] = p
	return true
}

// SetLanguage 切換目前語言並回傳是否切換成功。未知語言回傳 false。
//
// 切換只改變 active language；每個語言未完成的組字、候選與変換狀態都保留，
// 切回時原樣接續。這也是「語言切換不重啟 session」的實作方式。
func (s *Session) SetLanguage(id lang.ID) bool {
	if id == "" {
		return false
	}
	if id == lang.Zh {
		s.languageID = lang.Zh
		return true
	}
	if _, ok := s.profile(id); !ok {
		return false
	}
	if _, ok := s.runtime(id); !ok {
		return false
	}
	s.languageID = id
	return true
}

// Language 回傳目前的語言。零值 Session 視為注音。
func (s *Session) Language() lang.ID {
	if s == nil || s.languageID == "" {
		return lang.Zh
	}
	return s.languageID
}

// LanguageProfile 回傳目前語言的 profile；注音在沒有註冊時使用內建 adapter。
func (s *Session) LanguageProfile() lang.Profile {
	p, _ := s.profile(s.Language())
	return p
}

// InputMethod 回傳目前語言使用的輸入方式。
func (s *Session) InputMethod() lang.InputMethod {
	if s.Language() == lang.Zh {
		return lang.InputMethod{ID: "zhuyin", Name: "注音"}
	}
	if r, ok := s.activeRuntime(); ok {
		return r.method
	}
	if p, ok := s.profile(s.Language()); ok {
		if ms := p.InputMethods(); len(ms) > 0 {
			return ms[0]
		}
	}
	return lang.InputMethod{}
}

// InputMethods 回傳目前語言可選的輸入方式。
func (s *Session) InputMethods() []lang.InputMethod {
	if p, ok := s.profile(s.Language()); ok {
		return p.InputMethods()
	}
	return nil
}

// SetInputMethod 切換目前語言的輸入方式。有未完成的組字或変換時拒絕，
// 以免清掉使用者打到一半的內容；沒有改變時回傳 true。
func (s *Session) SetInputMethod(id string) bool {
	if s.Language() == lang.Zh {
		return id == "" || id == "zhuyin"
	}
	r, ok := s.activeRuntime()
	if !ok {
		return false
	}
	if r.method.ID == id {
		return true
	}
	if r.composing() {
		return false
	}
	for _, m := range s.InputMethods() {
		if m.ID != id {
			continue
		}
		r.method = m
		r.composer = r.profile.NewComposer(m)
		r.endConversion()
		return true
	}
	return false
}

func (s *Session) profile(id lang.ID) (lang.Profile, bool) {
	if s == nil {
		return nil, false
	}
	if p, ok := s.languages[id]; ok && p != nil {
		return p, true
	}
	if id == lang.Zh {
		return zhProfile{}, true
	}
	return nil, false
}

// runtime 取得或建立某個語言的狀態。同一個語言只建立一次 composer/converter。
func (s *Session) runtime(id lang.ID) (*languageRuntime, bool) {
	if id == lang.Zh {
		return nil, false
	}
	if s.runtimes == nil {
		s.runtimes = map[lang.ID]*languageRuntime{}
	}
	if r, ok := s.runtimes[id]; ok && r != nil {
		return r, true
	}
	p, ok := s.profile(id)
	if !ok {
		return nil, false
	}
	methods := p.InputMethods()
	var method lang.InputMethod
	if len(methods) > 0 {
		method = methods[0]
	}
	r := &languageRuntime{
		profile:      p,
		method:       method,
		policy:       p.KeyPolicy(),
		composer:     p.NewComposer(method),
		converter:    p.NewConverter(),
		candidateFor: -1,
	}
	if r.policy == nil {
		r.policy = noneKeyPolicy{}
	}
	s.runtimes[id] = r
	return r, true
}

func (s *Session) activeRuntime() (*languageRuntime, bool) {
	if s.Language() == lang.Zh {
		return nil, false
	}
	return s.runtime(s.Language())
}

func (s *Session) japaneseActive() bool { return s.Language() != lang.Zh }

func (s *Session) languageComposing() bool {
	r, ok := s.activeRuntime()
	return ok && r.composing()
}

func (s *Session) languageKeyState(r *languageRuntime) lang.KeyState {
	st := lang.KeyState{
		Composing:   r.composing(),
		Converting:  r.converting,
		Candidates:  len(r.candidates) > 0,
		ClauseIndex: r.clause,
		ClauseCount: len(r.clauses),
	}
	return st
}

// languageKey 把按鍵交給目前語言的 KeyPolicy。回傳 ok=false 表示目前語言不是
// 非注音語言（或正在英文模式），呼叫端照既有的注音／英文流程處理。
func (s *Session) languageKey(k string) (handled bool, commit string, ok bool) {
	r, active := s.activeRuntime()
	if !active || s.english {
		return false, "", false
	}
	if k == proto.KeyBackspace {
		return s.languageBackspace(), "", true
	}
	action, accepted := r.policy.Handle(k, s.languageKeyState(r))
	if !accepted {
		if k == " " {
			return true, " ", true
		}
		return false, "", true
	}
	switch action {
	case lang.ActionInsert:
		return s.languageInsert(k), "", true
	case lang.ActionConvert:
		if r.converting {
			s.languageNextCandidate()
		} else {
			s.languageConvert()
		}
		return true, "", true
	case lang.ActionCommit:
		return true, s.languageCommit(), true
	case lang.ActionNextClause:
		s.languageMoveClause(1)
		return true, "", true
	case lang.ActionPrevClause:
		s.languageMoveClause(-1)
		return true, "", true
	case lang.ActionCancel:
		if r.converting {
			s.dropLanguageSelections()
			r.endConversion()
		} else {
			r.reset()
		}
		return true, "", true
	case lang.ActionSelect:
		s.languageSelectKey(k)
		return true, "", true
	case lang.ActionComplete:
		return s.languageAccept(), "", true
	case lang.ActionToHiragana:
		s.languageTransform(lang.ActionToHiragana)
		return true, "", true
	case lang.ActionToKatakana:
		s.languageTransform(lang.ActionToKatakana)
		return true, "", true
	case lang.ActionToHalfWidthKana:
		s.languageTransform(lang.ActionToHalfWidthKana)
		return true, "", true
	case lang.ActionToFullWidthLatin:
		s.languageTransform(lang.ActionToFullWidthLatin)
		return true, "", true
	case lang.ActionToHalfWidthLatin:
		s.languageTransform(lang.ActionToHalfWidthLatin)
		return true, "", true
	}
	return accepted, "", true
}

func (s *Session) languageInsert(k string) bool {
	r, ok := s.activeRuntime()
	if !ok || k == "" {
		return false
	}
	if r.converting {
		s.dropLanguageSelections()
		r.endConversion() // 打字取消変換，かな原樣保留
	}
	r.surface = ""
	if r.composer == nil {
		return false
	}
	_, accepted := r.composer.Insert(k)
	return accepted
}

func (s *Session) languageBackspace() bool {
	r, ok := s.activeRuntime()
	if !ok {
		return false
	}
	if r.converting {
		s.dropLanguageSelections()
		r.endConversion()
		return true
	}
	if r.surface != "" {
		r.surface = ""
		return true
	}
	if r.composer == nil {
		return false
	}
	return r.composer.Backspace()
}

func (s *Session) languageConvert() {
	r, ok := s.activeRuntime()
	if !ok || r.composer == nil || r.converter == nil {
		return
	}
	if r.composer.Pending() != "" {
		if f, ok := r.composer.(interface {
			Flush() (rd.Sequence, bool)
		}); ok {
			f.Flush()
		}
	}
	units := r.composer.Units()
	if len(units) == 0 {
		return
	}
	res, err := r.converter.Convert(units, s.languageContext())
	if err != nil || len(res.Clauses) == 0 {
		return
	}
	r.clauses = cloneClauses(res.Clauses)
	r.clause = 0
	r.converting = true
	r.surface = ""
	r.candidates = nil
	r.candidate = 0
	r.candidateFor = -1
}

func (s *Session) languageContext() lang.Context {
	left := s.pendingLeft
	if left == "" {
		left = s.documentLeft
	}
	return lang.Context{Left: left}
}

func (s *Session) languageMoveClause(delta int) {
	r, ok := s.activeRuntime()
	if !ok || !r.converting || len(r.clauses) == 0 {
		return
	}
	next := r.clause + delta
	if next < 0 || next >= len(r.clauses) {
		return
	}
	r.clause = next
	r.candidates = nil
	r.candidate = 0
	r.candidateFor = -1
}

// languageNextCandidate 在目前的変換中前進一個候選；第一次呼叫時列出候選。
func (s *Session) languageNextCandidate() {
	r, ok := s.activeRuntime()
	if !ok || !r.converting {
		return
	}
	if len(r.candidates) == 0 {
		s.languageLoadCandidates()
		return
	}
	r.candidate = (r.candidate + 1) % len(r.candidates)
}

func (s *Session) languageLoadCandidates() {
	r, ok := s.activeRuntime()
	if !ok || !r.converting || r.converter == nil || r.clause >= len(r.clauses) {
		return
	}
	start, length := s.languageClauseSpan(r)
	if length <= 0 {
		return
	}
	units := r.composer.Units()
	cands, err := r.converter.Candidates(units, lang.Span{Start: start, Length: length}, s.languageContext())
	if err != nil {
		return
	}
	r.candidates = cands
	r.candidate = 0
	r.candidateFor = r.clause
}

// languageSelectKey 處理変換中的選字鍵：1–9 是前九項，0 是第十項。
func (s *Session) languageSelectKey(k string) {
	r, ok := s.activeRuntime()
	if !ok || !r.converting {
		return
	}
	runes := []rune(k)
	if len(runes) != 1 || !unicode.IsDigit(runes[0]) {
		return
	}
	n := int(runes[0] - '1')
	if runes[0] == '0' {
		n = 9
	}
	if len(r.candidates) == 0 {
		s.languageLoadCandidates()
	}
	n += (r.candidate / perPage) * perPage
	s.languageSelectIndex(n, true)
}

// languageSelectIndex 選第 n 個候選。advance 為 true 時移到下一個文節，
// 像一般 Japanese IME 一樣讓人連續選每個文節。
func (s *Session) languageSelectIndex(n int, advance bool) bool {
	r, ok := s.activeRuntime()
	if !ok || !r.converting || n < 0 || n >= len(r.candidates) {
		return false
	}
	cand := r.candidates[n]
	s.languageApplyCandidate(r, cand)
	r.candidate = n
	r.candidateFor = r.clause
	previous := s.lastLanguage
	if r.clause > 0 {
		previous = lang.ID(r.profile.ID())
	}
	s.noteLanguageSelection(cand.Surface, cand.Reading, lang.ID(r.profile.ID()), previous)
	if advance && r.clause+1 < len(r.clauses) {
		r.clause++
		r.candidates = nil
		r.candidate = 0
		r.candidateFor = -1
	}
	s.emitComplete(lang.ID(r.profile.ID()), cand.Surface)
	return true
}

func (s *Session) languageApplyCandidate(r *languageRuntime, cand lang.Candidate) {
	if r.clause < 0 || r.clause >= len(r.clauses) {
		return
	}
	if len(cand.Clauses) > 0 {
		next := make([]lang.Clause, 0, len(r.clauses)-1+len(cand.Clauses))
		next = append(next, r.clauses[:r.clause]...)
		next = append(next, cloneClauses(cand.Clauses)...)
		next = append(next, r.clauses[r.clause+1:]...)
		r.clauses = next
		return
	}
	r.clauses[r.clause].Surface = cand.Surface
}

// languageAccept 接受目前的候選（Tab／Accept）。回傳是否有東西被接受。
func (s *Session) languageAccept() bool {
	r, ok := s.activeRuntime()
	if !ok {
		return false
	}
	if !r.converting {
		return false
	}
	if len(r.candidates) == 0 {
		s.languageLoadCandidates()
	}
	if len(r.candidates) == 0 {
		return false
	}
	return s.languageSelectIndex(r.candidate, true)
}

// languageCommit 送出目前語言的組字並清空。
func (s *Session) languageCommit() string {
	r, ok := s.activeRuntime()
	if !ok {
		return ""
	}
	text := s.languageText(r)
	if text == "" {
		return ""
	}
	if !s.paused && !s.private {
		s.emitLearn(lang.ID(r.profile.ID()), text)
	}
	s.flushLanguageSelections(!s.paused && !s.private && !s.learningCleared)
	s.noteCommittedLanguage()
	r.reset()
	return text
}

func (s *Session) languageText(r *languageRuntime) string {
	if r.converting {
		var b strings.Builder
		for _, c := range r.clauses {
			b.WriteString(c.Surface)
		}
		return b.String()
	}
	if r.surface != "" {
		return r.surface
	}
	if r.composer == nil {
		return ""
	}
	var b strings.Builder
	for _, u := range r.composer.Units() {
		b.WriteString(string(u))
	}
	b.WriteString(r.composer.Pending())
	return b.String()
}

func (s *Session) languageTransform(action lang.Action) {
	r, ok := s.activeRuntime()
	if !ok {
		return
	}
	if r.converting && r.clause >= 0 && r.clause < len(r.clauses) {
		c := &r.clauses[r.clause]
		source := sequenceText(clauseReading(*c))
		if action == lang.ActionToFullWidthLatin || action == lang.ActionToHalfWidthLatin {
			source = c.Surface
		}
		c.Surface = transformText(source, action)
		r.candidates = nil
		r.candidate = 0
		r.candidateFor = -1
		return
	}
	if r.composer == nil {
		return
	}
	source := r.composer.Units()
	out := transformText(sequenceText(source)+r.composer.Pending(), action)
	if out == "" {
		return
	}
	if action == lang.ActionToHiragana {
		r.surface = "" // ひらがな就是組字讀音本身
		return
	}
	r.surface = out
}

func (s *Session) languageClauseSpan(r *languageRuntime) (int, int) {
	start := 0
	for i := 0; i < r.clause && i < len(r.clauses); i++ {
		start += len(clauseReading(r.clauses[i]))
	}
	if r.clause < 0 || r.clause >= len(r.clauses) {
		return start, 0
	}
	return start, len(clauseReading(r.clauses[r.clause]))
}

func sequenceText(seq rd.Sequence) string {
	var b strings.Builder
	for _, u := range seq {
		b.WriteString(string(u))
	}
	return b.String()
}

func clauseReading(c lang.Clause) rd.Sequence {
	if len(c.Reading) > 0 {
		return c.Reading
	}
	var out rd.Sequence
	for _, seg := range c.Segments {
		out = append(out, seg.Reading...)
	}
	return out
}

func cloneClauses(in []lang.Clause) []lang.Clause {
	out := make([]lang.Clause, len(in))
	for i, c := range in {
		out[i] = c
		out[i].Reading = append(rd.Sequence(nil), c.Reading...)
		out[i].Segments = append([]lang.CandidateSegment(nil), c.Segments...)
		for j := range out[i].Segments {
			out[i].Segments[j].Reading = append(rd.Sequence(nil), c.Segments[j].Reading...)
		}
	}
	return out
}

// languageSnapshot 填寫非注音語言的組字區與候選。
func (s *Session) languageSnapshot(snap proto.Snapshot, r *languageRuntime) proto.Snapshot {
	var text strings.Builder
	var cursor int
	if r.converting && len(r.clauses) > 0 {
		for i, c := range r.clauses {
			style := proto.StyleClause
			if i == r.clause {
				style = proto.StyleClauseActive
				cursor = len([]rune(text.String())) + len([]rune(c.Surface))
			}
			snap.Preedit = append(snap.Preedit, proto.Segment{Text: c.Surface, Style: style})
			text.WriteString(c.Surface)
		}
	} else {
		if r.surface != "" {
			snap.Preedit = []proto.Segment{{Text: r.surface, Style: proto.StyleNormal}}
			text.WriteString(r.surface)
		} else if r.composer != nil {
			for _, seg := range r.composer.Segments() {
				style := proto.StyleNormal
				if seg.Pending {
					style = proto.StylePending
				}
				snap.Preedit = append(snap.Preedit, proto.Segment{Text: seg.Text, Style: style})
				text.WriteString(seg.Text)
			}
		}
		cursor = len([]rune(text.String()))
	}
	runes := []rune(text.String())
	cursor = max(0, min(cursor, len(runes)))
	snap.Cursor.Rune = cursor
	snap.Cursor.UTF16 = len(utf16Encode(runes[:cursor]))
	snap.Converting = r.converting
	if r.converting {
		snap.ClauseIndex = r.clause // 0 起算，Converting 區分沒有変換
		snap.ClauseCount = len(r.clauses)
	}
	if r.converting && len(r.candidates) > 0 {
		snap.Candidates = s.languageCandidateList(r)
	}
	return snap
}

func (s *Session) languagePages(r *languageRuntime) int {
	return max(1, (len(r.candidates)+perPage-1)/perPage)
}

func (s *Session) languageCandidateList(r *languageRuntime) *proto.CandidateList {
	start, length := s.languageClauseSpan(r)
	page := r.candidate / perPage
	cl := &proto.CandidateList{
		Page:      page,
		Pages:     s.languagePages(r),
		Start:     start,
		Length:    length,
		Highlight: r.candidate % perPage,
		Title:     "変換 · Space 換候選 · Tab 換文節",
	}
	lo := page * perPage
	for id := lo; id < min(lo+perPage, len(r.candidates)); id++ {
		cl.Items = append(cl.Items, proto.CandidateItem{
			ID:   id,
			Text: r.candidates[id].Surface,
			Key:  string("1234567890"[id-lo]),
		})
	}
	return cl
}

func (s *Session) emitLearn(id lang.ID, text string) {
	if s.Events.Learn != nil {
		s.Events.Learn(id, text)
	}
}

func (s *Session) emitComplete(id lang.ID, text string) {
	if s.Events.Complete != nil {
		s.Events.Complete(id, text)
	}
}

func utf16Encode(runes []rune) []uint16 {
	out := make([]uint16, 0, len(runes))
	for _, r := range runes {
		out = append(out, utf16Units(r)...)
	}
	return out
}

func utf16Units(r rune) []uint16 {
	if r <= 0xffff {
		return []uint16{uint16(r)}
	}
	r -= 0x10000
	return []uint16{uint16(0xd800 + (r >> 10)), uint16(0xdc00 + (r & 0x3ff))}
}

func transformText(text string, action lang.Action) string {
	switch action {
	case lang.ActionToHiragana:
		return strings.Map(toHiragana, text)
	case lang.ActionToKatakana:
		return strings.Map(toKatakana, text)
	case lang.ActionToHalfWidthKana:
		var b strings.Builder
		for _, r := range strings.Map(toKatakana, text) {
			if h, ok := halfwidthKana[r]; ok {
				b.WriteString(h)
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	case lang.ActionToFullWidthLatin:
		return strings.Map(toFullWidthLatin, text)
	case lang.ActionToHalfWidthLatin:
		return strings.Map(toHalfWidthLatin, text)
	}
	return text
}

func toHiragana(r rune) rune {
	switch {
	case r >= 'ァ' && r <= 'ヶ':
		return r - 0x60
	case r == 'ヽ':
		return 'ゝ'
	case r == 'ヾ':
		return 'ゞ'
	}
	return r
}

func toKatakana(r rune) rune {
	switch {
	case r >= 'ぁ' && r <= 'ゖ':
		return r + 0x60
	case r == 'ゝ':
		return 'ヽ'
	case r == 'ゞ':
		return 'ヾ'
	}
	return r
}

func toFullWidthLatin(r rune) rune {
	switch {
	case r == ' ':
		return '\u3000'
	case r >= '!' && r <= '~':
		return r + 0xfee0
	}
	return r
}

func toHalfWidthLatin(r rune) rune {
	switch {
	case r == '\u3000':
		return ' '
	case r >= '\uff01' && r <= '\uff5e':
		return r - 0xfee0
	}
	return r
}

var halfwidthKana = map[rune]string{
	'ァ': "ｧ", 'ア': "ｱ", 'ィ': "ｨ", 'イ': "ｲ", 'ゥ': "ｩ", 'ウ': "ｳ", 'ェ': "ｪ", 'エ': "ｴ",
	'ォ': "ｫ", 'オ': "ｵ", 'カ': "ｶ", 'ガ': "ｶﾞ", 'キ': "ｷ", 'ギ': "ｷﾞ", 'ク': "ｸ", 'グ': "ｸﾞ",
	'ケ': "ｹ", 'ゲ': "ｹﾞ", 'コ': "ｺ", 'ゴ': "ｺﾞ", 'サ': "ｻ", 'ザ': "ｻﾞ", 'シ': "ｼ", 'ジ': "ｼﾞ",
	'ス': "ｽ", 'ズ': "ｽﾞ", 'セ': "ｾ", 'ゼ': "ｾﾞ", 'ソ': "ｿ", 'ゾ': "ｿﾞ", 'タ': "ﾀ", 'ダ': "ﾀﾞ",
	'チ': "ﾁ", 'ヂ': "ﾁﾞ", 'ッ': "ｯ", 'ツ': "ﾂ", 'ヅ': "ﾂﾞ", 'テ': "ﾃ", 'デ': "ﾃﾞ", 'ト': "ﾄ",
	'ド': "ﾄﾞ", 'ナ': "ﾅ", 'ニ': "ﾆ", 'ヌ': "ﾇ", 'ネ': "ﾈ", 'ノ': "ﾉ", 'ハ': "ﾊ", 'バ': "ﾊﾞ",
	'パ': "ﾊﾟ", 'ヒ': "ﾋ", 'ビ': "ﾋﾞ", 'ピ': "ﾋﾟ", 'フ': "ﾌ", 'ブ': "ﾌﾞ", 'プ': "ﾌﾟ", 'ヘ': "ﾍ",
	'ベ': "ﾍﾞ", 'ペ': "ﾍﾟ", 'ホ': "ﾎ", 'ボ': "ﾎﾞ", 'ポ': "ﾎﾟ", 'マ': "ﾏ", 'ミ': "ﾐ", 'ム': "ﾑ",
	'メ': "ﾒ", 'モ': "ﾓ", 'ャ': "ｬ", 'ヤ': "ﾔ", 'ュ': "ｭ", 'ユ': "ﾕ", 'ョ': "ｮ", 'ヨ': "ﾖ",
	'ラ': "ﾗ", 'リ': "ﾘ", 'ル': "ﾙ", 'レ': "ﾚ", 'ロ': "ﾛ", 'ヮ': "ﾜ", 'ワ': "ﾜ", 'ヰ': "ｦ",
	'ヱ': "ｴ", 'ヲ': "ｦ", 'ン': "ﾝ", 'ヴ': "ｳﾞ", 'ヵ': "ｶ", 'ヶ': "ｹ", 'ー': "ｰ",
	'、': "､", '。': "｡", '「': "｢", '」': "｣", '・': "･",
}

// noneKeyPolicy 是 profile 沒有提供 KeyPolicy 時的保底：只把可見字元交給
// composer，其他按鍵交還外殼。
type noneKeyPolicy struct{}

func (noneKeyPolicy) Handle(key string, _ lang.KeyState) (lang.Action, bool) {
	if _, size := utf8.DecodeRuneInString(key); size == len(key) && size > 0 && unicode.IsPrint([]rune(key)[0]) {
		return lang.ActionInsert, true
	}
	return lang.ActionNone, false
}
