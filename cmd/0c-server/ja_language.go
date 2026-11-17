package main

import (
	"errors"
	"os"
	"path/filepath"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/input"
	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/session"
)

// 日文語言接線。P2-01 移除外部推論後端之後，正式產品只提供統計轉換：
// 日文 profile 用 T11 composer 與 T12 decoder，不建立 worker、不載入模型，
// 也不讀取 models/ja。core/ja/rerank 的介面保留給 P2-30 的 ONNX 模型；
// 在那之前 decoder 的 Reranker 保持 nil，轉換直接回統計結果。
//
// 注音解碼器與注音 session 完全不經過這裡。

var errJaDataMissing = errors.New("japanese conversion data is not installed")

// jaLanguage 是一個行程內共用的日文語言 profile。同一個 decoder 不保存
// 輸入狀態，可同時服務多個 session。
type jaLanguage struct {
	profile lang.Profile
	lex     jalexicon.Source
	conn    *jalexicon.Matrix
}

// newJaLanguage 載入日文詞典並建立 profile。日文資料不在資料包內時回傳
// errJaDataMissing，呼叫端不需要視為錯誤；其他錯誤（詞典損壞）照常回報。
func newJaLanguage(dataDir string) (*jaLanguage, error) {
	dec, lex, conn, err := loadJaDecoder(dataDir)
	if err != nil {
		return nil, err
	}
	return &jaLanguage{profile: jaProfile{dec: dec}, lex: lex, conn: conn}, nil
}

// Available 回報日文 profile 是否已載入。
func (w *jaLanguage) Available() bool {
	return w != nil && w.profile != nil
}

// AttachProfile 把日文混打 runtime 註冊到 session，不切換目前語言。注音
// session 仍然維持注音；羅馬字按鍵會依按鍵推斷自動進入日文片段（P2-17），
// 不需要 SetLanguage 或語言模式。
func (w *jaLanguage) AttachProfile(s *session.Session) bool {
	if !w.Available() || s == nil {
		return false
	}
	return s.EnableJapanese(w.lex, w.conn)
}

// Attach 把日文 profile 註冊到 session 並切換過去。回傳 false 表示沒有
// 日文資料或 session 不接受這個 profile；注音 session 不受影響。
func (w *jaLanguage) Attach(s *session.Session) bool {
	return w.AttachProfile(s) && s.SetLanguage(lang.Ja)
}

// jaProfile 是日文語言 profile：composer 用 T11、converter 用 T12 decoder。
// 沒有神經 reranker；轉換結果就是統計結果。
type jaProfile struct {
	dec *jadecode.Decoder
}

var _ lang.Profile = jaProfile{}

func (p jaProfile) ID() lang.ID { return lang.Ja }

func (p jaProfile) InputMethods() []lang.InputMethod {
	return []lang.InputMethod{{ID: "romaji", Name: "ローマ字"}, {ID: "kana", Name: "かな"}}
}

func (p jaProfile) Limits() lang.Limits { return lang.DefaultLimits() }

func (p jaProfile) KeyPolicy() lang.KeyPolicy { return session.JapaneseKeyPolicy{} }

func (p jaProfile) NewComposer(method lang.InputMethod) lang.Composer {
	if method.ID == "kana" {
		return input.NewKanaComposer()
	}
	return input.NewRomajiComposer()
}

func (p jaProfile) NewConverter() lang.Converter { return p.dec }

func (p jaProfile) NewPunctuator() lang.Punctuator { return nil }

func loadJaDecoder(dataDir string) (*jadecode.Decoder, *jalexicon.Mem, *jalexicon.Matrix, error) {
	words, conns, ok := jaDataFiles(dataDir)
	if !ok {
		return nil, nil, nil, errJaDataMissing
	}
	wf, err := os.Open(words)
	if err != nil {
		return nil, nil, nil, err
	}
	defer wf.Close()
	lex, err := jalexicon.Load(wf)
	if err != nil {
		return nil, nil, nil, err
	}
	cf, err := os.Open(conns)
	if err != nil {
		return nil, nil, nil, err
	}
	defer cf.Close()
	conn, err := jalexicon.LoadConnections(cf)
	if err != nil {
		return nil, nil, nil, err
	}
	return &jadecode.Decoder{Lex: lex, Conn: conn}, lex, conn, nil
}

func jaDataFiles(dataDir string) (words, conns string, ok bool) {
	for _, dir := range []string{filepath.Join(dataDir, "ja"), dataDir} {
		words = filepath.Join(dir, "lexicon", "words.tsv")
		conns = filepath.Join(dir, "connection.tsv")
		if fileExists(words) && fileExists(conns) {
			return words, conns, true
		}
	}
	return "", "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
