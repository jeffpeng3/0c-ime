package mixed

import "github.com/0c-biggo/0c-ime/core/lang"

// Profile 包裝既有的日文 profile，換上日英混打的組字器與変換器。
//
// 這是接 T20 語言 profile 的入口：宿主把原本的日文 profile 包一層後再用
// Session.AddLanguageProfile 註冊即可。按鍵語意、輸入方式、上限、標點器與
// ID 都沿用底層 profile；只有 NewComposer 與 NewConverter 換成混打版本。
// 注音仍走原本的 session 路徑，完全不受影響。
type Profile struct {
	base    lang.Profile
	cfg     Config
	english *English
}

var _ lang.Profile = Profile{}

// NewProfile 建立混打 profile。base 為 nil 時仍可建立組字器與変換器，但
// ID、輸入方式與按鍵語意會是空值。
func NewProfile(base lang.Profile, cfg Config) Profile {
	cfg = cfg.normalized()
	return Profile{base: base, cfg: cfg, english: cfg.Vocabulary}
}

// English 回傳這個 profile 共用的英文查詢層。宿主可在送出事件裡呼叫 Learn。
func (p Profile) English() *English { return p.english }

// ID 回傳底層語言 ID。
func (p Profile) ID() lang.ID {
	if p.base == nil {
		return ""
	}
	return p.base.ID()
}

// InputMethods 回傳底層輸入方式。
func (p Profile) InputMethods() []lang.InputMethod {
	if p.base == nil {
		return nil
	}
	return p.base.InputMethods()
}

// Limits 回傳底層轉換上限。
func (p Profile) Limits() lang.Limits {
	if p.base == nil {
		return lang.DefaultLimits()
	}
	return p.base.Limits()
}

// KeyPolicy 回傳底層按鍵語意。
func (p Profile) KeyPolicy() lang.KeyPolicy {
	if p.base == nil {
		return nil
	}
	return p.base.KeyPolicy()
}

// NewComposer 建立指定輸入方式的混打組字器。
func (p Profile) NewComposer(method lang.InputMethod) lang.Composer {
	cfg := p.cfg
	if cfg.Vocabulary == nil {
		cfg.Vocabulary = p.english
	}
	return newComposer(method, cfg.normalized())
}

// NewConverter 建立混打変換器。
func (p Profile) NewConverter() lang.Converter {
	var base lang.Converter
	if p.base != nil {
		base = p.base.NewConverter()
	}
	cfg := p.cfg
	if cfg.Vocabulary == nil {
		cfg.Vocabulary = p.english
	}
	return NewConverter(base, cfg)
}

// NewPunctuator 回傳底層標點器。
func (p Profile) NewPunctuator() lang.Punctuator {
	if p.base == nil {
		return nil
	}
	return p.base.NewPunctuator()
}
