package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P2-33：模型常駐設定、舊檔遷移與預算。

func TestModelSettingsDefaults(t *testing.T) {
	s := Default()
	if s.ModelMode != ModelModeAuto || s.PrimaryLanguage != LanguageZh {
		t.Fatalf("模型模式預設不對：%+v", s)
	}
	if s.ModelResidencyMB != ModelResidencyAutoMB || s.AllowPlaceholders {
		t.Fatalf("預算或 placeholder 預設不對：%+v", s)
	}
	if got := s.ModelBudgetMB(false); got != ModelResidencySingleMB {
		t.Fatalf("單語言預算 %d", got)
	}
	if got := s.ModelBudgetMB(true); got != ModelResidencyDualMB {
		t.Fatalf("雙語言預算 %d", got)
	}
	if s.ModelBudgetBytes(false) != int64(ModelResidencySingleMB)<<20 || s.ModelBudgetSource(false) != "phase2-single" {
		t.Fatalf("單語言預算換算不對：%d %s", s.ModelBudgetBytes(false), s.ModelBudgetSource(false))
	}
	if s.ModelBudgetSource(true) != "phase2-dual" {
		t.Fatalf("雙語言預算來源 %s", s.ModelBudgetSource(true))
	}
}

func TestOldSettingsFileMigratesModelFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := `{"schema":2,"selection":"statistical","language":"ja","future":"x"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Read(path)
	if err != nil || r.ReadOnly {
		t.Fatalf("舊檔應可讀可寫：%+v %v", r, err)
	}
	s := r.Settings
	if s.ModelMode != ModelModeAuto || s.ModelResidencyMB != ModelResidencyAutoMB || s.AllowPlaceholders {
		t.Fatalf("舊檔沒有模型欄位時應套用預設：%+v", s)
	}
	if s.PrimaryLanguage != LanguageJa {
		t.Fatalf("主要語言應沿用既有的 language=ja：%q", s.PrimaryLanguage)
	}
	if err = Save(path, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, key := range []string{`"model_mode": "auto"`, `"primary_language": "ja"`, `"model_residency_mb": 0`, `"allow_placeholders": false`, `"future": "x"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("存檔應補上或保留 %s：\n%s", key, b)
		}
	}
	if strings.Contains(string(b), `"language":`) {
		t.Fatalf("舊的 language 遷移後應移除：\n%s", b)
	}
	var head struct {
		Schema int `json:"schema"`
	}
	if json.Unmarshal(b, &head) != nil || head.Schema != SchemaVersion || SchemaVersion != 2 {
		t.Fatalf("新增欄位不升 schema，舊版才能繼續寫：%s", b)
	}
	back, err := Load(path)
	if err != nil || back != s {
		t.Fatalf("來回不一致：%+v %v", back, err)
	}
}

func TestModelSettingsPersistThroughSetAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s := Default()
	for _, p := range [][2]string{
		{"model-mode", ModelModePrimary},
		{"primary-language", LanguageJa},
		{"model-residency-mb", "400"},
		{"allow-placeholders", "on"},
	} {
		if err := s.Set(p[0], p[1]); err != nil {
			t.Fatalf("%s: %v", p[0], err)
		}
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.ModelMode != ModelModePrimary || back.PrimaryLanguage != LanguageJa || back.ModelResidencyMB != 400 || !back.AllowPlaceholders {
		t.Fatalf("模型設定沒有持久化：%+v", back)
	}
	if back.ModelBudgetMB(false) != 400 || back.ModelBudgetSource(false) != "configured" {
		t.Fatalf("明確預算沒有生效：%d %s", back.ModelBudgetMB(false), back.ModelBudgetSource(false))
	}
	if v, ok := back.Value("model-residency-mb"); !ok || v != "400" {
		t.Fatalf("Value(model-residency-mb)=%q %v", v, ok)
	}
	if err := back.Set("model-residency-mb", "auto"); err != nil {
		t.Fatal(err)
	}
	if back.ModelResidencyMB != 0 || back.ModelBudgetMB(true) != ModelResidencyDualMB {
		t.Fatalf("auto 沒有回到 Phase 2 預算：%+v", back)
	}
}

func TestUnusableModelResidencyValueIsKeptUntilSet(t *testing.T) {
	for _, raw := range []string{`9999`, `-1`, `"many"`} {
		path := filepath.Join(t.TempDir(), FileName)
		body := `{"schema":2,"model_residency_mb":` + raw + `,"model_mode":"sometimes","primary_language":"ko","allow_placeholders":"maybe","model_set":"private"}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		s := r.Settings
		if s.ModelResidencyMB != 0 || s.ModelMode != ModelModeAuto || s.PrimaryLanguage != LanguageZh || s.AllowPlaceholders {
			t.Fatalf("%s：讀不懂的值應只退回該欄位預設：%+v", raw, s)
		}
		if len(r.Notes) == 0 {
			t.Fatalf("%s：應說明讀不懂的欄位", raw)
		}
		if err = Save(path, s); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		for _, key := range []string{`"model_residency_mb": ` + raw, `"model_mode": "sometimes"`, `"primary_language": "ko"`, `"allow_placeholders": "maybe"`, `"model_set": "private"`} {
			if !strings.Contains(string(b), key) {
				t.Fatalf("%s：讀不懂的值應原樣保留 %s：\n%s", raw, key, b)
			}
		}
		if err = s.Set("model-residency-mb", "250"); err != nil {
			t.Fatal(err)
		}
		if err = Save(path, s); err != nil {
			t.Fatal(err)
		}
		b, _ = os.ReadFile(path)
		if strings.Contains(string(b), `"model_residency_mb": `+raw) || !strings.Contains(string(b), `"model_residency_mb": 250`) {
			t.Fatalf("%s：明確設定後應蓋掉保留的原文：%s", raw, b)
		}
	}
}

func TestModelSettingsAreListedInItems(t *testing.T) {
	keys := Keys()
	for _, key := range []string{"model-mode", "primary-language", "model-residency-mb", "allow-placeholders"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("Keys 缺少 %s", key)
		}
	}
	found := map[string]Item{}
	for _, item := range Default().Items() {
		found[item.Key] = item
	}
	for _, key := range []string{"model-mode", "primary-language", "model-residency-mb", "allow-placeholders"} {
		item, ok := found[key]
		if !ok {
			t.Fatalf("P2-42：模型設定應列進 Items：%s", key)
		}
		if item.Label == "" || item.Description == "" || len(item.Values) == 0 {
			t.Fatalf("%s 的契約不完整：%+v", key, item)
		}
	}
	if _, ok := found["model-set"]; ok {
		t.Fatal("設定 UI 不應提供模型組選擇")
	}
}
