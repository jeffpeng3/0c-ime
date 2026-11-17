package personal

import (
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// 分語言與跨語言學習（P2-15）。
//
// 每次使用者選字都記在「這次選字所屬語言」的命名空間裡：中文的選字只增加
// Languages["zh"]，日文的選字只增加 Languages["ja"]。跨語言偏好另存
// CrossLanguage，只記「前一個片段語言 → 這次的語言」與各語言被選過的總數；
// 它只提供候選排序的弱偏好，不會把一個語言的詞面或讀音搬到另一個語言。
//
// 推斷不要求使用者先指定語言：注音與かな是強訊號；漢字沿用明確的候選語言，
// 沒有標籤時沿用前一個片段的語言；拉丁字母在沒有標籤時視為英文。全部都沒有
// 線索時才退回已學到的主要語言，再退回中文，讓注音既有行為不變。

const (
	// maxLanguageSurfaces 是每個語言保留的詞面數；超過時淘汰次數最少、最久未用的。
	maxLanguageSurfaces = 1024
	maxLanguageReadings = 1024
	maxLanguageContexts = 1024
	// maxLanguageTransitions 是跨語言轉移保留的筆數。
	maxLanguageTransitions = 128
	// maxLanguageUndo 是分語言學習保留的反悔步數；只在記憶體裡。
	maxLanguageUndo = 32
)

// LanguageEvidence 是分語言學習的一筆有界計數。
type LanguageEvidence struct {
	Count   int   `json:"Count,omitempty"`
	Updated int64 `json:"Updated,omitempty"`
}

func (e LanguageEvidence) add(at int64) LanguageEvidence {
	e.Count++
	if at > e.Updated {
		e.Updated = at
	}
	return e
}

// LanguageNamespace 是單一語言的學習資料。別語言的學習不會寫進來。
type LanguageNamespace struct {
	// Selections 是這個語言被選過的次數。Surfaces 有上限，清理後 Selections 仍保留
	// 歷史總數，兩者不一定相等。
	Selections int `json:"Selections,omitempty"`
	// Surfaces 是詞面 → 選取次數。
	Surfaces map[string]LanguageEvidence `json:"Surfaces,omitempty"`
	// Readings 是「讀音鍵\t詞面」→ 次數，保留同詞面不同讀音的區別。
	Readings map[string]LanguageEvidence `json:"Readings,omitempty"`
	// Contexts 是「左文\t詞面」→ 次數；換了前文就不會套用同一個弱偏好。
	Contexts map[string]LanguageEvidence `json:"Contexts,omitempty"`
	// Updated 是這個命名空間最後一次學習的時間。
	Updated int64 `json:"Updated,omitempty"`
}

// CrossLanguage 是跨語言的弱偏好。它不含任何詞面，只含語言之間的計數。
type CrossLanguage struct {
	// Primary 是語言 → 被選過的次數；用來判斷使用者主要寫哪個語言。
	Primary map[string]int `json:"Primary,omitempty"`
	// Transitions 是「前一個片段語言\t這次語言」→ 次數。
	Transitions map[string]int `json:"Transitions,omitempty"`
}

// LanguageSelection 是一次選字觀察。
//
// Language 是候選或片段已知的語言標籤；Previous 是左側片段的語言標籤。
// 兩者都可以是空字串，LearnLanguage 會依詞面推斷。Reading 可為空。
type LanguageSelection struct {
	Surface  string
	Reading  reading.Sequence
	Left     string
	Language lang.ID
	Previous lang.ID
	At       int64
}

// LanguageCandidate 是候選語言排序的一項。Surface 只用來查同語言的詞面計數。
type LanguageCandidate struct {
	Surface  string
	Language lang.ID
}

// LanguageUndo 是上一次分語言學習的反悔紀錄。只在記憶體裡，不寫進個人檔。
type languageUndo struct {
	language string

	surface       string
	surfaceBefore LanguageEvidence
	surfaceHad    bool

	reading       string
	readingBefore LanguageEvidence
	readingHad    bool

	context       string
	contextBefore LanguageEvidence
	contextHad    bool

	selectionsBefore int
	updatedBefore    int64

	primaryBefore int
	primaryHad    bool

	transition       string
	transitionBefore int
	transitionHad    bool
}

func (n *LanguageNamespace) clone() *LanguageNamespace {
	if n == nil {
		return nil
	}
	c := *n
	c.Surfaces = cloneLanguageEvidence(n.Surfaces)
	c.Readings = cloneLanguageEvidence(n.Readings)
	c.Contexts = cloneLanguageEvidence(n.Contexts)
	return &c
}

func cloneLanguageEvidence(in map[string]LanguageEvidence) map[string]LanguageEvidence {
	if in == nil {
		return nil
	}
	out := make(map[string]LanguageEvidence, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (c CrossLanguage) clone() CrossLanguage {
	out := CrossLanguage{}
	if c.Primary != nil {
		out.Primary = make(map[string]int, len(c.Primary))
		for k, v := range c.Primary {
			out.Primary[k] = v
		}
	}
	if c.Transitions != nil {
		out.Transitions = make(map[string]int, len(c.Transitions))
		for k, v := range c.Transitions {
			out.Transitions[k] = v
		}
	}
	return out
}

// cleanLanguages 讓舊檔、手改過的檔與本版結構一致。空語言 ID 與 nil 命名空間丟掉，
// 各類資料套用與學習時相同的上限。
func (s *Store) cleanLanguages() {
	if s.Languages == nil {
		s.Languages = map[string]*LanguageNamespace{}
	}
	for id, ns := range s.Languages {
		if id == "" || ns == nil {
			delete(s.Languages, id)
			continue
		}
		if ns.Surfaces == nil {
			ns.Surfaces = map[string]LanguageEvidence{}
		}
		if ns.Readings == nil {
			ns.Readings = map[string]LanguageEvidence{}
		}
		if ns.Contexts == nil {
			ns.Contexts = map[string]LanguageEvidence{}
		}
		trimLanguageEvidence(ns.Surfaces, maxLanguageSurfaces)
		trimLanguageEvidence(ns.Readings, maxLanguageReadings)
		trimLanguageEvidence(ns.Contexts, maxLanguageContexts)
		ns.Selections = 0
		for _, e := range ns.Surfaces {
			if e.Count > 0 {
				ns.Selections += e.Count
			}
		}
	}
	if s.CrossLanguage.Primary == nil {
		s.CrossLanguage.Primary = map[string]int{}
	}
	if s.CrossLanguage.Transitions == nil {
		s.CrossLanguage.Transitions = map[string]int{}
	}
	trimLanguageCounts(s.CrossLanguage.Transitions, maxLanguageTransitions)
}

// trimLanguageEvidence 依次數、更新時間、鍵值排序，留下前 limit 筆。
func trimLanguageEvidence(m map[string]LanguageEvidence, limit int) {
	if limit <= 0 || len(m) <= limit {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		ea, eb := m[a], m[b]
		if ea.Count != eb.Count {
			return ea.Count - eb.Count
		}
		if ea.Updated != eb.Updated {
			if ea.Updated < eb.Updated {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	for _, k := range keys[:len(keys)-limit] {
		delete(m, k)
	}
}

func trimLanguageCounts(m map[string]int, limit int) {
	if limit <= 0 || len(m) <= limit {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if m[a] != m[b] {
			return m[a] - m[b]
		}
		return strings.Compare(a, b)
	})
	for _, k := range keys[:len(keys)-limit] {
		delete(m, k)
	}
}

// GuessLanguage 只依詞面推斷語言，不讀取主要語言或既有學習。
//
// 注音與かな是強訊號；拉丁字母視為英文；漢字本身無法區分中文與日文，
// 只有明確的前文語言時才沿用，否則回傳 lang.Und。需要候選標籤或主要語言
// 的呼叫端請用 Model.LearnLanguage 或 Model.InferLanguage。
func GuessLanguage(surface string, previous lang.ID) lang.ID {
	if id, strong := strongScriptLanguage(surface); strong {
		return id
	}
	if latinText(surface) {
		return lang.En
	}
	if previous != lang.Und {
		return previous
	}
	return lang.Und
}

// InferLanguage 是 GuessLanguage 加上候選標籤與已學主要語言的版本。
// 明確的注音／かな標籤仍優先於 hint，避免把かな選字記到中文命名空間。
func (m *Model) InferLanguage(surface string, hint, previous lang.ID) lang.ID {
	if id, strong := strongScriptLanguage(surface); strong {
		return id
	}
	if hint != lang.Und {
		return hint
	}
	if latinText(surface) {
		return lang.En
	}
	if previous != lang.Und {
		return previous
	}
	if m != nil {
		if id := m.PrimaryLanguage(); id != lang.Und {
			return id
		}
	}
	return lang.Zh
}

func strongScriptLanguage(text string) (lang.ID, bool) {
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Bopomofo):
			return lang.Zh, true
		case unicode.In(r, unicode.Hiragana, unicode.Katakana) || r == 'ー' || r == 'ｰ':
			return lang.Ja, true
		}
	}
	return lang.Und, false
}

func latinText(text string) bool {
	saw := false
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Bopomofo, r) ||
			unicode.In(r, unicode.Hiragana, unicode.Katakana) {
			return false
		}
		if unicode.Is(unicode.Latin, r) {
			saw = true
		}
	}
	return saw
}

func (m *Model) languageNamespace(id lang.ID) *LanguageNamespace {
	s := m.Store
	if s.Languages == nil {
		s.Languages = map[string]*LanguageNamespace{}
	}
	key := string(id)
	ns := s.Languages[key]
	if ns == nil {
		ns = &LanguageNamespace{
			Surfaces: map[string]LanguageEvidence{},
			Readings: map[string]LanguageEvidence{},
			Contexts: map[string]LanguageEvidence{},
		}
		s.Languages[key] = ns
	}
	return ns
}

// LearnLanguage 記錄一次選字，回傳這次記到哪個語言。
//
// 語言推斷見套件註解。surface 為空時什麼都不做。學習會遞增 Gen，讓快取知道
// 偏好已改變；實際的注音解碼內容不受影響。
func (m *Model) LearnLanguage(sel LanguageSelection) lang.ID {
	if m == nil || m.Store == nil || sel.Surface == "" {
		return lang.Und
	}
	id := m.InferLanguage(sel.Surface, sel.Language, sel.Previous)
	if id == lang.Und {
		id = lang.Zh
	}
	at := sel.At
	if at == 0 {
		at = m.now()
	}

	ns := m.languageNamespace(id)
	readingKey := ""
	if len(sel.Reading) > 0 {
		readingKey = pair(reading.Key(sel.Reading), sel.Surface)
	}
	contextKey := ""
	if sel.Left != "" {
		contextKey = pair(sel.Left, sel.Surface)
	}
	transitionKey := ""
	if sel.Previous != lang.Und && sel.Previous != id {
		transitionKey = pair(string(sel.Previous), string(id))
	}

	u := languageUndo{
		language:         string(id),
		surface:          sel.Surface,
		surfaceBefore:    ns.Surfaces[sel.Surface],
		selectionsBefore: ns.Selections,
		updatedBefore:    ns.Updated,
		reading:          readingKey,
		context:          contextKey,
		transition:       transitionKey,
	}
	u.surfaceHad = languageEvidencePresent(ns.Surfaces, sel.Surface)
	if readingKey != "" {
		u.readingBefore, u.readingHad = ns.Readings[readingKey], languageEvidencePresent(ns.Readings, readingKey)
	}
	if contextKey != "" {
		u.contextBefore, u.contextHad = ns.Contexts[contextKey], languageEvidencePresent(ns.Contexts, contextKey)
	}
	if m.Store.CrossLanguage.Primary == nil {
		m.Store.CrossLanguage.Primary = map[string]int{}
	}
	u.primaryBefore, u.primaryHad = m.Store.CrossLanguage.Primary[string(id)], crossCountPresent(m.Store.CrossLanguage.Primary, string(id))
	if transitionKey != "" {
		if m.Store.CrossLanguage.Transitions == nil {
			m.Store.CrossLanguage.Transitions = map[string]int{}
		}
		u.transitionBefore, u.transitionHad = m.Store.CrossLanguage.Transitions[transitionKey], crossCountPresent(m.Store.CrossLanguage.Transitions, transitionKey)
	}
	m.languageJournal = append(m.languageJournal, u)
	if len(m.languageJournal) > maxLanguageUndo {
		m.languageJournal = m.languageJournal[len(m.languageJournal)-maxLanguageUndo:]
	}

	ns.Surfaces[sel.Surface] = ns.Surfaces[sel.Surface].add(at)
	ns.Selections++
	if at > ns.Updated {
		ns.Updated = at
	}
	if readingKey != "" {
		ns.Readings[readingKey] = ns.Readings[readingKey].add(at)
	}
	if contextKey != "" {
		ns.Contexts[contextKey] = ns.Contexts[contextKey].add(at)
	}
	m.Store.CrossLanguage.Primary[string(id)]++
	if transitionKey != "" {
		m.Store.CrossLanguage.Transitions[transitionKey]++
	}

	trimLanguageEvidence(ns.Surfaces, maxLanguageSurfaces)
	trimLanguageEvidence(ns.Readings, maxLanguageReadings)
	trimLanguageEvidence(ns.Contexts, maxLanguageContexts)
	trimLanguageCounts(m.Store.CrossLanguage.Transitions, maxLanguageTransitions)
	m.Gen++
	return id
}

func languageEvidencePresent[V any](m map[string]V, k string) bool {
	_, ok := m[k]
	return ok
}

func crossCountPresent(m map[string]int, k string) bool {
	_, ok := m[k]
	return ok
}

// UndoLanguage 復原最近一次分語言學習，回傳是否有東西可復原。
//
// 只復原最近一次觀察；比照 Undo，反悔紀錄不寫進個人檔。忘記或重置會清掉
// 反悔紀錄，避免把已要求刪除的內容放回去。
func (m *Model) UndoLanguage() bool {
	if m == nil || m.Store == nil || len(m.languageJournal) == 0 {
		return false
	}
	u := m.languageJournal[len(m.languageJournal)-1]
	m.languageJournal = m.languageJournal[:len(m.languageJournal)-1]
	if u.language == "" {
		return false
	}
	ns := m.Store.Languages[u.language]
	if ns == nil {
		ns = m.languageNamespace(lang.ID(u.language))
	}
	restoreLanguageEvidence(ns.Surfaces, u.surface, u.surfaceBefore, u.surfaceHad)
	if u.reading != "" {
		restoreLanguageEvidence(ns.Readings, u.reading, u.readingBefore, u.readingHad)
	}
	if u.context != "" {
		restoreLanguageEvidence(ns.Contexts, u.context, u.contextBefore, u.contextHad)
	}
	ns.Selections = u.selectionsBefore
	ns.Updated = u.updatedBefore
	restoreCrossCount(m.Store.CrossLanguage.Primary, u.language, u.primaryBefore, u.primaryHad)
	if u.transition != "" {
		restoreCrossCount(m.Store.CrossLanguage.Transitions, u.transition, u.transitionBefore, u.transitionHad)
	}
	if len(ns.Surfaces) == 0 && ns.Selections == 0 {
		delete(m.Store.Languages, u.language)
	}
	m.Gen++
	return true
}

func restoreLanguageEvidence(m map[string]LanguageEvidence, k string, before LanguageEvidence, had bool) {
	if had {
		m[k] = before
	} else {
		delete(m, k)
	}
}

func restoreCrossCount(m map[string]int, k string, before int, had bool) {
	if had {
		m[k] = before
	} else {
		delete(m, k)
	}
}

// ForgetLanguage 忘記某個語言裡的一個詞面，連同它的讀音與前文紀錄；
// 相關的跨語言總數一起調降，但其他詞面的學習不受影響。
func (m *Model) ForgetLanguage(id lang.ID, surface string) bool {
	if m == nil || m.Store == nil || id == lang.Und || surface == "" {
		return false
	}
	ns := m.Store.Languages[string(id)]
	if ns == nil {
		return false
	}
	e, ok := ns.Surfaces[surface]
	if !ok {
		return false
	}
	removed := max(0, e.Count)
	delete(ns.Surfaces, surface)
	for k := range ns.Readings {
		if strings.HasSuffix(k, "\t"+surface) {
			delete(ns.Readings, k)
		}
	}
	for k := range ns.Contexts {
		if strings.HasSuffix(k, "\t"+surface) {
			delete(ns.Contexts, k)
		}
	}
	ns.Selections = max(0, ns.Selections-removed)
	if primary := m.Store.CrossLanguage.Primary[string(id)]; primary > 0 {
		m.Store.CrossLanguage.Primary[string(id)] = max(0, primary-removed)
	}
	if len(ns.Surfaces) == 0 && ns.Selections == 0 {
		delete(m.Store.Languages, string(id))
	}
	m.languageJournal = nil
	m.Gen++
	return true
}

// ResetLanguage 清掉一個語言的全部學習，包括涉及它的跨語言轉移；
// 其他語言的命名空間保持原樣。回傳是否有資料被清掉。
func (m *Model) ResetLanguage(id lang.ID) bool {
	if m == nil || m.Store == nil || id == lang.Und {
		return false
	}
	key := string(id)
	found := false
	if _, ok := m.Store.Languages[key]; ok {
		delete(m.Store.Languages, key)
		found = true
	}
	if _, ok := m.Store.CrossLanguage.Primary[key]; ok {
		delete(m.Store.CrossLanguage.Primary, key)
		found = true
	}
	for k := range m.Store.CrossLanguage.Transitions {
		from, to, _ := strings.Cut(k, "\t")
		if from == key || to == key {
			delete(m.Store.CrossLanguage.Transitions, k)
			found = true
		}
	}
	if found {
		m.languageJournal = nil
		m.Gen++
	}
	return found
}

// ResetAllLanguages 清掉所有分語言學習與跨語言偏好，回傳清掉的語言數。
func (m *Model) ResetAllLanguages() int {
	if m == nil || m.Store == nil {
		return 0
	}
	n := len(m.Store.Languages)
	m.Store.Languages = map[string]*LanguageNamespace{}
	m.Store.CrossLanguage = CrossLanguage{Primary: map[string]int{}, Transitions: map[string]int{}}
	m.languageJournal = nil
	if n > 0 {
		m.Gen++
	}
	return n
}

// LanguageSurfaceCount 回傳某個語言裡 surface 被選過的次數。
func (m *Model) LanguageSurfaceCount(id lang.ID, surface string) int {
	if m == nil || m.Store == nil || surface == "" {
		return 0
	}
	ns := m.Store.Languages[string(id)]
	if ns == nil {
		return 0
	}
	return max(0, ns.Surfaces[surface].Count)
}

// LanguageSelections 回傳某個語言被選過的總次數。
func (m *Model) LanguageSelections(id lang.ID) int {
	if m == nil || m.Store == nil {
		return 0
	}
	ns := m.Store.Languages[string(id)]
	if ns == nil {
		return 0
	}
	return max(0, ns.Selections)
}

// PrimaryLanguage 回傳依選字次數推斷的主要語言；沒有學習時回傳 lang.Und。
// 同次數時依中文、日文、英文、其他語言 ID 的順序，結果可重現。
func (m *Model) PrimaryLanguage() lang.ID {
	if m == nil || m.Store == nil || len(m.Store.CrossLanguage.Primary) == 0 {
		return lang.Und
	}
	ids := make([]string, 0, len(m.Store.CrossLanguage.Primary))
	for id, n := range m.Store.CrossLanguage.Primary {
		if id != "" && n > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return lang.Und
	}
	slices.SortFunc(ids, func(a, b string) int {
		if ra, rb := languageOrder(lang.ID(a)), languageOrder(lang.ID(b)); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	best := ids[0]
	for _, id := range ids[1:] {
		if m.Store.CrossLanguage.Primary[id] > m.Store.CrossLanguage.Primary[best] {
			best = id
		}
	}
	return lang.ID(best)
}

func languageOrder(id lang.ID) int {
	switch id {
	case lang.Zh:
		return 0
	case lang.Ja:
		return 1
	case lang.En:
		return 2
	}
	return 3
}

// LanguagePreference 是語言 id 在 previous 之後的弱偏好，單位是自然對數。
//
// 0 表示沒有證據或正好是均勻先驗；正值表示比均勻先驗更可能，負值表示較不可能。
// 它是跨語言偏好加上各語言被選過的總數，不含任何詞面；候選排序另外用
// LanguageCandidateScore 加上同語言的詞面證據。
func (m *Model) LanguagePreference(id, previous lang.ID) float64 {
	if m == nil || m.Store == nil || id == lang.Und {
		return 0
	}
	s := m.Store
	n := float64(m.languageBaselineCount())
	total := 0
	for _, v := range s.CrossLanguage.Primary {
		if v > 0 {
			total += v
		}
	}
	pGlobal := (float64(max(0, s.CrossLanguage.Primary[string(id)])) + 1) / (float64(total) + n)
	p := pGlobal
	if previous != lang.Und {
		row := 0
		for k, v := range s.CrossLanguage.Transitions {
			from, _, _ := strings.Cut(k, "\t")
			if from == string(previous) && v > 0 {
				row += v
			}
		}
		if row > 0 {
			pPair := (float64(max(0, s.CrossLanguage.Transitions[pair(string(previous), string(id))])) + 1) / (float64(row) + n)
			p = 0.6*pGlobal + 0.4*pPair
		}
	}
	return math.Log(p * n)
}

func (m *Model) languageBaselineCount() int {
	seen := map[string]bool{
		string(lang.Zh): true,
		string(lang.Ja): true,
		string(lang.En): true,
	}
	if m != nil && m.Store != nil {
		for id := range m.Store.Languages {
			seen[id] = true
		}
		for id := range m.Store.CrossLanguage.Primary {
			seen[id] = true
		}
	}
	return max(1, len(seen))
}

// LanguageCandidateScore 是候選的弱語言偏好：跨語言偏好加上同語言的詞面計數。
// 別的語言的詞面不會加分；計數取對數且有界遞增。
func (m *Model) LanguageCandidateScore(c LanguageCandidate, previous lang.ID) float64 {
	if m == nil || c.Language == lang.Und {
		return 0
	}
	count := m.LanguageSurfaceCount(c.Language, c.Surface)
	return m.LanguagePreference(c.Language, previous) + math.Log1p(float64(count))
}

// RankLanguageCandidates 依 LanguageCandidateScore 由高到低穩定排序。
// 分數相同時保留呼叫端原本的順序，語言偏好不會無故打亂統計排序。
func (m *Model) RankLanguageCandidates(cands []LanguageCandidate, previous lang.ID) []LanguageCandidate {
	out := slices.Clone(cands)
	if m == nil || len(out) < 2 {
		return out
	}
	type scored struct {
		c LanguageCandidate
		s float64
	}
	items := make([]scored, len(cands))
	for i, c := range cands {
		items[i] = scored{c: c, s: m.LanguageCandidateScore(c, previous)}
	}
	slices.SortStableFunc(items, func(a, b scored) int {
		switch {
		case a.s > b.s:
			return -1
		case a.s < b.s:
			return 1
		}
		return 0
	})
	for i := range items {
		out[i] = items[i].c
	}
	return out
}
