// Package lexicon 提供日文詞典的詞條、詞性與讀音 trie。
//
// 讀音以 reading.Sequence 表示，一個かな字元是一個單位，所以詞條讀音可以是可変長：
// 詞面字數不必等於讀音單位數。這個套件只負責查詢，不負責搜尋路徑；
// Viterbi 與文節分割在 core/ja/decode。
package lexicon

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/0c-biggo/0c-ime/core/reading"
)

// POS 是詞性。值使用穩定的英文名稱，可寫進 TSV 與資料包 manifest。
type POS string

// 詞性常數。BOS 與 EOS 只用在連接成本矩陣，不會出現在詞條。
const (
	POSOther          POS = "other"
	POSNoun           POS = "noun"
	POSVerb           POS = "verb"
	POSAdjective      POS = "adjective"
	POSAdjectivalNoun POS = "adjectival_noun"
	POSAdverb         POS = "adverb"
	POSParticle       POS = "particle"
	POSAuxiliary      POS = "auxiliary"
	POSConjunction    POS = "conjunction"
	POSPrefix         POS = "prefix"
	POSSuffix         POS = "suffix"
	POSDeterminer     POS = "determiner"
	POSInterjection   POS = "interjection"
	POSNumber         POS = "number"
	POSSymbol         POS = "symbol"

	POSBOS POS = "<s>"
	POSEOS POS = "</s>"
)

// posNames 是 TSV 可接受的詞性名稱。沒有列出的名稱視為格式錯誤，
// 避免打錯字默默變成 other。
var posNames = map[string]POS{
	string(POSOther):          POSOther,
	string(POSNoun):           POSNoun,
	string(POSVerb):           POSVerb,
	string(POSAdjective):      POSAdjective,
	string(POSAdjectivalNoun): POSAdjectivalNoun,
	string(POSAdverb):         POSAdverb,
	string(POSParticle):       POSParticle,
	string(POSAuxiliary):      POSAuxiliary,
	string(POSConjunction):    POSConjunction,
	string(POSPrefix):         POSPrefix,
	string(POSSuffix):         POSSuffix,
	string(POSDeterminer):     POSDeterminer,
	string(POSInterjection):   POSInterjection,
	string(POSNumber):         POSNumber,
	string(POSSymbol):         POSSymbol,
}

// ParsePOS 把 TSV 的詞性名稱轉成 POS。
func ParsePOS(s string) (POS, error) {
	p, ok := posNames[s]
	if !ok {
		return "", fmt.Errorf("未知詞性 %q", s)
	}
	return p, nil
}

// Valid 回報 p 是否為可寫進詞條的詞性。BOS 與 EOS 不是合法詞條詞性。
func (p POS) Valid() bool {
	_, ok := posNames[string(p)]
	return ok
}

// Independent 回報 p 是否為自立語：可以開始一個新的文節。
func (p POS) Independent() bool {
	switch p {
	case POSParticle, POSAuxiliary, POSSuffix, POSPrefix, POSBOS, POSEOS:
		return false
	}
	return p.Valid()
}

// Dependent 回報 p 是否為付属語：依附前一個詞，不單獨形成文節。
func (p POS) Dependent() bool {
	switch p {
	case POSParticle, POSAuxiliary, POSSuffix:
		return true
	}
	return false
}

// Entry 是一筆（詞面, 讀音）詞條。
type Entry struct {
	Surface string
	Reading reading.Sequence
	// Cost 是 unigram 詞成本，越低越優先。0 表示未指定，由解碼器的保底值補上；
	// 負值合法，代表比 0 更優先。範圍見 MinWordCost 與 MaxWordCost。
	Cost float64
	POS  POS
	// LeftID 與 RightID 是來源詞典的連接 context ID（Mozc 的 lid/rid）。
	// HasContext 為 false 時，解碼器只用 POS 連接成本；未知詞 fallback 也是 false。
	// ID 0 是 BOS/EOS；一般詞條使用 1 以上。
	LeftID     uint16
	RightID    uint16
	HasContext bool
}

// 產品詞成本容許的範圍。0c-jadic 匯入把 pack 的 signed i16 成本乘上 0.001，
// 所以 -32767～32767 對應 -32.767～32.767；負值保留原資料語意，越小越優先。
// 這個範圍只約束單詞成本，連接成本仍要求非負。
const (
	MinWordCost = -32.767
	MaxWordCost = 32.767
)

// validCost 回報 c 是否在文件化的詞成本範圍內；NaN 與 Inf 一律不合法。
func validCost(c float64) bool {
	return !math.IsNaN(c) && !math.IsInf(c, 0) && c >= MinWordCost && c <= MaxWordCost
}

// Key 回傳詞條的穩定鍵，供除重與測試使用。
func (e Entry) Key() string {
	return e.Surface + "|" + reading.Key(e.Reading) + "|" + string(e.POS)
}

// Source 依讀音查詞條。
type Source interface {
	// Exact 回傳讀音恰為 seq 的詞條。
	Exact(seq reading.Sequence) []Entry
	// Prefixed 回傳讀音以 seq 開頭的詞條，含恰好相等者。
	Prefixed(seq reading.Sequence) []Entry
	// HasPrefix 回報是否有讀音以 seq 開頭的詞條。
	HasPrefix(seq reading.Sequence) bool
	// Matches 依序回報從 start 開始的每個詞條讀音與結束位置。
	// 實作必須走訪 seq[start:] 的所有前綴，讓可変長讀音能被 lattice 展開。
	Matches(seq reading.Sequence, start int, fn func(end int, e Entry))
}

type node struct {
	children map[reading.Unit]*node
	order    []reading.Unit // 插入順序，確保走訪結果可重現
	entries  []Entry
}

// Mem 是記憶體內的讀音 trie。適合測試與小型詞典；正式資料包由 T13 編譯。
type Mem struct {
	root     node
	longest  int
	surfaces map[string]bool
	byText   map[string][]Entry
}

// NewMem 建立空詞庫。
func NewMem() *Mem {
	return &Mem{
		surfaces: map[string]bool{},
		byText:   map[string][]Entry{},
	}
}

// Add 加入一筆詞條。詞面與讀音都不可為空，讀音單位必須合法。
func (m *Mem) Add(e Entry) error {
	if e.Surface == "" {
		return fmt.Errorf("詞面不可為空")
	}
	if len(e.Reading) == 0 {
		return fmt.Errorf("%q 的讀音不可為空", e.Surface)
	}
	for _, u := range e.Reading {
		if !u.Valid() {
			return fmt.Errorf("%q 的讀音單位 %q 不合法", e.Surface, u)
		}
	}
	if !validCost(e.Cost) {
		return fmt.Errorf("%q 的詞成本 %v 超出容許範圍 %v～%v", e.Surface, e.Cost, MinWordCost, MaxWordCost)
	}
	if !e.POS.Valid() {
		return fmt.Errorf("%q 的詞性 %q 不合法", e.Surface, e.POS)
	}
	n := &m.root
	for _, u := range e.Reading {
		if n.children == nil {
			n.children = map[reading.Unit]*node{}
		}
		c, ok := n.children[u]
		if !ok {
			c = &node{}
			n.children[u] = c
			n.order = append(n.order, u)
		}
		n = c
	}
	n.entries = append(n.entries, e)
	m.longest = max(m.longest, len(e.Reading))
	m.surfaces[e.Surface] = true
	m.byText[e.Surface] = append(m.byText[e.Surface], e)
	return nil
}

// LongestWord 回傳最長的詞條讀音單位數。
func (m *Mem) LongestWord() int { return m.longest }

func (m *Mem) find(seq reading.Sequence) *node {
	n := &m.root
	for _, u := range seq {
		c, ok := n.children[u]
		if !ok {
			return nil
		}
		n = c
	}
	return n
}

// Exact 回傳讀音恰為 seq 的詞條。
func (m *Mem) Exact(seq reading.Sequence) []Entry {
	if n := m.find(seq); n != nil {
		return n.entries
	}
	return nil
}

// Prefixed 回傳讀音以 seq 開頭的詞條。
func (m *Mem) Prefixed(seq reading.Sequence) []Entry {
	n := m.find(seq)
	if n == nil {
		return nil
	}
	var out []Entry
	var walk func(*node)
	walk = func(n *node) {
		out = append(out, n.entries...)
		for _, u := range n.order {
			walk(n.children[u])
		}
	}
	walk(n)
	return out
}

// HasPrefix 回報是否有讀音以 seq 開頭的詞條。
func (m *Mem) HasPrefix(seq reading.Sequence) bool { return m.find(seq) != nil }

// Matches 走訪 seq[start:] 的所有詞條前綴。詞條依插入順序回報。
func (m *Mem) Matches(seq reading.Sequence, start int, fn func(end int, e Entry)) {
	if start < 0 || start > len(seq) {
		return
	}
	n := &m.root
	for end := start; end < len(seq); end++ {
		c, ok := n.children[seq[end]]
		if !ok {
			return
		}
		n = c
		for _, e := range n.entries {
			fn(end+1, e)
		}
	}
}

// Walk 依插入順序走訪所有詞條。
func (m *Mem) Walk(fn func(Entry)) {
	var walk func(*node)
	walk = func(n *node) {
		for _, e := range n.entries {
			fn(e)
		}
		for _, u := range n.order {
			walk(n.children[u])
		}
	}
	walk(&m.root)
}

// Lookup 回傳詞面的所有讀音，依加入順序。
func (m *Mem) Lookup(surface string) []Entry {
	return m.byText[surface]
}

// Has 回報詞面是否存在。
func (m *Mem) Has(surface string) bool { return m.surfaces[surface] }

// Load 讀取詞典 TSV：詞面、讀音（空白分隔的かな）、詞成本、詞性。
// 以 # 起始的行與空行略過。詞成本可省略，預設 1；詞性可省略，預設 other。
func Load(r io.Reader) (*Mem, error) {
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
		e := Entry{
			Surface: f[0],
			Reading: parseReading(f[1]),
			Cost:    1,
			POS:     POSOther,
		}
		if len(e.Reading) == 0 {
			return nil, fmt.Errorf("第 %d 行：讀音不可為空", ln)
		}
		if len(f) > 2 && f[2] != "" {
			cost, err := strconv.ParseFloat(f[2], 64)
			if err != nil || !validCost(cost) {
				return nil, fmt.Errorf("第 %d 行：詞成本 %q 不合法；容許範圍 %v～%v", ln, f[2], MinWordCost, MaxWordCost)
			}
			e.Cost = cost
		}
		if len(f) > 3 && f[3] != "" {
			p, err := ParsePOS(f[3])
			if err != nil {
				return nil, fmt.Errorf("第 %d 行：%w", ln, err)
			}
			e.POS = p
		}
		// 第七、八欄是選填的 Mozc left/right context ID。舊的六欄資料沒有這兩欄，
		// 繼續走 POS 連接矩陣；有兩欄時解碼器可以改用 context 矩陣。
		if len(f) > 7 && f[6] != "" && f[7] != "" {
			left, err := strconv.Atoi(f[6])
			if err != nil || left < 0 || left > 65535 {
				return nil, fmt.Errorf("第 %d 行：left context ID %q 不合法", ln, f[6])
			}
			right, err := strconv.Atoi(f[7])
			if err != nil || right < 0 || right > 65535 {
				return nil, fmt.Errorf("第 %d 行：right context ID %q 不合法", ln, f[7])
			}
			e.LeftID = uint16(left)
			e.RightID = uint16(right)
			e.HasContext = true
		}
		if err := m.Add(e); err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", ln, err)
		}
	}
	return m, sc.Err()
}

// parseReading 把 TSV 的讀音轉成單位序列。日文單位是一個かな字元，
// 所以 "あした" 與 "あ し た" 都解析成三個單位；空格只用來分隔詞邊界，方便人工編輯。
func parseReading(s string) reading.Sequence {
	var seq reading.Sequence
	for _, f := range strings.Fields(s) {
		for _, r := range f {
			seq = append(seq, reading.Unit(r))
		}
	}
	return seq
}
