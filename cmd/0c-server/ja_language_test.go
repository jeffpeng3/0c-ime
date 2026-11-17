package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/session"
)

func jaLanguageTestData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "lexicon"), 0o755); err != nil {
		t.Fatal(err)
	}
	words := strings.Join([]string{
		"今日\tきょう\t1.0\tnoun\tcurated\t-",
		"京\tきょう\t3.0\tnoun\tcurated\t-",
		"は\tは\t1.0\tparticle\tcurated\t-",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "lexicon", "words.tsv"), []byte(words), 0o644); err != nil {
		t.Fatal(err)
	}
	conns := "# 左詞性\t右詞性\t成本\n<s>\tnoun\t0.2\nnoun\tparticle\t0.2\nparticle\t</s>\t0.2\nnoun\t</s>\t0.2\n"
	if err := os.WriteFile(filepath.Join(dir, "connection.tsv"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newJaLanguageTestSession(t *testing.T, w *jaLanguage) *session.Session {
	t.Helper()
	s := session.New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	if !w.Attach(s) {
		t.Fatal("Attach(ja) = false")
	}
	if s.Language() != lang.Ja {
		t.Fatalf("language = %q", s.Language())
	}
	return s
}

func typeJaLanguageKeys(s *session.Session, keys string) proto.Snapshot {
	var snap proto.Snapshot
	for _, k := range keys {
		snap = s.Handle(proto.Request{Type: proto.Key, Key: string(k)})
	}
	return snap
}

func jaLanguageCommit(t *testing.T, s *session.Session, readingKeys string) proto.Snapshot {
	t.Helper()
	typeJaLanguageKeys(s, readingKeys)
	snap := s.Handle(proto.Request{Type: proto.Key, Key: proto.KeyConvert})
	if !snap.Converting {
		t.Fatalf("変換されていない: %#v", snap)
	}
	return s.Handle(proto.Request{Type: proto.Key, Key: proto.KeyEnter})
}

// 正式路徑只有統計轉換：沒有 worker、沒有模型、decoder 的 Reranker 保持 nil。
func TestJaLanguageUsesStatisticalConversion(t *testing.T) {
	w, err := newJaLanguage(jaLanguageTestData(t))
	if err != nil {
		t.Fatal(err)
	}
	if !w.Available() {
		t.Fatal("日文 profile 未載入")
	}
	dec, ok := w.profile.NewConverter().(*jadecode.Decoder)
	if !ok {
		t.Fatalf("converter = %T", w.profile.NewConverter())
	}
	if dec.Reranker != nil {
		t.Fatal("正式路徑不應接上 reranker")
	}
	s := newJaLanguageTestSession(t, w)
	if commit := jaLanguageCommit(t, s, "kyouha"); commit.Commit != "今日は" {
		t.Fatalf("統計轉換 = %q", commit.Commit)
	}
}

// 沒有日文資料時回傳 errJaDataMissing，呼叫端不需要當成故障。
func TestJaLanguageMissingData(t *testing.T) {
	w, err := newJaLanguage(t.TempDir())
	if !errors.Is(err, errJaDataMissing) {
		t.Fatalf("err = %v, want errJaDataMissing", err)
	}
	if w != nil {
		t.Fatalf("missing data returned %#v", w)
	}
}

// 建立日文接線不會改到注音 session 的語言或 rerank 選項；注音 golden 由
// internal/golden 的既有測試守門。
func TestJaLanguageLeavesZhuyinSessionAlone(t *testing.T) {
	w, err := newJaLanguage(jaLanguageTestData(t))
	if err != nil {
		t.Fatal(err)
	}
	s := session.New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	if s.Language() != lang.Zh {
		t.Fatalf("注音 session 語言 = %q", s.Language())
	}
	if !w.AttachProfile(s) {
		t.Fatal("AttachProfile = false")
	}
	if s.Language() != lang.Zh {
		t.Fatalf("AttachProfile 改了語言：%q", s.Language())
	}
	if s.Rerank.Scorer != nil || s.Rerank.Weight != 0 {
		t.Fatalf("注音 rerank 選項被改動：%+v", s.Rerank)
	}
	ja := newJaLanguageTestSession(t, w)
	if ja.Language() != lang.Ja || s.Language() != lang.Zh {
		t.Fatalf("語言互相污染：ja=%q zh=%q", ja.Language(), s.Language())
	}
}
