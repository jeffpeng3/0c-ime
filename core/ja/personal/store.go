package personal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/reading"
)

// Format 是 personal.ja.json 的格式版本。新增或改變欄位意義時加一；
// 較新的檔案不得以本版結構寫回。
const Format = 1

// Language 是這份學習檔的語言 ID。日文與注音不共用同一個 JSON。
const Language = "ja"

// FileName 是日文個人學習檔的固定檔名。
const FileName = "personal.ja.json"

// MaxEntries 是學習檔的條目上限。超過時丟掉次數最少、最久未用的條目，
// 讓檔案大小與轉換成本有上界。
const MaxEntries = 20000

// maxUndo 是保留的反悔步數。
const maxUndo = 32

// maxFileBytes 是讀檔上限；超過視為損壞，不把整份檔案讀進記憶體。
const maxFileBytes = 8 << 20

// ContextHashBytes 是左文節雜湊的長度。8 bytes 足以區分日常前文，
// 也讓鍵值保持好讀。
const ContextHashBytes = 8

// maxContextRunes 是參與雜湊的前文長度上限。只取靠近組字區的尾巴，
// 文件再長也不會讓同一個前文的鍵跟著變長。
const maxContextRunes = 24

// Source 的慣用值。其他字串由呼叫端自訂，學習排序不假設只有這幾種。
const (
	SourceSelection = "selection" // 使用者選字
	SourceClause    = "clause"    // 使用者改文節
	SourceAccept    = "accept"    // Tab 接受
)

// Entry 是單一學習條目，形狀對應 contracts.md 第 5 節。
//
// Reading 與 Segments 逐項對應，一個元素是一個詞的かな讀音（例如 "きょう"）；
// 讀音單位本身仍是一個かな字元，查詢時再攤平成 reading.Sequence。
type Entry struct {
	Key      string   `json:"key"`
	Reading  []string `json:"reading"`
	Surface  string   `json:"surface"`
	Segments []string `json:"segments"`
	Count    int      `json:"count"`
	LastUsed int64    `json:"last_used"`
	Source   string   `json:"source"`
}

// Units 把條目的文節讀音攤平成讀音單位序列。
func (e Entry) Units() reading.Sequence {
	var seq reading.Sequence
	for _, part := range e.Reading {
		for _, r := range part {
			seq = append(seq, reading.Unit(r))
		}
	}
	return seq
}

// Selection 是一次學習觀察：某個文節在某個前文後面被選成某個詞面。
type Selection struct {
	// Reading 是這個文節的讀音。
	Reading reading.Sequence
	// Surface 是使用者選定的詞面。
	Surface string
	// Segments 是文節內各詞的詞面，供顯示與忘記使用；可為空。
	Segments []string
	// SegmentReadings 與 Segments 對齊，是各詞的讀音；可為空。
	// 有提供時學習檔的 reading 會逐段對應 segments，和 contracts.md 的範例一致。
	SegmentReadings []reading.Sequence
	// Left 是這個文節左側已確定的文字；句首填空字串。
	Left string
	// Source 是學習來源，見 SourceSelection 等常數。
	Source string
	// At 是宿主提供的 Unix 秒；核心不自行讀時鐘，0 表示沿用舊值。
	At int64
}

// NewerFormatError 表示檔案由較新版本寫成；呼叫端不得以目前結構寫回。
type NewerFormatError struct{ Format int }

func (e *NewerFormatError) Error() string {
	return fmt.Sprintf("日文學習檔格式 %d 比本版支援的 %d 新", e.Format, Format)
}

// LanguageError 表示檔案屬於其他語言，不得拿來當日文學習檔。
type LanguageError struct{ Language string }

func (e *LanguageError) Error() string {
	return fmt.Sprintf("學習檔語言是 %q，不是 %q", e.Language, Language)
}

// Store 是日文的個人學習資料。零值可用，但正式路徑請用 NewStore 或 LoadFile。
//
// 所有方法都可同時呼叫；存檔與按鍵學習不會互相看到半份資料。
type Store struct {
	Format   int     `json:"format"`
	Language string  `json:"language"`
	Entries  []Entry `json:"entries"`

	mu     sync.RWMutex
	index  map[string]int
	undo   []undoRecord
	paused bool
}

// undoRecord 是一次學習交易前的條目狀態；反悔時整筆還原。
type undoRecord []entryState

type entryState struct {
	key    string
	entry  Entry
	exists bool
}

// NewStore 回傳空白的日文學習資料。
func NewStore() *Store {
	return &Store{Format: Format, Language: Language, Entries: []Entry{}}
}

// Key 回傳學習檔的鍵值：reading.Key 加左文節正規化雜湊。
func Key(seq reading.Sequence, left string) string {
	return reading.Key(seq) + "|" + ContextHash(left)
}

// NormalizeLeft 正規化左文節：移除空白、英文字母轉小寫，只留最後
// maxContextRunes 個字元。換前文時雜湊一定不同，相同前文的空白差異不會拆成兩筆。
func NormalizeLeft(left string) string {
	var b strings.Builder
	b.Grow(len(left))
	for _, r := range left {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	s := b.String()
	if runes := []rune(s); len(runes) > maxContextRunes {
		s = string(runes[len(runes)-maxContextRunes:])
	}
	return s
}

// ContextHash 回傳左文節正規化後的雜湊，作為鍵值的後半。
func ContextHash(left string) string {
	sum := sha256.Sum256([]byte(NormalizeLeft(left)))
	return hex.EncodeToString(sum[:ContextHashBytes])
}

func readingParts(sel Selection) []string {
	full := reading.Key(sel.Reading)
	parts := make([]string, 0, max(1, len(sel.Segments)))
	if len(sel.SegmentReadings) == len(sel.Segments) && len(sel.Segments) > 0 {
		total := 0
		for _, rd := range sel.SegmentReadings {
			if len(rd) == 0 {
				return []string{strings.ReplaceAll(full, reading.KeySeparator, "")}
			}
			parts = append(parts, strings.ReplaceAll(reading.Key(rd), reading.KeySeparator, ""))
			total += len(rd)
		}
		if total == len(sel.Reading) { // 逐段讀音必須剛好拼回整個文節
			return parts
		}
	}
	return []string{strings.ReplaceAll(full, reading.KeySeparator, "")}
}

func entryFromSelection(sel Selection) (Entry, bool) {
	if len(sel.Reading) == 0 || sel.Surface == "" {
		return Entry{}, false
	}
	for _, u := range sel.Reading {
		if !u.Valid() {
			return Entry{}, false
		}
	}
	segments := append([]string(nil), sel.Segments...)
	if len(segments) == 0 {
		segments = []string{sel.Surface}
	}
	e := Entry{
		Key:      Key(sel.Reading, sel.Left),
		Reading:  readingParts(sel),
		Surface:  sel.Surface,
		Segments: segments,
		Count:    1,
		LastUsed: sel.At,
		Source:   sel.Source,
	}
	if e.Source == "" {
		e.Source = SourceSelection
	}
	return e, true
}

func (s *Store) initLocked() {
	if s.Entries == nil {
		s.Entries = []Entry{}
	}
	if s.index == nil {
		s.reindexLocked()
	}
}

func (s *Store) reindexLocked() {
	s.index = make(map[string]int, len(s.Entries))
	for i := range s.Entries {
		s.index[s.Entries[i].Key] = i
	}
}

func (s *Store) stateLocked(key string) entryState {
	if i, ok := s.index[key]; ok {
		return entryState{key: key, entry: s.Entries[i], exists: true}
	}
	return entryState{key: key}
}

// Record 記下一次學習。暫停學習或資料不合法時回傳 false。
func (s *Store) Record(sel Selection) bool {
	return s.RecordAll([]Selection{sel}) == 1
}

// RecordAll 把多個文節記成一次學習交易，回傳實際記下的筆數。
// 一次提交（含後續文節）只佔一步反悔。
func (s *Store) RecordAll(sels []Selection) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return 0
	}
	s.initLocked()
	if len(sels) == 0 {
		return 0
	}
	var rec undoRecord
	seen := make(map[string]bool, len(sels))
	n := 0
	for _, sel := range sels {
		e, ok := entryFromSelection(sel)
		if !ok {
			continue
		}
		if !seen[e.Key] {
			rec = append(rec, s.stateLocked(e.Key))
			seen[e.Key] = true
		}
		if i, ok := s.index[e.Key]; ok {
			cur := &s.Entries[i]
			cur.Count++
			cur.LastUsed = max(cur.LastUsed, e.LastUsed)
			cur.Surface = e.Surface
			cur.Segments = e.Segments
			cur.Reading = e.Reading
			cur.Source = e.Source
		} else {
			s.Entries = append(s.Entries, e)
			s.index[e.Key] = len(s.Entries) - 1
		}
		n++
	}
	if n == 0 {
		return 0
	}
	s.undo = append(s.undo, rec)
	if len(s.undo) > maxUndo {
		s.undo = s.undo[len(s.undo)-maxUndo:]
	}
	s.pruneLocked()
	return n
}

// Lookup 回傳同讀音、同前文的學習條目。
func (s *Store) Lookup(seq reading.Sequence, left string) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Entries) == 0 {
		return Entry{}, false
	}
	key := Key(seq, left)
	if s.index == nil { // 直接建構的 Store 沒有索引：線性掃描一次，不修改共享狀態
		for _, e := range s.Entries {
			if e.Key == key {
				return e, true
			}
		}
		return Entry{}, false
	}
	i, ok := s.index[key]
	if !ok {
		return Entry{}, false
	}
	return s.Entries[i], true
}

// Undo 還原最近一次學習交易。沒有可反悔的紀錄時回傳 false。
func (s *Store) Undo() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.undo) == 0 {
		return false
	}
	rec := s.undo[len(s.undo)-1]
	s.undo = s.undo[:len(s.undo)-1]
	s.initLocked()
	for _, st := range rec {
		if i, ok := s.index[st.key]; ok {
			if st.exists {
				s.Entries[i] = st.entry
			} else {
				s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
				s.reindexLocked()
			}
			continue
		}
		if st.exists {
			s.Entries = append(s.Entries, st.entry)
			s.index[st.key] = len(s.Entries) - 1
		}
	}
	return true
}

// Forget 移除與詞面有關的學習：主詞面相同，或出現在文節內任一段。
// 回傳移除的筆數。忘記本身可以反悔。
func (s *Store) Forget(surface string) int {
	return s.ForgetAll([]string{surface})
}

// ForgetAll 移除與任一個詞面有關的學習，每筆資料只掃一遍。
func (s *Store) ForgetAll(surfaces []string) int {
	if s == nil || len(surfaces) == 0 {
		return 0
	}
	want := make(map[string]bool, len(surfaces))
	for _, w := range surfaces {
		if w != "" {
			want[w] = true
		}
	}
	if len(want) == 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
	match := func(e Entry) bool {
		if want[e.Surface] {
			return true
		}
		for _, seg := range e.Segments {
			if want[seg] {
				return true
			}
		}
		return false
	}
	var rec undoRecord
	kept := s.Entries[:0:0]
	removed := 0
	for _, e := range s.Entries {
		if match(e) {
			rec = append(rec, entryState{key: e.Key, entry: e, exists: true})
			removed++
			continue
		}
		kept = append(kept, e)
	}
	if removed == 0 {
		return 0
	}
	s.Entries = kept
	s.reindexLocked()
	s.undo = append(s.undo, rec)
	if len(s.undo) > maxUndo {
		s.undo = s.undo[len(s.undo)-maxUndo:]
	}
	return removed
}

// Reset 清空全部日文學習與反悔紀錄。注音學習檔不在這裡，不會被碰到。
// 暫停狀態保留：使用者按了暫停，不會因為清除資料就被偷偷恢復學習。
func (s *Store) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Entries = []Entry{}
	s.index = map[string]int{}
	s.undo = nil
}

// SetPaused 設定是否暫停學習。暫停時仍會套用既有學習排序，只是不再記新的。
func (s *Store) SetPaused(paused bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.paused = paused
	s.mu.Unlock()
}

// Paused 回報是否暫停學習。
func (s *Store) Paused() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paused
}

// Len 回傳學習條目數。
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Entries)
}

// LookupKey 依完整鍵值回傳學習條目，供設定程式顯示學到的內容。
func (s *Store) LookupKey(key string) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Entries) == 0 {
		return Entry{}, false
	}
	if s.index != nil {
		i, ok := s.index[key]
		if !ok {
			return Entry{}, false
		}
		return s.Entries[i], true
	}
	for _, e := range s.Entries {
		if e.Key == key {
			return e, true
		}
	}
	return Entry{}, false
}

func (s *Store) pruneLocked() {
	if len(s.Entries) <= MaxEntries {
		return
	}
	sort.SliceStable(s.Entries, func(i, j int) bool {
		a, b := s.Entries[i], s.Entries[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.LastUsed != b.LastUsed {
			return a.LastUsed > b.LastUsed
		}
		return a.Key < b.Key
	})
	s.Entries = s.Entries[:MaxEntries]
	s.reindexLocked()
}

// Load 讀回日文學習檔。較新版本或別的語言回傳錯誤，不猜測內容。
func Load(r io.Reader) (*Store, error) {
	var raw struct {
		Format   int     `json:"format"`
		Language string  `json:"language"`
		Entries  []Entry `json:"entries"`
	}
	dec := json.NewDecoder(io.LimitReader(r, maxFileBytes))
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if raw.Format > Format {
		return nil, &NewerFormatError{Format: raw.Format}
	}
	if raw.Language != "" && raw.Language != Language {
		return nil, &LanguageError{Language: raw.Language}
	}
	s := NewStore()
	s.index = make(map[string]int, len(raw.Entries))
	for _, e := range raw.Entries {
		e.Key = strings.TrimSpace(e.Key)
		if e.Key == "" || e.Surface == "" {
			continue
		}
		if len(e.Reading) == 0 {
			if rk, _, ok := strings.Cut(e.Key, "|"); ok {
				if rk = strings.ReplaceAll(rk, reading.KeySeparator, ""); rk != "" {
					e.Reading = []string{rk}
				}
			}
		}
		units := e.Units()
		if len(units) == 0 {
			continue
		}
		valid := true
		for _, u := range units {
			if !u.Valid() {
				valid = false
				break
			}
		}
		if !valid || e.Count <= 0 {
			continue
		}
		e.LastUsed = max(e.LastUsed, 0)
		if i, ok := s.index[e.Key]; ok { // 重複鍵：合併次數，保留較新的詞面
			cur := &s.Entries[i]
			newer := e.LastUsed >= cur.LastUsed
			cur.Count += e.Count
			cur.LastUsed = max(cur.LastUsed, e.LastUsed)
			if newer {
				cur.Surface = e.Surface
				cur.Segments = e.Segments
				cur.Reading = e.Reading
				cur.Source = e.Source
			}
			continue
		}
		s.Entries = append(s.Entries, e)
		s.index[e.Key] = len(s.Entries) - 1
	}
	s.Format, s.Language = Format, Language
	return s, nil
}

// Save 以固定順序寫出 JSON：鍵值排序，存檔內容可重現。
func (s *Store) Save(w io.Writer) error {
	b, err := s.encode()
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func (s *Store) encode() ([]byte, error) {
	if s == nil {
		return nil, errors.New("nil 日文學習資料")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := struct {
		Format   int     `json:"format"`
		Language string  `json:"language"`
		Entries  []Entry `json:"entries"`
	}{Format: Format, Language: Language, Entries: append([]Entry(nil), s.Entries...)}
	sort.SliceStable(out.Entries, func(i, j int) bool {
		return out.Entries[i].Key < out.Entries[j].Key
	})
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// CorruptError 表示檔案內容不是可解析的日文學習檔。
//
// core 不碰檔案系統，不能替宿主改名；宿主收到這個錯誤時應把原檔改名保留
// （例如 personal.ja.json.corrupt-時間），再用空白 Store 繼續，下一次存檔從乾淨的版本開始。
// 較新版本或別的語言不會包成 CorruptError，呼叫端不得寫回。
type CorruptError struct {
	Name string
	Err  error
}

func (e *CorruptError) Error() string {
	return fmt.Sprintf("%s 內容損壞：%v", e.Name, e.Err)
}

func (e *CorruptError) Unwrap() error { return e.Err }

// LoadFile 從 fsys 讀回 name。檔案不存在時回傳空白資料。
//
// 內容損壞時回傳空白 Store 與 *CorruptError；呼叫端應保留原檔並照常輸入。
// 較新版本或別的語言回傳 *NewerFormatError、*LanguageError，不得寫回。
func LoadFile(fsys fs.FS, name string) (*Store, error) {
	f, err := fsys.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return NewStore(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s：%w", name, err)
	}
	defer f.Close()
	st, err := Load(f)
	if err == nil {
		return st, nil
	}
	var newer *NewerFormatError
	if errors.As(err, &newer) {
		return nil, fmt.Errorf("%s：%w", name, err)
	}
	var language *LanguageError
	if errors.As(err, &language) {
		return nil, fmt.Errorf("%s：%w", name, err)
	}
	return NewStore(), &CorruptError{Name: name, Err: err}
}
