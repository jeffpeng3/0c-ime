package mixed

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind 是一段文字在日英混打裡的種類。
type Kind int

const (
	// KindOther 是空白、標點或其他不屬於英數詞的字元。
	KindOther Kind = iota
	// KindJapanese 是日文字母（かな、漢字、長音符等）。
	KindJapanese
	// KindEnglish 是 core/english 詞典或學習詞彙認得的英文單字。
	KindEnglish
	// KindLatin 是詞典不認得、但全由拉丁字母組成的字。
	KindLatin
	// KindURL 是網址。
	KindURL
	// KindEmail 是 email 位址。
	KindEmail
	// KindIdentifier 是程式識別字、路徑、查詢字串或含英數的編號。
	KindIdentifier
	// KindFullWidthAlnum 是全角英數字。
	KindFullWidthAlnum
	// KindNumber 是純數字。
	KindNumber
)

// Literal 回報這一類文字是否必須照原樣保留，不交給日文変換。
//
// 日文字母以外都算照字面；KindOther 由外殼自行處理標點，也不在這裡変換。
func (k Kind) Literal() bool {
	switch k {
	case KindEnglish, KindLatin, KindURL, KindEmail, KindIdentifier, KindFullWidthAlnum, KindNumber:
		return true
	}
	return false
}

// String 回傳種類名稱，方便測試與除錯。
func (k Kind) String() string {
	switch k {
	case KindJapanese:
		return "japanese"
	case KindEnglish:
		return "english"
	case KindLatin:
		return "latin"
	case KindURL:
		return "url"
	case KindEmail:
		return "email"
	case KindIdentifier:
		return "identifier"
	case KindFullWidthAlnum:
		return "fullwidth_alnum"
	case KindNumber:
		return "number"
	default:
		return "other"
	}
}

// Lookup 是英文字詞查詢，core/english.Dictionary 與 English 都符合。
type Lookup interface {
	Has(string) bool
}

// Token 是 Scan 切出的一段文字，Start 與 End 是 byte offset，範圍是 [Start, End)。
type Token struct {
	Kind  Kind
	Text  string
	Start int
	End   int
}

// Classify 判斷一段文字在日英混打裡的種類。lookup 可以為 nil。
func Classify(text string, lookup Lookup) Kind {
	text = trimTokenRight(text)
	if text == "" {
		return KindOther
	}
	if allJapanese(text) {
		return KindJapanese
	}
	ascii, fullwidth, letters, digits, alnum := analyzeAlnum(text)
	if fullwidth && alnum {
		return KindFullWidthAlnum
	}
	if IsEmail(text) {
		return KindEmail
	}
	if IsURL(text) {
		return KindURL
	}
	if ascii && letters && !digits && allASCIILettersOrApostrophe(text) {
		if lookup != nil && lookup.Has(strings.ReplaceAll(text, "'", "")) {
			return KindEnglish
		}
		return KindLatin
	}
	if digits && !letters && ascii {
		return KindNumber
	}
	if IsIdentifier(text) {
		return KindIdentifier
	}
	return KindOther
}

// Scan 把一段文字切成日文、英數詞與其他字元，並標出 byte offset。
//
// 空白會被跳過。英數詞尾端不屬於詞的標點（句點、逗號、括號等）會切成
// KindOther，因此「hello.」會得到 hello 與句點兩段；網址、email 與識別字
// 的內部標點則保留在同一段。
func Scan(text string, lookup Lookup) []Token {
	var tokens []Token
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if isJapaneseLetter(r) {
			start := i
			for i < len(text) {
				r, size = utf8.DecodeRuneInString(text[i:])
				if !isJapaneseLetter(r) {
					break
				}
				i += size
			}
			tokens = append(tokens, Token{Kind: KindJapanese, Text: text[start:i], Start: start, End: i})
			continue
		}
		if canStartToken(r) {
			start := i
			for i < len(text) {
				r, size = utf8.DecodeRuneInString(text[i:])
				if !isTokenRune(r) {
					break
				}
				i += size
			}
			core, rest := splitToken(text[start:i])
			coreEnd := start + len(core)
			if core != "" {
				tokens = append(tokens, Token{Kind: Classify(core, lookup), Text: core, Start: start, End: coreEnd})
			}
			at := coreEnd
			for _, p := range rest {
				tokens = append(tokens, Token{Kind: KindOther, Text: string(p), Start: at, End: at + utf8.RuneLen(p)})
				at += utf8.RuneLen(p)
			}
			continue
		}
		tokens = append(tokens, Token{Kind: KindOther, Text: string(r), Start: i, End: i + size})
		i += size
	}
	return tokens
}

// IsURL 回報 text 是否為網址。結尾的句點、逗號與右括號不算網址的一部分。
func IsURL(text string) bool {
	t := trimTokenRight(text)
	if t == "" || strings.ContainsFunc(t, func(r rune) bool { return unicode.IsSpace(r) || isJapaneseLetter(r) }) {
		return false
	}
	lower := strings.ToLower(t)
	if i := strings.Index(lower, "://"); i > 0 && validScheme(t[:i]) {
		return true
	}
	if strings.ContainsRune(t, '@') {
		return false // email（或類似 email 的字）交給 IsEmail／IsIdentifier
	}
	if strings.HasPrefix(lower, "www.") {
		return isDomainHost(strings.TrimPrefix(lower, "www."))
	}
	if strings.HasPrefix(lower, "//") {
		return isDomainHost(hostPart(lower[2:]))
	}
	host := hostPart(lower)
	if host == "" || !isDomainHost(host) {
		return false
	}
	return strings.ContainsAny(t, "/?#") || strings.Count(host, ".") >= 1
}

// IsEmail 回報 text 是否為 email 位址。
func IsEmail(text string) bool {
	t := trimTokenRight(text)
	if strings.Count(t, "@") != 1 || strings.ContainsFunc(t, unicode.IsSpace) {
		return false
	}
	at := strings.IndexByte(t, '@')
	local, domain := t[:at], t[at+1:]
	if local == "" || domain == "" || !validEmailLocal(local) || !isDomainHost(domain) {
		return false
	}
	dot := strings.LastIndexByte(domain, '.')
	if dot <= 0 || dot == len(domain)-1 {
		return false
	}
	tld := domain[dot+1:]
	return len(tld) >= 2 && allASCIILetterString(tld)
}

// IsIdentifier 回報 text 是否為程式識別字、路徑、查詢字串或含英數的編號。
func IsIdentifier(text string) bool {
	t := trimTokenRight(text)
	if t == "" || strings.ContainsFunc(t, func(r rune) bool { return unicode.IsSpace(r) || isJapaneseLetter(r) }) {
		return false
	}
	hasLetter, hasDigit, hasAlnum := false, false, false
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			hasLetter, hasAlnum = true, true
		case r >= '0' && r <= '9':
			hasDigit, hasAlnum = true, true
		case r > unicode.MaxASCII && unicode.IsLetter(r):
			hasLetter, hasAlnum = true, true
		}
	}
	if !hasAlnum {
		return false
	}
	if hasLetter && hasDigit {
		return true
	}
	switch {
	case strings.ContainsAny(t, "_\\/:?&=#%@+~|"):
		return true
	case strings.ContainsRune(t, '-') && hasLetter:
		return true
	}
	return false
}

func isDomainHost(host string) bool {
	if host == "" || strings.ContainsAny(host, "_/\\") {
		return false
	}
	// 去掉 port。
	if colon := strings.LastIndexByte(host, ':'); colon >= 0 {
		if colon == len(host)-1 || !allASCIIDigitString(host[colon+1:]) {
			return false
		}
		host = host[:colon]
	}
	if host == "" || !strings.ContainsRune(host, '.') {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func hostPart(s string) string {
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if at := strings.LastIndexByte(s, '@'); at >= 0 {
		s = s[at+1:]
	}
	return s
}

func validScheme(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				return false
			}
			continue
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func validEmailLocal(s string) bool {
	if s == "" || s[0] == '.' || s[len(s)-1] == '.' {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("._%+-", r) {
			continue
		}
		return false
	}
	return !strings.Contains(s, "..")
}

func allASCIILetterString(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
			return false
		}
	}
	return true
}

func allASCIIDigitString(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func allASCIILettersOrApostrophe(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] == '\'') {
			return false
		}
	}
	return true
}

func allJapanese(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isJapaneseLetter(r) {
			return false
		}
	}
	return true
}

func analyzeAlnum(text string) (ascii, fullwidth, letters, digits, alnum bool) {
	alnum = true
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			ascii, letters = true, true
		case r >= '0' && r <= '9':
			ascii, digits = true, true
		case IsFullWidthAlnum(r):
			fullwidth = true
			switch {
			case r >= 'Ａ' && r <= 'Ｚ' || r >= 'ａ' && r <= 'ｚ':
				letters = true
			default:
				digits = true
			}
		case r == '\'':
			// 英文縮寫（don't）留在同一段，但不算英數。
		default:
			alnum = false
		}
	}
	return ascii, fullwidth, letters, digits, alnum
}

func canStartToken(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		IsFullWidthAlnum(r) || strings.ContainsRune("@#$", r)
}

func isTokenRune(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || IsFullWidthAlnum(r) {
		return true
	}
	return strings.ContainsRune("._~:/?#[]@!$&'()*+,;=%\\-|{}", r)
}

func splitToken(s string) (string, []rune) {
	runes := []rune(s)
	n := len(runes)
	for n > 0 && strings.ContainsRune(".,;:!?)]}\"'>", runes[n-1]) {
		n--
	}
	if n == 0 {
		return "", runes
	}
	return string(runes[:n]), runes[n:]
}

// trimTokenRight 去掉不屬於詞尾的標點，讓 Classify 對外部傳進來的整段文字也
// 有一致的結果。
func trimTokenRight(s string) string {
	core, _ := splitToken(s)
	return core
}
