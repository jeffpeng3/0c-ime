package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/proto"
)

// T27：語言選擇與日文選項的預設值、舊檔載入、遷移與能力旗標。

func TestLanguageDefaultsKeepOldSettingsOnZhuyin(t *testing.T) {
	s := Default()
	if s.Language != LanguageZh || s.LanguageMemory != LanguageMemoryField || !s.LanguageHotkey {
		t.Fatalf("語言預設不對：%+v", s)
	}
	if s.JaInputMethod != JaInputRomaji || s.JaLiveConversion || !s.JaSymbols || !s.JaLearning || !s.JaModel {
		t.Fatalf("日文選項預設不對：%+v", s)
	}
	if s.Japanese() {
		t.Fatal("沒有選日文時 Japanese 應為 false")
	}
}

// 舊設定檔沒有 language 與 ja_* 欄位：照注音與日文預設值載入，其他欄位不動；存檔時補上新欄位，
// 再把檔案交回舊版也不會被拒收（schema 沒有升版）。
func TestOldSettingsFileLoadsAndMigratesLanguageFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := `{"schema":2,"selection":"statistical","punctuation":true,"keyboard_layout":"hsu"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Read(path)
	if err != nil || r.ReadOnly {
		t.Fatalf("舊檔應可讀可寫：%+v %v", r, err)
	}
	s := r.Settings
	if s.Language != LanguageZh || s.JaInputMethod != JaInputRomaji || s.Selection != Statistical || !s.Punctuation || s.KeyboardLayout != LayoutHsu {
		t.Fatalf("舊檔載入不對：%+v", s)
	}
	if err = Save(path, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, key := range []string{`"language": "zh"`, `"language_memory": "field"`, `"language_hotkey": true`, `"ja_input_method": "romaji"`, `"ja_model": true`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("存檔應補上 %s：\n%s", key, b)
		}
	}
	var head struct {
		Schema int `json:"schema"`
	}
	if json.Unmarshal(b, &head) != nil || head.Schema != SchemaVersion || SchemaVersion != 2 {
		t.Fatalf("新增欄位不應升 schema，舊版才能繼續寫：%s", b)
	}
	back, err := Load(path)
	if err != nil || back != s {
		t.Fatalf("來回不一致：%+v %v", back, err)
	}
}

// 讀不懂的語言值只退回預設，原文保留到下次存檔；使用者明確改它才蓋掉。
func TestUnknownLanguageValueFallsBackAndIsPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := `{"schema":2,"language":"ko","language_memory":"per_app","ja_input_method":"t9"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Settings
	if s.Language != LanguageZh || s.LanguageMemory != LanguageMemoryField || s.JaInputMethod != JaInputRomaji {
		t.Fatalf("不認得的值應退回預設：%+v", s)
	}
	if len(r.Notes) == 0 {
		t.Fatal("應說明哪些欄位讀不懂")
	}
	if err = Save(path, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"language": "ko"`) {
		t.Fatalf("讀不懂的值應原樣保留：%s", b)
	}
	if err = s.Set("language", LanguageJa); err != nil {
		t.Fatal(err)
	}
	if err = Save(path, s); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), `"language": "ko"`) || !strings.Contains(string(b), `"language": "ja"`) {
		t.Fatalf("明確設定應蓋掉保留的原文：%s", b)
	}
}

func TestJapaneseSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s := Default()
	for key, value := range map[string]string{
		"language": LanguageJa, "language-memory": LanguageMemoryGlobal, "language-hotkey": "off",
		"ja-input-method": JaInputKana, "ja-live-conversion": "on", "ja-symbols": "off",
		"ja-learning": "off", "ja-model": "off",
	} {
		if err := s.Set(key, value); err != nil {
			t.Fatalf("%s：%v", key, err)
		}
	}
	if !s.Japanese() || !s.JaLiveConversion || s.JaSymbols || s.JaLearning || s.JaModel {
		t.Fatalf("設定沒有套用：%+v", s)
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil || back != s || !back.Japanese() || back.JaInputMethod != JaInputKana {
		t.Fatalf("來回不一致：%+v %v", back, err)
	}
	if v, ok := back.Value("language"); !ok || v != LanguageJa {
		t.Fatalf("Value(language) = %q %v", v, ok)
	}
}

func TestLanguageItemsSayWhichLanguageAndCapabilityTheyNeed(t *testing.T) {
	s := Default()
	items := map[string]Item{}
	for _, it := range append(s.Items(), s.LanguageItems()...) {
		items[it.Key] = it
	}
	for _, key := range []string{"keyboard-layout", "toneless", "zhuyin-error-hint", "output-simplified", "simplified-hotkey", "fuzzy-z-zh", "style-tab", "space-selects", "emoji-shortcut"} {
		if got := items[key].Languages; len(got) != 1 || got[0] != LanguageZh {
			t.Errorf("%s 應只在注音模式出現：%v", key, got)
		}
	}
	for _, key := range []string{"ja-input-method", "ja-live-conversion", "ja-symbols", "ja-learning", "ja-model"} {
		if got := items[key].Languages; len(got) != 1 || got[0] != LanguageJa {
			t.Errorf("%s 應只在日文模式出現：%v", key, got)
		}
	}
	if got := items["language"].Capabilities; len(got) != 1 || got[0] != proto.CapJapanese {
		t.Errorf("語言選擇要標示 japanese 能力：%v", got)
	}
	if got := items["ja-live-conversion"].Capabilities; len(got) != 2 || got[0] != proto.CapJapanese || got[1] != proto.CapLiveConversion {
		t.Errorf("ライブ変換要標示 japanese 與 live_conversion：%v", got)
	}
	for _, key := range []string{"selection", "candidate-layout", "language", "language-memory", "language-hotkey"} {
		if got := items[key].Languages; len(got) != 0 {
			t.Errorf("%s 應不分語言：%v", key, got)
		}
	}
	if len(s.Items()) != len(specs()) || len(s.LanguageItems()) != len(languageSpecs()) {
		t.Fatal("語言選項不應混進引擎設定清單")
	}
	// 設定程式從 profiles.language 讀，0c-update 才不必改程式就能轉送。
	if got := s.Profiles().Language; len(got) != len(s.LanguageItems()) || got[0].Key != "language" {
		t.Fatalf("Profiles().Language 應帶語言選項：%+v", got)
	}
	b, err := json.Marshal(s.Profiles())
	if err != nil || !strings.Contains(string(b), `"language":[`) {
		t.Fatalf("profiles 的 JSON 應有 language：%s %v", b, err)
	}
	if b, _ := json.Marshal(items["keyboard-layout"]); !strings.Contains(string(b), `"languages":["zh"]`) {
		t.Fatalf("item 應帶適用語言：%s", b)
	}
	if b, _ := json.Marshal(items["ja-live-conversion"]); !strings.Contains(string(b), `"japanese"`) || !strings.Contains(string(b), `"live_conversion"`) {
		t.Fatalf("item 應帶能力旗標：%s", b)
	}
	for _, it := range s.Items() {
		if it.Key == "language" || it.Key == "ja-model" {
			t.Fatalf("引擎設定清單不應有語言選項：%s", it.Key)
		}
	}
}

func TestLanguageSetRejectsUnknownValuesAndKeepsOldOnes(t *testing.T) {
	s := Default()
	for _, c := range []struct{ key, value string }{
		{"language", "ko"}, {"language-memory", "per_app"}, {"ja-input-method", "t9"},
		{"language-hotkey", "yes"}, {"ja-live-conversion", "maybe"},
	} {
		before := s
		var se *SetError
		if err := s.Set(c.key, c.value); !errors.As(err, &se) || se.Code != CodeInvalidValue || se.Key != c.key || s != before {
			t.Errorf("%v：%v", c, err)
		}
	}
	if _, ok := Keys()["language"]; !ok {
		t.Fatal("命令列說明應列出 language")
	}
	if _, ok := Keys()["ja-model"]; !ok {
		t.Fatal("命令列說明應列出 ja-model")
	}
}

// 0c-update set language … 的路徑：寫入鎖內讀改寫，下一次讀就是新語言。
func TestLanguageChangeThroughSettingsFile(t *testing.T) {
	user := filepath.Join(t.TempDir(), "user.json")
	if err := Save(Path(user), Default()); err != nil {
		t.Fatal(err)
	}
	changed, err := Change(user, [2]string{"language", LanguageJa})
	if err != nil || !changed {
		t.Fatalf("Change：%v %v", changed, err)
	}
	s, err := Load(Path(user))
	if err != nil || !s.Japanese() || s.JaModel != true {
		t.Fatalf("語言沒有寫入：%+v %v", s, err)
	}
	if changed, err = Change(user, [2]string{"language", LanguageJa}); err != nil || changed {
		t.Fatalf("同值不應重寫：%v %v", changed, err)
	}
}

// 寫入失敗要有明確錯誤，不能默默當成已儲存。
func TestLanguageSaveReportsWriteFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Default()
	if err := s.Set("language", LanguageJa); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(file, FileName), s); err == nil {
		t.Fatal("寫不進去的設定檔應回錯誤")
	}
	s.readOnly = true
	if err := Save(filepath.Join(t.TempDir(), FileName), s); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("來自較新版本的設定應拒絕寫入：%v", err)
	}
}
