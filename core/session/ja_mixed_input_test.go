package session

import (
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/0c-biggo/0c-ime/core/english"
	"github.com/0c-biggo/0c-ime/core/lang"
	jlang "github.com/0c-biggo/0c-ime/core/lang/ja"
	"github.com/0c-biggo/0c-ime/core/layout"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	rd "github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jadiclex"
)

// 參考 pack 不進 repo。與 internal/jadic 的測試使用同一個環境變數。
const japanesePackEnv = "OC_JADIC_REFERENCE_PACK"

var (
	jaPackOnce   sync.Once
	jaPackSource jlang.Source
	jaPackErr    error
)

// referenceJapaneseLexicon 只載入一次 57 MB 的參考 pack，讓多個測試共用索引。
func referenceJapaneseLexicon(t *testing.T) jlang.Source {
	t.Helper()
	path := os.Getenv(japanesePackEnv)
	if path == "" {
		t.Skipf("未設定 %s，跳過 0c-jadic 參考 pack 測試", japanesePackEnv)
	}
	jaPackOnce.Do(func() {
		jaPackSource, jaPackErr = jadiclex.Load(path)
	})
	if jaPackErr != nil {
		t.Fatal(jaPackErr)
	}
	return jaPackSource
}

// japaneseSession 建立 profile-free session：沒有 SetLanguage、沒有語言模式，
// 只有開啟按鍵推斷的空白輸入框。
func japaneseSession(t *testing.T, src jlang.Source) *Session {
	t.Helper()
	s := New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	s.MixedInput = true
	if !s.EnableJapanese(src, nil) {
		t.Fatal("EnableJapanese = false")
	}
	return s
}

func typeSessionKeys(s *Session, keys string) proto.Snapshot {
	var snap proto.Snapshot
	for _, k := range keys {
		snap = key(s, string(k))
	}
	return snap
}

// 羅馬字在日文片段直接組成かな，不需要 SetLanguage 或切換模式。
func TestJapaneseRomajiComposesKanaWithoutModeSwitch(t *testing.T) {
	s := japaneseSession(t, nil)
	if _, ok := s.profile(lang.Ja); !ok {
		t.Fatal("profile-free session 應內建日文 runtime")
	}
	if s.Language() != "zh" {
		t.Fatalf("一開始的語言 = %q，want zh", s.Language())
	}
	if got := typeSessionKeys(s, "kyou").Text(); got != "きょう" {
		t.Fatalf("羅馬字組字 = %q，want きょう", got)
	}
	if got := s.japaneseReading(); got != "きょう" {
		t.Fatalf("日文讀音 = %q，want きょう", got)
	}
	if s.Language() != "ja" {
		t.Fatalf("日文片段語言 = %q，want ja", s.Language())
	}
	// かなは漢字変換せず Enter でそのまま送出できる。
	if snap := key(s, proto.KeyEnter); snap.Commit != "きょう" || snap.Text() != "" {
		t.Fatalf("かな送出 = %q / %q", snap.Commit, snap.Text())
	}
	if s.Language() != "zh" {
		t.Fatalf("送出後應回到注音推斷：%q", s.Language())
	}
}

// かな讀音走 0c-jadic 詞典，候選帶詞面與讀音，選字後送出漢字。
func TestJapaneseKanaToKanjiCandidatesFromReferencePack(t *testing.T) {
	src := referenceJapaneseLexicon(t)
	s := japaneseSession(t, src)
	typeSessionKeys(s, "ashita")

	// 第一次 Space 変換，第二次 Space 列出目前文節的候選。
	snap := key(s, proto.KeyConvert)
	if !snap.Converting {
		t.Fatalf("変換されていない: %#v", snap)
	}
	snap = key(s, " ")
	if snap.Candidates == nil || len(snap.Candidates.Items) == 0 {
		t.Fatalf("沒有かな→漢字候選：%#v", snap)
	}
	texts := itemTexts(snap)
	if !containsText(texts, "明日") {
		t.Fatalf("候選應含 pack 的 明日：%#v", texts)
	}
	if !containsText(texts, "あした") {
		t.Fatalf("候選應保留原讀音 あした：%#v", texts)
	}
	// 候選逐片段、無語言標籤：整個清單只有文字與選字鍵。
	if snap.Candidates.Title != "変換 · Space 換候選 · Tab 換文節" {
		t.Fatalf("候選標題 = %q", snap.Candidates.Title)
	}
	for _, item := range snap.Candidates.Items {
		if strings.Contains(item.Text, "日文") || strings.Contains(item.Text, "中文") {
			t.Fatalf("候選不應帶語言標籤：%#v", item)
		}
	}

	pick := indexText(texts, "明日")
	if pick < 0 || pick >= len(snap.Candidates.Items) {
		t.Fatalf("明日 的位置 = %d", pick)
	}
	// 同一份 pack、同一串按鍵的候選順序必須可重現。
	again := japaneseSession(t, src)
	typeSessionKeys(again, "ashita")
	key(again, proto.KeyConvert)
	if got := itemTexts(key(again, " ")); !reflect.DeepEqual(got, texts) {
		t.Fatalf("候選順序不穩定：\n%#v\n%#v", got, texts)
	}
	snap = key(s, string("1234567890"[pick]))
	if snap.Text() != "明日" {
		t.Fatalf("選字後 = %q，want 明日", snap.Text())
	}
	if snap := key(s, proto.KeyEnter); snap.Commit != "明日" || snap.Text() != "" {
		t.Fatalf("送出 = %q / %q，want 明日", snap.Commit, snap.Text())
	}
}

// 注音、羅馬字與英文字在同一個 session、同一個輸入框依按鍵自動切換，
// 全程沒有 SetLanguage。
func TestJapaneseMixedZhuyinRomajiLatinSameBuffer(t *testing.T) {
	src := referenceJapaneseLexicon(t)
	lex := lexicon.NewMem()
	lex.Add(lexicon.Entry{Surface: "我", Reading: rd.Sequence{"ㄨㄛˇ"}, PRW: 1})
	s := New(personal.NewModel(lex, lm.NewMem()))
	s.MixedInput = true
	s.English = english.New([]english.Word{{Text: "hello", Weight: 100}})
	if !s.EnableJapanese(src, nil) {
		t.Fatal("EnableJapanese = false")
	}

	var commits []string
	for _, k := range layout.Standard.Keys("ㄨㄛˇ") {
		key(s, string(k))
	}
	commits = append(commits, key(s, proto.KeyEnter).Commit)

	typeSessionKeys(s, "ashita")
	key(s, proto.KeyConvert)
	snap := key(s, " ")
	texts := itemTexts(snap)
	pick := indexText(texts, "明日")
	if pick < 0 {
		t.Fatalf("日文候選缺少 明日：%#v", texts)
	}
	key(s, string("1234567890"[pick]))
	commits = append(commits, key(s, proto.KeyEnter).Commit)

	typeSessionKeys(s, "hello")
	commits = append(commits, key(s, proto.KeyEnter).Commit)

	if got := strings.Join(commits, ""); got != "我明日hello" {
		t.Fatalf("混合輸入 = %q，want 我明日hello（各段 %#v）", got, commits)
	}
	if s.Language() != "zh" {
		t.Fatalf("送出後語言 = %q，want zh", s.Language())
	}
}

// 開啟日文推斷不能改變注音 golden：同一串注音按鍵在兩個 session 產生完全
// 相同的快照、候選與送出文字。
func TestZhuyinGoldenUnchangedWithJapaneseEnabled(t *testing.T) {
	for _, keys := range []string{
		"ji3", "ji3cl3", "su3", "a86z06", "ji3hellot ", "ji3,ji3",
		"ji3ji3ji3", "u3vu;3", "5j4",
	} {
		plain := mixedSession()
		withJa := mixedSession()
		if !withJa.EnableJapanese(nil, nil) {
			t.Fatal("EnableJapanese = false")
		}
		for _, k := range keys {
			got := key(plain, string(k))
			want := key(withJa, string(k))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%q 的第 %q 鍵：日文推斷改變注音快照\nplain=%#v\nwithJa=%#v", keys, string(k), got, want)
			}
		}
		got := key(plain, proto.KeyEnter)
		want := key(withJa, proto.KeyEnter)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%q 送出：plain=%#v withJa=%#v", keys, got, want)
		}
	}
}

func containsText(texts []string, want string) bool { return indexText(texts, want) >= 0 }

func indexText(texts []string, want string) int {
	for i, text := range texts {
		if text == want {
			return i
		}
	}
	return -1
}
