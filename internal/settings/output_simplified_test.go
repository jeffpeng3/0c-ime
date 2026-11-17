package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P2-42：#118 的「輸出簡體字」改成 output-variant。舊設定檔的 output_simplified
// 讀取時換成 simplified／traditional，存檔後只留 output_variant。

func TestOutputVariantDefaultsTraditionalAndMigratesOldSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if s, err := Load(path); err != nil || s.OutputVariant != VariantTraditional || s.OutputSimplified {
		t.Fatalf("預設繁體：%+v %v", s, err)
	}
	if v, _ := Default().Value("output-variant"); v != VariantTraditional {
		t.Fatalf("命令列預設值：%q", v)
	}
	for _, tc := range []struct {
		body    string
		variant string
		simple  bool
	}{
		{`{"schema":1,"output_simplified":true,"selection":"statistical","future":42}`, VariantSimplified, true},
		{`{"schema":1,"output_simplified":false}`, VariantTraditional, false},
	} {
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := Load(path)
		if err != nil || s.OutputVariant != tc.variant || s.OutputSimplified != tc.simple {
			t.Fatalf("舊欄位遷移 %s：%+v %v", tc.body, s, err)
		}
		if err := Save(path, s); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		if strings.Contains(string(b), `"output_simplified"`) {
			t.Fatalf("舊鍵應移除：%s", b)
		}
		if !strings.Contains(string(b), `"output_variant": "`+tc.variant+`"`) || (strings.Contains(tc.body, `"future": 42`) && !strings.Contains(string(b), `"future": 42`)) {
			t.Fatalf("settings.json：%s", b)
		}
	}
}

func TestOutputVariantNewKeyWinsOverOldSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(`{"schema":2,"output_variant":"shinjitai","output_simplified":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil || s.OutputVariant != VariantShinjitai || s.OutputSimplified {
		t.Fatalf("新欄位應優先：%+v %v", s, err)
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), `"output_simplified"`) || !strings.Contains(string(b), `"output_variant": "shinjitai"`) {
		t.Fatalf("存檔只留新欄位：%s", b)
	}
}

func TestOutputVariantPersistsAndOldKeyStillWorks(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s := Default()
	for _, value := range []string{VariantSimplified, VariantShinjitai, VariantTraditional, VariantSimplified} {
		if err := s.Set("output-variant", value); err != nil {
			t.Fatal(err)
		}
		if s.OutputVariant != value || s.OutputSimplified != (value == VariantSimplified) {
			t.Fatalf("%s：投影不對 %+v", value, s)
		}
		if err := Save(path, s); err != nil {
			t.Fatal(err)
		}
		back, err := Load(path)
		if err != nil || back.OutputVariant != value || back.OutputSimplified != (value == VariantSimplified) || back.Selection != Neural {
			t.Fatalf("重新讀取：%+v %v", back, err)
		}
	}
	// 舊腳本與外殼選單仍用 output-simplified：只切換簡體，不動日文新字體。
	if err := s.Set("output-variant", VariantShinjitai); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("output-simplified", "on"); err != nil || s.OutputVariant != VariantSimplified {
		t.Fatalf("舊鍵 on：%+v %v", s, err)
	}
	if err := s.Set("output-simplified", "off"); err != nil || s.OutputVariant != VariantTraditional {
		t.Fatalf("舊鍵 off：%+v %v", s, err)
	}
	if err := os.WriteFile(path, []byte(`{"schema":1,"output_variant":"rainbow"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil || s.OutputVariant != VariantTraditional {
		t.Fatalf("讀不懂的變體退回繁體：%+v %v", s, err)
	}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"output_variant": "rainbow"`) {
		t.Fatalf("讀不懂的原值應保留：%s", b)
	}
	for _, it := range Default().Items() {
		if it.Key == "output-variant" {
			if it.Value != VariantTraditional || it.Default != VariantTraditional || it.Label != "輸出變體" || !strings.Contains(it.Description, "組字和候選") {
				t.Fatalf("設定程式的項目：%+v", it)
			}
			if labels := it.ValueLabels; labels[VariantSimplified] != "簡體中文" || labels[VariantShinjitai] != "日文新字體" {
				t.Fatalf("輸出變體的名稱：%v", labels)
			}
			return
		}
	}
	t.Fatal("設定程式看不到輸出變體")
}
