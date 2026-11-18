package lexicon

// 這個檔案實作 lexicon.bin（magic 0CJLEX01）的執行期載入器。
//
// 舊路徑 lexicon.Load 會為每一筆詞條建立 Go 字串、讀音切片與 trie 的 map，
// 全量詞典常駐數百 MiB。Bin 改把同一份二進位檔拆成幾個大陣列：
//
//   - 詞面與讀音各自放在一條連續字串，詞條只存 offset 與長度；
//   - 讀音 trie 的節點、邊與詞條索引放在四個平鋪切片；
//   - 詞面索引是排序後的詞條編號。
//
// 載入時逐段讀檔並驗證，不保留原始 63 MiB 檔案，也不建立逐詞的 Go 字串。
// 對外語意與 Mem 相同：Exact、Prefixed、HasPrefix、Matches、Lookup、Has、
// Walk 與連接成本查詢都回傳相同順序與內容。

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/reading"
)

const binLexiconMagic = "0CJLEX01"

// 損壞的檔案不可以讓載入器配置無上限的記憶體。上限遠高於產品詞典
// （約 92 萬筆、79 萬個 trie 節點）。
const (
	maxBinEntries     = 1 << 22
	maxBinNodes       = 1 << 23
	maxBinConnections = 1 << 20
	maxBinStringBytes = 1<<16 - 1
)

// binPOSList 的順序必須與 internal/jaimport 的 posList 完全相同；二進位檔只
// 存索引。bin_test.go 會用 jaimport 產生的檔案驗證每一個詞性。
var binPOSList = []POS{
	POSOther, POSNoun, POSVerb, POSAdjective, POSAdjectivalNoun, POSAdverb,
	POSParticle, POSAuxiliary, POSConjunction, POSPrefix, POSSuffix,
	POSDeterminer, POSInterjection, POSNumber, POSSymbol,
}

// binConnPOSList 對應連接成本矩陣可用的詞性，額外包含句首與句尾。
var binConnPOSList = append(append([]POS(nil), binPOSList...), POSBOS, POSEOS)

// binEntry 是一筆緊湊詞條。surfaceOff/readingOff 指向 Bin 的連續字串。
type binEntry struct {
	cost       float64
	surfaceOff uint32
	readingOff uint32
	surfaceLen uint16
	readingLen uint16
	pos        uint8
}

// binNode 是讀音 trie 的一個節點。詞條索引與子節點都放在平鋪切片裡，
// 避免每個節點各帶一個 slice header。
type binNode struct {
	entryStart uint32
	entryCount uint32
	childStart uint32
	childCount uint32
}

// binChild 是一條 trie 邊。日文讀音單位是一個かな字元，所以存 rune 就夠。
type binChild struct {
	unit  rune
	child uint32
}

// Bin 是從 lexicon.bin 解出的緊湊日文詞典，實作 Source。
type Bin struct {
	entries      []binEntry
	surfaces     string
	readings     string
	nodes        []binNode
	nodeEntries  []uint32
	nodeChildren []binChild
	surfaceIndex []uint32
	units        map[rune]reading.Unit
	matrix       *Matrix
	longest      int
}

var _ Source = (*Bin)(nil)

// LoadBin 從 r 載入 lexicon.bin。只接受完整格式，尾端多餘位元組視為錯誤。
// 檔案開啟由宿主負責，core 不碰檔案系統。
func LoadBin(r io.Reader) (*Bin, error) { return loadBin(r, -1) }

// LoadBinSize 與 LoadBin 相同，但已知輸入大小時可先擋掉誇大的計數，
// 避免損壞檔讓載入器配置遠大於檔案的記憶體。
func LoadBinSize(r io.Reader, size int64) (*Bin, error) { return loadBin(r, size) }

func loadBin(r io.Reader, size int64) (*Bin, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	magic := make([]byte, len(binLexiconMagic))
	if _, err := io.ReadFull(br, magic); err != nil {
		return nil, fmt.Errorf("日文詞典：讀取檔頭：%w", err)
	}
	if string(magic) != binLexiconMagic {
		return nil, fmt.Errorf("不是 0c 日文詞典檔")
	}

	longest, err := readBinUvarint(br)
	if err != nil {
		return nil, fmt.Errorf("日文詞典：最長讀音：%w", err)
	}
	if longest == 0 || longest > maxBinStringBytes {
		return nil, fmt.Errorf("日文詞典：最長讀音 %d 不合法", longest)
	}
	count, err := readBinUvarint(br)
	if err != nil {
		return nil, fmt.Errorf("日文詞典：詞條數：%w", err)
	}
	if count == 0 || count > maxBinEntries || size >= 0 && count > uint64(size)/16 {
		return nil, fmt.Errorf("日文詞典：詞條數 %d 不合法", count)
	}

	var surfaces, readings strings.Builder
	surfaceCap := int(count) * 12
	if surfaceCap > 128<<20 {
		surfaceCap = 128 << 20
	}
	surfaces.Grow(surfaceCap)
	readingCap := int(count) * 15
	if readingCap > 128<<20 {
		readingCap = 128 << 20
	}
	readings.Grow(readingCap)

	b := &Bin{
		entries: make([]binEntry, count),
		units:   map[rune]reading.Unit{},
		longest: int(longest),
	}
	surfaceScratch := make([]byte, 0, 64)
	unitScratch := make([]byte, 0, 8)
	for i := range b.entries {
		e := &b.entries[i]

		raw, _, err := readBinStringInto(br, surfaceScratch, maxBinStringBytes)
		if err != nil {
			return nil, fmt.Errorf("日文詞典：第 %d 筆詞面：%w", i+1, err)
		}
		surfaceScratch = raw
		if len(raw) == 0 || !utf8.Valid(raw) {
			return nil, fmt.Errorf("日文詞典：第 %d 筆詞面不合法", i+1)
		}
		e.surfaceOff = uint32(surfaces.Len())
		e.surfaceLen = uint16(len(raw))
		surfaces.Write(raw)

		units, err := readBinUvarint(br)
		if err != nil {
			return nil, fmt.Errorf("日文詞典：第 %d 筆讀音：%w", i+1, err)
		}
		if units == 0 || units > longest {
			return nil, fmt.Errorf("日文詞典：第 %d 筆讀音長度 %d 不合法", i+1, units)
		}
		e.readingOff = uint32(readings.Len())
		for j := uint64(0); j < units; j++ {
			raw, _, err := readBinStringInto(br, unitScratch, 8)
			if err != nil {
				return nil, fmt.Errorf("日文詞典：第 %d 筆讀音：%w", i+1, err)
			}
			unitScratch = raw
			if _, ok := binUnit(b.units, raw); !ok {
				return nil, fmt.Errorf("日文詞典：第 %d 筆讀音單位不合法", i+1)
			}
			readings.Write(raw)
		}
		e.readingLen = uint16(readings.Len() - int(e.readingOff))

		cost, err := readBinFloat(br)
		if err != nil {
			return nil, fmt.Errorf("日文詞典：第 %d 筆成本：%w", i+1, err)
		}
		if !validCost(cost) {
			return nil, fmt.Errorf("日文詞典：第 %d 筆成本 %v 超出容許範圍", i+1, cost)
		}
		e.cost = cost

		pi, err := readBinUvarint(br)
		if err != nil || pi >= uint64(len(binPOSList)) {
			return nil, fmt.Errorf("日文詞典：第 %d 筆詞性不合法", i+1)
		}
		e.pos = uint8(pi)
		if _, err := readBinUvarint(br); err != nil { // 活用型，執行期不使用
			return nil, fmt.Errorf("日文詞典：第 %d 筆活用型：%w", i+1, err)
		}
		okuri, err := readBinUvarint(br)
		if err != nil || okuri > units {
			return nil, fmt.Errorf("日文詞典：第 %d 筆送り仮名不合法", i+1)
		}
	}
	b.surfaces = surfaces.String()
	b.readings = readings.String()

	if err := b.loadSurfaceIndex(br, count); err != nil {
		return nil, err
	}
	if err := b.loadTrie(br, count, size); err != nil {
		return nil, err
	}
	if err := b.loadConnections(br); err != nil {
		return nil, err
	}
	if _, err := br.ReadByte(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("日文詞典：檔尾有未使用的位元組")
		}
		return nil, fmt.Errorf("日文詞典：讀取檔尾：%w", err)
	}
	return b, nil
}

func (b *Bin) loadSurfaceIndex(br *bufio.Reader, count uint64) error {
	sn, err := readBinUvarint(br)
	if err != nil {
		return fmt.Errorf("日文詞典：詞面索引：%w", err)
	}
	if sn == 0 || sn > count {
		return fmt.Errorf("日文詞典：詞面索引數量 %d 不合法", sn)
	}
	b.surfaceIndex = make([]uint32, 0, count)
	seen := make([]uint64, (count+63)/64)
	scratch := make([]byte, 0, 64)
	prevKey := make([]byte, 0, 64)
	haveKey := false
	for i := uint64(0); i < sn; i++ {
		scratch, _, err = readBinStringInto(br, scratch, maxBinStringBytes)
		if err != nil {
			return fmt.Errorf("日文詞典：詞面索引：%w", err)
		}
		if len(scratch) == 0 || !utf8.Valid(scratch) {
			return fmt.Errorf("日文詞典：詞面索引的詞面不合法")
		}
		if haveKey && bytes.Compare(prevKey, scratch) >= 0 {
			return fmt.Errorf("日文詞典：詞面索引未依序排列")
		}
		prevKey = append(prevKey[:0], scratch...)
		haveKey = true
		c, err := readBinUvarint(br)
		if err != nil {
			return fmt.Errorf("日文詞典：詞面索引：%w", err)
		}
		if c == 0 || c > count {
			return fmt.Errorf("日文詞典：詞面 %q 的索引數量不合法", scratch)
		}
		for j := uint64(0); j < c; j++ {
			v, err := readBinUvarint(br)
			if err != nil {
				return fmt.Errorf("日文詞典：詞面索引：%w", err)
			}
			if v >= count {
				return fmt.Errorf("日文詞典：詞面 %q 的詞條索引不合法", scratch)
			}
			idx := uint32(v)
			if !equalStringBytes(b.surfaceAt(idx), scratch) {
				return fmt.Errorf("日文詞典：詞面索引與第 %d 筆詞條不符", idx)
			}
			if seen[idx/64]&(1<<(idx%64)) != 0 {
				return fmt.Errorf("日文詞典：第 %d 筆詞條在詞面索引重複", idx)
			}
			seen[idx/64] |= 1 << (idx % 64)
			b.surfaceIndex = append(b.surfaceIndex, idx)
		}
	}
	if uint64(len(b.surfaceIndex)) != count {
		return fmt.Errorf("日文詞典：詞面索引涵蓋 %d 筆，want %d", len(b.surfaceIndex), count)
	}
	return nil
}

func (b *Bin) loadTrie(br *bufio.Reader, count uint64, size int64) error {
	nn, err := readBinUvarint(br)
	if err != nil {
		return fmt.Errorf("日文詞典：讀音 trie：%w", err)
	}
	if nn == 0 || nn > maxBinNodes || size >= 0 && nn > uint64(size)/3 {
		return fmt.Errorf("日文詞典：讀音 trie 節點數 %d 不合法", nn)
	}
	b.nodes = make([]binNode, nn)
	b.nodeEntries = make([]uint32, 0, count)
	b.nodeChildren = make([]binChild, 0, nn)
	entriesSeen := make([]uint64, (count+63)/64)
	parentsSeen := make([]uint64, (nn+63)/64)
	var entryCount, edgeCount uint64
	scratch := make([]byte, 0, 8)
	for i := uint64(0); i < nn; i++ {
		n := &b.nodes[i]
		ec, err := readBinUvarint(br)
		if err != nil {
			return fmt.Errorf("日文詞典：trie 節點 %d：%w", i, err)
		}
		if ec > count || entryCount+ec > count {
			return fmt.Errorf("日文詞典：trie 節點 %d 的詞條數不合法", i)
		}
		n.entryStart = uint32(len(b.nodeEntries))
		n.entryCount = uint32(ec)
		for j := uint64(0); j < ec; j++ {
			v, err := readBinUvarint(br)
			if err != nil {
				return fmt.Errorf("日文詞典：trie 節點 %d：%w", i, err)
			}
			if v >= count {
				return fmt.Errorf("日文詞典：trie 節點 %d 的詞條索引不合法", i)
			}
			if j > 0 && uint32(v) <= b.nodeEntries[len(b.nodeEntries)-1] {
				return fmt.Errorf("日文詞典：trie 節點 %d 的詞條索引未排序", i)
			}
			if entriesSeen[v/64]&(1<<(v%64)) != 0 {
				return fmt.Errorf("日文詞典：第 %d 筆詞條在 trie 重複", v)
			}
			entriesSeen[v/64] |= 1 << (v % 64)
			entryCount++
			b.nodeEntries = append(b.nodeEntries, uint32(v))
		}

		cc, err := readBinUvarint(br)
		if err != nil {
			return fmt.Errorf("日文詞典：trie 節點 %d：%w", i, err)
		}
		if cc > nn || edgeCount+cc > nn-1 {
			return fmt.Errorf("日文詞典：trie 節點 %d 的子節點數不合法", i)
		}
		n.childStart = uint32(len(b.nodeChildren))
		n.childCount = uint32(cc)
		prev := rune(-1)
		for j := uint64(0); j < cc; j++ {
			scratch, _, err = readBinStringInto(br, scratch, 8)
			if err != nil {
				return fmt.Errorf("日文詞典：trie 節點 %d 的讀音：%w", i, err)
			}
			unit, ok := binUnit(b.units, scratch)
			if !ok {
				return fmt.Errorf("日文詞典：trie 節點 %d 的讀音不合法", i)
			}
			unitRune, size := utf8.DecodeRuneInString(string(unit))
			if size != len(unit) {
				return fmt.Errorf("日文詞典：trie 節點 %d 的讀音不是單一單位", i)
			}
			if j > 0 && unitRune <= prev {
				return fmt.Errorf("日文詞典：trie 節點 %d 的子節點未排序", i)
			}
			prev = unitRune
			child, err := readBinUvarint(br)
			if err != nil {
				return fmt.Errorf("日文詞典：trie 節點 %d：%w", i, err)
			}
			if child == 0 || child <= i || child >= nn {
				return fmt.Errorf("日文詞典：trie 節點 %d 的子節點索引不合法", i)
			}
			if parentsSeen[child/64]&(1<<(child%64)) != 0 {
				return fmt.Errorf("日文詞典：trie 節點 %d 有多個父節點", child)
			}
			parentsSeen[child/64] |= 1 << (child % 64)
			edgeCount++
			b.nodeChildren = append(b.nodeChildren, binChild{unit: unitRune, child: uint32(child)})
		}
	}
	if entryCount != count {
		return fmt.Errorf("日文詞典：trie 涵蓋 %d 筆詞條，want %d", entryCount, count)
	}
	if edgeCount != nn-1 {
		return fmt.Errorf("日文詞典：trie 邊數 %d 與節點數 %d 不符", edgeCount, nn)
	}
	return nil
}

func (b *Bin) loadConnections(br *bufio.Reader) error {
	cn, err := readBinUvarint(br)
	if err != nil {
		return fmt.Errorf("日文詞典：連接成本：%w", err)
	}
	if cn > maxBinConnections {
		return fmt.Errorf("日文詞典：連接成本數量 %d 不合法", cn)
	}
	m := NewMatrix()
	for i := uint64(0); i < cn; i++ {
		li, err := readBinUvarint(br)
		if err != nil || li >= uint64(len(binConnPOSList)) {
			return fmt.Errorf("日文詞典：連接成本左詞性不合法")
		}
		ri, err := readBinUvarint(br)
		if err != nil || ri >= uint64(len(binConnPOSList)) {
			return fmt.Errorf("日文詞典：連接成本右詞性不合法")
		}
		cost, err := readBinFloat(br)
		if err != nil || math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
			return fmt.Errorf("日文詞典：連接成本不合法")
		}
		m.Set(binConnPOSList[li], binConnPOSList[ri], cost)
	}
	fallback, err := readBinFloat(br)
	if err != nil || math.IsNaN(fallback) || math.IsInf(fallback, 0) || fallback < 0 {
		return fmt.Errorf("日文詞典：連接成本保底值不合法")
	}
	m.SetFallback(fallback)
	b.matrix = m
	return nil
}

func (b *Bin) surfaceAt(i uint32) string {
	e := &b.entries[i]
	return b.surfaces[e.surfaceOff : uint32(e.surfaceOff)+uint32(e.surfaceLen)]
}

func (b *Bin) readingAt(i uint32) string {
	e := &b.entries[i]
	return b.readings[e.readingOff : uint32(e.readingOff)+uint32(e.readingLen)]
}

// entry 把緊湊詞條轉成公開的 Entry。讀音單位取自共用的 intern 表，
// 所以只有回傳的切片需要配置，不會為每個字元建立新字串。
func (b *Bin) entry(i uint32) Entry {
	e := &b.entries[i]
	raw := b.readingAt(i)
	seq := make(reading.Sequence, 0, utf8.RuneCountInString(raw))
	for len(raw) > 0 {
		r, size := utf8.DecodeRuneInString(raw)
		u, ok := b.units[r]
		if !ok {
			u = reading.Unit(r)
		}
		seq = append(seq, u)
		raw = raw[size:]
	}
	return Entry{
		Surface: b.surfaceAt(i),
		Reading: seq,
		Cost:    e.cost,
		POS:     binPOSList[e.pos],
	}
}

func (b *Bin) child(node uint32, u reading.Unit) (uint32, bool) {
	r, size := utf8.DecodeRuneInString(string(u))
	if size == 0 || size != len(u) {
		return 0, false
	}
	n := &b.nodes[node]
	lo, hi := int(n.childStart), int(n.childStart+n.childCount)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if b.nodeChildren[mid].unit < r {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= int(n.childStart+n.childCount) || b.nodeChildren[lo].unit != r {
		return 0, false
	}
	return b.nodeChildren[lo].child, true
}

func (b *Bin) node(seq reading.Sequence) (uint32, bool) {
	n := uint32(0)
	for _, u := range seq {
		c, ok := b.child(n, u)
		if !ok {
			return 0, false
		}
		n = c
	}
	return n, true
}

func (b *Bin) entriesAt(node uint32) []Entry {
	n := &b.nodes[node]
	if n.entryCount == 0 {
		return nil
	}
	out := make([]Entry, 0, n.entryCount)
	for _, idx := range b.nodeEntries[n.entryStart : n.entryStart+n.entryCount] {
		out = append(out, b.entry(idx))
	}
	return out
}

// LongestWord 回傳最長的詞條讀音單位數。
func (b *Bin) LongestWord() int { return b.longest }

// Len 回傳詞條數。
func (b *Bin) Len() int { return len(b.entries) }

// Matrix 回傳 lexicon.bin 內附的連接成本矩陣。
func (b *Bin) Matrix() *Matrix { return b.matrix }

// Exact 回傳讀音恰為 seq 的詞條。
func (b *Bin) Exact(seq reading.Sequence) []Entry {
	n, ok := b.node(seq)
	if !ok {
		return nil
	}
	return b.entriesAt(n)
}

// Prefixed 回傳讀音以 seq 開頭的詞條。順序與 Mem 相同：先節點自身詞條，
// 再依讀音單位排序走訪子樹。
func (b *Bin) Prefixed(seq reading.Sequence) []Entry {
	n, ok := b.node(seq)
	if !ok {
		return nil
	}
	var out []Entry
	stack := []uint32{n}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		nd := &b.nodes[node]
		for _, idx := range b.nodeEntries[nd.entryStart : nd.entryStart+nd.entryCount] {
			out = append(out, b.entry(idx))
		}
		for j := nd.childCount; j > 0; j-- {
			stack = append(stack, b.nodeChildren[nd.childStart+j-1].child)
		}
	}
	return out
}

// HasPrefix 回報是否有讀音以 seq 開頭的詞條。
func (b *Bin) HasPrefix(seq reading.Sequence) bool {
	_, ok := b.node(seq)
	return ok
}

// Matches 走訪 seq[start:] 的所有詞條前綴。詞條依讀音順序回報。
func (b *Bin) Matches(seq reading.Sequence, start int, fn func(end int, e Entry)) {
	if start < 0 || start > len(seq) {
		return
	}
	n := uint32(0)
	for end := start; end < len(seq); end++ {
		c, ok := b.child(n, seq[end])
		if !ok {
			return
		}
		n = c
		nd := &b.nodes[n]
		for _, idx := range b.nodeEntries[nd.entryStart : nd.entryStart+nd.entryCount] {
			fn(end+1, b.entry(idx))
		}
	}
}

// Lookup 回傳詞面的所有讀音，順序與 Mem 相同。
func (b *Bin) Lookup(surface string) []Entry {
	i := sort.Search(len(b.surfaceIndex), func(i int) bool {
		return b.surfaceAt(b.surfaceIndex[i]) >= surface
	})
	var out []Entry
	for ; i < len(b.surfaceIndex) && b.surfaceAt(b.surfaceIndex[i]) == surface; i++ {
		out = append(out, b.entry(b.surfaceIndex[i]))
	}
	return out
}

// Has 回報詞面是否存在。
func (b *Bin) Has(surface string) bool {
	i := sort.Search(len(b.surfaceIndex), func(i int) bool {
		return b.surfaceAt(b.surfaceIndex[i]) >= surface
	})
	return i < len(b.surfaceIndex) && b.surfaceAt(b.surfaceIndex[i]) == surface
}

// Walk 依讀音順序走訪所有詞條。
func (b *Bin) Walk(fn func(Entry)) {
	for i := range b.entries {
		fn(b.entry(uint32(i)))
	}
}

func readBinUvarint(br *bufio.Reader) (uint64, error) {
	return binary.ReadUvarint(br)
}

func readBinFloat(br *bufio.Reader) (float64, error) {
	var tmp [8]byte
	if _, err := io.ReadFull(br, tmp[:]); err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(tmp[:])), nil
}

// readBinStringInto 讀一個長度前綴字串到可重用的緩衝，回傳切片。
// 回傳值可能與 buf 共用記憶體；呼叫端必須在下次讀取前用完。
func readBinStringInto(br *bufio.Reader, buf []byte, max int) ([]byte, int, error) {
	n, err := readBinUvarint(br)
	if err != nil {
		return nil, 0, err
	}
	if n > uint64(max) {
		return nil, 0, fmt.Errorf("字串長度 %d 不合法", n)
	}
	if int(n) > cap(buf) {
		buf = make([]byte, n)
	} else {
		buf = buf[:n]
	}
	if _, err := io.ReadFull(br, buf); err != nil {
		return nil, 0, err
	}
	return buf, int(n), nil
}

func equalStringBytes(s string, p []byte) bool {
	if len(s) != len(p) {
		return false
	}
	for i := range p {
		if s[i] != p[i] {
			return false
		}
	}
	return true
}

// binUnit 把一個讀音單位的位元組轉成 reading.Unit，同一個字元只建立一次。
// 只接受單一かな字元，與 jaimport.NormalizeReading 的輸出契約一致。
func binUnit(table map[rune]reading.Unit, raw []byte) (reading.Unit, bool) {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return "", false
	}
	r, size := utf8.DecodeRune(raw)
	if size != len(raw) || (r == utf8.RuneError && size == 1) {
		return "", false
	}
	if u, ok := table[r]; ok {
		return u, u.Valid()
	}
	u := reading.Unit(string(raw))
	if !u.Valid() {
		return "", false
	}
	table[r] = u
	return u, true
}
