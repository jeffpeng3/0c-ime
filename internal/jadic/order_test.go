package jadic

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompareEntries(t *testing.T) {
	base := Entry{Surface: "同", Reading: "あ", POS: "名詞,普通名詞,一般,*", ConjType: "*", ConjForm: "*", Cost: 100}
	same := base
	if got := CompareEntries(base, same); got != 0 {
		t.Fatalf("完全相同應回 0，得到 %d", got)
	}

	cases := []struct {
		name string
		a, b Entry
	}{
		{"成本", Entry{Cost: 100}, Entry{Cost: 101}},
		{"讀音", Entry{Cost: 100, Reading: "あ"}, Entry{Cost: 100, Reading: "い"}},
		{"詞性", Entry{Cost: 100, Reading: "あ", POS: "動詞"}, Entry{Cost: 100, Reading: "あ", POS: "名詞"}},
		{"活用型", Entry{Cost: 100, Reading: "あ", POS: "p", ConjType: "一段"}, Entry{Cost: 100, Reading: "あ", POS: "p", ConjType: "五段"}},
		{"活用形", Entry{Cost: 100, Reading: "あ", POS: "p", ConjType: "t", ConjForm: "終止形"}, Entry{Cost: 100, Reading: "あ", POS: "p", ConjType: "t", ConjForm: "連用形"}},
	}
	for _, c := range cases {
		if got := CompareEntries(c.a, c.b); got >= 0 {
			t.Fatalf("%s：CompareEntries = %d，預期負值", c.name, got)
		}
		if got := CompareEntries(c.b, c.a); got <= 0 {
			t.Fatalf("%s：反向 CompareEntries = %d，預期正值", c.name, got)
		}
	}
}

func TestSortSurfaceEntriesStable(t *testing.T) {
	// Flags 只用來辨識詞條；排序欄位完全相同時必須保留輸入順序。
	entries := []Entry{
		{Cost: 500, Reading: "あ", POS: "p", ConjType: "t", ConjForm: "z", Flags: 1},
		{Cost: 500, Reading: "あ", POS: "p", ConjType: "t", ConjForm: "a", Flags: 2},
		{Cost: 500, Reading: "あ", POS: "p", ConjType: "t", ConjForm: "a", Flags: 3},
		{Cost: 400, Reading: "え", POS: "p", ConjType: "t", ConjForm: "a", Flags: 4},
	}
	SortSurfaceEntries(entries)
	var got []uint16
	for _, e := range entries {
		got = append(got, e.Flags)
	}
	want := []uint16{4, 2, 3, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("排序後 Flags = %v，預期 %v", got, want)
		}
	}
}

func TestConvertSignedCostsKeepOrder(t *testing.T) {
	// mapping D 保留 signed cost：-4.215 比 -3.955 更小，所以
	// 「けんきゅうじょ」排在「けんきゅうしょ」之前。clamp 會讓兩筆同為 0，
	// 改由讀音決勝，順序相反。
	data := encodeTestPack(t, []testEntry{
		{surface: "研究所", reading: "けんきゅうじょ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: -4215},
		{surface: "研究所", reading: "けんきゅうしょ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: -3955},
	}, nil, nil, nil)
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Convert(&buf, p, ConvertOptions{}); err != nil {
		t.Fatal(err)
	}
	var readings []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.HasPrefix(line, "研究所\t") {
			readings = append(readings, strings.Split(line, "\t")[1])
		}
	}
	want := []string{"け ん き ゅ う じ ょ", "け ん き ゅ う し ょ"}
	if len(readings) != 2 || readings[0] != want[0] || readings[1] != want[1] {
		t.Fatalf("讀音順序 = %v，預期 %v", readings, want)
	}
}

func TestConvertSignedCostsRankWithinSurface(t *testing.T) {
	// 負成本保留原值：同 surface 內必須排在 0 成本與正成本之前。
	data := encodeTestPack(t, []testEntry{
		{surface: "順", reading: "う", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 500},
		{surface: "順", reading: "あ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: -500},
		{surface: "順", reading: "い", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 0},
	}, nil, nil, nil)
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	stats, err := Convert(&buf, p, ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.NegativeCosts != 1 {
		t.Fatalf("NegativeCosts = %d", stats.NegativeCosts)
	}
	var costs []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		costs = append(costs, strings.Split(line, "\t")[2])
	}
	want := []string{"-0.5", "0", "0.5"}
	if len(costs) != len(want) {
		t.Fatalf("成本 = %v，預期 %v", costs, want)
	}
	for i := range want {
		if costs[i] != want[i] {
			t.Fatalf("成本 = %v，預期 %v", costs, want)
		}
	}
}

func TestConvertOrdersSameSurface(t *testing.T) {
	data := encodeTestPack(t, []testEntry{
		{surface: "順", reading: "い", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 500},
		{surface: "順", reading: "あ", pos: "名詞,普通名詞,一般,*", conjType: "*", conjForm: "*", cost: 500},
		{surface: "順", reading: "あ", pos: "動詞,一般,*,*", conjType: "五段", conjForm: "終止形-一般", cost: 500},
		{surface: "順", reading: "あ", pos: "動詞,一般,*,*", conjType: "五段", conjForm: "連用形-一般", cost: 500},
		{surface: "順", reading: "あ", pos: "動詞,一般,*,*", conjType: "五段", conjForm: "終止形-一般", cost: 100},
	}, []string{"名詞,普通名詞,一般,*", "動詞,一般,*,*"}, []string{"*", "五段"}, []string{"*", "終止形-一般", "連用形-一般"})
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Convert(&buf, p, ConvertOptions{}); err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if !strings.HasPrefix(line, "#") {
			rows = append(rows, line)
		}
	}
	want := []string{
		"順\tあ\t0.1\tverb",
		"順\tあ\t0.5\tverb",
		"順\tあ\t0.5\tverb",
		"順\tあ\t0.5\tnoun",
		"順\tい\t0.5\tnoun",
	}
	for i, prefix := range want {
		if !strings.HasPrefix(rows[i], prefix) {
			t.Fatalf("第 %d 行 = %q，預期前綴 %q\n全部：\n%s", i, rows[i], prefix, buf.String())
		}
	}
}
