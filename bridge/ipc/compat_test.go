package ipc

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/proto"
)

func readFixture(t *testing.T, name string, v any) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return b
}

// 舊版引擎與外殼的 fixture 解碼結果不變；新欄位全部 omitempty，重新編碼不會多出鍵。
func TestLegacyFixturesDecodeUnchanged(t *testing.T) {
	var open Reply
	readFixture(t, "legacy_open.json", &open)
	if !open.OK || open.Session != 7 || open.Info["version"] != "0.2.12" || open.Info["vocab_keys"] != "true" {
		t.Fatalf("legacy open: %+v", open)
	}
	for _, cap := range []string{proto.CapJapanese, proto.CapConvertKeys, proto.CapClauseDisplay, proto.CapLiveConversion} {
		if proto.Capability(open.Info, cap) {
			t.Errorf("舊引擎不該回報 %s", cap)
		}
	}

	var health Reply
	readFixture(t, "legacy_health.json", &health)
	if !health.OK || health.Info["pid"] != "4242" || health.Info["composing"] != "0" {
		t.Fatalf("legacy health: %+v", health)
	}
	for _, cap := range []string{proto.CapJapanese, proto.CapConvertKeys, proto.CapClauseDisplay, proto.CapLiveConversion} {
		if proto.Capability(health.Info, cap) {
			t.Errorf("舊引擎不該回報 %s", cap)
		}
	}

	var req Envelope
	readFixture(t, "legacy_request.json", &req)
	if req.Op != OpHandle || req.Session != 7 || req.Req == nil || req.Req.Type != proto.Key || req.Req.Key != "a" {
		t.Fatalf("legacy request: %+v", req)
	}

	var reply Reply
	readFixture(t, "legacy_snapshot.json", &reply)
	if !reply.OK || reply.EngineUS != 42 || reply.Snapshot == nil {
		t.Fatalf("legacy snapshot reply: %+v", reply)
	}
	snap := reply.Snapshot
	if snap.Revision != 7 || !snap.Handled || snap.Mode != proto.ModeZhuyin {
		t.Fatalf("legacy snapshot: %+v", snap)
	}
	if got := snap.Text(); got != "麻婆ㄉㄡˋ" {
		t.Fatalf("組字文字 %q", got)
	}
	if len(snap.Preedit) != 2 || snap.Preedit[1].Style != proto.StylePending || snap.Cursor.Rune != 6 {
		t.Fatalf("legacy preedit: %+v", snap)
	}
	if snap.Language != "" || snap.InputMethod != "" || snap.Converting ||
		snap.ClauseIndex != 0 || snap.ClauseCount != 0 {
		t.Fatalf("舊 fixture 不該解出日文欄位：%+v", snap)
	}

	encoded, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"language", "input_method", "converting", "clause_index", "clause_count"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Errorf("零值欄位 %q 必須 omitempty：%s", key, encoded)
		}
	}
}

// 新 fixture 解出日文欄位與文節樣式；零值 Snapshot 不會產生任何日文鍵。
func TestJapaneseFixtureDecodesNewFields(t *testing.T) {
	var open Reply
	readFixture(t, "ja_open.json", &open)
	if !open.OK || open.Session != 3 {
		t.Fatalf("ja open: %+v", open)
	}
	for _, cap := range []string{proto.CapJapanese, proto.CapConvertKeys, proto.CapClauseDisplay, proto.CapLiveConversion} {
		if !proto.Capability(open.Info, cap) {
			t.Errorf("日文引擎要回報 %s", cap)
		}
	}

	var req Envelope
	readFixture(t, "ja_convert_request.json", &req)
	if req.Req == nil || req.Req.Type != proto.Key || req.Req.Key != proto.KeyConvert {
		t.Fatalf("ja request: %+v", req)
	}

	var reply Reply
	readFixture(t, "ja_snapshot.json", &reply)
	if reply.Snapshot == nil {
		t.Fatal("ja snapshot missing")
	}
	snap := reply.Snapshot
	if snap.Language != "ja" || snap.InputMethod != "romaji" || !snap.Converting ||
		snap.ClauseIndex != 1 || snap.ClauseCount != 2 {
		t.Fatalf("ja snapshot fields: %+v", snap)
	}
	if got := snap.Text(); got != "今日は" {
		t.Fatalf("組字文字 %q", got)
	}
	if len(snap.Preedit) != 2 || snap.Preedit[0].Style != proto.StyleClause || snap.Preedit[1].Style != proto.StyleClauseActive {
		t.Fatalf("ja preedit: %+v", snap.Preedit)
	}

	empty, err := json.Marshal(proto.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"language", "input_method", "converting", "clause_index", "clause_count"} {
		if strings.Contains(string(empty), `"`+key+`"`) {
			t.Errorf("零值 %q 必須省略：%s", key, empty)
		}
	}
}

// 舊外殼讀新引擎的快照：未知欄位被忽略，既有欄位照舊。這是跨版本相容測試。
func TestOldReaderIgnoresJapaneseFields(t *testing.T) {
	type legacySegment struct {
		Text  string `json:"text"`
		Style string `json:"style"`
	}
	type legacyCursor struct {
		Rune  int `json:"rune"`
		UTF16 int `json:"utf16"`
	}
	type legacySnapshot struct {
		Revision int             `json:"revision"`
		Handled  bool            `json:"handled"`
		Preedit  []legacySegment `json:"preedit"`
		Cursor   legacyCursor    `json:"cursor"`
		Mode     string          `json:"mode"`
	}
	var reply struct {
		OK       bool           `json:"ok"`
		Snapshot legacySnapshot `json:"snapshot"`
	}
	readFixture(t, "ja_snapshot.json", &reply)
	if !reply.OK || reply.Snapshot.Revision != 12 || !reply.Snapshot.Handled ||
		reply.Snapshot.Mode != proto.ModeJapanese || len(reply.Snapshot.Preedit) != 2 {
		t.Fatalf("舊讀者解析新快照：%+v", reply)
	}
	text := ""
	for _, g := range reply.Snapshot.Preedit {
		text += g.Text
	}
	if text != "今日は" || reply.Snapshot.Cursor.Rune != 3 {
		t.Fatalf("舊讀者看到 %q cursor=%+v", text, reply.Snapshot.Cursor)
	}
}

// 契約名稱固定，外殼與核心不能各自改名。
func TestJapaneseContractNames(t *testing.T) {
	names := map[string]string{
		"ModeJapanese":      proto.ModeJapanese,
		"KeyConvert":        proto.KeyConvert,
		"KeyNonConvert":     proto.KeyNonConvert,
		"KeyHiragana":       proto.KeyHiragana,
		"KeyKatakana":       proto.KeyKatakana,
		"KeyHalfWidthKana":  proto.KeyHalfWidthKana,
		"KeyFullWidthLatin": proto.KeyFullWidthLatin,
		"KeyHalfWidthLatin": proto.KeyHalfWidthLatin,
		"StyleClause":       proto.StyleClause,
		"StyleClauseActive": proto.StyleClauseActive,
		"CapJapanese":       proto.CapJapanese,
		"CapConvertKeys":    proto.CapConvertKeys,
		"CapClauseDisplay":  proto.CapClauseDisplay,
		"CapLiveConversion": proto.CapLiveConversion,
	}
	want := map[string]string{
		"ModeJapanese":      "japanese",
		"KeyConvert":        "Convert",
		"KeyNonConvert":     "NonConvert",
		"KeyHiragana":       "F6",
		"KeyKatakana":       "F7",
		"KeyHalfWidthKana":  "F8",
		"KeyFullWidthLatin": "F9",
		"KeyHalfWidthLatin": "F10",
		"StyleClause":       "clause",
		"StyleClauseActive": "clause_active",
		"CapJapanese":       "japanese",
		"CapConvertKeys":    "convert_keys",
		"CapClauseDisplay":  "clause_display",
		"CapLiveConversion": "live_conversion",
	}
	for name, got := range names {
		if got != want[name] {
			t.Errorf("%s = %q，契約是 %q", name, got, want[name])
		}
	}
}
