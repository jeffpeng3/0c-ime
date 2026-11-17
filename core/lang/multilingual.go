package lang

// En 是英文的語言 ID。英文只使用詞典與個人詞彙，不訓練模型。
const En ID = "en"

// Und 是未指定語言的零值。片段、候選或學習紀錄不應長期停在 Und，
// 但介面需要一個明確的「還沒有標籤」值。
const Und ID = ""

// LanguageTag 是片段或候選攜帶的語言標籤。
//
// 標籤只描述這段輸入比較可能是哪個語言，不是模式開關：同一個 session 可以
// 同時保存多個不同標籤的片段，任何標籤都不得阻擋其他語言的 composer 或
// converter。Confidence 為 0 表示只是一個標籤，沒有可信度資訊。
type LanguageTag struct {
	Language   ID      `json:"language"`
	Confidence float64 `json:"confidence,omitempty"`
}

// Tag 建立沒有可信度資訊的語言標籤。
func Tag(id ID) LanguageTag { return LanguageTag{Language: id} }

// WithConfidence 回傳帶可信度的標籤。c 會被限制在 [0, 1]。
func (t LanguageTag) WithConfidence(c float64) LanguageTag {
	switch {
	case c < 0:
		c = 0
	case c > 1:
		c = 1
	}
	t.Confidence = c
	return t
}

// IsZero 回報標籤是否未指定語言。
func (t LanguageTag) IsZero() bool { return t.Language == Und }

// Valid 回報標籤是否可使用。語言 ID 不能是空白，可信度必須在 [0, 1]。
// 這裡不限制只有中文、日文、英文，後續新增語言不必改契約。
func (t LanguageTag) Valid() bool {
	if t.Language == Und {
		return false
	}
	if t.Confidence < 0 || t.Confidence > 1 {
		return false
	}
	for _, r := range string(t.Language) {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return false
		}
	}
	return true
}

// String 回傳方便記錄的標籤文字。未指定語言時是 "und"，方便和合法的 "und" ID 區分。
func (t LanguageTag) String() string {
	if t.IsZero() {
		return "und"
	}
	return string(t.Language)
}

// OutputVariant 是片段最終輸出的字形慣例。
//
// 語言標籤決定「這段是什麼語言」，輸出變體決定「輸出要用哪套字形」。
// 兩者分開，所以同一段日文可以在第一版輸出 ja（新字體），之後再增加
// ja-Kyujitai；中文可在 zh-Hant 與 zh-Hans 之間切換。
type OutputVariant string

const (
	// VariantZhHant 是繁體中文輸出。
	VariantZhHant OutputVariant = "zh-Hant"
	// VariantZhHans 是簡體中文輸出。
	VariantZhHans OutputVariant = "zh-Hans"
	// VariantJaShinjitai 是日文新字體輸出，也是第一版日文的預設值。
	// 字串沿用資料包 manifest 既有的 "ja"，避免既有 manifest 失效。
	VariantJaShinjitai OutputVariant = "ja"
	// VariantJaKyujitai 是日文舊字體輸出。第一版不實作，但契約先保留。
	VariantJaKyujitai OutputVariant = "ja-Kyujitai"
	// VariantEnglish 是英文原文輸出，不做字形轉換。
	VariantEnglish OutputVariant = "en"
)

// 慣用別名，讓呼叫端不必記住字形名稱的長短。
const (
	VariantJa = VariantJaShinjitai
	VariantEn = VariantEnglish
)

// Valid 回報變體是否為已定義的輸出慣例。
func (v OutputVariant) Valid() bool {
	switch v {
	case VariantZhHant, VariantZhHans, VariantJaShinjitai, VariantJaKyujitai, VariantEnglish:
		return true
	}
	return false
}

// Language 回傳變體所屬的語言。未知變體回傳 Und。
func (v OutputVariant) Language() ID {
	switch v {
	case VariantZhHant, VariantZhHans:
		return Zh
	case VariantJaShinjitai, VariantJaKyujitai:
		return Ja
	case VariantEnglish:
		return En
	}
	return Und
}

// DefaultVariant 回傳語言的預設輸出變體。
//
// 中文預設繁中，日文預設新字體，英文保留原文；未知語言回傳空字串，
// 由呼叫端決定是否保留原文。
func DefaultVariant(id ID) OutputVariant {
	switch id {
	case Zh:
		return VariantZhHant
	case Ja:
		return VariantJaShinjitai
	case En:
		return VariantEnglish
	}
	return ""
}
