package session

import (
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/layout"
	rd "github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// zhProfile 是注音邏輯的語言 adapter。
//
// 正式 session 的注音狀態仍走原本的欄位（comp、syls、decode 路徑），行為完全不變；
// 這個 profile 提供同一組契約，讓語言切換、KeyPolicy 與外殼能用一致的方式看待注音。
type zhProfile struct{}

var (
	_ lang.Profile    = zhProfile{}
	_ lang.Composer   = (*zhComposer)(nil)
	_ lang.KeyPolicy  = ZhuyinKeyPolicy{}
	_ lang.Punctuator = (*zhPunctuator)(nil)
)

func (zhProfile) ID() lang.ID { return lang.Zh }

func (zhProfile) InputMethods() []lang.InputMethod {
	return []lang.InputMethod{{ID: "zhuyin", Name: "注音"}}
}

func (zhProfile) Limits() lang.Limits { return lang.DefaultLimits() }

func (zhProfile) KeyPolicy() lang.KeyPolicy { return ZhuyinKeyPolicy{} }

func (zhProfile) NewComposer(lang.InputMethod) lang.Composer { return &zhComposer{} }

// 注音的轉換由 session 的個人模型與 decode.Decoder 提供；profile 本身沒有
// 獨立的詞庫，回傳 nil 表示沿用 session 既有的轉換路徑。
func (zhProfile) NewConverter() lang.Converter { return nil }

func (zhProfile) NewPunctuator() lang.Punctuator { return nil }

// zhComposer 是 zhuyin.Composer 的 lang.Composer adapter，使用標準排列把
// 實體按鍵解讀成注音符號。它不取代 session 的注音狀態，只用來證明 profile
// 契約可以完整表達注音的組字生命週期。
type zhComposer struct {
	comp  zhuyin.Composer
	units rd.Sequence
}

func (c *zhComposer) Insert(key string) (lang.ComposeResult, bool) {
	runes := []rune(key)
	if len(runes) != 1 {
		return lang.ComposeResult{Pending: c.comp.String()}, false
	}
	next, sym, ok := layout.Standard.Press(c.comp, runes[0])
	if !ok {
		// 也接受直接送來的注音符號（觸控或測試）。
		if zhuyin.ClassOf(runes[0]) == zhuyin.None {
			return lang.ComposeResult{Pending: c.comp.String()}, false
		}
		if done, added := c.comp.Add(runes[0]); added {
			c.units = append(c.units, rd.Unit(done))
			return lang.ComposeResult{Completed: rd.Sequence{rd.Unit(done)}}, true
		}
		return lang.ComposeResult{Pending: c.comp.String()}, true
	}
	if zhuyin.ClassOf(sym) == zhuyin.Tone {
		if c.comp.Empty() {
			return lang.ComposeResult{Pending: c.comp.String()}, false
		}
		c.comp = next
		done, added := c.comp.Add(sym)
		if !added {
			return lang.ComposeResult{Pending: c.comp.String()}, false
		}
		c.units = append(c.units, rd.Unit(done))
		return lang.ComposeResult{Completed: rd.Sequence{rd.Unit(done)}}, true
	}
	c.comp = next
	return lang.ComposeResult{Pending: c.comp.String()}, true
}

func (c *zhComposer) Backspace() bool {
	if !c.comp.Empty() {
		c.comp = layout.Standard.Backspace(c.comp)
		return true
	}
	if len(c.units) == 0 {
		return false
	}
	c.units = c.units[:len(c.units)-1]
	return true
}

func (c *zhComposer) Reset() {
	c.comp.Reset()
	c.units = nil
}

func (c *zhComposer) Units() rd.Sequence {
	return append(rd.Sequence(nil), c.units...)
}

func (c *zhComposer) Pending() string { return c.comp.String() }

func (c *zhComposer) Segments() []lang.DisplaySegment {
	segs := make([]lang.DisplaySegment, 0, len(c.units)+1)
	for _, u := range c.units {
		segs = append(segs, lang.DisplaySegment{Reading: rd.Sequence{u}, Text: string(u)})
	}
	if pending := c.comp.String(); pending != "" {
		segs = append(segs, lang.DisplaySegment{
			Reading: rd.Sequence{rd.Unit(pending)},
			Text:    pending,
			Pending: true,
		})
	}
	return segs
}

// zhPunctuator 只保留契約形狀；注音標點由 session 既有的 layout 與 punct
// 路徑處理，profile 不另外改寫文字。
type zhPunctuator struct{}

func (zhPunctuator) Insert(text string) string { return text }
