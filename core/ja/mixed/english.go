package mixed

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/english"
	"github.com/0c-biggo/0c-ime/core/lang"
)

// 候選來源。lang 允許實作自訂 Source，外殼不得假設只有內建值。
const (
	// SourceLiteral 是使用者原樣輸入的文字。
	SourceLiteral = "literal"
	// SourceEnglish 是英文詞典或學習詞彙的候選。
	SourceEnglish = "english"
	// SourceKana 是同一串按鍵的かな解釋。
	SourceKana = "kana"
)

// 候選標註。
const (
	// AnnotationEnglish 標示詞典英文詞。
	AnnotationEnglish = "英文"
	// AnnotationLearned 標示使用者學過的英文詞。
	AnnotationLearned = "學過"
	// AnnotationCorrection 標示拼字建議。
	AnnotationCorrection = "拼字建議"
	// AnnotationWidth 標示全角或半角英數。
	AnnotationWidth = "字寬"
)

// MaxLiteralBytes 是學習與候選可接受的單一英數詞長度上限，和
// core/english.MaxWordBytes 採相同量級。
const MaxLiteralBytes = 96

// MaxSuggestions 是單次查詢最多回傳的英文候選數。
const MaxSuggestions = english.MaxCandidates

// Suggestion 是英文自動補完的一項。
type Suggestion struct {
	Text    string
	Weight  float64
	Learned bool
}

// English 包住 core/english 的唯讀詞典，加上日文混打需要的個人學習詞。
//
// 學習詞只存在這個物件裡，不寫檔；宿主可以把送出事件轉給 Learn，把個人檔
// 載入後再逐筆 Learn。English 不是 goroutine-safe，和 session 一樣由單一
// 執行緒使用。
type English struct {
	dict    *english.Dictionary
	learned map[string]*learnedWord
	order   []*learnedWord
}

type learnedWord struct {
	text  string
	count int
}

// NewEnglish 建立英文查詢層。dict 為 nil 時使用 core/english 的內建原型詞彙，
// 方便測試與第一次混打，不會載入檔案或連網。
func NewEnglish(dict *english.Dictionary) *English {
	if dict == nil {
		dict = english.Prototype()
	}
	return &English{dict: dict, learned: map[string]*learnedWord{}}
}

// Dictionary 回傳底層唯讀詞典。
func (e *English) Dictionary() *english.Dictionary {
	if e == nil {
		return nil
	}
	return e.dict
}

// Has 回報 text 是否為詞典或學習詞彙認得的英文詞（不分大小寫）。這個方法讓
// English 符合 Lookup，可直接傳給 Classify 與 Scan。
func (e *English) Has(text string) bool {
	if e == nil {
		return false
	}
	if e.Learned(text) {
		return true
	}
	return e.dict != nil && e.dict.Has(text)
}

// Known 是 Has 的別名，語意上強調「已知的英文詞」。
func (e *English) Known(text string) bool { return e.Has(text) }

// Learned 回報 text 是否為使用者學過的詞（不分大小寫）。
func (e *English) Learned(text string) bool {
	if e == nil || text == "" {
		return false
	}
	_, ok := e.learned[strings.ToLower(text)]
	return ok
}

// Learn 記下一次英文詞的使用。空白、空字串與過長的內容會被拒絕。
//
// 大小寫照第一次學到的寫法保留，查詢與排序不分大小寫。
func (e *English) Learn(text string) bool {
	if e == nil {
		return false
	}
	text = strings.TrimSpace(text)
	if !validLiteral(text) {
		return false
	}
	key := strings.ToLower(text)
	if e.learned == nil {
		e.learned = map[string]*learnedWord{}
	}
	if w, ok := e.learned[key]; ok {
		w.count++
		return true
	}
	w := &learnedWord{text: text, count: 1}
	e.learned[key] = w
	e.order = append(e.order, w)
	return true
}

// Forget 移除一個學習詞；有移除時回傳 true。
func (e *English) Forget(text string) bool {
	if e == nil {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(text))
	w, ok := e.learned[key]
	if !ok {
		return false
	}
	delete(e.learned, key)
	for i, item := range e.order {
		if item == w {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
	return true
}

// LearnedWords 回傳學過的詞，依使用次數由高到低。回傳的是副本。
func (e *English) LearnedWords() []string {
	if e == nil {
		return nil
	}
	words := append([]*learnedWord(nil), e.order...)
	sort.SliceStable(words, func(i, j int) bool {
		if words[i].count != words[j].count {
			return words[i].count > words[j].count
		}
		return words[i].text < words[j].text
	})
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = w.text
	}
	return out
}

// Suggestions 回傳以 prefix 開頭的英文詞，學習詞優先，其次才是詞典詞。
//
// prefix 不分大小寫；詞典的原本大小寫規則仍由 core/english 決定（全大寫會
// 回傳全大寫）。精確等於 prefix 的詞也會包含在結果裡，呼叫端要補完時可用
// Complete。
func (e *English) Suggestions(prefix string, limit int) []Suggestion {
	if e == nil || prefix == "" || limit <= 0 {
		return nil
	}
	limit = min(limit, MaxSuggestions)
	lower := strings.ToLower(prefix)
	seen := map[string]bool{}
	var out []Suggestion

	var learned []*learnedWord
	for _, w := range e.order {
		if strings.HasPrefix(strings.ToLower(w.text), lower) {
			learned = append(learned, w)
		}
	}
	sort.SliceStable(learned, func(i, j int) bool {
		if learned[i].count != learned[j].count {
			return learned[i].count > learned[j].count
		}
		return learned[i].text < learned[j].text
	})
	for _, w := range learned {
		seen[strings.ToLower(w.text)] = true
		out = append(out, Suggestion{Text: w.text, Weight: 1e6 + float64(w.count), Learned: true})
	}

	if e.dict != nil && asciiLetters(prefix) {
		for _, c := range e.dict.Prefix(prefix, limit) {
			key := strings.ToLower(c.Text)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Suggestion{Text: c.Text, Weight: c.Weight})
			if len(out) == limit {
				break
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Complete 回傳 prefix 最長或最優先的補完詞。沒有比 prefix 更長的候選時
// ok 為 false。
func (e *English) Complete(prefix string) (string, bool) {
	if e == nil || prefix == "" {
		return "", false
	}
	count := len([]rune(prefix))
	for _, s := range e.Suggestions(prefix, MaxSuggestions) {
		if len([]rune(s.Text)) > count && !strings.EqualFold(s.Text, prefix) {
			return s.Text, true
		}
	}
	return "", false
}

// Candidates 把英文補完與拼字建議轉成 lang.Candidate。原樣輸入的候選由
// 呼叫端（Composer 或 Converter）自己放在最前面。
func (e *English) Candidates(prefix string, limit int) []lang.Candidate {
	if e == nil || prefix == "" || limit <= 0 {
		return nil
	}
	limit = min(limit, MaxSuggestions)
	var out []lang.Candidate
	seen := map[string]bool{}
	for _, s := range e.Suggestions(prefix, limit) {
		key := strings.ToLower(s.Text)
		if seen[key] {
			continue
		}
		seen[key] = true
		annotation := AnnotationEnglish
		if s.Learned {
			annotation = AnnotationLearned
		}
		out = append(out, lang.Candidate{
			Surface:    s.Text,
			Score:      s.Weight,
			Source:     SourceEnglish,
			Annotation: annotation,
		})
	}
	if len(out) < limit && asciiLetters(prefix) && e.dict != nil && !e.dict.Has(prefix) {
		for _, c := range e.dict.Corrections(prefix, limit-len(out)) {
			key := strings.ToLower(c.Text)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, lang.Candidate{
				Surface:    c.Text,
				Score:      c.Weight / 1000,
				Source:     SourceEnglish,
				Annotation: AnnotationCorrection,
			})
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func validLiteral(text string) bool {
	if text == "" || len(text) > MaxLiteralBytes || !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if r == '\u3000' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r < 0x20 {
			return false
		}
	}
	return true
}

func asciiLetters(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
			return false
		}
	}
	return true
}
