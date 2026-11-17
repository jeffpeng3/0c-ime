package output

import (
	"fmt"
	"strings"

	"github.com/0c-biggo/0c-ime/core/segment"
)

// DefaultAssembler 是 Assembler 的標準實作：逐片段轉換輸出變體，再依
// Options 補標點與 CJK/Latin 間距。
//
// 三個相依元件都可以為 nil，nil 時使用 VariantRenderer、LanguagePunctuator
// 與 CJKLatinSpacer。沒有提供 Simplifier 時仍可組裝繁體、日文與英文，
// 只有遇到簡體片段才會回傳 ErrMissingSimplifier。
type DefaultAssembler struct {
	Renderer   Renderer
	Punctuator Punctuator
	Spacer     Spacer
}

// NewAssembler 建立組裝器。任一參數為 nil 時使用預設實作。
func NewAssembler(renderer Renderer, punctuator Punctuator, spacer Spacer) DefaultAssembler {
	return DefaultAssembler{Renderer: renderer, Punctuator: punctuator, Spacer: spacer}
}

// Assemble 依輸入順序組成輸出文字。
//
// 每個片段先用自己的輸出變體轉換，再套用該片段的語言標點與相鄰間距；
// 不會把整段文字當成單一語言重新轉換。任何片段失敗都會回傳錯誤，錯誤
// 帶有片段索引，且 errors.Is 可判斷原始原因。
func (a DefaultAssembler) Assemble(segments []segment.Segment, opts Options) (string, error) {
	if len(segments) == 0 {
		return "", nil
	}
	renderer := a.Renderer
	if renderer == nil {
		renderer = VariantRenderer{}
	}
	punctuator := a.Punctuator
	if punctuator == nil {
		punctuator = LanguagePunctuator{}
	}
	spacer := a.Spacer
	if spacer == nil {
		spacer = CJKLatinSpacer{}
	}

	var b strings.Builder
	var prev segment.Segment
	havePrev := false
	for i, seg := range segments {
		text, err := renderer.Render(seg)
		if err != nil {
			return "", fmt.Errorf("output: 片段 %d: %w", i, err)
		}
		if opts.Punctuate && text != "" {
			text = punctuator.Insert(text, effectiveTag(seg))
		}
		if text == "" {
			continue
		}
		if havePrev && opts.SpaceCJKLatin {
			b.WriteString(spacer.Between(prev, seg))
		}
		b.WriteString(text)
		prev, havePrev = seg, true
	}
	return b.String(), nil
}
