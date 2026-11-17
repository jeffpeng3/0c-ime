package jadic

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseSamplePack(t *testing.T) {
	data := samplePack(t)
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.EntryCount() != 5 || p.SurfaceCount() != 4 {
		t.Fatalf("counts = %d/%d", p.EntryCount(), p.SurfaceCount())
	}
	h := p.Header()
	if h.FormatVersion != FormatVersion || h.HeaderSize != HeaderSize || h.EntryCount != 5 {
		t.Fatalf("header = %+v", h)
	}
	fileSum := sha256.Sum256(data)
	if p.FileSHA256() != hexString(fileSum[:]) {
		t.Fatalf("整檔 SHA 不符：%s", p.FileSHA256())
	}
	payloadSum := sha256.Sum256(data[:len(data)-TrailerSize])
	if p.PayloadSHA256() != hexString(payloadSum[:]) {
		t.Fatalf("payload SHA 不符：%s", p.PayloadSHA256())
	}

	got, ok := p.Lookup("明日"), p.Has("明日")
	if !ok || len(got) != 2 {
		t.Fatalf("Lookup(明日) = %#v", got)
	}
	if got[0].Reading != "あした" || got[1].Reading != "あす" {
		t.Fatalf("同 surface 的讀音順序 = %q, %q", got[0].Reading, got[1].Reading)
	}
	if got[0].POS != "名詞,普通名詞,一般,*" || got[0].ConjType != "*" || got[0].ConjForm != "*" {
		t.Fatalf("entry = %+v", got[0])
	}
	if e := p.Lookup("亜")[0]; e.Split != 1 || e.Flags != 1 {
		t.Fatalf("split = %d flags = %#x", e.Split, e.Flags)
	}
	if p.Lookup("なし") != nil {
		t.Fatal("不存在的 surface 應回 nil")
	}
	if pre := p.Prefix("明"); len(pre) != 2 {
		t.Fatalf("Prefix(明) = %d 筆", len(pre))
	}
	if len(p.POSList()) != 2 || len(p.ConjTypes()) != 2 || len(p.ConjForms()) != 2 {
		t.Fatalf("tables = %v / %v / %v", p.POSList(), p.ConjTypes(), p.ConjForms())
	}
	st := p.Stats()
	if st.CostMin != 800 || st.CostMax != 3000 || st.MaxReadings != 2 || st.MaxReadingUnit != 3 {
		t.Fatalf("stats = %+v", st)
	}
	if st.SplitCounts != [3]int{4, 1, 0} {
		t.Fatalf("split counts = %v", st.SplitCounts)
	}
	all, err := p.Entries()
	if err != nil || len(all) != 5 {
		t.Fatalf("Entries = %d, %v", len(all), err)
	}
}

func TestParseRejectsStructuralCorruption(t *testing.T) {
	base := samplePack(t)
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, "太小"},
		{"short", base[:HeaderSize], "太小"},
		{"magic", mutate(base, 0, 'X'), "magic"},
		{"version", putU16At(base, 8, 2), "版本"},
		{"header_size", putU16At(base, 10, 79), "header size"},
		{"reserved", putU16At(base, 30, 1), "reserved"},
		{"flags", putU32At(base, 12, 1), "flags"},
		{"count", reseal(putU32At(base, 16, 4)), "entries section"},
		{"surface_count", reseal(putU32At(base, 20, 3)), "surface index"},
		{"entries_offset", reseal(putU32At(base, 48, 0)), "檔頭內"},
		{"entries_overrun", reseal(putU32At(base, 52, 0xFFFFFF)), "超出 payload"},
		{"overlap", reseal(putU32At(base, 40, 80)), "重疊"},
		{"pos_count", reseal(putU16At(base, 24, 1)), "POS table"},
		{"conj_type_count", reseal(putU16At(base, 26, 1)), "活用 型"},
		{"conj_form_count", reseal(putU16At(base, 28, 1)), "活用 形"},
		{"payload_byte", toggle(base, HeaderSize+3), "payload SHA-256"},
		{"trailer_byte", toggle(base, len(base)-1), "payload SHA-256"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.data)
			if err == nil {
				t.Fatal("預期解析失敗")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("錯誤 %q 未包含 %q", err, tc.want)
			}
		})
	}
}

func TestParseRejectsEntryCorruption(t *testing.T) {
	base := samplePack(t)
	h := mustHeader(t, base)
	entryOff := int(h.EntriesOffset)

	cases := []struct {
		name string
		edit func([]byte) []byte
		want string
	}{
		{
			name: "negative_cost_min",
			edit: func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[entryOff+16:], 0x8000) // -32768
				return b
			},
			want: "詞成本",
		},
		{
			name: "pos_id",
			edit: func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[entryOff+12:], uint16(h.POSCount))
				return b
			},
			want: "POS id",
		},
		{
			name: "conj_type",
			edit: func(b []byte) []byte {
				b[entryOff+14] = byte(h.ConjTypeCount)
				return b
			},
			want: "活用型 id",
		},
		{
			name: "conj_form",
			edit: func(b []byte) []byte {
				b[entryOff+15] = byte(h.ConjFormCount)
				return b
			},
			want: "活用形 id",
		},
		{
			name: "reading_overrun",
			edit: func(b []byte) []byte {
				binary.LittleEndian.PutUint32(b[entryOff+6:], h.ReadingBlobLength)
				return b
			},
			want: "reading",
		},
		{
			name: "surface_overrun",
			edit: func(b []byte) []byte {
				binary.LittleEndian.PutUint32(b[entryOff+0:], h.SurfaceBlobLength)
				return b
			},
			want: "surface",
		},
		{
			name: "split",
			edit: func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[entryOff+18:], 3)
				return b
			},
			want: "split",
		},
		{
			name: "unknown_flags",
			edit: func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[entryOff+18:], 4)
				return b
			},
			want: "flags",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := reseal(tc.edit(append([]byte(nil), base...)))
			_, err := Parse(data)
			if err == nil {
				t.Fatal("預期解析失敗")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("錯誤 %q 未包含 %q", err, tc.want)
			}
		})
	}

	// POS table 必須依字串嚴格排序；用等長字串才不會動到長度前綴。
	unsorted := encodeTestPack(t, []testEntry{
		{surface: "x", reading: "あ", pos: "aaa", conjType: "*", conjForm: "*", cost: 1},
	}, []string{"aaa", "bbb"}, nil, nil)
	uh := mustHeader(t, unsorted)
	copy(unsorted[int(uh.POSOffset)+4:], []byte("zzz"))
	if _, err := Parse(reseal(unsorted)); err == nil || !strings.Contains(err.Error(), "排序") {
		t.Fatalf("POS 排序錯誤 = %v", err)
	}
}

func TestParseRejectsIndexCorruption(t *testing.T) {
	base := samplePack(t)
	h := mustHeader(t, base)
	idx := int(h.IndexOffset)

	// 第一筆 offset 不是 0。
	bad := append([]byte(nil), base...)
	binary.LittleEndian.PutUint32(bad[idx:], 1)
	if _, err := Parse(reseal(bad)); err == nil || !strings.Contains(err.Error(), "第一筆 offset") {
		t.Fatalf("第一筆 offset 錯誤 = %v", err)
	}

	// first_entry 未遞增。
	bad = append([]byte(nil), base...)
	binary.LittleEndian.PutUint32(bad[idx+IndexEntrySize+4:], 0)
	if _, err := Parse(reseal(bad)); err == nil || !strings.Contains(err.Error(), "first_entry") {
		t.Fatalf("first_entry 錯誤 = %v", err)
	}

	// index 指到的 surface 與詞條不一致。
	ascii := encodeTestPack(t, []testEntry{
		{surface: "aa", reading: "あ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 1},
		{surface: "bb", reading: "い", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 2},
	}, nil, nil, nil)
	ah := mustHeader(t, ascii)
	bad = append([]byte(nil), ascii...)
	binary.LittleEndian.PutUint32(bad[int(ah.IndexOffset)+IndexEntrySize:], 1) // 指向 "aa" 的第二個位元組
	if _, err := Parse(reseal(bad)); err == nil || !strings.Contains(err.Error(), "index") {
		t.Fatalf("index 與詞條不一致錯誤 = %v", err)
	}
}

func mustHeader(t *testing.T, data []byte) Header {
	t.Helper()
	h, err := parseHeader(data[:HeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}

func mutate(data []byte, off int, c byte) []byte {
	out := append([]byte(nil), data...)
	out[off] = c
	return out
}

func toggle(data []byte, off int) []byte {
	out := append([]byte(nil), data...)
	out[off] ^= 0xFF
	return out
}

func putU16At(data []byte, off int, v uint16) []byte {
	out := append([]byte(nil), data...)
	binary.LittleEndian.PutUint16(out[off:], v)
	return out
}

func putU32At(data []byte, off int, v uint32) []byte {
	out := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(out[off:], v)
	return out
}
