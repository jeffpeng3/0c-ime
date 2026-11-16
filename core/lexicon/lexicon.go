// Package lexicon 提供依讀音查詞條的介面與記憶體內實作。
//
// 每筆詞條是一個（詞面, 讀音）配對，帶讀音模型的機率 P(r|w)（docs/13）。
// M0 直接讀 TSV；編譯後的二進位格式與 L2／L3 讀音展開屬 M1 的 0c-compile。
package lexicon

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// Entry 是一筆（詞面, 讀音）。
//
// 讀音長度不要求等於詞面長度。注音的讀音仍是嚴格的一字一音節；
// 日文可以有可変長讀音（送り仮名、活用形）。
type Entry struct {
	Surface string
	Reading reading.Sequence
	// PRW 是讀音相容權重，沿用讀音模型欄位名；目前由來源等級與詞內相對校準產生，
	// 並非已量測的 P(讀音 | 詞)。合法低偏好讀音仍保持可達。
	PRW float64
	// Correction 表示此讀音是自動修正用的錯誤讀音。
	Correction bool
	// Rare 表示罕用字（只來自罕用來源，即全字庫獨有或不在 Big5 的字）：候選視窗排在其他字之後；整句解碼不受影響。
	Rare bool
}

// Source 依讀音查詞條。
type Source interface {
	// Exact 回傳讀音恰為 syls 的詞條。
	Exact(syls []zhuyin.Syllable) []Entry
	// Prefixed 回傳讀音以 syls 開頭的詞條（含恰好相等者）。
	Prefixed(syls []zhuyin.Syllable) []Entry
	// HasPrefix 回報是否存在讀音以 syls 開頭的詞條；供搜尋剪枝。
	HasPrefix(syls []zhuyin.Syllable) bool
	// Has 回報詞面是否存在（任一讀音）。
	Has(surface string) bool
}

// Long 是 Source 的選用擴充：回報詞條讀音的最長音節數。解碼器依它決定搜尋多長的讀音（decode.MaxLen）；
// 系統詞庫可以收超過一般上限的長詞（#142），自訂詞也可以（#122）。
type Long interface {
	LongestWord() int
}

// Longest 回傳 src 最長的詞條讀音音節數；src 沒有實作 Long 時回傳 0（由呼叫端套用預設上限）。
func Longest(src Source) int {
	if l, ok := src.(Long); ok {
		return l.LongestWord()
	}
	return 0
}

type node struct {
	children map[reading.Unit]*node
	order    []reading.Unit // 子節點的插入順序，確保走訪結果可重現
	entries  []Entry
}

// Mem 是記憶體內的讀音 trie。
type Mem struct {
	root     node
	surfaces map[string]bool
	longest  int
}

// NewMem 建立空詞庫。
func NewMem() *Mem { return &Mem{surfaces: map[string]bool{}} }

// Add 加入一筆詞條。
func (m *Mem) Add(e Entry) {
	n := &m.root
	for _, s := range e.Reading {
		if n.children == nil {
			n.children = map[reading.Unit]*node{}
		}
		c, ok := n.children[s]
		if !ok {
			c = &node{}
			n.children[s] = c
			n.order = append(n.order, s)
		}
		n = c
	}
	n.entries = append(n.entries, e)
	m.surfaces[e.Surface] = true
	m.longest = max(m.longest, len(e.Reading))
}

// LongestWord 回報最長的詞條讀音音節數。
func (m *Mem) LongestWord() int { return m.longest }

func (m *Mem) find(syls []zhuyin.Syllable) *node {
	n := &m.root
	for _, s := range syls {
		c, ok := n.children[reading.Unit(s)]
		if !ok {
			return nil
		}
		n = c
	}
	return n
}

func (m *Mem) Exact(syls []zhuyin.Syllable) []Entry {
	if n := m.find(syls); n != nil {
		return n.entries
	}
	return nil
}

func (m *Mem) Prefixed(syls []zhuyin.Syllable) []Entry {
	n := m.find(syls)
	if n == nil {
		return nil
	}
	var out []Entry
	var walk func(*node)
	walk = func(n *node) {
		out = append(out, n.entries...)
		for _, s := range n.order {
			walk(n.children[s])
		}
	}
	walk(n)
	return out
}

// Walk 依插入順序走訪所有詞條。
func (m *Mem) Walk(fn func(Entry)) {
	var walk func(*node)
	walk = func(n *node) {
		for _, e := range n.entries {
			fn(e)
		}
		for _, s := range n.order {
			walk(n.children[s])
		}
	}
	walk(&m.root)
}

func (m *Mem) HasPrefix(syls []zhuyin.Syllable) bool { return m.find(syls) != nil }

func (m *Mem) Has(surface string) bool { return m.surfaces[surface] }

// Lookup 回傳某詞面的所有讀音（與 Compact.Lookup 相同；逐筆走訪，只供測試規模的詞庫）。
func (m *Mem) Lookup(surface string) []Entry {
	if !m.surfaces[surface] {
		return nil
	}
	var out []Entry
	m.Walk(func(e Entry) {
		if e.Surface == surface {
			out = append(out, e)
		}
	})
	return out
}

// All 走訪所有詞條（與 Compact.All 相同的介面）。
func (m *Mem) All(fn func(Entry)) { m.Walk(fn) }

// LoadOptions 是讀取 TSV 時的長度規則。
type LoadOptions struct {
	// Language 宣告資料的語言："zh"、"ja"，空字串表示依讀音自動判斷。
	// 注音一律嚴格要求詞面字數等於讀音單位數；日文允許可変長。
	Language string
	// MaxUnits 是單一詞條允許的最大讀音單位數，0 表示格式上限 255。
	// 呼叫端可從資料包 manifest 的 limits.max_word_units 取得。
	MaxUnits int
}

// Load 讀取 TSV：詞面、讀音（空白分隔）、P(r|w)（可省略，預設 1）、旗標（可省略；correction）。
// 以 # 起始的行與空行略過。
//
// 讀音含かな時視為日文，允許可変長；其餘（注音或無法判定的資料）維持嚴格的
// 「字數等於讀音單位數」檢查。需要明確指定語言時用 LoadWithOptions。
func Load(r io.Reader) (*Mem, error) {
	return LoadWithOptions(r, LoadOptions{})
}

// LoadWithOptions 依 options 讀取 TSV。語言或 manifest limits 由呼叫端提供。
func LoadWithOptions(r io.Reader, o LoadOptions) (*Mem, error) {
	maxUnits := o.MaxUnits
	if maxUnits <= 0 || maxUnits > 255 {
		maxUnits = 255
	}
	m := NewMem()
	sc := bufio.NewScanner(r)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			return nil, fmt.Errorf("第 %d 行：至少需要詞面與讀音", ln)
		}
		e := Entry{Surface: f[0], Reading: zhuyin.ReadingUnits(zhuyin.ParseReading(f[1])), PRW: 1}
		if len(e.Reading) == 0 {
			return nil, fmt.Errorf("第 %d 行：缺少讀音", ln)
		}
		if len(e.Reading) > maxUnits {
			return nil, fmt.Errorf("第 %d 行：讀音有 %d 個單位，超過上限 %d", ln, len(e.Reading), maxUnits)
		}
		zhuyinReading := hasZhuyin(e.Reading)
		strict := true
		switch o.Language {
		case "zh":
			strict = true
		case "ja":
			strict = false
		default:
			// 自動判斷：含かな視為日文可変長，其餘（注音或無法判定的資料）維持嚴格。
			strict = !hasKana(e.Reading)
		}
		for _, u := range e.Reading {
			if !reading.Valid(u) {
				return nil, fmt.Errorf("第 %d 行：讀音單位 %q 不合法", ln, u)
			}
			if (strict || zhuyinReading) && !zhuyin.Syllable(u).Valid() {
				return nil, fmt.Errorf("第 %d 行：音節 %q 不合法", ln, u)
			}
		}
		if strict {
			if n := len([]rune(e.Surface)); n != len(e.Reading) {
				return nil, fmt.Errorf("第 %d 行：%q 有 %d 字但讀音有 %d 個單位", ln, e.Surface, n, len(e.Reading))
			}
		}
		if len(f) > 2 && f[2] != "" {
			p, err := strconv.ParseFloat(f[2], 64)
			if err != nil || p <= 0 || p > 1 {
				return nil, fmt.Errorf("第 %d 行：P(r|w) %q 須在 (0,1]", ln, f[2])
			}
			e.PRW = p
		}
		if len(f) > 3 {
			e.Correction = strings.Contains(f[3], "correction")
		}
		m.Add(e)
	}
	return m, sc.Err()
}

// hasZhuyin 回報讀音是否含有注音符號；有的話一定要通過注音結構檢查。
func hasZhuyin(seq reading.Sequence) bool {
	for _, u := range seq {
		for _, r := range string(u) {
			if zhuyin.ClassOf(r) != zhuyin.None {
				return true
			}
		}
	}
	return false
}

// hasKana 回報讀音是否含有日文かな；用來區分可変長的日文讀音。
func hasKana(seq reading.Sequence) bool {
	for _, u := range seq {
		for _, r := range string(u) {
			if unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
				return true
			}
		}
	}
	return false
}
