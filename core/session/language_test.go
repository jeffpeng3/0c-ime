package session_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/session"
)

func TestZhuyinProfileAdapter(t *testing.T) {
	s := session.New(personal.NewModel(lexicon.NewMem(), lm.NewMem()))
	p := s.LanguageProfile()
	if p == nil || p.ID() != lang.Zh {
		t.Fatalf("預設 profile = %#v", p)
	}
	if p.KeyPolicy() == nil {
		t.Fatal("注音 profile 必須有 KeyPolicy")
	}
	methods := p.InputMethods()
	if len(methods) != 1 || methods[0].ID != "zhuyin" {
		t.Fatalf("InputMethods = %#v", methods)
	}
	c := p.NewComposer(methods[0])
	if _, ok := c.Insert("j"); !ok || c.Pending() != "ㄨ" {
		t.Fatalf("Insert(j) = pending %q ok %v", c.Pending(), ok)
	}
	res, ok := c.Insert("3")
	if !ok || len(res.Completed) != 1 || string(res.Completed[0]) != "ㄨˇ" {
		t.Fatalf("Insert(3) = %#v, %v", res, ok)
	}
	if got := c.Units(); len(got) != 1 || string(got[0]) != "ㄨˇ" {
		t.Fatalf("Units = %#v", got)
	}
	segs := c.Segments()
	if len(segs) != 1 || segs[0].Text != "ㄨˇ" {
		t.Fatalf("Segments = %#v", segs)
	}
	if !c.Backspace() || len(c.Units()) != 0 {
		t.Fatalf("Backspace 後 Units = %#v", c.Units())
	}
}

func TestKeyPolicySemanticsTable(t *testing.T) {
	zh := session.ZhuyinKeyPolicy{}
	ja := session.JapaneseKeyPolicy{}
	cases := []struct {
		name  string
		pol   lang.KeyPolicy
		key   string
		state lang.KeyState
		want  lang.Action
		ok    bool
	}{
		{"注音 Space 開候選", zh, " ", lang.KeyState{Composing: true}, lang.ActionConvert, true},
		{"注音 Space 沒組字插入", zh, " ", lang.KeyState{}, lang.ActionInsert, true},
		{"注音 Enter 送出", zh, proto.KeyEnter, lang.KeyState{Composing: true}, lang.ActionCommit, true},
		{"注音 Tab 補全", zh, proto.KeyTab, lang.KeyState{Composing: true}, lang.ActionComplete, true},
		{"注音 Shift+Tab 不換文節", zh, proto.KeyBackTab, lang.KeyState{Converting: true, ClauseCount: 2}, lang.ActionNone, false},
		{"注音 Escape 取消", zh, proto.KeyEscape, lang.KeyState{Composing: true}, lang.ActionCancel, true},
		{"注音 Convert 不處理", zh, proto.KeyConvert, lang.KeyState{Composing: true}, lang.ActionNone, false},
		{"日文 Space 変換", ja, " ", lang.KeyState{Composing: true}, lang.ActionConvert, true},
		{"日文 Space 変換中換候選", ja, " ", lang.KeyState{Converting: true}, lang.ActionConvert, true},
		{"日文 Enter 送出", ja, proto.KeyEnter, lang.KeyState{Converting: true}, lang.ActionCommit, true},
		{"日文 Tab 換文節", ja, proto.KeyTab, lang.KeyState{Converting: true}, lang.ActionNextClause, true},
		{"日文 Shift+Tab 退文節", ja, proto.KeyBackTab, lang.KeyState{Converting: true}, lang.ActionPrevClause, true},
		{"日文 Escape 回かな", ja, proto.KeyEscape, lang.KeyState{Converting: true}, lang.ActionCancel, true},
		{"日文 無変換 回かな", ja, proto.KeyNonConvert, lang.KeyState{Converting: true}, lang.ActionCancel, true},
		{"日文 Convert 変換", ja, proto.KeyConvert, lang.KeyState{Composing: true}, lang.ActionConvert, true},
		{"日文 F6", ja, proto.KeyHiragana, lang.KeyState{Converting: true}, lang.ActionToHiragana, true},
		{"日文 F7", ja, proto.KeyKatakana, lang.KeyState{Converting: true}, lang.ActionToKatakana, true},
		{"日文 F8", ja, proto.KeyHalfWidthKana, lang.KeyState{Converting: true}, lang.ActionToHalfWidthKana, true},
		{"日文 F9", ja, proto.KeyFullWidthLatin, lang.KeyState{Converting: true}, lang.ActionToFullWidthLatin, true},
		{"日文 F10", ja, proto.KeyHalfWidthLatin, lang.KeyState{Converting: true}, lang.ActionToHalfWidthLatin, true},
		{"日文選字鍵", ja, "1", lang.KeyState{Converting: true}, lang.ActionSelect, true},
		{"日文かな插入", ja, "あ", lang.KeyState{}, lang.ActionInsert, true},
	}
	for _, c := range cases {
		got, ok := c.pol.Handle(c.key, c.state)
		if got != c.want || ok != c.ok {
			t.Errorf("%s：Handle(%q) = %v,%v want %v,%v", c.name, c.key, got, ok, c.want, c.ok)
		}
	}
}

func TestCoreSessionProductionDoesNotImportJapanese(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, "github.com/0c-biggo/0c-ime/core/ja/") {
				t.Errorf("%s 不得 import %q", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
