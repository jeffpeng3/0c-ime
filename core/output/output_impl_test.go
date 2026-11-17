package output_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/output"
	"github.com/0c-biggo/0c-ime/core/segment"
	"github.com/0c-biggo/0c-ime/core/zhconv"
)

func tinySimplifier(t *testing.T) *zhconv.Converter {
	t.Helper()
	c, err := zhconv.TW2S(zhconv.Data{
		CJKCompatibility:     "# 相容表意字\n\uF900\t豈\n",
		TWVariants:           "着\t著\n",
		TWVariantsRevPhrases: "什麼\t什麼\n",
		TSPhrases:            "計畫\t计划\n",
		TSCharacters:         "臺\t台\n灣\t湾\n學\t学\n計\t计\n畫\t画\n師\t师\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestVariantRendererPerVariant(t *testing.T) {
	simplifier := tinySimplifier(t)
	r := output.NewRenderer(simplifier)
	tests := []struct {
		name string
		seg  segment.Segment
		want string
		err  error
	}{
		{
			name: "繁中原樣輸出",
			seg:  segment.Segment{Surface: "臺灣", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHant},
			want: "臺灣",
		},
		{
			name: "簡中逐片段轉換",
			seg:  segment.Segment{Surface: "臺灣計畫", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHans},
			want: "台湾计划",
		},
		{
			name: "日文新字體原樣輸出",
			seg:  segment.Segment{Surface: "日本語です", Language: lang.Tag(lang.Ja), Variant: output.VariantJaShinjitai},
			want: "日本語です",
		},
		{
			name: "日文舊字體第一版不支援",
			seg:  segment.Segment{Surface: "國", Language: lang.Tag(lang.Ja), Variant: output.VariantJaKyujitai},
			err:  output.ErrUnsupportedVariant,
		},
		{
			name: "英文原文不動",
			seg:  segment.Segment{Surface: "Hello, world! 123", Language: lang.Tag(lang.En), Variant: output.VariantEnglish},
			want: "Hello, world! 123",
		},
		{
			name: "空變體依中文預設繁中",
			seg:  segment.Segment{Surface: "臺灣", Language: lang.Tag(lang.Zh)},
			want: "臺灣",
		},
		{
			name: "空變體依日文預設新字體",
			seg:  segment.Segment{Surface: "日本語", Language: lang.Tag(lang.Ja)},
			want: "日本語",
		},
		{
			name: "空變體依英文預設原文",
			seg:  segment.Segment{Surface: "verbatim", Language: lang.Tag(lang.En)},
			want: "verbatim",
		},
		{
			name: "未知語言沒有變體時保留原文",
			seg:  segment.Segment{Surface: "raw"},
			want: "raw",
		},
		{
			name: "未知變體報錯",
			seg:  segment.Segment{Surface: "臺灣", Language: lang.Tag(lang.Zh), Variant: "zh-TW"},
			err:  output.ErrUnsupportedVariant,
		},
		{
			name: "語言與變體不符報錯",
			seg:  segment.Segment{Surface: "臺灣", Language: lang.Tag(lang.En), Variant: output.VariantZhHant},
			err:  output.ErrVariantMismatch,
		},
		{
			name: "無效片段報錯",
			seg:  segment.Segment{Span: segment.Span{Start: -1, End: 2}, Surface: "臺灣"},
			err:  output.ErrInvalidSegment,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Render(tt.seg)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("Render error = %v，預期 %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Render = %q，預期 %q", got, tt.want)
			}
		})
	}
}

func TestVariantRendererNeedsSimplifierOnlyForHans(t *testing.T) {
	calls := 0
	r := output.NewRenderer(output.SimplifierFunc(func(text string) string {
		calls++
		return "簡"
	}))
	if _, err := r.Render(segment.Segment{Surface: "繁", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHant}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("繁中不應呼叫簡體轉換器，呼叫 %d 次", calls)
	}
	got, err := r.Render(segment.Segment{Surface: "繁", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHans})
	if err != nil || got != "簡" || calls != 1 {
		t.Fatalf("Render = %q, %v；呼叫 %d 次", got, err, calls)
	}

	if _, err := output.NewRenderer(nil).Render(segment.Segment{Surface: "繁", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHans}); !errors.Is(err, output.ErrMissingSimplifier) {
		t.Fatalf("沒有轉換器時 error = %v，預期 ErrMissingSimplifier", err)
	}
}

func TestVariantRendererDoesNotModifySegment(t *testing.T) {
	seg := segment.Segment{
		Span:     segment.Span{Start: 0, End: 2},
		Script:   segment.ScriptHan,
		Surface:  "臺灣",
		Language: lang.Tag(lang.Zh),
		Variant:  output.VariantZhHans,
	}
	before := seg
	if _, err := output.NewRenderer(tinySimplifier(t)).Render(seg); err != nil {
		t.Fatal(err)
	}
	if seg.Surface != before.Surface || seg.Variant != before.Variant || seg.Language != before.Language {
		t.Fatalf("Render 修改了片段: %#v", seg)
	}
}

func TestLanguagePunctuator(t *testing.T) {
	p := output.LanguagePunctuator{}
	tests := []struct {
		name string
		text string
		tag  lang.LanguageTag
		want string
	}{
		{"中文逗號", "你好,世界", lang.Tag(lang.Zh), "你好，世界"},
		{"中文句號與驚嘆號", "完成了!", lang.Tag(lang.Zh), "完成了！"},
		{"中文問號與冒號", "這是什麼?:好", lang.Tag(lang.Zh), "這是什麼？：好"},
		{"日文逗號用頓號", "こんにちは,世界", lang.Tag(lang.Ja), "こんにちは、世界"},
		{"日文句號", "です.", lang.Tag(lang.Ja), "です。"},
		{"英文原文不動", "Hello, world!", lang.Tag(lang.En), "Hello, world!"},
		{"未知語言不動", "Hello, world!", lang.LanguageTag{}, "Hello, world!"},
		{"小數點不動", "版本3.14", lang.Tag(lang.Zh), "版本3.14"},
		{"檔名不動", "檔案a.txt", lang.Tag(lang.Zh), "檔案a.txt"},
		{"純英文片段即使標中文也不動", "hello,world", lang.Tag(lang.Zh), "hello,world"},
		{"已是全形不動", "你好，世界。", lang.Tag(lang.Zh), "你好，世界。"},
		{"只有單側 CJK 也轉換", "OK,你好", lang.Tag(lang.Zh), "OK，你好"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Insert(tt.text, tt.tag); got != tt.want {
				t.Fatalf("Insert(%q) = %q，預期 %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestCJKLatinSpacer(t *testing.T) {
	spacer := output.CJKLatinSpacer{}
	seg := func(script segment.Script, surface string) segment.Segment {
		return segment.Segment{Script: script, Surface: surface}
	}
	tests := []struct {
		name        string
		left, right segment.Segment
		want        string
	}{
		{"漢字到英文", seg(segment.ScriptHan, "私"), seg(segment.ScriptLatin, "is"), " "},
		{"英文到漢字", seg(segment.ScriptLatin, "is"), seg(segment.ScriptHan, "學生"), " "},
		{"かな到英文", seg(segment.ScriptKana, "です"), seg(segment.ScriptLatin, "OK"), " "},
		{"長音符到英文", seg(segment.ScriptKana, "コンピューター"), seg(segment.ScriptLatin, "ABC"), " "},
		{"漢字到數字", seg(segment.ScriptHan, "私"), seg(segment.ScriptDigit, "123"), ""},
		{"英文到英文", seg(segment.ScriptLatin, "abc"), seg(segment.ScriptLatin, "def"), ""},
		{"既有空白不重複", seg(segment.ScriptHan, "私 "), seg(segment.ScriptLatin, "is"), ""},
		{"空片段", seg(segment.ScriptHan, ""), seg(segment.ScriptLatin, "is"), ""},
		{"英文到漢字檔案", seg(segment.ScriptLatin, "Python"), seg(segment.ScriptHan, "文件"), " "},
		{"標點不算 CJK", seg(segment.ScriptSymbol, "「"), seg(segment.ScriptLatin, "a"), ""},
		{"踊り字算 CJK", seg(segment.ScriptKana, "々"), seg(segment.ScriptLatin, "a"), " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := spacer.Between(tt.left, tt.right); got != tt.want {
				t.Fatalf("Between(%q, %q) = %q，預期 %q", tt.left.Surface, tt.right.Surface, got, tt.want)
			}
		})
	}
}

func TestDefaultAssemblerKeepsPerSegmentVariants(t *testing.T) {
	a := output.NewAssembler(output.NewRenderer(tinySimplifier(t)), nil, nil)
	segs := []segment.Segment{
		{Script: segment.ScriptHan, Surface: "私", Language: lang.Tag(lang.Ja), Variant: output.VariantJaShinjitai},
		{Script: segment.ScriptLatin, Surface: "use", Language: lang.Tag(lang.En), Variant: output.VariantEnglish},
		{Script: segment.ScriptHan, Surface: "臺灣", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHant},
		{Script: segment.ScriptLatin, Surface: "and", Language: lang.Tag(lang.En), Variant: output.VariantEnglish},
		{Script: segment.ScriptHan, Surface: "工程師", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHans},
		{Script: segment.ScriptKana, Surface: "です", Language: lang.Tag(lang.Ja), Variant: output.VariantJaShinjitai},
	}
	got, err := a.Assemble(segs, output.Options{SpaceCJKLatin: true})
	if err != nil {
		t.Fatal(err)
	}
	const want = "私 use 臺灣 and 工程师です"
	if got != want {
		t.Fatalf("Assemble = %q，預期 %q", got, want)
	}
}

func TestDefaultAssemblerPunctuationFollowsSegmentLanguage(t *testing.T) {
	a := output.NewAssembler(nil, nil, nil)
	segs := []segment.Segment{
		{Script: segment.ScriptHan, Surface: "你好,世界", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHant},
		{Script: segment.ScriptLatin, Surface: "Hello, world!", Language: lang.Tag(lang.En), Variant: output.VariantEnglish},
		{Script: segment.ScriptKana, Surface: "これは,ペン", Language: lang.Tag(lang.Ja), Variant: output.VariantJaShinjitai},
	}

	got, err := a.Assemble(segs, output.Options{SpaceCJKLatin: true, Punctuate: true})
	if err != nil {
		t.Fatal(err)
	}
	const want = "你好，世界 Hello, world!これは、ペン"
	if got != want {
		t.Fatalf("Assemble = %q，預期 %q", got, want)
	}

	got, err = a.Assemble(segs, output.Options{SpaceCJKLatin: true})
	if err != nil {
		t.Fatal(err)
	}
	const raw = "你好,世界 Hello, world!これは,ペン"
	if got != raw {
		t.Fatalf("關閉標點後 Assemble = %q，預期 %q", got, raw)
	}

	got, err = a.Assemble(segs, output.Options{Punctuate: true})
	if err != nil {
		t.Fatal(err)
	}
	const noSpace = "你好，世界Hello, world!これは、ペン"
	if got != noSpace {
		t.Fatalf("關閉間距後 Assemble = %q，預期 %q", got, noSpace)
	}
}

func TestDefaultAssemblerSimplifiesOnlyHansSegments(t *testing.T) {
	a := output.NewAssembler(output.NewRenderer(tinySimplifier(t)), nil, nil)
	segs := []segment.Segment{
		{Script: segment.ScriptHan, Surface: "臺灣,計畫", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHans},
	}
	got, err := a.Assemble(segs, output.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got != "台湾，计划" {
		t.Fatalf("Assemble = %q，預期 台湾，计划", got)
	}
}

func TestDefaultAssemblerReportsSegmentIndexAndCause(t *testing.T) {
	a := output.NewAssembler(nil, nil, nil)
	segs := []segment.Segment{
		{Surface: "ok", Language: lang.Tag(lang.En), Variant: output.VariantEnglish},
		{Surface: "臺灣", Language: lang.Tag(lang.Zh), Variant: "zh-TW"},
	}
	_, err := a.Assemble(segs, output.DefaultOptions())
	if !errors.Is(err, output.ErrUnsupportedVariant) {
		t.Fatalf("error = %v，預期 ErrUnsupportedVariant", err)
	}
	if !strings.Contains(err.Error(), "片段 1") {
		t.Fatalf("error = %v，應含片段索引", err)
	}

	_, err = a.Assemble([]segment.Segment{
		{Surface: "臺灣", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHans},
	}, output.DefaultOptions())
	if !errors.Is(err, output.ErrMissingSimplifier) {
		t.Fatalf("error = %v，預期 ErrMissingSimplifier", err)
	}
}

func TestDefaultAssemblerSkipsEmptySegments(t *testing.T) {
	var a output.DefaultAssembler // 零值也必須可用
	segs := []segment.Segment{
		{Surface: "私", Language: lang.Tag(lang.Ja), Variant: output.VariantJa},
		{Surface: "", Language: lang.Tag(lang.En), Variant: output.VariantEn},
		{Surface: "is", Language: lang.Tag(lang.En), Variant: output.VariantEn},
	}
	got, err := a.Assemble(segs, output.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got != "私 is" {
		t.Fatalf("Assemble = %q，預期 私 is", got)
	}

	got, err = a.Assemble(nil, output.DefaultOptions())
	if err != nil || got != "" {
		t.Fatalf("空輸入 = %q, %v", got, err)
	}
}

func TestDefaultAssemblerDoesNotModifySegments(t *testing.T) {
	a := output.NewAssembler(output.NewRenderer(tinySimplifier(t)), nil, nil)
	segs := []segment.Segment{
		{Surface: "臺灣,計畫", Language: lang.Tag(lang.Zh), Variant: output.VariantZhHant},
		{Surface: "is", Language: lang.Tag(lang.En), Variant: output.VariantEn},
	}
	before := append([]segment.Segment(nil), segs...)
	if _, err := a.Assemble(segs, output.DefaultOptions()); err != nil {
		t.Fatal(err)
	}
	for i := range segs {
		if !reflect.DeepEqual(segs[i], before[i]) {
			t.Fatalf("片段 %d 被修改: %#v，原本 %#v", i, segs[i], before[i])
		}
	}
}
