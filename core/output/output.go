// Package output 定義逐片段輸出組裝的契約。
//
// 每個片段保有自己的語言標籤與輸出變體：中文片段可以是繁中或簡中，日文片段
// 第一版是新字體，英文保留原文。組裝器只在片段之間處理標點與 CJK/Latin
// 間距，不得把整段文字當成單一語言重新轉換。
//
// 標準實作是 DefaultAssembler；它搭配 VariantRenderer、LanguagePunctuator
// 與 CJKLatinSpacer。簡體轉換器由宿主提供，實作見 *zhconv.Converter。
package output

import (
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/segment"
)

// OutputVariant 是 lang.OutputVariant 的別名，讓輸出相關程式可以只 import
// 這個套件。值與語意都由 core/lang 定義。
type OutputVariant = lang.OutputVariant

// 輸出變體常數的別名。
const (
	VariantZhHant      = lang.VariantZhHant
	VariantZhHans      = lang.VariantZhHans
	VariantJaShinjitai = lang.VariantJaShinjitai
	VariantJaKyujitai  = lang.VariantJaKyujitai
	VariantEnglish     = lang.VariantEnglish

	// VariantJa 與 VariantEn 是慣用短名。
	VariantJa = lang.VariantJa
	VariantEn = lang.VariantEn
)

// DefaultVariant 回傳語言的預設輸出變體，與 lang.DefaultVariant 相同。
func DefaultVariant(id lang.ID) OutputVariant { return lang.DefaultVariant(id) }

// Options 是輸出組裝選項。
type Options struct {
	// SpaceCJKLatin 在 CJK 片段與 Latin 片段之間插入一個半角空格。
	SpaceCJKLatin bool
	// Punctuate 允許標點器依片段語言補標點；關閉時只輸出候選詞面。
	Punctuate bool
}

// DefaultOptions 是產品預設：CJK/Latin 之間加空格，並套用標點。
func DefaultOptions() Options {
	return Options{SpaceCJKLatin: true, Punctuate: true}
}

// Renderer 把單一片段轉成輸出文字。
//
// Segment.Variant 為空時，實作使用 DefaultVariant(Segment.Language.Language)。
// 不支援的變體必須回傳錯誤，不得默默輸出另一種字形。Renderer 不得修改片段。
type Renderer interface {
	Render(seg segment.Segment) (string, error)
}

// Punctuator 依片段語言補標點。
//
// Insert 只處理要插入的標點或空白，不得改寫既有詞面；未知語言可原樣回傳。
type Punctuator interface {
	Insert(text string, tag lang.LanguageTag) string
}

// Spacer 決定兩個相鄰片段之間的分隔字串。
//
// 回傳空字串表示不加空格。實作只看兩側片段，不讀取全域語言模式；
// 英文與 CJK 之間是否加空格由 Options.SpaceCJKLatin 控制。
type Spacer interface {
	Between(left, right segment.Segment) string
}

// Assembler 把已選定的片段依序組成輸出文字。
//
// segments 必須依輸入順序排列；實作逐一套用每個片段自己的 Variant，
// 不得用單一語言轉換整段文字。任何不支援的變體都應回傳錯誤。
type Assembler interface {
	Assemble(segments []segment.Segment, opts Options) (string, error)
}
