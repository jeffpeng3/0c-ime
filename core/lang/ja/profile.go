// Package ja 是日文的具體語言 profile 實作。
//
// core/session 只依賴這裡的型別與建構函式，不直接 import core/ja/*；日文特有的
// 詞典、組字器與変換器都留在這個套件後面。P2-17 的日英混打 profile 也在這裡
// 組裝：core/ja/mixed 包住 core/ja/input，轉換走 core/ja/decode 的 lattice。
package ja

import (
	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	jainput "github.com/0c-biggo/0c-ime/core/ja/input"
	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/ja/mixed"
	"github.com/0c-biggo/0c-ime/core/lang"
)

// Source 是日文讀音詞典介面，對應 core/ja/lexicon.Source。
type Source = jalexicon.Source

// Matrix 是日文連接成本矩陣。
type Matrix = jalexicon.Matrix

// Config 是日英混打的設定，對應 core/ja/mixed.Config。
type Config = mixed.Config

// DefaultConfig 回傳日英混打的預設設定。
func DefaultConfig() Config { return mixed.DefaultConfig() }

// mixedBase 提供詞典轉換器；mixed.Profile 會換上混打組字器與変換器。
type mixedBase struct {
	lex  Source
	conn *Matrix
}

func (mixedBase) ID() lang.ID { return lang.Ja }

func (mixedBase) InputMethods() []lang.InputMethod {
	return []lang.InputMethod{{ID: "romaji", Name: "ローマ字"}, {ID: "kana", Name: "かな"}}
}

func (mixedBase) Limits() lang.Limits { return lang.DefaultLimits() }

func (mixedBase) KeyPolicy() lang.KeyPolicy { return Policy{} }

func (mixedBase) NewComposer(method lang.InputMethod) lang.Composer {
	if method.ID == "kana" {
		return jainput.NewKanaComposer()
	}
	return jainput.NewRomajiComposer()
}

func (b mixedBase) NewConverter() lang.Converter {
	if b.lex == nil {
		return nil
	}
	dec := &jadecode.Decoder{Lex: b.lex}
	if b.conn != nil {
		dec.Conn = b.conn
	}
	return dec
}

func (mixedBase) NewPunctuator() lang.Punctuator { return nil }

// NewMixedProfile 建立日英混打 profile。lex 為 nil 時仍可用羅馬字組かな，
// 変換只會原樣顯示讀音；conn 是選填的連接成本矩陣。
func NewMixedProfile(lex Source, conn *Matrix, cfg Config) lang.Profile {
	return mixed.NewProfile(mixedBase{lex: lex, conn: conn}, cfg)
}

// IsLiteral 回報一段 ASCII 按鍵是否會被混打組字器照字面處理。session 用它
// 判斷待定按鍵是否仍是英文，避免搶走打到一半的英文詞。
func IsLiteral(raw string, cfg Config) bool { return mixed.LiteralFor(raw, cfg) }
