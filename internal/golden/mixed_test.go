package golden

import (
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/segment"
)

const mixedDir = "../../testdata/golden/mixed"

// TestMixedFixtures 守住 P2-03 的交付：情境 A/B/C 與長詞跨片段案例都要在，
// 而且整份 fixtures 通過 LoadMixed 的契約檢查。LoadMixed 用 DisallowUnknownFields，
// 所以 fixture 不可能偷偷加一個 single active language 欄位。
func TestMixedFixtures(t *testing.T) {
	s, err := LoadMixed(mixedDir)
	if err != nil {
		t.Fatal(err)
	}
	cases := s.Cases()
	if len(cases) < 6 {
		t.Errorf("混合 fixture 只有 %d 個，至少需要 6 個", len(cases))
	}
	long := 0
	for _, c := range cases {
		if strings.HasPrefix(c.Name, "long-word-cross-segment-") {
			long++
		}
	}
	if long < 3 {
		t.Errorf("長詞跨片段案例只有 %d 個，至少需要 3 個", long)
	}
	for _, name := range []string{
		"scenario-a-primary-ja",
		"scenario-b-primary-zh",
		"scenario-c-proper-nouns",
	} {
		if _, ok := s.Case(name); !ok {
			t.Errorf("缺少案例 %s", name)
		}
	}
}

// TestMixedScenarios 檢查 A/B/C 的語言片段與輸出變體，並確認主要語言只是
// 偏好：A 與 B 用同一串輸入、不同 primary，片段與輸出必須完全相同。
func TestMixedScenarios(t *testing.T) {
	s, err := LoadMixed(mixedDir)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := s.Case("scenario-a-primary-ja")
	if !ok {
		t.Fatal("缺少情境 A")
	}
	b, ok := s.Case("scenario-b-primary-zh")
	if !ok {
		t.Fatal("缺少情境 B")
	}
	if a.Primary != lang.Ja || b.Primary != lang.Zh {
		t.Fatalf("情境 A/B 的 primary 應是 ja/zh，實際是 %q/%q", a.Primary, b.Primary)
	}
	if a.Input != b.Input {
		t.Fatalf("情境 A/B 應該是同一串輸入：%q vs %q", a.Input, b.Input)
	}
	if a.Output != b.Output {
		t.Errorf("主要語言不得改變同一串輸入的輸出：%q vs %q", a.Output, b.Output)
	}
	if len(a.Segments) != len(b.Segments) {
		t.Fatalf("情境 A/B 片段數不同：%d vs %d", len(a.Segments), len(b.Segments))
	}
	for i := range a.Segments {
		x, y := a.Segments[i], b.Segments[i]
		if x.Span != y.Span || x.Script != y.Script || x.Surface != y.Surface ||
			x.Language != y.Language || x.Variant != y.Variant {
			t.Errorf("情境 A/B 第 %d 個片段不同：%+v vs %+v", i+1, x.Segment, y.Segment)
		}
	}
	for _, c := range []MixedCase{a, b} {
		langs := map[lang.ID]bool{}
		variants := map[lang.OutputVariant]bool{}
		for _, seg := range c.Segments {
			langs[seg.Language.Language] = true
			variants[seg.Variant] = true
		}
		for _, id := range []lang.ID{lang.Zh, lang.Ja, lang.En} {
			if !langs[id] {
				t.Errorf("%s 缺少 %s 片段", c.Name, id)
			}
		}
		for _, v := range []lang.OutputVariant{lang.VariantZhHant, lang.VariantJa, lang.VariantEnglish} {
			if !variants[v] {
				t.Errorf("%s 缺少輸出變體 %s", c.Name, v)
			}
		}
		if !strings.Contains(c.Output, "私わ大雞巴です") {
			t.Errorf("%s 的輸出 %q 少了跨語言部分", c.Name, c.Output)
		}
		if !strings.HasSuffix(c.Output, "is a good sentence") {
			t.Errorf("%s 的英文尾段沒有保留原文：%q", c.Name, c.Output)
		}
	}

	c, ok := s.Case("scenario-c-proper-nouns")
	if !ok {
		t.Fatal("缺少情境 C")
	}
	if c.Primary != lang.Zh {
		t.Errorf("情境 C 的 primary 應是 zh，實際 %q", c.Primary)
	}
	for _, seg := range c.Segments {
		switch seg.Script {
		case segment.ScriptLatin:
			want := string([]rune(c.Input)[seg.Span.Start:seg.Span.End])
			if seg.Surface != want {
				t.Errorf("專有名詞 %q 沒有保留原文（span 內容是 %q）", seg.Surface, want)
			}
			if seg.Variant != lang.VariantEnglish || seg.Language.Language != lang.En {
				t.Errorf("專有名詞 %q 應是 en/en 變體，實際 %q/%q", seg.Surface, seg.Language.Language, seg.Variant)
			}
		case segment.ScriptSymbol:
		default:
			t.Errorf("情境 C 不該有 %s 片段（%q）", seg.Script, seg.Surface)
		}
	}
}

// TestMixedCandidateAlternatives 確認候選清單保留其他語言：主要語言只影響
// 排序，拉丁片段同時有 ja 與 en 候選，注音片段帶 zh 標籤。
func TestMixedCandidateAlternatives(t *testing.T) {
	s, err := LoadMixed(mixedDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"scenario-a-primary-ja", "scenario-b-primary-zh"} {
		c, ok := s.Case(name)
		if !ok {
			t.Fatalf("缺少案例 %s", name)
		}
		crossLanguage := 0
		for _, seg := range c.Segments {
			for _, alt := range seg.Also {
				if alt.Language.Language != seg.Language.Language {
					crossLanguage++
				}
			}
		}
		if crossLanguage < 2 {
			t.Errorf("%s 只有 %d 個跨語言替代候選，Latin 應該同時保留 ja 與 en", name, crossLanguage)
		}
	}

	c, ok := s.Case("scenario-c-proper-nouns")
	if !ok {
		t.Fatal("缺少情境 C")
	}
	for _, seg := range c.Segments {
		if seg.Source != lattice.SourcePersonal {
			continue
		}
		if seg.Surface != "GitHub" {
			t.Errorf("personal 片段應該是 GitHub，實際 %q", seg.Surface)
		}
		if len(seg.Also) == 0 || seg.Also[0].Source != lattice.SourceLiteral {
			t.Errorf("personal 片段 %q 應該保留 literal 候選", seg.Surface)
		}
	}
}

// TestMixedValidationRejects 證明上面的檢查不是裝飾：span 漏字、變體與語言
// 不一致、輸出對不上、假裝跨片段都會被擋下來。
func TestMixedValidationRejects(t *testing.T) {
	s, err := LoadMixed(mixedDir)
	if err != nil {
		t.Fatal(err)
	}
	base, ok := s.Case("long-word-cross-segment-zhuyin")
	if !ok {
		t.Fatal("缺少長詞案例")
	}

	cases := []struct {
		name   string
		mutate func(*MixedCase)
		want   string
	}{
		{
			name: "span 漏字",
			mutate: func(c *MixedCase) {
				c.Segments = append([]MixedSegment(nil), c.Segments...)
				c.Segments[0].Span.Start = 1
			},
			want: "第 1 段從 rune 1 開始",
		},
		{
			name: "輸出變體與語言不一致",
			mutate: func(c *MixedCase) {
				c.Segments = append([]MixedSegment(nil), c.Segments...)
				c.Segments[0].Variant = lang.VariantEnglish
			},
			want: "variant",
		},
		{
			name: "輸出對不上片段詞面",
			mutate: func(c *MixedCase) {
				c.Output = "麻婆豆"
			},
			want: "output 應是",
		},
		{
			name: "piece 邊界都落在片段邊界上",
			mutate: func(c *MixedCase) {
				c.Segments = append([]MixedSegment(nil), c.Segments...)
				c.Segments[0].Span = segment.Span{Start: 0, End: 6}
				c.Segments = append(c.Segments,
					MixedSegment{Segment: segment.Segment{
						Span: segment.Span{Start: 6, End: 12}, Script: segment.ScriptZhuyin,
						Surface: "豆腐", Language: lang.Tag(lang.Zh), Variant: lang.VariantZhHant,
					}, Source: lattice.SourceDictionary})
			},
			want: "沒有跨片段的長詞合併",
		},
		{
			name: "personal 詞沒有對應片段",
			mutate: func(c *MixedCase) {
				c.Personal = []string{"豆花"}
			},
			want: "沒有對應的 personal 片段",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.mutate(&c)
			err := c.Validate()
			if err == nil {
				t.Fatal("預期驗證失敗，實際通過")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("錯誤訊息應含 %q，實際：%v", tc.want, err)
			}
		})
	}
}

// TestMixedLongWordCrossSegment 確認長詞案例真的是「跨片段」：piece 邊界落在
// 合併後片段的中間，合併片段的 span 嚴格大於每個 piece，讀音至少兩個單位。
func TestMixedLongWordCrossSegment(t *testing.T) {
	s, err := LoadMixed(mixedDir)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, c := range s.Cases() {
		if !strings.HasPrefix(c.Name, "long-word-cross-segment-") {
			continue
		}
		found++
		if len(c.Pieces) < 2 {
			t.Errorf("%s 只有 %d 個 piece", c.Name, len(c.Pieces))
			continue
		}
		merged := 0
		for _, seg := range c.Segments {
			absorbed := 0
			for i := 0; i < len(c.Pieces)-1; i++ {
				if end := c.Pieces[i].Span.End; end > seg.Span.Start && end < seg.Span.End {
					absorbed++
				}
			}
			if absorbed == 0 {
				continue
			}
			merged++
			if n := len([]rune(seg.Surface)); n < 2 {
				t.Errorf("%s 的合併片段 %q 只有 %d 個字，不像長詞", c.Name, seg.Surface, n)
			}
			if len(seg.Reading) < 2 {
				t.Errorf("%s 的合併片段 %q 只有 %d 個讀音單位", c.Name, seg.Surface, len(seg.Reading))
			}
			if c.Output != seg.Surface {
				t.Errorf("%s 的輸出 %q 應等於合併片段 %q", c.Name, c.Output, seg.Surface)
			}
		}
		if merged != 1 {
			t.Errorf("%s 有 %d 個片段吸收 piece 邊界，應該恰好 1 個", c.Name, merged)
		}
	}
	if found == 0 {
		t.Fatal("沒有長詞跨片段案例")
	}
}
