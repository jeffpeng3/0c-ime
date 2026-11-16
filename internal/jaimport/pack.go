package jaimport

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/0c-biggo/0c-ime/core/reading"
)

// 日文資料包二進位格式。所有整數用無號 varint，字串是長度加 UTF-8 位元組，
// 浮點數固定 8 位元 little-endian，因此同一份輸入在不同平台得到相同位元組。
const (
	lexiconMagic = "0CJLEX01"
	lmMagic      = "0CJLM001"
)

// Lexicon 是編譯後的日文詞典：詞條、讀音 trie、詞面索引與連接成本。
type Lexicon struct {
	entries  []Entry
	nodes    []lexNode
	surfaces map[string][]int
	longest  int
	matrix   *Matrix
	encoded  int
}

type lexNode struct {
	entries  []int
	children []lexChild
}

type lexChild struct {
	unit  reading.Unit
	index int
}

// NewLexicon 驗證、排序並建立讀音 trie 與詞面索引。
func NewLexicon(entries []Entry, conns []Connection) (*Lexicon, error) {
	merged := Merge(entries)
	for i, e := range merged {
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("第 %d 筆詞條：%w", i+1, err)
		}
	}
	l := &Lexicon{
		entries:  merged,
		nodes:    []lexNode{{}},
		surfaces: map[string][]int{},
		matrix:   BuildMatrix(conns),
	}
	for i, e := range merged {
		l.longest = max(l.longest, len(e.Reading))
		l.surfaces[e.Surface] = append(l.surfaces[e.Surface], i)
		l.insert(e.Reading, i)
	}
	for i := range l.nodes {
		sort.Slice(l.nodes[i].children, func(a, b int) bool {
			return l.nodes[i].children[a].unit < l.nodes[i].children[b].unit
		})
	}
	return l, nil
}

func (l *Lexicon) insert(seq reading.Sequence, index int) {
	n := 0
	for _, u := range seq {
		found := -1
		for _, c := range l.nodes[n].children {
			if c.unit == u {
				found = c.index
				break
			}
		}
		if found < 0 {
			found = len(l.nodes)
			l.nodes = append(l.nodes, lexNode{})
			l.nodes[n].children = append(l.nodes[n].children, lexChild{unit: u, index: found})
		}
		n = found
	}
	l.nodes[n].entries = append(l.nodes[n].entries, index)
}

// Len 回傳詞條數。
func (l *Lexicon) Len() int { return len(l.entries) }

// LongestWord 回傳最長讀音的單位數。
func (l *Lexicon) LongestWord() int { return l.longest }

// Entry 回傳第 i 筆詞條。
func (l *Lexicon) Entry(i int) Entry { return l.entries[i] }

// Entries 回傳全部詞條的副本。
func (l *Lexicon) Entries() []Entry {
	return append([]Entry(nil), l.entries...)
}

// Matrix 回傳連接成本表。
func (l *Lexicon) Matrix() *Matrix { return l.matrix }

// EncodedSize 是編碼後的位元組數；解碼時也會填入。
func (l *Lexicon) EncodedSize() int { return l.encoded }

// RuntimeBytesEstimate 是解碼後常駐記憶體的保守估計，供打包報告使用。
// 以編碼大小乘上 2，加上每筆詞條的切片與 map 開銷。
func (l *Lexicon) RuntimeBytesEstimate() int {
	return l.encoded*2 + len(l.entries)*64 + len(l.nodes)*48
}

func (l *Lexicon) node(seq reading.Sequence) int {
	n := 0
	for _, u := range seq {
		found := -1
		for _, c := range l.nodes[n].children {
			if c.unit == u {
				found = c.index
				break
			}
		}
		if found < 0 {
			return -1
		}
		n = found
	}
	return n
}

// Exact 回傳讀音恰為 seq 的詞條。
func (l *Lexicon) Exact(seq reading.Sequence) []Entry {
	n := l.node(seq)
	if n < 0 {
		return nil
	}
	out := make([]Entry, 0, len(l.nodes[n].entries))
	for _, i := range l.nodes[n].entries {
		out = append(out, l.entries[i])
	}
	return out
}

// Prefixed 回傳讀音以 seq 開頭的詞條，含恰好相等者。
func (l *Lexicon) Prefixed(seq reading.Sequence) []Entry {
	n := l.node(seq)
	if n < 0 {
		return nil
	}
	var out []Entry
	var walk func(int)
	walk = func(i int) {
		for _, idx := range l.nodes[i].entries {
			out = append(out, l.entries[idx])
		}
		for _, c := range l.nodes[i].children {
			walk(c.index)
		}
	}
	walk(n)
	return out
}

// HasPrefix 回報是否有讀音以 seq 開頭。
func (l *Lexicon) HasPrefix(seq reading.Sequence) bool { return l.node(seq) >= 0 }

// Matches 走訪 seq[start:] 的所有詞條前綴。
func (l *Lexicon) Matches(seq reading.Sequence, start int, fn func(end int, e Entry)) {
	if start < 0 || start > len(seq) {
		return
	}
	n := 0
	for end := start; end < len(seq); end++ {
		found := -1
		for _, c := range l.nodes[n].children {
			if c.unit == seq[end] {
				found = c.index
				break
			}
		}
		if found < 0 {
			return
		}
		n = found
		for _, idx := range l.nodes[n].entries {
			fn(end+1, l.entries[idx])
		}
	}
}

// Lookup 回傳詞面的所有讀音。
func (l *Lexicon) Lookup(surface string) []Entry {
	var out []Entry
	for _, i := range l.surfaces[surface] {
		out = append(out, l.entries[i])
	}
	return out
}

// Has 回報詞面是否存在。
func (l *Lexicon) Has(surface string) bool { return len(l.surfaces[surface]) > 0 }

// Walk 依讀音排序走訪所有詞條。
func (l *Lexicon) Walk(fn func(Entry)) {
	for _, e := range l.entries {
		fn(e)
	}
}

// Encode 把詞典編成可重現的二進位資料包。
func (l *Lexicon) Encode() []byte {
	var b bytes.Buffer
	b.WriteString(lexiconMagic)
	putUvarint(&b, uint64(l.longest))
	putUvarint(&b, uint64(len(l.entries)))
	for _, e := range l.entries {
		putString(&b, e.Surface)
		putReading(&b, e.Reading)
		putFloat(&b, e.Cost)
		putUvarint(&b, uint64(mustPOSIndex(e.POS)))
		putUvarint(&b, uint64(conjIndex(e.Conjugation)))
		putUvarint(&b, uint64(e.Okurigana))
	}
	surfaces := make([]string, 0, len(l.surfaces))
	for s := range l.surfaces {
		surfaces = append(surfaces, s)
	}
	sort.Strings(surfaces)
	putUvarint(&b, uint64(len(surfaces)))
	for _, s := range surfaces {
		putString(&b, s)
		idxs := l.surfaces[s]
		putUvarint(&b, uint64(len(idxs)))
		for _, i := range idxs {
			putUvarint(&b, uint64(i))
		}
	}
	putUvarint(&b, uint64(len(l.nodes)))
	for _, n := range l.nodes {
		putUvarint(&b, uint64(len(n.entries)))
		for _, i := range n.entries {
			putUvarint(&b, uint64(i))
		}
		putUvarint(&b, uint64(len(n.children)))
		for _, c := range n.children {
			putString(&b, string(c.unit))
			putUvarint(&b, uint64(c.index))
		}
	}
	conns := l.matrix.Connections()
	putUvarint(&b, uint64(len(conns)))
	for _, c := range conns {
		putUvarint(&b, uint64(mustConnPOSIndex(c.Left)))
		putUvarint(&b, uint64(mustConnPOSIndex(c.Right)))
		putFloat(&b, c.Cost)
	}
	putFloat(&b, l.matrix.fallback)
	l.encoded = b.Len()
	return b.Bytes()
}

// DecodeLexicon 解碼 lexicon.bin。格式錯誤回傳錯誤，不嘗試修補。
func DecodeLexicon(data []byte) (*Lexicon, error) {
	r := bytes.NewReader(data)
	magic := make([]byte, len(lexiconMagic))
	if n, err := r.Read(magic); err != nil || n != len(magic) || string(magic) != lexiconMagic {
		return nil, fmt.Errorf("不是 0c 日文詞典檔")
	}
	longest, err := readUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("最長讀音：%w", err)
	}
	n, err := readUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("詞條數：%w", err)
	}
	if n > uint64(len(data)) { // 每筆至少 1 位元組，避免配置過大
		return nil, fmt.Errorf("詞條數 %d 不合法", n)
	}
	l := &Lexicon{entries: make([]Entry, n), surfaces: map[string][]int{}, longest: int(longest)}
	for i := range l.entries {
		e := Entry{}
		if e.Surface, err = readString(r); err != nil {
			return nil, fmt.Errorf("第 %d 筆詞面：%w", i+1, err)
		}
		if e.Reading, err = readReading(r); err != nil {
			return nil, fmt.Errorf("第 %d 筆讀音：%w", i+1, err)
		}
		if e.Cost, err = readFloat(r); err != nil {
			return nil, fmt.Errorf("第 %d 筆成本：%w", i+1, err)
		}
		pi, err := readUvarint(r)
		if err != nil || pi >= uint64(len(posList)) {
			return nil, fmt.Errorf("第 %d 筆詞性不合法", i+1)
		}
		e.POS = posList[pi]
		ci, err := readUvarint(r)
		if err != nil || ci >= uint64(len(conjList)) {
			return nil, fmt.Errorf("第 %d 筆活用型不合法", i+1)
		}
		e.Conjugation = conjList[ci]
		ok, err := readUvarint(r)
		if err != nil {
			return nil, fmt.Errorf("第 %d 筆送り仮名：%w", i+1, err)
		}
		e.Okurigana = int(ok)
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("第 %d 筆詞條：%w", i+1, err)
		}
		l.entries[i] = e
	}
	sn, err := readUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("詞面索引：%w", err)
	}
	for i := uint64(0); i < sn; i++ {
		s, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("詞面索引：%w", err)
		}
		c, err := readUvarint(r)
		if err != nil || c > n {
			return nil, fmt.Errorf("詞面 %q 的索引不合法", s)
		}
		idxs := make([]int, c)
		for j := range idxs {
			v, err := readUvarint(r)
			if err != nil || v >= n {
				return nil, fmt.Errorf("詞面 %q 的詞條索引不合法", s)
			}
			idxs[j] = int(v)
		}
		l.surfaces[s] = idxs
	}
	nn, err := readUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("讀音 trie：%w", err)
	}
	if nn == 0 || nn > uint64(len(data)) {
		return nil, fmt.Errorf("讀音 trie 節點數 %d 不合法", nn)
	}
	l.nodes = make([]lexNode, nn)
	for i := range l.nodes {
		ec, err := readUvarint(r)
		if err != nil {
			return nil, fmt.Errorf("trie 節點 %d：%w", i, err)
		}
		l.nodes[i].entries = make([]int, ec)
		for j := range l.nodes[i].entries {
			v, err := readUvarint(r)
			if err != nil || v >= n {
				return nil, fmt.Errorf("trie 節點 %d 的詞條索引不合法", i)
			}
			l.nodes[i].entries[j] = int(v)
		}
		cc, err := readUvarint(r)
		if err != nil {
			return nil, fmt.Errorf("trie 節點 %d：%w", i, err)
		}
		l.nodes[i].children = make([]lexChild, cc)
		for j := range l.nodes[i].children {
			s, err := readString(r)
			if err != nil {
				return nil, fmt.Errorf("trie 子節點讀音：%w", err)
			}
			child, err := readUvarint(r)
			if err != nil || child >= nn || child <= uint64(i) {
				return nil, fmt.Errorf("trie 節點 %d 的子節點索引不合法", i)
			}
			l.nodes[i].children[j] = lexChild{unit: reading.Unit(s), index: int(child)}
		}
	}
	cn, err := readUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("連接成本：%w", err)
	}
	if cn > uint64(len(data)) {
		return nil, fmt.Errorf("連接成本數量 %d 不合法", cn)
	}
	conns := make([]Connection, cn)
	for i := range conns {
		li, err := readUvarint(r)
		if err != nil || li >= uint64(len(connPOSList)) {
			return nil, fmt.Errorf("連接成本左詞性不合法")
		}
		ri, err := readUvarint(r)
		if err != nil || ri >= uint64(len(connPOSList)) {
			return nil, fmt.Errorf("連接成本右詞性不合法")
		}
		cost, err := readFloat(r)
		if err != nil || math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
			return nil, fmt.Errorf("連接成本不合法")
		}
		conns[i] = Connection{Left: connPOSList[li], Right: connPOSList[ri], Cost: cost}
	}
	fallback, err := readFloat(r)
	if err != nil {
		return nil, fmt.Errorf("連接成本保底值：%w", err)
	}
	l.matrix = BuildMatrix(conns)
	l.matrix.SetFallback(fallback)
	if r.Len() != 0 {
		return nil, fmt.Errorf("詞典檔尾端有 %d 位元組未使用", r.Len())
	}
	l.encoded = len(data)
	return l, nil
}

// LM 是日文語言模型：詞面 unigram 成本與詞面 bigram 成本（越低越好）。
type LM struct {
	Unigram map[string]float64
	Bigram  map[[2]string]float64
	encoded int
}

// NewLM 建立語言模型；nil map 會初始化。
func NewLM(unigram map[string]float64, bigram map[[2]string]float64) *LM {
	if unigram == nil {
		unigram = map[string]float64{}
	}
	if bigram == nil {
		bigram = map[[2]string]float64{}
	}
	return &LM{Unigram: unigram, Bigram: bigram}
}

// Len 回傳 unigram 詞數。
func (m *LM) Len() int { return len(m.Unigram) }

// BigramLen 回傳 bigram 筆數。
func (m *LM) BigramLen() int { return len(m.Bigram) }

// EncodedSize 是編碼後的位元組數。
func (m *LM) EncodedSize() int { return m.encoded }

// RuntimeBytesEstimate 是解碼後常駐記憶體的保守估計。
func (m *LM) RuntimeBytesEstimate() int { return m.encoded*2 + len(m.Unigram)*64 + len(m.Bigram)*96 }

// Encode 把語言模型編成可重現的二進位資料。
func (m *LM) Encode() []byte {
	var b bytes.Buffer
	b.WriteString(lmMagic)
	keys := make([]string, 0, len(m.Unigram))
	for k := range m.Unigram {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	putUvarint(&b, uint64(len(keys)))
	for _, k := range keys {
		putString(&b, k)
		putFloat(&b, m.Unigram[k])
	}
	pairs := make([][2]string, 0, len(m.Bigram))
	for k := range m.Bigram {
		pairs = append(pairs, k)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	putUvarint(&b, uint64(len(pairs)))
	for _, k := range pairs {
		putString(&b, k[0])
		putString(&b, k[1])
		putFloat(&b, m.Bigram[k])
	}
	m.encoded = b.Len()
	return b.Bytes()
}

// DecodeLM 解碼 lm.bin。
func DecodeLM(data []byte) (*LM, error) {
	r := bytes.NewReader(data)
	magic := make([]byte, len(lmMagic))
	if n, err := r.Read(magic); err != nil || n != len(magic) || string(magic) != lmMagic {
		return nil, fmt.Errorf("不是 0c 日文語言模型檔")
	}
	n, err := readUvarint(r)
	if err != nil || n > uint64(len(data)) {
		return nil, fmt.Errorf("unigram 數量不合法")
	}
	m := NewLM(map[string]float64{}, map[[2]string]float64{})
	for i := uint64(0); i < n; i++ {
		k, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("unigram 詞面：%w", err)
		}
		v, err := readFloat(r)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return nil, fmt.Errorf("unigram %q 成本不合法", k)
		}
		m.Unigram[k] = v
	}
	bn, err := readUvarint(r)
	if err != nil || bn > uint64(len(data)) {
		return nil, fmt.Errorf("bigram 數量不合法")
	}
	for i := uint64(0); i < bn; i++ {
		a, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("bigram 左詞：%w", err)
		}
		b, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("bigram 右詞：%w", err)
		}
		v, err := readFloat(r)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return nil, fmt.Errorf("bigram %q→%q 成本不合法", a, b)
		}
		m.Bigram[[2]string{a, b}] = v
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("語言模型檔尾端有 %d 位元組未使用", r.Len())
	}
	m.encoded = len(data)
	return m, nil
}

func mustPOSIndex(p POS) int {
	i, ok := posIndex(p)
	if !ok {
		panic("jaimport: 未定義詞性 " + string(p))
	}
	return i
}

func mustConnPOSIndex(p POS) int {
	for i, x := range connPOSList {
		if x == p {
			return i
		}
	}
	panic("jaimport: 未定義連接詞性 " + string(p))
}

func putUvarint(b *bytes.Buffer, v uint64) {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	b.Write(tmp[:n])
}

func putString(b *bytes.Buffer, s string) {
	putUvarint(b, uint64(len(s)))
	b.WriteString(s)
}

func putReading(b *bytes.Buffer, seq reading.Sequence) {
	putUvarint(b, uint64(len(seq)))
	for _, u := range seq {
		putString(b, string(u))
	}
}

func putFloat(b *bytes.Buffer, f float64) {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], math.Float64bits(f))
	b.Write(tmp[:])
}

func readUvarint(r *bytes.Reader) (uint64, error) {
	return binary.ReadUvarint(r)
}

func readString(r *bytes.Reader) (string, error) {
	n, err := readUvarint(r)
	if err != nil {
		return "", err
	}
	if n > uint64(r.Len()) {
		return "", fmt.Errorf("字串長度 %d 超出資料", n)
	}
	buf := make([]byte, n)
	if _, err := r.Read(buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readReading(r *bytes.Reader) (reading.Sequence, error) {
	n, err := readUvarint(r)
	if err != nil {
		return nil, err
	}
	if n > uint64(r.Len()) {
		return nil, fmt.Errorf("讀音長度 %d 超出資料", n)
	}
	seq := make(reading.Sequence, n)
	for i := range seq {
		s, err := readString(r)
		if err != nil {
			return nil, err
		}
		seq[i] = reading.Unit(s)
	}
	return seq, nil
}

func readFloat(r *bytes.Reader) (float64, error) {
	var tmp [8]byte
	n, err := r.Read(tmp[:])
	if err != nil {
		return 0, err
	}
	if n != len(tmp) {
		return 0, fmt.Errorf("浮點數不足 8 位元組")
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(tmp[:])), nil
}
