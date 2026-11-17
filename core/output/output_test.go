package output_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/output"
	"github.com/0c-biggo/0c-ime/core/segment"
)

var (
	_ output.Renderer   = fakeRenderer{}
	_ output.Punctuator = fakePunctuator{}
	_ output.Spacer     = fakeSpacer{}
	_ output.Assembler  = fakeAssembler{}
)

func TestVariantAliases(t *testing.T) {
	if output.VariantZhHant != lang.VariantZhHant ||
		output.VariantZhHans != lang.VariantZhHans ||
		output.VariantJaShinjitai != lang.VariantJaShinjitai ||
		output.VariantJaKyujitai != lang.VariantJaKyujitai ||
		output.VariantEnglish != lang.VariantEnglish {
		t.Fatal("output 變體常數必須與 lang 相同")
	}
	if got := output.DefaultVariant(lang.Zh); got != output.VariantZhHant {
		t.Errorf("DefaultVariant(Zh) = %q", got)
	}
	if got := output.DefaultVariant(lang.Ja); got != output.VariantJa {
		t.Errorf("DefaultVariant(Ja) = %q", got)
	}
	if got := output.DefaultVariant(lang.En); got != output.VariantEn {
		t.Errorf("DefaultVariant(En) = %q", got)
	}
	var v output.OutputVariant = output.VariantJaKyujitai
	if !v.Valid() || v.Language() != lang.Ja {
		t.Errorf("variant alias 遺失語意: %q", v)
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := output.DefaultOptions()
	if !opts.SpaceCJKLatin || !opts.Punctuate {
		t.Fatalf("DefaultOptions = %#v", opts)
	}
}

func TestRendererAndPunctuator(t *testing.T) {
	seg := segment.Segment{Surface: "です", Variant: output.VariantJa}
	got, err := fakeRenderer{}.Render(seg)
	if err != nil || got != "です" {
		t.Fatalf("Render = %q, %v", got, err)
	}
	if _, err := (fakeRenderer{}).Render(segment.Segment{Variant: "zh-TW"}); err == nil {
		t.Error("不支援的變體應回傳錯誤")
	}
	if got := (fakePunctuator{}).Insert("です", lang.Tag(lang.Ja)); got != "です。" {
		t.Errorf("Insert = %q", got)
	}
	if got := (fakePunctuator{}).Insert("is", lang.Tag(lang.En)); got != "is" {
		t.Errorf("英文不應補日文標點: %q", got)
	}
}

func TestAssemblerKeepsPerSegmentVariants(t *testing.T) {
	segs := []segment.Segment{
		{Script: segment.ScriptHan, Surface: "私", Language: lang.Tag(lang.Ja), Variant: output.VariantJa},
		{Script: segment.ScriptLatin, Surface: "is", Language: lang.Tag(lang.En), Variant: output.VariantEnglish},
	}
	got, err := fakeAssembler{}.Assemble(segs, output.Options{SpaceCJKLatin: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "私 is" {
		t.Fatalf("Assemble = %q, want 私 is", got)
	}
	if _, err := (fakeAssembler{}).Assemble(segs, output.Options{}); err != nil {
		t.Fatalf("關閉間距仍應成功: %v", err)
	}
}

type fakeRenderer struct{}

func (fakeRenderer) Render(seg segment.Segment) (string, error) {
	if seg.Variant != "" && !seg.Variant.Valid() {
		return "", errors.New("unsupported variant")
	}
	return seg.Surface, nil
}

type fakePunctuator struct{}

func (fakePunctuator) Insert(text string, tag lang.LanguageTag) string {
	if tag.Language == lang.Ja {
		return text + "。"
	}
	return text
}

type fakeSpacer struct{}

func (fakeSpacer) Between(left, right segment.Segment) string {
	if left.Script == segment.ScriptHan && right.Script == segment.ScriptLatin {
		return " "
	}
	return ""
}

type fakeAssembler struct{}

func (fakeAssembler) Assemble(segs []segment.Segment, opts output.Options) (string, error) {
	var b strings.Builder
	for i, seg := range segs {
		text, err := (fakeRenderer{}).Render(seg)
		if err != nil {
			return "", err
		}
		if opts.Punctuate {
			text = (fakePunctuator{}).Insert(text, seg.Language)
		}
		if i > 0 && opts.SpaceCJKLatin {
			b.WriteString((fakeSpacer{}).Between(segs[i-1], seg))
		}
		b.WriteString(text)
	}
	return b.String(), nil
}
