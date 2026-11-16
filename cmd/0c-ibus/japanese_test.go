//go:build linux

package main

import (
	"net"
	"slices"
	"sync"
	"testing"

	"github.com/0c-biggo/0c-ime/bridge/ipc"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/godbus/dbus/v5"
)

// T24：日文模式、文節與実体鍵。

func TestJapaneseKeyMapping(t *testing.T) {
	for _, c := range []struct {
		keyval uint32
		want   string
	}{
		{keyHenkan, proto.KeyConvert}, {keyMuhenkan, proto.KeyNonConvert},
		{keyF6, proto.KeyHiragana}, {keyF7, proto.KeyKatakana}, {keyF8, proto.KeyHalfWidthKana},
		{keyF9, proto.KeyFullWidthLatin}, {keyF10, proto.KeyHalfWidthLatin},
		{'k', ""}, {keyF11, ""}, {keyReturn, ""},
	} {
		if got := japaneseKey(c.keyval); got != c.want {
			t.Errorf("japaneseKey(%#x) = %q，要 %q", c.keyval, got, c.want)
		}
	}
}

// 舊引擎、注音／英文模式、帶修飾鍵與放開都不送日文鍵。
func TestJapaneseKeyGating(t *testing.T) {
	for _, c := range []struct {
		name     string
		keyval   uint32
		state    uint32
		japanese bool
		convert  bool
		want     string
	}{
		{"日文模式且認得鍵", keyHenkan, 0, true, true, proto.KeyConvert},
		{"不是日文模式", keyHenkan, 0, false, true, ""},
		{"引擎沒有 convert_keys", keyHenkan, 0, true, false, ""},
		{"帶 Ctrl", keyHenkan, maskControl, true, true, ""},
		{"帶 Alt", keyF6, maskMod1, true, true, ""},
		{"放開", keyHenkan, maskRelease, true, true, ""},
		{"不是日文鍵", 'k', 0, true, true, ""},
	} {
		if got := japaneseKeyFor(c.keyval, c.state, c.japanese, c.convert); got != c.want {
			t.Errorf("%s：%q，要 %q", c.name, got, c.want)
		}
	}
}

func TestJapanesePreeditAttrs(t *testing.T) {
	has := func(attrs []attribute, typ, value uint32) bool {
		return slices.ContainsFunc(attrs, func(a attribute) bool {
			return a.typ == typ && a.value == value && a.start == 2 && a.end == 4
		})
	}
	if attrs := preeditAttrs(proto.StyleClause, 2, 4); len(attrs) != 1 || !has(attrs, attrUnderline, underlineSingle) {
		t.Fatalf("一般文節應是細底線：%+v", attrs)
	}
	if attrs := preeditAttrs(proto.StyleClauseActive, 2, 4); len(attrs) != 1 || !has(attrs, attrUnderline, underlineDouble) {
		t.Fatalf("目前文節應是粗底線：%+v", attrs)
	}
}

func TestJapaneseIndicator(t *testing.T) {
	for _, c := range []struct{ method, symbol, label string }{
		{"", "あ", "羅馬字"}, {"romaji", "あ", "羅馬字"},
		{"kana", "カ", "かな"}, {"katakana", "カ", "かな"},
		{"half_width_kana", "ｶ", "半角カナ"},
		{"full_width_latin", "Ａ", "全角英數"}, {"half_width_latin", "A", "半角英數"},
	} {
		if symbol, label := japaneseIndicator(c.method); symbol != c.symbol || label != c.label {
			t.Errorf("japaneseIndicator(%q) = %q %q，要 %q %q", c.method, symbol, label, c.symbol, c.label)
		}
	}
}

func TestClauseTitle(t *testing.T) {
	for _, c := range []struct {
		index, count int
		want         string
	}{
		{0, 0, ""}, {0, 3, "文節 1/3"}, {1, 3, "文節 2/3"}, {5, 3, "文節 3/3"},
	} {
		got := clauseTitle(snapshot{Snapshot: proto.Snapshot{ClauseIndex: c.index, ClauseCount: c.count}})
		if got != c.want {
			t.Errorf("clauseTitle(%d/%d) = %q，要 %q", c.index, c.count, got, c.want)
		}
	}
}

// fakeJapanese 是只回固定快照的 server：open 回報能力，handle 記下送來的鍵並回日文快照。
type fakeJapanese struct {
	mu   sync.Mutex
	keys []string
}

func startFakeJapanese(t *testing.T, sock string, info map[string]string) (*fakeJapanese, func()) {
	t.Helper()
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeJapanese{}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				for {
					var env ipc.Envelope
					if ipc.ReadFrame(conn, &env) != nil {
						return
					}
					reply := ipc.Reply{OK: true}
					switch env.Op {
					case ipc.OpOpen:
						reply.Session, reply.Info = 1, info
					case ipc.OpHandle:
						if env.Req != nil && env.Req.Type == proto.Key {
							f.mu.Lock()
							f.keys = append(f.keys, env.Req.Key)
							f.mu.Unlock()
						}
						reply.Snapshot = &proto.Snapshot{
							Mode: proto.ModeJapanese, Language: "ja", InputMethod: "romaji", Handled: true,
							Preedit: []proto.Segment{{Text: "きょう", Style: proto.StyleNormal}},
						}
					}
					if ipc.WriteFrame(conn, reply) != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return f, func() { l.Close() }
}

func (f *fakeJapanese) takeKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.keys
	f.keys = nil
	return out
}

// 日文実体鍵只在日文模式而且引擎回報 japanese 與 convert_keys 時送；其他情況照舊交給應用程式。
func TestJapaneseNamedKeysForwardedOnlyWithCapabilityAndMode(t *testing.T) {
	full := map[string]string{proto.CapJapanese: "true", proto.CapConvertKeys: "true"}
	for _, c := range []struct {
		name     string
		info     map[string]string
		japanese bool
		keyval   uint32
		state    uint32
		wantKey  string
		handled  bool
	}{
		{"日文模式，変換", full, true, keyHenkan, 0, proto.KeyConvert, true},
		{"日文模式，F7", full, true, keyF7, 0, proto.KeyKatakana, true},
		{"日文模式，放開不送", full, true, keyHenkan, maskRelease, "", false},
		{"日文模式，帶 Ctrl 不送", full, true, keyHenkan, maskControl, "", false},
		{"引擎沒有 convert_keys 時交給應用程式", map[string]string{proto.CapJapanese: "true"}, true, keyHenkan, 0, "", false},
		{"不是日文模式時交給應用程式", full, false, keyHenkan, 0, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, _, sock := newTestEngine(t)
			fake, stop := startFakeJapanese(t, sock, c.info)
			defer stop()
			e.session.japanese = c.japanese
			if got := e.processKey(c.keyval, 0, c.state); got != c.handled {
				t.Fatalf("handled = %v，要 %v", got, c.handled)
			}
			keys := fake.takeKeys()
			if c.wantKey == "" {
				if len(keys) != 0 {
					t.Fatalf("不該送日文鍵：%v", keys)
				}
				return
			}
			if !slices.Equal(keys, []string{c.wantKey}) {
				t.Fatalf("送出的鍵 = %v，要 [%s]", keys, c.wantKey)
			}
			// 快照是日文模式：指示器顯示「あ」，模式跟著快照。
			if !e.japanese || e.method != "romaji" {
				t.Fatalf("日文模式 = %v method=%q", e.japanese, e.method)
			}
			if p := e.modeProperty(); p.symbol != "あ" || p.label != "日文・羅馬字" {
				t.Fatalf("指示器 = %q %q", p.symbol, p.label)
			}
		})
	}
}

// 指示器照快照的 input_method 顯示；沒有 japanese 能力時退回注音。
func TestJapaneseModePropertyFollowsInputMethod(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.session.id, e.session.generation = 1, e.svc.client.generation
	e.session.info = map[string]string{proto.CapJapanese: "true"}
	e.updateMode(snapshot{Snapshot: proto.Snapshot{Mode: proto.ModeJapanese, InputMethod: "kana"}})
	if !e.japanese || e.method != "kana" {
		t.Fatalf("日文模式 = %v method=%q", e.japanese, e.method)
	}
	if p := e.modeProperty(); p.symbol != "カ" || p.label != "日文・かな" {
		t.Fatalf("かな指示器 = %q %q", p.symbol, p.label)
	}
	e.updateMode(snapshot{Snapshot: proto.Snapshot{Mode: proto.ModeJapanese, InputMethod: "romaji"}})
	if p := e.modeProperty(); p.symbol != "あ" {
		t.Fatalf("羅馬字指示器 = %q", p.symbol)
	}
	// 舊引擎（沒有 japanese）送 japanese 模式時不顯示日文。
	e.session.info = map[string]string{}
	e.updateMode(snapshot{Snapshot: proto.Snapshot{Mode: proto.ModeJapanese, InputMethod: "kana"}})
	if e.japanese {
		t.Fatal("沒有 japanese 能力時不該顯示日文")
	}
	if p := e.modeProperty(); p.symbol != "注" {
		t.Fatalf("退回注音指示器 = %q", p.symbol)
	}
}

// 文節樣式與「文節 N/M」只在 clause_display 能力開啟時畫；沒有時退回一般組字。
func TestJapaneseClauseDisplayGatedByCapability(t *testing.T) {
	snap := snapshot{ok: true}
	snap.Handled = true
	snap.Mode = proto.ModeJapanese
	snap.Language = "ja"
	snap.InputMethod = "romaji"
	snap.ClauseIndex, snap.ClauseCount = 1, 2
	snap.Preedit = []proto.Segment{{Text: "今日", Style: proto.StyleClause}, {Text: "は", Style: proto.StyleClauseActive}}
	snap.Cursor = proto.CursorPos{Rune: 3, UTF16: 3}

	e, rec, _ := newTestEngine(t)
	e.session.id, e.session.generation = 1, e.svc.client.generation
	e.session.info = map[string]string{proto.CapJapanese: "true", proto.CapClauseDisplay: "true"}
	e.render(snap)
	if e.aux != "文節 2/2" {
		t.Fatalf("提示列 = %q，要 文節 2/2", e.aux)
	}
	attrs := lastPreeditAttrs(t, rec.take())
	if !slices.ContainsFunc(attrs, func(a ibusAttribute) bool {
		return a.Type == attrUnderline && a.Value == underlineSingle && a.Start == 0 && a.End == 2
	}) || !slices.ContainsFunc(attrs, func(a ibusAttribute) bool {
		return a.Type == attrUnderline && a.Value == underlineDouble && a.Start == 2 && a.End == 3
	}) {
		t.Fatalf("文節樣式 = %+v", attrs)
	}
	if p := e.modeProperty(); p.symbol != "あ" {
		t.Fatalf("指示器 = %q", p.symbol)
	}

	// 引擎沒有 clause_display：同樣的分段照一般組字畫，也不顯示文節位置。
	e.session.info = map[string]string{proto.CapJapanese: "true"}
	e.render(snap)
	if e.aux != "" {
		t.Fatalf("沒有 clause_display 時不該顯示文節：%q", e.aux)
	}
	attrs = lastPreeditAttrs(t, rec.take())
	for _, a := range attrs {
		if a.Type == attrUnderline && a.Value != underlineSingle {
			t.Fatalf("沒有 clause_display 時應退回細底線：%+v", attrs)
		}
	}
	// 快照再來一次相同分段也不重畫（樣式已降級後保持穩定）。
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("相同畫面不該重送：%+v", got)
	}
}

// 有候選時文節位置接在候選標題後面。
func TestJapaneseClauseTitleJoinsCandidateTitle(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.session.id, e.session.generation = 1, e.svc.client.generation
	e.session.info = map[string]string{proto.CapJapanese: "true", proto.CapClauseDisplay: "true"}
	snap := snapshot{ok: true}
	snap.Handled = true
	snap.Mode = proto.ModeJapanese
	snap.ClauseIndex, snap.ClauseCount = 0, 2
	snap.Preedit = []proto.Segment{{Text: "きょう", Style: proto.StyleClauseActive}}
	snap.Candidates = &proto.CandidateList{Items: []proto.CandidateItem{{ID: 1, Text: "今日"}}, Title: "候補"}
	e.render(snap)
	if want := "候補　文節 1/2"; e.aux != want {
		t.Fatalf("提示列 = %q，要 %q", e.aux, want)
	}
}

func lastPreeditAttrs(t *testing.T, sig []recorded) []ibusAttribute {
	t.Helper()
	for i := len(sig) - 1; i >= 0; i-- {
		if sig[i].name != "UpdatePreeditText" {
			continue
		}
		text := sig[i].args[0].(dbus.Variant).Value().(ibusText)
		list := text.Attrs.Value().(ibusAttrList)
		out := make([]ibusAttribute, 0, len(list.Attrs))
		for _, v := range list.Attrs {
			out = append(out, v.Value().(ibusAttribute))
		}
		return out
	}
	return nil
}
