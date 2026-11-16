package lexicon

import (
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

func TestLoadValidates(t *testing.T) {
	good := "# 註解\n丼\tㄉㄨㄥˋ\n丼\tㄐㄧㄥˇ\t0.1\n應該\tㄧㄣ ㄍㄞ\t0.3\tcorrection\n"
	m, err := Load(strings.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	if es := m.Exact(zhuyin.ParseReading("ㄧㄣ ㄍㄞ")); len(es) != 1 || !es[0].Correction || es[0].PRW != 0.3 {
		t.Errorf("%+v", es)
	}
	if es := m.Prefixed(zhuyin.ParseReading("ㄧㄣ")); len(es) != 1 {
		t.Errorf("前綴查詢：%+v", es)
	}
	if !m.Has("丼") || m.Has("景") || !m.HasPrefix(zhuyin.ParseReading("ㄧㄣ")) || m.HasPrefix(zhuyin.ParseReading("ㄍㄞ")) {
		t.Error("Has／HasPrefix")
	}
	for name, bad := range map[string]string{
		"音節數與字數不符": "親子丼\tㄑㄧㄣ ㄗˇ\n",
		"音節不合法":    "丼\tㄉㄨㄥx\n",
		"機率超出範圍":   "丼\tㄉㄨㄥˋ\t1.5\n",
		"缺少讀音":     "丼\n",
	} {
		if _, err := Load(strings.NewReader(bad)); err == nil {
			t.Errorf("%s：應回報錯誤", name)
		}
	}
}

// #142：系統詞庫回報最長的詞條讀音，讓解碼器在有長詞時搜尋到 8 個音節以後。
// Compact 由詞條長度算出，不寫進資料檔：舊資料包讀進來一樣有值，格式不變。
func TestLongestWord(t *testing.T) {
	long := "阿姆斯特朗回旋加速噴氣式阿姆斯特朗炮"
	reading := "ㄚ ㄇㄨˇ ㄙ ㄊㄜˋ ㄌㄤˇ ㄏㄨㄟˊ ㄒㄩㄢˊ ㄐㄧㄚ ㄙㄨˋ ㄆㄣ ㄑㄧˋ ㄕˋ ㄚ ㄇㄨˇ ㄙ ㄊㄜˋ ㄌㄤˇ ㄆㄠˋ"
	es := []Entry{
		{Surface: "阿姆斯特朗", Reading: zhuyin.ReadingUnits(zhuyin.ParseReading("ㄚ ㄇㄨˇ ㄙ ㄊㄜˋ ㄌㄤˇ")), PRW: 1},
		{Surface: "喔", Reading: zhuyin.ReadingUnits(zhuyin.ParseReading("ㄛ")), PRW: 1},
	}
	short, err := Build(es)
	if err != nil {
		t.Fatal(err)
	}
	if got := Longest(short); got != 5 {
		t.Errorf("短詞庫：%d", got)
	}
	es = append(es, Entry{Surface: long, Reading: zhuyin.ReadingUnits(zhuyin.ParseReading(reading)), PRW: 1})
	mem := NewMem()
	for _, e := range es {
		mem.Add(e)
	}
	built, err := Build(es)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(built.Encode())
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]Source{"Mem": mem, "Build": built, "Decode": decoded, "輕聲語氣詞": NeutralParticles(decoded)} {
		if got := Longest(src); got != 18 {
			t.Errorf("%s：%d", name, got)
		}
	}
}

// 日文可変長讀音：兩字的詞可以對上四個單位，送り仮名也各自算單位。
func TestLoadVariableLengthReading(t *testing.T) {
	tsv := "妖精\tよ う せ い\n今日\tき ょ う\n食べた\tた べ た\nお茶\tお ち ゃ\n"
	m, err := Load(strings.NewReader(tsv))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		reading reading.Sequence
		want    string
	}{
		{reading.Sequence{"よ", "う", "せ", "い"}, "妖精"},
		{reading.Sequence{"き", "ょ", "う"}, "今日"},
		{reading.Sequence{"た", "べ", "た"}, "食べた"},
		{reading.Sequence{"お", "ち", "ゃ"}, "お茶"},
	}
	for _, c := range cases {
		es := m.Exact(zhuyin.Syllables(c.reading))
		if len(es) != 1 || es[0].Surface != c.want {
			t.Errorf("Exact(%s) = %+v, want %q", reading.Key(c.reading), es, c.want)
		}
		if len(es) == 1 && reading.Key(es[0].Reading) != reading.Key(c.reading) {
			t.Errorf("%q 的讀音 = %s, want %s", c.want, reading.Key(es[0].Reading), reading.Key(c.reading))
		}
	}
	if got := m.LongestWord(); got != 4 {
		t.Errorf("LongestWord = %d, want 4", got)
	}
	// 前綴查詢也要跨過詞面長度。
	if es := m.Prefixed(zhuyin.Syllables(reading.Sequence{"た", "べ"})); len(es) != 1 || es[0].Surface != "食べた" {
		t.Errorf("Prefixed = %+v", es)
	}
}

// 注音仍然嚴格：詞面字數必須等於讀音單位數。
func TestLoadKeepsZhuyinStrict(t *testing.T) {
	for name, line := range map[string]string{
		"少讀音": "親子丼\tㄑㄧㄣ ㄗˇ\n",
		"多讀音": "丼\tㄉㄨㄥˋ ㄐㄧㄥˇ\n",
	} {
		if _, err := Load(strings.NewReader(line)); err == nil {
			t.Errorf("%s：應回報錯誤", name)
		}
	}
}

func TestLoadExplicitLanguageAndLimit(t *testing.T) {
	if _, err := LoadWithOptions(strings.NewReader("妖精\tよ う せ い\n"), LoadOptions{Language: "zh"}); err == nil {
		t.Error("指定注音時，かな讀音應被拒絕")
	}
	if _, err := LoadWithOptions(strings.NewReader("妖精\tよ う せ い\n"), LoadOptions{Language: "ja"}); err != nil {
		t.Errorf("指定日文時應接受可変長：%v", err)
	}
	if _, err := LoadWithOptions(strings.NewReader("妖精\tよ う せ い\n"), LoadOptions{Language: "ja", MaxUnits: 3}); err == nil {
		t.Error("超過 manifest 宣告的詞長上限應被拒絕")
	}
}
