// Package jadic 讀取 0c-jadic v1 詞典 pack（`.pack`）與其 manifest.json。
//
// Pack 是上游詞典建置流程的產物，不是產品執行期格式。這個套件只負責解析、
// 驗證與匯出；產品執行期仍然讀 data/ja 編譯出的 lexicon.bin。
// 格式定義見 plan/0c-ja/phase2/reference/pack-format.md。
package jadic

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"unicode/utf8"
)

const (
	// Magic 是 pack 檔頭前 8 位元組。
	Magic = "0CJADIC\x00"
	// FormatVersion 是目前支援的 pack 格式版本。
	FormatVersion = 1
	// HeaderSize 是 v1 固定檔頭大小。
	HeaderSize = 80
	// EntrySize 是 v1 固定詞條大小。
	EntrySize = 20
	// IndexEntrySize 是 v1 surface index 每筆大小。
	IndexEntrySize = 8
	// TrailerSize 是檔尾 payload SHA-256 的大小。
	TrailerSize = sha256.Size
	// MaxCost 與 MinCost 是格式允許的詞成本範圍（i16，但 -32768 保留）。
	MinCost = -32767
	MaxCost = 32767
)

// Header 是 pack 的 80 位元組檔頭。所有 offset 從檔案開頭算起。
type Header struct {
	FormatVersion uint16
	HeaderSize    uint16
	Flags         uint32
	EntryCount    uint32
	SurfaceCount  uint32
	POSCount      uint16
	ConjTypeCount uint16
	ConjFormCount uint16
	Reserved      uint16

	SurfaceBlobOffset uint32
	SurfaceBlobLength uint32
	ReadingBlobOffset uint32
	ReadingBlobLength uint32
	EntriesOffset     uint32
	EntriesLength     uint32
	IndexOffset       uint32
	IndexLength       uint32
	POSOffset         uint32
	POSLength         uint32
	ConjOffset        uint32
	ConjLength        uint32
}

// Entry 是一筆解碼後的詞條。Surface 與 Reading 是 UTF-8 字串，Cost 越小越優先。
// Split 是 flags 的低 2 位：0=C、1=A、2=B。Flags 保留完整原始值。
type Entry struct {
	Surface  string
	Reading  string
	POS      string
	ConjType string
	ConjForm string
	Cost     int16
	Flags    uint16
	Split    uint8
}

// Stats 是解析時算出的 pack 統計，供 inspect 與匯入回執使用。
type Stats struct {
	CostMin        int16
	CostMax        int16
	NegativeCosts  int
	SplitCounts    [3]int
	MaxReadings    int // 同一 surface 的最大詞條數
	MaxReadingUnit int
}

// Pack 是已驗證的 pack。data 保留原始位元組，Entries 與查詢都從這裡解碼。
type Pack struct {
	hdr        Header
	data       []byte
	payloadSum [32]byte
	fileSum    [32]byte
	surfaces   []surfaceRef
	pos        []string
	conjTypes  []string
	conjForms  []string
	stats      Stats
}

// surfaceRef 是 surface index 的一筆，補上詞條讀到的長度，供二分搜尋使用。
type surfaceRef struct {
	offset     uint32
	length     uint16
	firstEntry uint32
	count      uint32
}

// Header 回傳 pack 檔頭。
func (p *Pack) Header() Header { return p.hdr }

// EntryCount 回傳詞條數。
func (p *Pack) EntryCount() int { return int(p.hdr.EntryCount) }

// SurfaceCount 回傳唯一 surface 數。
func (p *Pack) SurfaceCount() int { return int(p.hdr.SurfaceCount) }

// POSList 回傳 POS table，順序與 pack 內一致。
func (p *Pack) POSList() []string { return append([]string(nil), p.pos...) }

// ConjTypes 回傳活用型 table。
func (p *Pack) ConjTypes() []string { return append([]string(nil), p.conjTypes...) }

// ConjForms 回傳活用形 table。
func (p *Pack) ConjForms() []string { return append([]string(nil), p.conjForms...) }

// Stats 回傳解析時算出的統計。
func (p *Pack) Stats() Stats { return p.stats }

// PayloadSHA256 回傳 trailer 內的 payload 雜湊。
func (p *Pack) PayloadSHA256() string { return hex.EncodeToString(p.payloadSum[:]) }

// FileSHA256 回傳整檔雜湊，對應 manifest 的 outputs.pack.sha256。
func (p *Pack) FileSHA256() string { return hex.EncodeToString(p.fileSum[:]) }

// Bytes 回傳檔案大小。
func (p *Pack) Bytes() int64 { return int64(len(p.data)) }

// Load 讀取並驗證 pack 檔。錯誤會指出損壞的欄位或 section。
func Load(path string) (*Pack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", path, err)
	}
	return p, nil
}

// Parse 解析並驗證 pack 位元組。驗證範圍：
// magic、格式版本、檔頭大小、section 界線與重疊、count 與 section 長度、
// 詞條索引、surface 排序、POS／活用 table，以及 trailer 的 payload SHA-256。
func Parse(data []byte) (*Pack, error) {
	if len(data) < HeaderSize+TrailerSize {
		return nil, fmt.Errorf("jadic: 檔案太小：只有 %d 位元組，小於檔頭加 trailer 的 %d", len(data), HeaderSize+TrailerSize)
	}
	if string(data[:len(Magic)]) != Magic {
		return nil, fmt.Errorf("jadic: magic 不符，不是 0c-jadic pack")
	}
	h, err := parseHeader(data[:HeaderSize])
	if err != nil {
		return nil, err
	}
	if h.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("jadic: 不支援的 pack 格式版本 %d（本版只支援 %d）", h.FormatVersion, FormatVersion)
	}
	if h.HeaderSize != HeaderSize {
		return nil, fmt.Errorf("jadic: header size 為 %d，預期 %d", h.HeaderSize, HeaderSize)
	}
	if h.Reserved != 0 {
		return nil, fmt.Errorf("jadic: reserved 欄位為 %d，v1 必須是 0", h.Reserved)
	}
	if h.Flags != 0 {
		return nil, fmt.Errorf("jadic: 不支援的 pack flags %#x，v1 必須是 0", h.Flags)
	}
	if h.EntryCount == 0 {
		return nil, fmt.Errorf("jadic: entry_count 為 0")
	}
	if h.SurfaceCount == 0 {
		return nil, fmt.Errorf("jadic: surface_count 為 0")
	}
	if h.POSCount == 0 {
		return nil, fmt.Errorf("jadic: pos_count 為 0")
	}

	payloadEnd := len(data) - TrailerSize
	payload := data[:payloadEnd]
	payloadSum := sha256.Sum256(payload)
	if !bytes.Equal(payloadSum[:], data[payloadEnd:]) {
		return nil, fmt.Errorf("jadic: payload SHA-256 不符（檔頭宣告 %s，實際 %s），pack 已損壞",
			hex.EncodeToString(data[payloadEnd:]), hex.EncodeToString(payloadSum[:]))
	}

	if _, err := checkSections(h, payloadEnd); err != nil {
		return nil, err
	}

	if want := uint64(h.EntryCount) * EntrySize; uint64(h.EntriesLength) != want {
		return nil, fmt.Errorf("jadic: entries section 長度 %d 與 entry_count×%d=%d 不符", h.EntriesLength, EntrySize, want)
	}
	if want := uint64(h.SurfaceCount) * IndexEntrySize; uint64(h.IndexLength) != want {
		return nil, fmt.Errorf("jadic: surface index 長度 %d 與 surface_count×%d=%d 不符", h.IndexLength, IndexEntrySize, want)
	}

	p := &Pack{hdr: h, data: data, payloadSum: payloadSum}
	p.fileSum = sha256.Sum256(data)

	pos, err := parseStringTable(data, h.POSOffset, h.POSLength, int(h.POSCount), "POS table")
	if err != nil {
		return nil, err
	}
	p.pos = pos
	types, forms, err := parseConjTable(data, h.ConjOffset, h.ConjLength, int(h.ConjTypeCount), int(h.ConjFormCount))
	if err != nil {
		return nil, err
	}
	p.conjTypes, p.conjForms = types, forms
	if err := p.buildSurfaces(); err != nil {
		return nil, err
	}
	if err := p.validateEntries(); err != nil {
		return nil, err
	}
	return p, nil
}

func parseHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, fmt.Errorf("jadic: 檔頭不足 %d 位元組", HeaderSize)
	}
	u16 := func(off int) uint16 { return binary.LittleEndian.Uint16(b[off:]) }
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(b[off:]) }
	return Header{
		FormatVersion:     u16(8),
		HeaderSize:        u16(10),
		Flags:             u32(12),
		EntryCount:        u32(16),
		SurfaceCount:      u32(20),
		POSCount:          u16(24),
		ConjTypeCount:     u16(26),
		ConjFormCount:     u16(28),
		Reserved:          u16(30),
		SurfaceBlobOffset: u32(32),
		SurfaceBlobLength: u32(36),
		ReadingBlobOffset: u32(40),
		ReadingBlobLength: u32(44),
		EntriesOffset:     u32(48),
		EntriesLength:     u32(52),
		IndexOffset:       u32(56),
		IndexLength:       u32(60),
		POSOffset:         u32(64),
		POSLength:         u32(68),
		ConjOffset:        u32(72),
		ConjLength:        u32(76),
	}, nil
}

type section struct {
	name  string
	start uint64
	end   uint64
}

// checkSections 檢查每個 section 都在 payload 內、長度一致且互不重疊。
func checkSections(h Header, payloadEnd int) ([]section, error) {
	items := []section{
		{"surface blob", uint64(h.SurfaceBlobOffset), uint64(h.SurfaceBlobOffset) + uint64(h.SurfaceBlobLength)},
		{"reading blob", uint64(h.ReadingBlobOffset), uint64(h.ReadingBlobOffset) + uint64(h.ReadingBlobLength)},
		{"entries", uint64(h.EntriesOffset), uint64(h.EntriesOffset) + uint64(h.EntriesLength)},
		{"surface index", uint64(h.IndexOffset), uint64(h.IndexOffset) + uint64(h.IndexLength)},
		{"POS table", uint64(h.POSOffset), uint64(h.POSOffset) + uint64(h.POSLength)},
		{"conjugation table", uint64(h.ConjOffset), uint64(h.ConjOffset) + uint64(h.ConjLength)},
	}
	for _, s := range items {
		if s.start < HeaderSize {
			return nil, fmt.Errorf("jadic: %s 起點 %d 落在檔頭內", s.name, s.start)
		}
		if s.end < s.start {
			return nil, fmt.Errorf("jadic: %s 的長度溢位", s.name)
		}
		if s.end > uint64(payloadEnd) {
			return nil, fmt.Errorf("jadic: %s 結束於 %d，超出 payload 結尾 %d", s.name, s.end, payloadEnd)
		}
	}
	sorted := append([]section(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].start < sorted[i-1].end {
			return nil, fmt.Errorf("jadic: section 重疊：%s [%d,%d) 與 %s [%d,%d)",
				sorted[i-1].name, sorted[i-1].start, sorted[i-1].end,
				sorted[i].name, sorted[i].start, sorted[i].end)
		}
	}
	return items, nil
}

// parseStringTable 讀 u16 count + (u16 length + UTF-8) × count，並檢查排序與重複。
func parseStringTable(data []byte, off, length uint32, want int, name string) ([]string, error) {
	section := data[off : off+length]
	if len(section) < 2 {
		return nil, fmt.Errorf("jadic: %s 太短", name)
	}
	n := int(binary.LittleEndian.Uint16(section))
	if n != want {
		return nil, fmt.Errorf("jadic: %s 宣告 %d 筆，檔頭 pos_count 為 %d", name, n, want)
	}
	out := make([]string, 0, n)
	pos := 2
	for i := 0; i < n; i++ {
		if pos+2 > len(section) {
			return nil, fmt.Errorf("jadic: %s 第 %d 筆缺少長度", name, i+1)
		}
		l := int(binary.LittleEndian.Uint16(section[pos:]))
		pos += 2
		if l == 0 || pos+l > len(section) {
			return nil, fmt.Errorf("jadic: %s 第 %d 筆長度 %d 超出 section", name, i+1, l)
		}
		s := string(section[pos : pos+l])
		pos += l
		if !utf8.ValidString(s) {
			return nil, fmt.Errorf("jadic: %s 第 %d 筆不是合法 UTF-8", name, i+1)
		}
		if s == "" {
			return nil, fmt.Errorf("jadic: %s 第 %d 筆是空字串", name, i+1)
		}
		if len(out) > 0 && out[len(out)-1] >= s {
			return nil, fmt.Errorf("jadic: %s 未依字串嚴格排序（%q 之後是 %q）", name, out[len(out)-1], s)
		}
		out = append(out, s)
	}
	if pos != len(section) {
		return nil, fmt.Errorf("jadic: %s 尾端有 %d 位元組未使用", name, len(section)-pos)
	}
	return out, nil
}

// parseConjTable 讀活用型與活用形兩張表：u16 count + strings。
func parseConjTable(data []byte, off, length uint32, wantTypes, wantForms int) ([]string, []string, error) {
	section := data[off : off+length]
	pos := 0
	readTable := func(name string, want int) ([]string, error) {
		if pos+2 > len(section) {
			return nil, fmt.Errorf("jadic: 活用 table 缺少 %s 筆數", name)
		}
		n := int(binary.LittleEndian.Uint16(section[pos:]))
		pos += 2
		if n != want {
			return nil, fmt.Errorf("jadic: 活用 %s 宣告 %d 筆，檔頭為 %d", name, n, want)
		}
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			if pos+2 > len(section) {
				return nil, fmt.Errorf("jadic: 活用 %s 第 %d 筆缺少長度", name, i+1)
			}
			l := int(binary.LittleEndian.Uint16(section[pos:]))
			pos += 2
			if l == 0 || pos+l > len(section) {
				return nil, fmt.Errorf("jadic: 活用 %s 第 %d 筆長度 %d 超出 section", name, i+1, l)
			}
			s := string(section[pos : pos+l])
			pos += l
			if !utf8.ValidString(s) {
				return nil, fmt.Errorf("jadic: 活用 %s 第 %d 筆不是合法 UTF-8", name, i+1)
			}
			if len(out) > 0 && out[len(out)-1] >= s {
				return nil, fmt.Errorf("jadic: 活用 %s 未依字串嚴格排序（%q 之後是 %q）", name, out[len(out)-1], s)
			}
			out = append(out, s)
		}
		return out, nil
	}
	types, err := readTable("型", wantTypes)
	if err != nil {
		return nil, nil, err
	}
	forms, err := readTable("形", wantForms)
	if err != nil {
		return nil, nil, err
	}
	if pos != len(section) {
		return nil, nil, fmt.Errorf("jadic: 活用 table 尾端有 %d 位元組未使用", len(section)-pos)
	}
	return types, forms, nil
}

// surfaceAt 解碼 surface blob 內的字串。
func (p *Pack) surfaceAt(off uint32, length uint16) (string, error) {
	end := uint64(off) + uint64(length)
	if end > uint64(p.hdr.SurfaceBlobLength) {
		return "", fmt.Errorf("surface offset %d + length %d 超出 surface blob %d", off, length, p.hdr.SurfaceBlobLength)
	}
	b := p.data[uint64(p.hdr.SurfaceBlobOffset)+uint64(off) : uint64(p.hdr.SurfaceBlobOffset)+end]
	if !utf8.Valid(b) {
		return "", fmt.Errorf("surface 不是合法 UTF-8")
	}
	return string(b), nil
}

func (p *Pack) readingAt(off uint32, length uint16) (string, error) {
	end := uint64(off) + uint64(length)
	if end > uint64(p.hdr.ReadingBlobLength) {
		return "", fmt.Errorf("reading offset %d + length %d 超出 reading blob %d", off, length, p.hdr.ReadingBlobLength)
	}
	b := p.data[uint64(p.hdr.ReadingBlobOffset)+uint64(off) : uint64(p.hdr.ReadingBlobOffset)+end]
	if !utf8.Valid(b) {
		return "", fmt.Errorf("reading 不是合法 UTF-8")
	}
	return string(b), nil
}

// entryAt 解碼第 i 筆詞條，不保留結果。
func (p *Pack) entryAt(i uint32) (Entry, error) {
	off := uint64(p.hdr.EntriesOffset) + uint64(i)*EntrySize
	b := p.data[off : off+EntrySize]
	e := Entry{
		Cost:  int16(binary.LittleEndian.Uint16(b[16:])),
		Flags: binary.LittleEndian.Uint16(b[18:]),
	}
	po := binary.LittleEndian.Uint16(b[12:])
	ct, cf := b[14], b[15]
	if int(po) >= len(p.pos) {
		return Entry{}, fmt.Errorf("第 %d 筆 POS id %d 超出 POS table %d", i, po, len(p.pos))
	}
	if int(ct) >= len(p.conjTypes) {
		return Entry{}, fmt.Errorf("第 %d 筆活用型 id %d 超出 table %d", i, ct, len(p.conjTypes))
	}
	if int(cf) >= len(p.conjForms) {
		return Entry{}, fmt.Errorf("第 %d 筆活用形 id %d 超出 table %d", i, cf, len(p.conjForms))
	}
	var err error
	if e.Surface, err = p.surfaceAt(binary.LittleEndian.Uint32(b[0:]), binary.LittleEndian.Uint16(b[4:])); err != nil {
		return Entry{}, fmt.Errorf("第 %d 筆 %w", i, err)
	}
	if e.Reading, err = p.readingAt(binary.LittleEndian.Uint32(b[6:]), binary.LittleEndian.Uint16(b[10:])); err != nil {
		return Entry{}, fmt.Errorf("第 %d 筆 %w", i, err)
	}
	if e.Surface == "" {
		return Entry{}, fmt.Errorf("第 %d 筆 surface 是空字串", i)
	}
	if e.Reading == "" {
		return Entry{}, fmt.Errorf("第 %d 筆 reading 是空字串", i)
	}
	e.POS = p.pos[po]
	e.ConjType = p.conjTypes[ct]
	e.ConjForm = p.conjForms[cf]
	e.Split = uint8(e.Flags & 0x3)
	return e, nil
}

// buildSurfaces 解析 surface index，並從第一筆詞條補上 surface 長度。
func (p *Pack) buildSurfaces() error {
	n := int(p.hdr.SurfaceCount)
	p.surfaces = make([]surfaceRef, n)
	prevOffset := uint32(0)
	prevFirst := uint32(0)
	for i := 0; i < n; i++ {
		off := uint64(p.hdr.IndexOffset) + uint64(i)*IndexEntrySize
		b := p.data[off : off+IndexEntrySize]
		ref := surfaceRef{
			offset:     binary.LittleEndian.Uint32(b[0:]),
			firstEntry: binary.LittleEndian.Uint32(b[4:]),
		}
		if i > 0 {
			if ref.offset <= prevOffset {
				return fmt.Errorf("jadic: surface index 第 %d 筆 offset %d 未遞增（前一筆 %d）", i+1, ref.offset, prevOffset)
			}
			if ref.firstEntry <= prevFirst {
				return fmt.Errorf("jadic: surface index 第 %d 筆 first_entry %d 未遞增（前一筆 %d）", i+1, ref.firstEntry, prevFirst)
			}
		} else if ref.offset != 0 {
			return fmt.Errorf("jadic: surface index 第一筆 offset 為 %d，預期 0", ref.offset)
		}
		if ref.firstEntry >= p.hdr.EntryCount {
			return fmt.Errorf("jadic: surface index 第 %d 筆 first_entry %d 超出詞條數 %d", i+1, ref.firstEntry, p.hdr.EntryCount)
		}
		if uint64(ref.offset) >= uint64(p.hdr.SurfaceBlobLength) {
			return fmt.Errorf("jadic: surface index 第 %d 筆 offset %d 超出 surface blob", i+1, ref.offset)
		}
		p.surfaces[i] = ref
		prevOffset, prevFirst = ref.offset, ref.firstEntry
	}
	for i := range p.surfaces {
		start := p.surfaces[i].firstEntry
		var end uint32
		if i+1 < len(p.surfaces) {
			end = p.surfaces[i+1].firstEntry
		} else {
			end = p.hdr.EntryCount
		}
		if end <= start {
			return fmt.Errorf("jadic: surface index 第 %d 筆沒有詞條", i+1)
		}
		p.surfaces[i].count = end - start
		// surface 長度以第一筆詞條為準；validateEntries 會檢查其餘詞條一致。
		e, err := p.entryAt(start)
		if err != nil {
			return err
		}
		if len(e.Surface) > 0xFFFF {
			return fmt.Errorf("jadic: surface 長度 %d 超出 u16", len(e.Surface))
		}
		p.surfaces[i].length = uint16(len(e.Surface))
	}
	return nil
}

// validateEntries 逐筆檢查引用範圍、排序、surface index 對應與 flags。
func (p *Pack) validateEntries() error {
	var prevSurface string
	costMin := int16(MaxCost)
	costMax := int16(MinCost)
	stats := Stats{}
	surfaceIdx := 0
	entriesInSurface := 0
	for i := uint32(0); i < p.hdr.EntryCount; i++ {
		e, err := p.entryAt(i)
		if err != nil {
			return fmt.Errorf("jadic: %w", err)
		}
		if e.Cost < MinCost {
			return fmt.Errorf("jadic: 第 %d 筆 %q 的詞成本 %d 低於合法下限 %d", i, e.Surface, e.Cost, MinCost)
		}
		if e.Flags&^uint16(0x3) != 0 {
			return fmt.Errorf("jadic: 第 %d 筆 %q 帶有未定義的 flags %#x", i, e.Surface, e.Flags)
		}
		switch e.Split {
		case 0, 1, 2:
		default:
			return fmt.Errorf("jadic: 第 %d 筆 %q 的 split 值 %d 不合法", i, e.Surface, e.Split)
		}
		if i > 0 && e.Surface < prevSurface {
			return fmt.Errorf("jadic: 第 %d 筆 surface %q 未依 code point 排序（前一筆 %q）", i, e.Surface, prevSurface)
		}
		if e.Surface != prevSurface {
			if surfaceIdx >= len(p.surfaces) {
				return fmt.Errorf("jadic: 詞條 %d 的 surface %q 不在 surface index 內", i, e.Surface)
			}
			ref := p.surfaces[surfaceIdx]
			if ref.firstEntry != i {
				return fmt.Errorf("jadic: surface index 第 %d 筆 first_entry 為 %d，實際是 %d", surfaceIdx+1, ref.firstEntry, i)
			}
			if ref.length != uint16(len(e.Surface)) {
				return fmt.Errorf("jadic: surface %q 的長度 %d 與 index 記錄 %d 不符", e.Surface, len(e.Surface), ref.length)
			}
			idxSurface, err := p.surfaceAt(ref.offset, ref.length)
			if err != nil {
				return fmt.Errorf("jadic: surface index 第 %d 筆 %w", surfaceIdx+1, err)
			}
			if idxSurface != e.Surface {
				return fmt.Errorf("jadic: surface index 第 %d 筆指向 %q，詞條卻是 %q", surfaceIdx+1, idxSurface, e.Surface)
			}
			surfaceIdx++
			entriesInSurface = 0
		}
		entriesInSurface++
		if entriesInSurface > stats.MaxReadings {
			stats.MaxReadings = entriesInSurface
		}
		if e.Cost < costMin {
			costMin = e.Cost
		}
		if e.Cost > costMax {
			costMax = e.Cost
		}
		if e.Cost < 0 {
			stats.NegativeCosts++
		}
		stats.SplitCounts[e.Split]++
		if n := utf8.RuneCountInString(e.Reading); n > stats.MaxReadingUnit {
			stats.MaxReadingUnit = n
		}
		prevSurface = e.Surface
	}
	if surfaceIdx != len(p.surfaces) {
		return fmt.Errorf("jadic: surface index 有 %d 筆，詞條只對應到 %d 筆", len(p.surfaces), surfaceIdx)
	}
	last := p.surfaces[len(p.surfaces)-1]
	if last.offset+uint32(last.length) != p.hdr.SurfaceBlobLength {
		return fmt.Errorf("jadic: surface blob 尾端有未索引的位元組")
	}
	stats.CostMin, stats.CostMax = costMin, costMax
	p.stats = stats
	return nil
}

// Entries 依 pack 順序解碼全部詞條。大 pack 會佔用大量記憶體，串流處理請用 ForEach。
func (p *Pack) Entries() ([]Entry, error) {
	out := make([]Entry, 0, p.hdr.EntryCount)
	if err := p.ForEach(func(e Entry) error {
		out = append(out, e)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// ForEach 依 pack 順序走訪所有詞條。
func (p *Pack) ForEach(fn func(Entry) error) error {
	for i := uint32(0); i < p.hdr.EntryCount; i++ {
		e, err := p.entryAt(i)
		if err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

// Lookup 回傳表面字串完全相等的所有詞條，順序與 pack 內相同。
// 同一 surface 的多筆讀音會依 pack 建置順序回傳，這個套件不重新排序。
func (p *Pack) Lookup(surface string) []Entry {
	i := sort.Search(len(p.surfaces), func(i int) bool {
		s, err := p.surfaceAt(p.surfaces[i].offset, p.surfaces[i].length)
		return err != nil || s >= surface
	})
	if i == len(p.surfaces) {
		return nil
	}
	s, err := p.surfaceAt(p.surfaces[i].offset, p.surfaces[i].length)
	if err != nil || s != surface {
		return nil
	}
	ref := p.surfaces[i]
	out := make([]Entry, 0, ref.count)
	for j := ref.firstEntry; j < ref.firstEntry+ref.count; j++ {
		e, err := p.entryAt(j)
		if err != nil {
			return nil
		}
		out = append(out, e)
	}
	return out
}

// Has 回報 surface 是否存在。
func (p *Pack) Has(surface string) bool { return len(p.Lookup(surface)) > 0 }

// Prefix 回傳以 prefix 開頭的 surface 的所有詞條，依 surface 排序。
// 這是 inspect 用的方便函式，不建立額外索引。
func (p *Pack) Prefix(prefix string) []Entry {
	i := sort.Search(len(p.surfaces), func(i int) bool {
		s, err := p.surfaceAt(p.surfaces[i].offset, p.surfaces[i].length)
		return err != nil || s >= prefix
	})
	var out []Entry
	for ; i < len(p.surfaces); i++ {
		s, err := p.surfaceAt(p.surfaces[i].offset, p.surfaces[i].length)
		if err != nil || !hasPrefix(s, prefix) {
			break
		}
		ref := p.surfaces[i]
		for j := ref.firstEntry; j < ref.firstEntry+ref.count; j++ {
			e, err := p.entryAt(j)
			if err != nil {
				return out
			}
			out = append(out, e)
		}
	}
	return out
}

func hasPrefix(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }
