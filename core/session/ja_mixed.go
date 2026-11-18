package session

import (
	"strings"

	"github.com/0c-biggo/0c-ime/core/lang"
	jlang "github.com/0c-biggo/0c-ime/core/lang/ja"
)

// 日文混打接線（P2-17）。
//
// session.New 會註冊一個內建的日文 runtime，使用的是 core/ja/mixed：ASCII 羅馬字
// 由 core/ja/input 組成かな，英文、網址與識別字照字面留在同一個讀音序列裡。
// 沒有任何 SetLanguage 或模式切換；片段語言由按鍵推斷：
//
//   - 合法的注音按鍵序列留在注音路徑，golden 不變。
//   - 注音組不成、但可以完整組成羅馬字的按鍵序列（例如 kyo、ashi）轉到
//     日文 runtime，畫面直接顯示かな。
//   - 英文詞、英文詞前綴、大寫與英數寫法仍然照字面，不搶英文輸入。
//
// かな讀音由 core/ja/decode 的 lattice／Viterbi 轉換；詞典來源是注入的
// Source（正式環境是 data/ja，參考測試是 0c-jadic pack 經 internal/jadiclex
// 轉接）。沒有詞典時仍可組かな，只是沒有漢字候選。
//
// 按鍵推斷沿用中英混打的待定按鍵路徑，因此宿主要把 MixedInput 打開（P2-40 的
// 單一 Mixed profile 在 Windows 已無條件打開）。沒有 MixedInput 時注音路徑
// 完全不變，enable 也不會偷偷改寫使用者的混打設定。

// EnableJapanese 註冊內建的日文混打 profile 並開啟按鍵推斷。
//
// lex 可以是 nil：仍然可以用羅馬字組かな，只是変換只會原樣顯示讀音。
// conn 是選填的連接成本矩陣。重複呼叫會換掉詞典並重建日文 runtime，其他
// 語言的狀態不受影響。
func (s *Session) EnableJapanese(lex jlang.Source, conn *jlang.Matrix) bool {
	return s.EnableJapaneseConfigured(lex, conn, nil)
}

// EnableJapaneseConfigured is EnableJapanese with a converter hook. The host
// uses it to attach the Japanese reranker; an empty hook keeps the statistical
// conversion and the previous behavior unchanged.
func (s *Session) EnableJapaneseConfigured(lex jlang.Source, conn *jlang.Matrix, configure jlang.ConverterConfig) bool {
	if s == nil {
		return false
	}
	s.japaneseConfigure = configure
	s.installJapanese(lex, conn)
	s.japaneseOn = true
	return true
}

// installJapanese 建立內建日文 profile，但不改變按鍵推斷開關。session.New 用
// 它註冊 runtime；EnableJapanese 另外把推斷打開。
func (s *Session) installJapanese(lex jlang.Source, conn *jlang.Matrix) {
	if s.languages == nil {
		s.languages = map[lang.ID]lang.Profile{}
	}
	if s.runtimes == nil {
		s.runtimes = map[lang.ID]*languageRuntime{}
	}
	cfg := jlang.DefaultConfig()
	cfg.Dictionary = s.English
	s.japaneseCfg = cfg
	s.languages[lang.Ja] = jlang.NewMixedProfileConfigured(lex, conn, cfg, s.japaneseConfigure)
	delete(s.runtimes, lang.Ja)
	if s.Language() != lang.Zh {
		// 換詞典不應把人留在一個正在変換的舊狀態。
		s.languageID = lang.Zh
		s.japaneseRun = false
	}
}

// JapaneseEnabled 回報按鍵推斷是否已開啟。
func (s *Session) JapaneseEnabled() bool {
	return s != nil && s.japaneseOn
}

// japanesePending 回報目前的待定按鍵是否應該交給日文 runtime。
//
// 判定刻意保守，只看整段 ASCII 字母：
//   - 注音組得起來的序列永遠留在注音，注音 golden 不會變。
//   - core/ja/mixed 判定可以完整組成かな（不是照字面英文）才轉。
//   - session 自己的英文詞典與「我的詞彙」前綴優先，英文打到一半不會被吃掉。
func (s *Session) japanesePending() bool {
	if s == nil || !s.japaneseOn || s.Language() != lang.Zh {
		return false
	}
	raw := s.mixed.raw
	if len(raw) < 2 || !asciiLetters(raw) {
		return false
	}
	if _, phonetic := s.mixedComposer(raw); phonetic {
		return false
	}
	if jlang.IsLiteral(raw, s.japaneseCfg) {
		return false
	}
	if s.englishPrior(raw) {
		return false
	}
	return true
}

// englishPrior 回報 raw 是否仍是英文詞或「我的詞彙」的前綴。
func (s *Session) englishPrior(raw string) bool {
	if s == nil || s.model == nil {
		return false
	}
	if len(s.englishPrefix(raw, 1)) > 0 {
		return true
	}
	if s.Preview02 && s.model.VocabEnglishPrefix(raw) {
		return true
	}
	return false
}

// leaveLiteralJapanese 在推斷出來的日文片段整段變成字面英數時回到注音。
//
// 推斷只看按鍵前綴：kubectl 的前四個鍵 kube 是合法羅馬字，因此會先進入日文
// runtime；下一個鍵 c 讓 kubec 不再是合法羅馬字，整段就只剩字面英數。這時
// 留在日文會把 kubectl 標成 ja，空白又會被日文的変換鍵吃掉。回到注音後，
// mixed 待定按鍵照原文保留，空白由中英混打的路徑送出，後續英文詞也各自
// 成為獨立片段。
//
// 只有小寫 ASCII 字母讓整段失效時才回退：かな後面接大寫或數字是刻意的字面
// 尾端，不能把前面的かな還原成羅馬字。
func (s *Session) leaveLiteralJapanese(k string) bool {
	if s == nil || !s.japaneseOn || !s.japaneseRun || s.Language() != lang.Ja {
		return false
	}
	if len(k) != 1 || k[0] < 'a' || k[0] > 'z' {
		return false
	}
	r, ok := s.activeRuntime()
	if !ok || r.composer == nil || r.converting {
		return false
	}
	var b strings.Builder
	for _, u := range r.composer.Units() {
		b.WriteString(string(u))
	}
	b.WriteString(r.composer.Pending())
	text := b.String()
	// 還有かな，或整段仍是合法羅馬字（kyou、gakkou）就留在日文。
	if !asciiLetters(text) {
		return false
	}
	if jlang.IsKana(text, s.japaneseCfg) {
		return false
	}
	r.reset()
	s.languageID = lang.Zh
	s.japaneseRun = false
	s.mixed = pendingKeys{raw: text}
	s.mixedOrigins = nil
	s.cand = nil
	s.refresh()
	return true
}

// startJapaneseRun 把目前的待定按鍵原樣交給日文 runtime。呼叫端已確認
// japanesePending 為 true，且注音狀態是空的。
func (s *Session) startJapaneseRun() bool {
	if !s.japanesePending() {
		return false
	}
	raw := s.mixed.raw
	s.clearMixed()
	s.mixedOrigins = nil
	s.cand = nil
	s.languageID = lang.Ja
	r, ok := s.runtime(lang.Ja)
	if !ok {
		s.languageID = lang.Zh
		return false
	}
	for _, k := range raw {
		r.composer.Insert(string(k))
	}
	s.japaneseRun = true
	s.refresh()
	return true
}

// maybeJapanese 在每一鍵之後檢查待定按鍵。只有注音狀態是空的、沒有候選、
// 也沒有已在変換的日文時才會轉換路徑。
func (s *Session) maybeJapanese() bool {
	if s == nil || !s.japaneseOn || s.Language() != lang.Zh || s.cand != nil {
		return false
	}
	if len(s.syls) > 0 || !s.comp.Empty() {
		return false
	}
	return s.startJapaneseRun()
}

// leaveEmptyJapanese 在日文 runtime 完全沒有組字時回到注音。這個情況只會出現
// 在退格刪光かな之後；回到注音後，下一個按鍵重新由按鍵推斷語言。
func (s *Session) leaveEmptyJapanese() {
	if s == nil || !s.japaneseOn || !s.japaneseRun || s.Language() == lang.Zh {
		return
	}
	r, ok := s.activeRuntime()
	if !ok || r.composing() || r.converting {
		return
	}
	s.languageID = lang.Zh
	s.japaneseRun = false
}

// finishJapaneseRun 在送出或取消一段由推斷開始的日文後回到注音，讓下一個
// 片段重新依按鍵推斷。使用者自己 SetLanguage(ja) 的日文不受影響。
func (s *Session) finishJapaneseRun() {
	if s == nil || !s.japaneseRun {
		return
	}
	s.japaneseRun = false
	s.languageID = lang.Zh
}

// japaneseReading 回傳日文 runtime 目前的かな讀音，供測試與診斷使用。
func (s *Session) japaneseReading() string {
	r, ok := s.activeRuntime()
	if !ok || r.composer == nil {
		return ""
	}
	var b strings.Builder
	for _, u := range r.composer.Units() {
		b.WriteString(string(u))
	}
	b.WriteString(r.composer.Pending())
	return b.String()
}
