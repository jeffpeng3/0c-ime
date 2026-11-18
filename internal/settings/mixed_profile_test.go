package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P2-42：Mixed profile 的設定程式契約。主要語言、輸出變體、模型模式／預算、模型組與
// placeholder 政策要能持久化；舊的 English profile 欄位（language、language_*、
// output_simplified、english_profile）遷移後從檔案移除。

func TestMixedProfileDefaults(t *testing.T) {
	s := Default()
	if s.PrimaryLanguage != LanguageZh || s.OutputVariant != VariantTraditional {
		t.Fatalf("主要語言或輸出變體預設不對：%+v", s)
	}
	if s.ModelMode != ModelModeAuto || s.ModelResidencyMB != ModelResidencyAutoMB {
		t.Fatalf("模型模式或預算預設不對：%+v", s)
	}
	if s.ModelSet != ModelSetCCBYSA || s.AllowPlaceholders {
		t.Fatalf("模型組或 placeholder 預設不對：%+v", s)
	}
}

func TestMixedProfileMigratesAndRemovesEnglishProfileKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := `{
		"schema": 2,
		"language": "ja",
		"language_memory": "global",
		"language_hotkey": false,
		"english_profile": true,
		"output_simplified": true,
		"start_mode": "english",
		"toggle_key": "both",
		"future": {"keep": true}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Read(path)
	if err != nil || r.ReadOnly {
		t.Fatalf("舊檔應可讀可寫：%+v %v", r, err)
	}
	s := r.Settings
	if s.PrimaryLanguage != LanguageJa || !s.Japanese() || s.OutputVariant != VariantSimplified || !s.OutputSimplified {
		t.Fatalf("舊欄位沒有被沿用：%+v", s)
	}
	// 中英模式的舊設定仍有引擎會用，不能被遷移吃掉。
	if s.StartMode != StartEnglish || s.ToggleKey != ToggleBoth {
		t.Fatalf("中英模式設定不應被移除：%+v", s)
	}
	if err = Save(path, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, key := range []string{`"language":`, `"language_memory"`, `"language_hotkey"`, `"english_profile"`, `"output_simplified"`} {
		if strings.Contains(string(b), key) {
			t.Fatalf("English profile 舊鍵應移除 %s：\n%s", key, b)
		}
	}
	for _, key := range []string{`"primary_language": "ja"`, `"output_variant": "simplified"`, `"model_set": "cc-by-sa"`, `"future": {`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("存檔應有 %s：\n%s", key, b)
		}
	}
	back, err := Load(path)
	if err != nil || back != s {
		t.Fatalf("遷移後來回不一致：%+v %v", back, err)
	}
}

func TestMixedProfileNewKeysWinOverOldEnglishSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := `{"schema":2,"language":"ja","primary_language":"zh","output_simplified":true,"output_variant":"shinjitai"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.PrimaryLanguage != LanguageZh || s.OutputVariant != VariantShinjitai || s.OutputSimplified {
		t.Fatalf("新欄位應優先：%+v", s)
	}
}

func TestMixedProfileSettingsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s := Default()
	for _, pair := range [][2]string{
		{"primary-language", LanguageJa},
		{"output-variant", VariantShinjitai},
		{"model-mode", ModelModePrimary},
		{"model-residency-mb", "512"},
		{"model-set", ModelSetCCBYSA},
		{"allow-placeholders", "on"},
		{"mixed-input", "on"},
		{"english-autocomplete", "on"},
		{"cjk-latin-space", "on"},
	} {
		if err := s.Set(pair[0], pair[1]); err != nil {
			t.Fatalf("%s=%s：%v", pair[0], pair[1], err)
		}
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil || back != s {
		t.Fatalf("來回不一致：%+v %v", back, err)
	}
	if back.PrimaryLanguage != LanguageJa || back.OutputVariant != VariantShinjitai || back.ModelMode != ModelModePrimary ||
		back.ModelResidencyMB != 512 || back.ModelSet != ModelSetCCBYSA || !back.AllowPlaceholders ||
		!back.MixedInput || !back.EnglishAutocomplete || !back.CJKLatinSpace {
		t.Fatalf("Mixed 設定沒有持久化：%+v", back)
	}
	// 舊的輸出簡體開關不可以蓋掉日文新字體。
	if err := back.Set("output-simplified", "off"); err != nil || back.OutputVariant != VariantTraditional {
		t.Fatalf("舊鍵 off 應回到繁體：%+v %v", back, err)
	}
}

func TestMixedProfileHidesEnglishProfileItemsAndMarksZhuyinOnly(t *testing.T) {
	s := Default()
	items := map[string]Item{}
	for _, it := range s.Items() {
		items[it.Key] = it
	}
	for _, key := range []string{"language", "language-memory", "language-hotkey", "output-simplified", "english-profile"} {
		if _, ok := items[key]; ok {
			t.Fatalf("English profile 設定不應出現在契約：%s", key)
		}
	}
	for _, key := range []string{"primary-language", "output-variant", "model-mode", "model-residency-mb", "model-set", "allow-placeholders", "mixed-input", "english-autocomplete", "cjk-latin-space"} {
		if _, ok := items[key]; !ok {
			t.Fatalf("Mixed 設定應出現在契約：%s", key)
		}
	}
	// 注音專屬的欄位仍帶 languages=zh，設定程式在 Mixed 模式可以據此標示不適用。
	for _, key := range []string{"keyboard-layout", "toneless", "zhuyin-error-hint", "space-selects", "caps-lock-english"} {
		if got := items[key].Languages; len(got) != 1 || got[0] != LanguageZh {
			t.Fatalf("%s 應標成注音專屬：%v", key, got)
		}
	}
	// 主要語言與混合輸入不分語言。
	for _, key := range []string{"primary-language", "mixed-input", "english-autocomplete", "output-variant"} {
		if got := items[key].Languages; len(got) != 0 {
			t.Fatalf("%s 不應限制語言：%v", key, got)
		}
	}
}

func TestMixedProfileReportShapeRoundTrips(t *testing.T) {
	// -model-report 的欄位由 cmd/0c-server 提供；設定檔只保存選擇。這裡確認新欄位能穩定序列化，
	// 設定程式可把它和報告的 set_id／variant／placeholder／resident_bytes 一起顯示。
	s := Default()
	s.ModelSet = ModelSetCCBYSA
	b, err := json.Marshal(s)
	if err != nil || !strings.Contains(string(b), `"model_set":"cc-by-sa"`) {
		t.Fatalf("序列化模型組：%s %v", b, err)
	}
}
