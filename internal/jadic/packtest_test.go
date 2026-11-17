package jadic

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"testing"
)

// 測試用 pack 建構器。刻意照 pack-format.md 的版面寫，讓讀取端與格式文件互相驗證。

type testEntry struct {
	surface  string
	reading  string
	pos      string
	conjType string
	conjForm string
	cost     int16
	flags    uint16
}

func encodeTestPack(t *testing.T, entries []testEntry, pos, types, forms []string) []byte {
	t.Helper()
	if len(entries) == 0 {
		t.Fatal("測試 pack 至少需要一筆詞條")
	}
	pos = append([]string(nil), pos...)
	if len(pos) == 0 {
		pos = []string{"名詞,普通名詞,一般,*"}
	}
	sort.Strings(pos)
	types = append([]string(nil), types...)
	if len(types) == 0 {
		types = []string{"*"}
	}
	sort.Strings(types)
	forms = append([]string(nil), forms...)
	if len(forms) == 0 {
		forms = []string{"*"}
	}
	sort.Strings(forms)

	posID := map[string]int{}
	for i, s := range pos {
		posID[s] = i
	}
	typeID := map[string]int{}
	for i, s := range types {
		typeID[s] = i
	}
	formID := map[string]int{}
	for i, s := range forms {
		formID[s] = i
	}

	sorted := append([]testEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].surface < sorted[j].surface })

	var surfaceBlob, readingBlob bytes.Buffer
	surfaceOff := map[string]uint32{}
	type entryRec struct {
		surfOff, readOff uint32
		surfLen, readLen uint16
		posID            uint16
		typeID, formID   uint8
		cost             int16
		flags            uint16
	}
	recs := make([]entryRec, len(sorted))
	for i, e := range sorted {
		off, ok := surfaceOff[e.surface]
		if !ok {
			off = uint32(surfaceBlob.Len())
			surfaceOff[e.surface] = off
			surfaceBlob.WriteString(e.surface)
		}
		readOff := uint32(readingBlob.Len())
		readingBlob.WriteString(e.reading)
		pi, ok := posID[e.pos]
		if !ok {
			t.Fatalf("測試詞條 %q 的 POS %q 不在 table", e.surface, e.pos)
		}
		ti, ok := typeID[e.conjType]
		if !ok {
			t.Fatalf("測試詞條 %q 的活用型 %q 不在 table", e.surface, e.conjType)
		}
		fi, ok := formID[e.conjForm]
		if !ok {
			t.Fatalf("測試詞條 %q 的活用形 %q 不在 table", e.surface, e.conjForm)
		}
		recs[i] = entryRec{
			surfOff: off, surfLen: uint16(len(e.surface)),
			readOff: readOff, readLen: uint16(len(e.reading)),
			posID: uint16(pi), typeID: uint8(ti), formID: uint8(fi),
			cost: e.cost, flags: e.flags,
		}
	}

	// surface index：唯一 surface 的 offset 與第一筆 entry index。
	var index bytes.Buffer
	seen := map[string]bool{}
	for i, e := range sorted {
		if seen[e.surface] {
			continue
		}
		seen[e.surface] = true
		var b [8]byte
		binary.LittleEndian.PutUint32(b[0:], surfaceOff[e.surface])
		binary.LittleEndian.PutUint32(b[4:], uint32(i))
		index.Write(b[:])
	}

	var posTable bytes.Buffer
	putU16(&posTable, uint16(len(pos)))
	for _, s := range pos {
		putTableString(&posTable, s)
	}
	var conjTable bytes.Buffer
	putU16(&conjTable, uint16(len(types)))
	for _, s := range types {
		putTableString(&conjTable, s)
	}
	putU16(&conjTable, uint16(len(forms)))
	for _, s := range forms {
		putTableString(&conjTable, s)
	}

	var entryBlob bytes.Buffer
	for _, r := range recs {
		var b [EntrySize]byte
		binary.LittleEndian.PutUint32(b[0:], r.surfOff)
		binary.LittleEndian.PutUint16(b[4:], r.surfLen)
		binary.LittleEndian.PutUint32(b[6:], r.readOff)
		binary.LittleEndian.PutUint16(b[10:], r.readLen)
		binary.LittleEndian.PutUint16(b[12:], r.posID)
		b[14] = r.typeID
		b[15] = r.formID
		binary.LittleEndian.PutUint16(b[16:], uint16(r.cost))
		binary.LittleEndian.PutUint16(b[18:], r.flags)
		entryBlob.Write(b[:])
	}

	off := HeaderSize
	var h Header
	h.FormatVersion = FormatVersion
	h.HeaderSize = HeaderSize
	h.EntryCount = uint32(len(recs))
	h.SurfaceCount = uint32(len(seen))
	h.POSCount = uint16(len(pos))
	h.ConjTypeCount = uint16(len(types))
	h.ConjFormCount = uint16(len(forms))

	set := func(offset, length *uint32, n int) {
		*offset = uint32(off)
		*length = uint32(n)
		off += n
	}
	set(&h.SurfaceBlobOffset, &h.SurfaceBlobLength, surfaceBlob.Len())
	set(&h.ReadingBlobOffset, &h.ReadingBlobLength, readingBlob.Len())
	set(&h.EntriesOffset, &h.EntriesLength, entryBlob.Len())
	set(&h.IndexOffset, &h.IndexLength, index.Len())
	set(&h.POSOffset, &h.POSLength, posTable.Len())
	set(&h.ConjOffset, &h.ConjLength, conjTable.Len())

	var out bytes.Buffer
	var hb [HeaderSize]byte
	copy(hb[0:], Magic)
	binary.LittleEndian.PutUint16(hb[8:], h.FormatVersion)
	binary.LittleEndian.PutUint16(hb[10:], h.HeaderSize)
	binary.LittleEndian.PutUint32(hb[12:], h.Flags)
	binary.LittleEndian.PutUint32(hb[16:], h.EntryCount)
	binary.LittleEndian.PutUint32(hb[20:], h.SurfaceCount)
	binary.LittleEndian.PutUint16(hb[24:], h.POSCount)
	binary.LittleEndian.PutUint16(hb[26:], h.ConjTypeCount)
	binary.LittleEndian.PutUint16(hb[28:], h.ConjFormCount)
	binary.LittleEndian.PutUint16(hb[30:], h.Reserved)
	binary.LittleEndian.PutUint32(hb[32:], h.SurfaceBlobOffset)
	binary.LittleEndian.PutUint32(hb[36:], h.SurfaceBlobLength)
	binary.LittleEndian.PutUint32(hb[40:], h.ReadingBlobOffset)
	binary.LittleEndian.PutUint32(hb[44:], h.ReadingBlobLength)
	binary.LittleEndian.PutUint32(hb[48:], h.EntriesOffset)
	binary.LittleEndian.PutUint32(hb[52:], h.EntriesLength)
	binary.LittleEndian.PutUint32(hb[56:], h.IndexOffset)
	binary.LittleEndian.PutUint32(hb[60:], h.IndexLength)
	binary.LittleEndian.PutUint32(hb[64:], h.POSOffset)
	binary.LittleEndian.PutUint32(hb[68:], h.POSLength)
	binary.LittleEndian.PutUint32(hb[72:], h.ConjOffset)
	binary.LittleEndian.PutUint32(hb[76:], h.ConjLength)
	out.Write(hb[:])
	out.Write(surfaceBlob.Bytes())
	out.Write(readingBlob.Bytes())
	out.Write(entryBlob.Bytes())
	out.Write(index.Bytes())
	out.Write(posTable.Bytes())
	out.Write(conjTable.Bytes())
	sum := sha256.Sum256(out.Bytes())
	out.Write(sum[:])
	return out.Bytes()
}

func putU16(b *bytes.Buffer, v uint16) {
	var tmp [2]byte
	binary.LittleEndian.PutUint16(tmp[:], v)
	b.Write(tmp[:])
}

func putTableString(b *bytes.Buffer, s string) {
	putU16(b, uint16(len(s)))
	b.WriteString(s)
}

// reseal 重算 trailer，讓結構測試能繞過雜湊檢查。
func reseal(data []byte) []byte {
	out := append([]byte(nil), data...)
	sum := sha256.Sum256(out[:len(out)-TrailerSize])
	copy(out[len(out)-TrailerSize:], sum[:])
	return out
}

func samplePack(t *testing.T) []byte {
	t.Helper()
	return encodeTestPack(t, []testEntry{
		{surface: "明日", reading: "あした", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1000},
		{surface: "明日", reading: "あす", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 2500},
		{surface: "味", reading: "あじ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1200},
		{surface: "亜", reading: "あ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 3000, flags: 1},
		{surface: "食べる", reading: "たべる", pos: "動詞,一般,*,*", conjType: "五段", conjForm: "終止形-一般", cost: 800},
	}, []string{"名詞,普通名詞,一般,*", "動詞,一般,*,*"}, []string{"*", "五段"}, []string{"*", "終止形-一般"})
}
