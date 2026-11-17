package output

import (
	"errors"
	"fmt"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/segment"
)

// 輸出組裝的錯誤。呼叫端用 errors.Is 判斷原因；錯誤訊息會帶上片段索引。
var (
	// ErrInvalidSegment 表示片段本身的欄位不一致。
	ErrInvalidSegment = errors.New("output: 片段無效")
	// ErrUnsupportedVariant 表示變體不在契約內，或第一版尚未實作。
	ErrUnsupportedVariant = errors.New("output: 不支援的輸出變體")
	// ErrVariantMismatch 表示片段的語言標籤與輸出變體屬於不同語言。
	ErrVariantMismatch = errors.New("output: 語言標籤與輸出變體不符")
	// ErrMissingSimplifier 表示要輸出簡體，但沒有提供繁體轉簡體的轉換器。
	ErrMissingSimplifier = errors.New("output: 缺少簡體轉換器")
)

// Simplifier 把繁體中文詞面轉成簡體。
//
// *zhconv.Converter 直接滿足這個介面。轉換器必須是唯讀、可同時給多個
// session 使用；core/output 不自己載入字典，字典來源由宿主決定。
type Simplifier interface {
	Convert(text string) string
}

// SimplifierFunc 讓函式可以直接當成 Simplifier，方便測試與小型宿主。
type SimplifierFunc func(string) string

// Convert 呼叫 f。
func (f SimplifierFunc) Convert(text string) string { return f(text) }

// VariantRenderer 依片段自己的輸出變體產生文字。
//
// 中文繁體、日文新字體與英文都原樣輸出；中文簡體經 Simplifier 轉換。
// 日文第一版只支援新字體，ja-Kyujitai 會回傳 ErrUnsupportedVariant。
// 未知語言且沒有指定變體時保留原文，因為沒有可套用的字形慣例。
//
// 零值 VariantRenderer 可用，但只能輸出非簡體變體；要輸出簡體必須用
// NewRenderer 提供轉換器。
type VariantRenderer struct {
	simplifier Simplifier
}

// NewRenderer 建立輸出轉換器。simplifier 可以為 nil，表示不能輸出簡體。
func NewRenderer(simplifier Simplifier) VariantRenderer {
	return VariantRenderer{simplifier: simplifier}
}

// Render 把單一片段轉成輸出文字。Render 不修改片段。
func (r VariantRenderer) Render(seg segment.Segment) (string, error) {
	variant := seg.Variant
	if variant != "" && !variant.Valid() {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedVariant, variant)
	}
	if !seg.Valid() {
		return "", ErrInvalidSegment
	}
	if variant == "" {
		variant = DefaultVariant(seg.Language.Language)
	}
	if variant == "" {
		// 未知語言沒有預設變體；沒有字形可轉換，保留原文。
		return seg.Surface, nil
	}
	if id := seg.Language.Language; id != lang.Und && variant.Language() != id {
		return "", fmt.Errorf("%w: 語言 %q、變體 %q", ErrVariantMismatch, id, variant)
	}

	switch variant {
	case VariantZhHant, VariantJaShinjitai, VariantEnglish:
		return seg.Surface, nil
	case VariantZhHans:
		if r.simplifier == nil {
			return "", fmt.Errorf("%w: %q", ErrMissingSimplifier, variant)
		}
		return r.simplifier.Convert(seg.Surface), nil
	case VariantJaKyujitai:
		return "", fmt.Errorf("%w: 日文舊字體 %q 第一版不支援", ErrUnsupportedVariant, variant)
	}
	return "", fmt.Errorf("%w: %q", ErrUnsupportedVariant, variant)
}

// effectiveTag 回傳片段實際使用的語言標籤。片段沒有標籤時，改由變體推回
// 語言，讓只設 Variant 的呼叫端也能得到正確語言的標點。
func effectiveTag(seg segment.Segment) lang.LanguageTag {
	if !seg.Language.IsZero() {
		return seg.Language
	}
	variant := seg.Variant
	if variant == "" {
		variant = DefaultVariant(seg.Language.Language)
	}
	if id := variant.Language(); id != lang.Und {
		return lang.Tag(id)
	}
	return seg.Language
}
