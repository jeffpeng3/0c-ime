package personal

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

func langModel() *Model {
	m := NewModel(nil, nil)
	m.Now = func() int64 { return 1_700_000_000 }
	return m
}

func zhSel(surface string, previous lang.ID) LanguageSelection {
	return LanguageSelection{
		Surface:  surface,
		Reading:  reading.Sequence{reading.Unit("ㄐㄧㄥˇ")},
		Left:     "我",
		Language: lang.Zh,
		Previous: previous,
	}
}

func jaSel(surface string, previous lang.ID) LanguageSelection {
	return LanguageSelection{
		Surface:  surface,
		Reading:  reading.Sequence{reading.Unit("き"), reading.Unit("ょ"), reading.Unit("う")},
		Left:     "今日",
		Language: lang.Ja,
		Previous: previous,
	}
}

// 中文與日文各有命名空間：記了中文，日文的計數仍是零。
func TestLearnLanguageIsNamespaced(t *testing.T) {
	m := langModel()
	if got := m.LearnLanguage(zhSel("景", lang.Und)); got != lang.Zh {
		t.Fatalf("中文選字記到 %q", got)
	}
	if got := m.LearnLanguage(jaSel("京", lang.Zh)); got != lang.Ja {
		t.Fatalf("日文選字記到 %q", got)
	}
	if got := m.LanguageSelections(lang.Zh); got != 1 {
		t.Fatalf("中文選字數 = %d, want 1", got)
	}
	if got := m.LanguageSelections(lang.Ja); got != 1 {
		t.Fatalf("日文選字數 = %d, want 1", got)
	}
	if got := m.LanguageSurfaceCount(lang.Zh, "京"); got != 0 {
		t.Fatalf("中文命名空間不應有日文詞面：%d", got)
	}
	if got := m.LanguageSurfaceCount(lang.Ja, "景"); got != 0 {
		t.Fatalf("日文命名空間不應有中文詞面：%d", got)
	}
	if got := m.LanguageSurfaceCount(lang.Zh, "景"); got != 1 {
		t.Fatalf("中文詞面次數 = %d", got)
	}
	if got := m.LanguageSurfaceCount(lang.Ja, "京"); got != 1 {
		t.Fatalf("日文詞面次數 = %d", got)
	}
}

func TestInferLanguageUsesStrongScriptAndSegmentTag(t *testing.T) {
	m := langModel()
	cases := []struct {
		name     string
		surface  string
		hint     lang.ID
		previous lang.ID
		want     lang.ID
	}{
		{"明確候選標籤優先於漢字", "京", lang.Ja, lang.Zh, lang.Ja},
		{"漢字沿用前文語言", "京", lang.Und, lang.Ja, lang.Ja},
		{"注音是中文強訊號", "ㄓㄨˋ", lang.Und, lang.Ja, lang.Zh},
		{"かな是日文強訊號", "かな", lang.Zh, lang.Und, lang.Ja},
		{"拉丁字母視為英文", "hello", lang.Und, lang.Ja, lang.En},
		{"毫無線索時退回中文", "京", lang.Und, lang.Und, lang.Zh},
	}
	for _, c := range cases {
		if got := m.InferLanguage(c.surface, c.hint, c.previous); got != c.want {
			t.Errorf("%s：InferLanguage(%q,%q,%q) = %q, want %q", c.name, c.surface, c.hint, c.previous, got, c.want)
		}
	}
}

// 跨語言偏好可以由候選排序使用；同語言的詞面計數不會跨到別的語言。
func TestCrossLanguagePreferenceRanksCandidates(t *testing.T) {
	m := langModel()
	for range 5 {
		m.LearnLanguage(jaSel("京", lang.Zh))
	}
	for range 5 {
		m.LearnLanguage(zhSel("景", lang.Zh))
	}
	zhScore := m.LanguageCandidateScore(LanguageCandidate{Surface: "景", Language: lang.Zh}, lang.Zh)
	jaScore := m.LanguageCandidateScore(LanguageCandidate{Surface: "京", Language: lang.Ja}, lang.Zh)
	if jaScore <= zhScore {
		t.Fatalf("中文後接日文的偏好應較高：ja %g zh %g", jaScore, zhScore)
	}
	ranked := m.RankLanguageCandidates([]LanguageCandidate{
		{Surface: "景", Language: lang.Zh},
		{Surface: "京", Language: lang.Ja},
	}, lang.Zh)
	if ranked[0].Language != lang.Ja {
		t.Fatalf("跨語言偏好未影響排序：%#v", ranked)
	}
	// 沒有前文證據時，詞面計數只在同一語言內生效。
	base := m.LanguageCandidateScore(LanguageCandidate{Surface: "不存在", Language: lang.Ja}, lang.Und)
	if got := m.LanguageCandidateScore(LanguageCandidate{Surface: "景", Language: lang.Ja}, lang.Und); got != base {
		t.Fatalf("不應把中文詞面算進日文：%g != %g", got, base)
	}
}

func TestLanguageUndoForgetReset(t *testing.T) {
	m := langModel()
	m.LearnLanguage(zhSel("景", lang.Und))
	if !m.UndoLanguage() {
		t.Fatal("UndoLanguage = false")
	}
	if m.LanguageSelections(lang.Zh) != 0 || m.LanguageSurfaceCount(lang.Zh, "景") != 0 {
		t.Fatalf("反悔後仍有紀錄：%d %d", m.LanguageSelections(lang.Zh), m.LanguageSurfaceCount(lang.Zh, "景"))
	}
	if m.UndoLanguage() {
		t.Fatal("沒有紀錄時 UndoLanguage 應回傳 false")
	}

	m.LearnLanguage(zhSel("景", lang.Und))
	m.LearnLanguage(jaSel("京", lang.Zh))
	m.LearnLanguage(jaSel("京", lang.Zh))
	if !m.ForgetLanguage(lang.Ja, "京") {
		t.Fatal("ForgetLanguage = false")
	}
	if m.LanguageSurfaceCount(lang.Ja, "京") != 0 || m.LanguageSelections(lang.Ja) != 0 {
		t.Fatalf("忘記後仍有日文紀錄：%d %d", m.LanguageSelections(lang.Ja), m.LanguageSurfaceCount(lang.Ja, "京"))
	}
	if m.LanguageSurfaceCount(lang.Zh, "景") != 1 {
		t.Fatal("忘記日文不應影響中文")
	}

	if !m.ResetLanguage(lang.Ja) {
		t.Fatal("ResetLanguage 應回報清掉跨語言紀錄")
	}
	if m.LanguageSurfaceCount(lang.Zh, "景") != 1 {
		t.Fatal("重置日文不應影響中文")
	}
	m.LearnLanguage(jaSel("京", lang.Zh))
	if n := m.ResetAllLanguages(); n != 2 {
		t.Fatalf("ResetAllLanguages = %d, want 2", n)
	}
	if m.LanguageSelections(lang.Zh) != 0 || m.LanguageSelections(lang.Ja) != 0 || len(m.Store.CrossLanguage.Primary) != 0 {
		t.Fatalf("重置全部後仍有紀錄：%+v", m.Store.CrossLanguage)
	}
}

func TestLanguageLearningStoreRoundTrip(t *testing.T) {
	m := langModel()
	m.LearnLanguage(zhSel("景", lang.Und))
	m.LearnLanguage(jaSel("京", lang.Zh))
	var buf bytes.Buffer
	if err := m.Store.Save(&buf); err != nil {
		t.Fatal(err)
	}
	back, err := LoadStore(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Store, back) {
		t.Fatalf("分語言學習存檔來回不一致\n%+v\n%+v", m.Store, back)
	}
}

func TestLanguageStoreCloneDoesNotShareMaps(t *testing.T) {
	s := NewStore()
	s.Languages["zh"] = &LanguageNamespace{Surfaces: map[string]LanguageEvidence{"景": {Count: 1}}}
	s.CrossLanguage.Primary = map[string]int{"zh": 1}
	s.CrossLanguage.Transitions = map[string]int{"ja\tzh": 1}
	c := s.Clone()
	c.Languages["zh"].Surfaces["景"] = LanguageEvidence{Count: 9}
	c.Languages["ja"] = &LanguageNamespace{}
	c.CrossLanguage.Primary["zh"] = 9
	c.CrossLanguage.Transitions["ja\tzh"] = 9
	if s.Languages["zh"].Surfaces["景"].Count != 1 || s.Languages["ja"] != nil ||
		s.CrossLanguage.Primary["zh"] != 1 || s.CrossLanguage.Transitions["ja\tzh"] != 1 {
		t.Fatal("修改複本影響到原本的分語言學習")
	}
}

func TestLanguageFieldsOldFormatLoad(t *testing.T) {
	s, err := LoadStore(bytes.NewBufferString(`{"Format":7,"Uni":{"景":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Languages == nil || s.CrossLanguage.Primary == nil || s.CrossLanguage.Transitions == nil {
		t.Fatalf("舊檔讀入後應初始化分語言欄位：%+v", s)
	}
	if s.Uni["景"] != 2 {
		t.Fatal("舊資料應保留")
	}
}
