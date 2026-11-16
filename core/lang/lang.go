// Package lang 定義語言 profile 的介面與共用型別。
//
// 這個套件只放介面、列舉與資料形狀，不 import 任何具體語言（core/ja/*、core/decode、
// core/zhuyin），避免循環依賴。具體實作放在 core/lang/zh 與 core/lang/ja；
// core/session 只依賴這裡的型別。
package lang

// ID 是語言的穩定識別碼，也用在資料包與個人學習檔的 language 欄位。
type ID string

const (
	Zh ID = "zh"
	Ja ID = "ja"
)

// InputMethod 是一種輸入方式，例如注音或羅馬字。
type InputMethod struct {
	ID   string // "zhuyin"、"romaji"、"kana"
	Name string // 顯示名稱，由外殼照原樣呈現
}

// Profile 是一種語言的完整能力組合。
//
// 同一個引擎可同時註冊多個 profile；切換語言時重建 composer 與 converter，
// 不重啟引擎。NewPunctuator 可以回傳 nil，表示這個語言沒有專用標點器。
type Profile interface {
	ID() ID
	InputMethods() []InputMethod
	Limits() Limits
	KeyPolicy() KeyPolicy
	NewComposer(InputMethod) Composer
	NewConverter() Converter
	NewPunctuator() Punctuator
}

// Limits 是語言 profile 的轉換上限，對應資料包 manifest 的 limits。
//
// 0 表示這個 profile 不指定，由資料包或引擎的保底值決定。
type Limits struct {
	// MaxReadingUnits 是單次轉換可接受的讀音單位數。
	MaxReadingUnits int
	// MaxWordUnits 是單一詞條可佔用的讀音單位數。
	MaxWordUnits int
	// MaxCandidates 是候選視窗一次最多列出的候選數。
	MaxCandidates int
}

// DefaultLimits 是沒有資料包 limits 時的保底值。
func DefaultLimits() Limits {
	return Limits{
		MaxReadingUnits: 64,
		MaxWordUnits:    32,
		MaxCandidates:   16,
	}
}
