package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/input"
	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	jalm "github.com/0c-biggo/0c-ime/core/ja/lm"
	jarerank "github.com/0c-biggo/0c-ime/core/ja/rerank"
	"github.com/0c-biggo/0c-ime/core/lang"
	jlang "github.com/0c-biggo/0c-ime/core/lang/ja"
	"github.com/0c-biggo/0c-ime/core/session"
	"github.com/0c-biggo/0c-ime/internal/jaload"
	"github.com/0c-biggo/0c-ime/internal/neural"
)

// 日文語言接線。統計轉換永遠是底層；模型包可用時，session 的 decoder 會
// 掛上 core/ja/rerank 的 reranker，提交時使用日文標點模型。模型缺失、逾時
// 或取消時 decoder 保留統計順序，標點保留原文。
//
// 注音解碼器與注音 session 完全不經過這裡。

var errJaDataMissing = errors.New("japanese conversion data is not installed")

// jaLoadWarnf 是日文資料載入的警告輸出。main 會換成伺服器 logger；測試可替換。
// 每次載入最多輸出一行，避免資料包損壞時洗版。
var jaLoadWarnf = func(string, ...any) {}

// jaLanguage 是一個行程內共用的日文語言 profile。同一個 decoder 不保存
// 輸入狀態，可同時服務多個 session。
type jaLanguage struct {
	profile  lang.Profile
	lex      jalexicon.Source
	conn     *jalexicon.Matrix
	lm       *jalm.Model
	verifier *neural.JAVerifier
	punct    *neural.Handle
	rerank   jarerank.Config
}

// newJaLanguage 載入日文詞典並建立 profile。日文資料不在資料包內時回傳
// errJaDataMissing，呼叫端不需要視為錯誤；其他錯誤（詞典損壞）照常回報。
func newJaLanguage(dataDir string) (*jaLanguage, error) {
	return newJaLanguageWithModels(dataDir, nil, nil, jarerank.Config{})
}

func newJaLanguageWithModels(dataDir string, verifier *neural.JAVerifier, punct *neural.Handle, cfg jarerank.Config) (*jaLanguage, error) {
	dec, lex, conn, model, err := loadJaDecoder(dataDir)
	if err != nil {
		return nil, err
	}
	return &jaLanguage{profile: jaProfile{dec: dec}, lex: lex, conn: conn, lm: model, verifier: verifier, punct: punct, rerank: cfg}, nil
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
	var configure jlang.ConverterConfig
	if w.verifier != nil || w.lm != nil {
		configure = func(dec *jadecode.Decoder) {
			if w.lm != nil {
				dec.LM = w.lm
				dec.LMWeight = jadecode.DefaultLMWeight
			}
			if w.verifier != nil {
				dec.Reranker = jarerank.New(dec, w.verifier, w.rerank)
			}
		}
	}
	ok := s.EnableJapaneseConfigured(w.lex, w.conn, configure)
	if ok && w.punct != nil {
		s.JaPunct = w.punct
	}
	return ok
}

// Attach 把日文 profile 註冊到 session 並切換過去。回傳 false 表示沒有
// 日文資料或 session 不接受這個 profile；注音 session 不受影響。
func (w *jaLanguage) Attach(s *session.Session) bool {
	return w.AttachProfile(s) && s.SetLanguage(lang.Ja)
}

// jaProfile 是日文語言 profile：composer 用 T11、converter 用 T12 decoder。
// 這是統計轉換的參考 profile；session 使用 core/lang/ja 的混打 profile，並
// 由 AttachProfile 注入 reranker 與標點模型。
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

func loadJaDecoder(dataDir string) (*jadecode.Decoder, jalexicon.Source, *jalexicon.Matrix, *jalm.Model, error) {
	dir, ok := jaDataDir(dataDir)
	if !ok {
		return nil, nil, nil, nil, errJaDataMissing
	}
	bin := filepath.Join(dir, "lexicon.bin")
	words := filepath.Join(dir, "lexicon", "words.tsv")
	conns := filepath.Join(dir, "connection.tsv")
	haveTSV := fileExists(words) && fileExists(conns)

	// 資料包有 lexicon.bin 時優先走緊湊載入器。檔案缺失就直接用 TSV；
	// 檔案存在但無法開啟或解碼時視為「二進位不可用」，記一行警告後改用 TSV。
	// 載入成功就一律使用二進位結果，不會因為 TSV 同時存在而被蓋掉。
	var binErr error
	if fileExists(bin) {
		lex, err := loadJaBin(bin)
		if err == nil {
			conn := lex.Matrix()
			dec, model, err := jaDecoderWithLM(lex, conn, dir)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			return dec, lex, conn, model, nil
		}
		binErr = fmt.Errorf("%s：%w", bin, err)
		if haveTSV {
			jaLoadWarnf("日文詞典 %s 無法使用，改用 %s：%v", bin, words, err)
		}
	}
	if !haveTSV {
		if binErr != nil {
			return nil, nil, nil, nil, binErr
		}
		return nil, nil, nil, nil, errJaDataMissing
	}

	wf, err := os.Open(words)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer wf.Close()
	lex, err := jalexicon.Load(wf)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cf, err := os.Open(conns)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer cf.Close()
	conn, err := jalexicon.LoadConnections(cf)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var decodeConn jalexicon.Connection = conn
	if ctxPath := filepath.Join(dir, "connection.bin"); fileExists(ctxPath) {
		bf, err := os.Open(ctxPath)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		ctx, err := jalexicon.LoadContextMatrix(bf)
		bf.Close()
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s：%w", ctxPath, err)
		}
		decodeConn = &jalexicon.Connections{POS: conn, Context: ctx}
	}
	dec, model, err := jaDecoderWithLM(lex, decodeConn, dir)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return dec, lex, conn, model, nil
}

// jaDecoderWithLM 建立 decoder，並在資料目錄有 lm.bin 時掛上語言模型。
// lm.bin 是選填的編譯產物；不存在時回傳 nil 模型，decoder 保持原本的詞成本
// 加連接成本排序。權重固定用 DefaultLMWeight，目前為 0，因此預設排序不變。
func jaDecoderWithLM(lex jalexicon.Source, conn jalexicon.Connection, dir string) (*jadecode.Decoder, *jalm.Model, error) {
	model, err := jaload.LoadLM(filepath.Join(dir, "lm.bin"))
	if err != nil {
		return nil, nil, err
	}
	dec := &jadecode.Decoder{Lex: lex, Conn: conn, LM: model}
	if model != nil {
		dec.LMWeight = jadecode.DefaultLMWeight
	}
	return dec, model, nil
}

// loadJaBin 開啟並解碼 lexicon.bin。任何開啟或解碼錯誤都回傳給呼叫端，
// 由 loadJaDecoder 決定是否退回 TSV。
func loadJaBin(path string) (*jalexicon.Bin, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var size int64 = -1
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	return jalexicon.LoadBinSize(f, size)
}

// jaDataDir 找日文資料目錄：先看 <data>/ja，再看 data 本身。第二個候選只有在
// 目錄看起來是日文包時才採用，避免把注音包的 lexicon.bin 當成日文詞典。
func jaDataDir(dataDir string) (string, bool) {
	for _, dir := range []string{filepath.Join(dataDir, "ja"), dataDir} {
		if dir == dataDir && !looksJapanese(dir) {
			continue
		}
		if fileExists(filepath.Join(dir, "lexicon.bin")) ||
			fileExists(filepath.Join(dir, "lexicon", "words.tsv")) && fileExists(filepath.Join(dir, "connection.tsv")) {
			return dir, true
		}
	}
	return "", false
}

// looksJapanese 判斷目錄是不是日文資料包。connection.tsv 與
// lexicon/words.tsv 是日文來源格式的標記；只有二進位檔時看 manifest language。
func looksJapanese(dir string) bool {
	if fileExists(filepath.Join(dir, "connection.tsv")) || fileExists(filepath.Join(dir, "lexicon", "words.tsv")) {
		return true
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return false
	}
	var m struct {
		Format   int    `json:"format"`
		Language string `json:"language"`
	}
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	return m.Format >= 2 && m.Language == "ja"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
