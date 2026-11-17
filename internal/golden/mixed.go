// Package golden 也凍結混合語言 fixtures 的形狀（P2-03）。
//
// 注音 baseline（zh/）驅動正式引擎；混合 fixtures（mixed/）則只描述期望結果：
// 同一串輸入應該切成哪些帶語言標籤的片段、每段用哪個輸出變體、最後組出什麼
// 文字。P2-10 的分段器與 P2-12 的 lattice 還沒實作，所以這裡不呼叫產品程式，
// 只驗證 fixture 本身符合 P2-02 的契約：span 依序覆蓋輸入、語言標籤與輸出
// 變體合法、片段詞面依序串起等於預期輸出、跨片段長詞真的跨越了 piece 邊界。
//
// 這樣 P2-12／P2-13／P2-14 實作完成後，直接拿同一份 fixtures 當驗收資料，
// 不必回頭修改期望值；也保證現在的 fixtures 沒有打錯 span 或變體。
package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/segment"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// MixedFormat 是 testdata/golden/mixed/cases.json 的格式版本；改變欄位語意時加一。
const MixedFormat = 1

// MixedFile 是 testdata/golden/mixed/cases.json。
type MixedFile struct {
	Format int         `json:"format"`
	Cases  []MixedCase `json:"cases"`
}

// MixedCase 是一個混合語言 fixture。
//
// Input 是使用者依序送出的整串字元，Span 以 rune 為單位對應 Input。Primary 只是
// 偏好，不是模式開關：同一個 case 的 Segments 可以同時含中文、日文與英文片段。
type MixedCase struct {
	Name     string         `json:"name"`
	Note     string         `json:"note,omitempty"`
	Primary  lang.ID        `json:"primary"`
	Input    string         `json:"input"`
	Personal []string       `json:"personal,omitempty"` // 已在個人詞彙的詞面
	Pieces   []MixedPiece   `json:"pieces,omitempty"`   // 合併前的輸入片段，用來證明長詞跨片段
	Segments []MixedSegment `json:"segments"`
	Output   string         `json:"output"`
}

// MixedPiece 是長詞合併前的輸入片段。它只記錄使用者實際分幾段打完，以及每段的
// 書寫系統與語言；合併後的結果寫在 Segments。
type MixedPiece struct {
	Span     segment.Span     `json:"span"`
	Script   segment.Script   `json:"script"`
	Language lang.LanguageTag `json:"language"`
	Note     string           `json:"note,omitempty"`
}

// MixedSegment 是期望的輸出片段：P2-02 的 segment.Segment 加上候選來源，
// 以及同一片段的其他語言候選。JSON 形狀與 lattice.Candidate 相同。
type MixedSegment struct {
	segment.Segment
	Source lattice.Source     `json:"source,omitempty"`
	Also   []MixedAlternative `json:"also,omitempty"`
}

// MixedAlternative 是同一片段沒有被選中、但必須留在候選清單裡的候選。
// 例如拉丁片段同時有日文與英文候選；主要語言只影響排序，不得刪掉其他候選。
type MixedAlternative struct {
	segment.Segment
	Source lattice.Source `json:"source,omitempty"`
}

// Candidate 把期望片段轉成契約候選，方便比對鍵與來源。
func (s MixedSegment) Candidate() lattice.Candidate {
	return lattice.Candidate{Segment: s.Segment, Source: s.Source}
}

// Candidate 把替代候選轉成契約候選。
func (a MixedAlternative) Candidate() lattice.Candidate {
	return lattice.Candidate{Segment: a.Segment, Source: a.Source}
}

// MixedSuite 是載入後的混合 fixtures。
type MixedSuite struct {
	dir   string
	cases []MixedCase
}

// LoadMixed 載入 dir/cases.json 並驗證所有 fixture 的結構。
//
// 驗證成功代表：span 沒有漏字或重疊、語言標籤與輸出變體合法且互相一致、
// 片段詞面串起來就是預期輸出、pieces 真的有片段跨界合併。這不是分段器或
// 輸出組裝器的實作，只是把期望值鎖在契約上。
func LoadMixed(dir string) (*MixedSuite, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		return nil, err
	}
	var f MixedFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("mixed/cases.json：%w", err)
	}
	if f.Format != MixedFormat {
		return nil, fmt.Errorf("mixed/cases.json：格式 %d，本版支援 %d", f.Format, MixedFormat)
	}
	if len(f.Cases) == 0 {
		return nil, fmt.Errorf("mixed/cases.json：沒有案例")
	}
	seen := map[string]bool{}
	for i := range f.Cases {
		c := &f.Cases[i]
		if c.Name == "" {
			return nil, fmt.Errorf("第 %d 個混合案例沒有名稱", i+1)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("混合案例名稱重複：%s", c.Name)
		}
		seen[c.Name] = true
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%s：%w", c.Name, err)
		}
	}
	return &MixedSuite{dir: dir, cases: f.Cases}, nil
}

// Cases 回傳載入的混合案例（複本）。
func (s *MixedSuite) Cases() []MixedCase { return append([]MixedCase(nil), s.cases...) }

// Case 依名稱找一個案例。
func (s *MixedSuite) Case(name string) (MixedCase, bool) {
	for _, c := range s.cases {
		if c.Name == name {
			return c, true
		}
	}
	return MixedCase{}, false
}

// Validate 檢查單一 fixture。
func (c MixedCase) Validate() error {
	if c.Primary == lang.Und || lang.DefaultVariant(c.Primary) == "" {
		return fmt.Errorf("primary %q 沒有預設輸出變體", c.Primary)
	}
	runes := []rune(c.Input)
	if len(runes) == 0 {
		return fmt.Errorf("input 不能是空的")
	}
	if len(c.Segments) == 0 {
		return fmt.Errorf("沒有 segments")
	}
	spans := make([]segment.Span, len(c.Segments))
	for i, s := range c.Segments {
		spans[i] = s.Span
	}
	if err := tileSpans("segments", spans, len(runes)); err != nil {
		return err
	}
	personal := map[string]bool{}
	for _, w := range c.Personal {
		if w == "" {
			return fmt.Errorf("personal 不能有空字串")
		}
		personal[w] = true
	}
	var out strings.Builder
	for i, s := range c.Segments {
		if err := s.validateSegment(runes); err != nil {
			return fmt.Errorf("第 %d 個片段：%w", i+1, err)
		}
		if s.Source == lattice.SourcePersonal && !personal[s.Surface] {
			return fmt.Errorf("第 %d 個片段來源是 personal，但 %q 不在 personal 清單", i+1, s.Surface)
		}
		out.WriteString(s.Surface)
	}
	for w := range personal {
		found := false
		for _, s := range c.Segments {
			if s.Surface == w && s.Source == lattice.SourcePersonal {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("personal 詞 %q 沒有對應的 personal 片段", w)
		}
	}
	if err := c.validatePieces(runes); err != nil {
		return err
	}
	if out.String() != c.Output {
		return fmt.Errorf("output 應是 %q，但片段詞面依序串起是 %q", c.Output, out.String())
	}
	return nil
}

func (s MixedSegment) validateSegment(input []rune) error {
	c := s.Candidate()
	if err := validateCandidate(c); err != nil {
		return err
	}
	if err := checkScript(c.Span, c.Script, input); err != nil {
		return err
	}
	chosen := c.Key()
	for j, alt := range s.Also {
		ac := alt.Candidate()
		if err := validateCandidate(ac); err != nil {
			return fmt.Errorf("also 第 %d 項：%w", j+1, err)
		}
		if ac.Span != c.Span || ac.Script != c.Script {
			return fmt.Errorf("also 第 %d 項必須沿用同一個 span 與 script", j+1)
		}
		if ac.Key() == chosen {
			return fmt.Errorf("also 第 %d 項與選中的候選完全相同", j+1)
		}
	}
	return nil
}

func validateCandidate(c lattice.Candidate) error {
	if !c.Valid() {
		return fmt.Errorf("欄位不合法（span、reading、language 或 variant）：%+v", c.Segment)
	}
	if c.Language.IsZero() {
		return fmt.Errorf("language 必須明確標記")
	}
	if c.Surface == "" {
		return fmt.Errorf("surface 不能是空的")
	}
	want := lang.DefaultVariant(c.Language.Language)
	if want == "" {
		return fmt.Errorf("語言 %q 沒有預設輸出變體", c.Language.Language)
	}
	if c.Variant != want {
		return fmt.Errorf("variant %q 應為語言 %q 的預設 %q", c.Variant, c.Language.Language, want)
	}
	if !knownSources[c.Source] {
		return fmt.Errorf("未知的候選來源 %q", c.Source)
	}
	return nil
}

var knownSources = map[lattice.Source]bool{
	lattice.SourceDictionary: true,
	lattice.SourcePersonal:   true,
	lattice.SourceFuzzy:      true,
	lattice.SourcePrediction: true,
	lattice.SourceModel:      true,
	lattice.SourceLiteral:    true,
	lattice.SourceSymbol:     true,
}

func (c MixedCase) validatePieces(input []rune) error {
	if len(c.Pieces) == 0 {
		return nil
	}
	spans := make([]segment.Span, len(c.Pieces))
	for i, p := range c.Pieces {
		if p.Script == segment.ScriptUnknown {
			return fmt.Errorf("第 %d 個 piece 沒有 script", i+1)
		}
		if !p.Language.Valid() {
			return fmt.Errorf("第 %d 個 piece 的 language %q 不合法", i+1, p.Language)
		}
		if err := checkScript(p.Span, p.Script, input); err != nil {
			return fmt.Errorf("第 %d 個 piece：%w", i+1, err)
		}
		spans[i] = p.Span
	}
	if err := tileSpans("pieces", spans, len(input)); err != nil {
		return err
	}
	for i, p := range c.Pieces {
		covered := false
		for _, s := range c.Segments {
			if p.Span.Start >= s.Span.Start && p.Span.End <= s.Span.End {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("第 %d 個 piece 沒有被任何 segment 覆蓋", i+1)
		}
	}
	for _, s := range c.Segments {
		for i := 0; i < len(c.Pieces)-1; i++ {
			if end := c.Pieces[i].Span.End; end > s.Span.Start && end < s.Span.End {
				return nil
			}
		}
	}
	return fmt.Errorf("pieces 的邊界都落在 segment 邊界上，沒有跨片段的長詞合併")
}

func tileSpans(name string, spans []segment.Span, n int) error {
	next := 0
	for i, sp := range spans {
		if !sp.Valid() {
			return fmt.Errorf("%s 第 %d 段 span %v 不合法", name, i+1, sp)
		}
		if sp.Start != next {
			return fmt.Errorf("%s 第 %d 段從 rune %d 開始，前一段結束在 %d", name, i+1, sp.Start, next)
		}
		next = sp.End
	}
	if next != n {
		return fmt.Errorf("%s 結束在 rune %d，input 長度是 %d", name, next, n)
	}
	return nil
}

// checkScript 確認 span 內的輸入字元真的屬於宣告的書寫系統。它只做 range
// 檢查，不是分段規則；範圍刻意寫死在這裡，避免 Go 版 Unicode 表升級改變結果。
func checkScript(sp segment.Span, script segment.Script, input []rune) error {
	if sp.End > len(input) {
		return fmt.Errorf("span %v 超出 input 的 %d 個 rune", sp, len(input))
	}
	for i := sp.Start; i < sp.End; i++ {
		r := input[i]
		var ok bool
		switch script {
		case segment.ScriptZhuyin:
			ok = zhuyin.ClassOf(r) != zhuyin.None
		case segment.ScriptKana:
			ok = isKanaRune(r)
		case segment.ScriptLatin:
			ok = (r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r))) || r == ' '
		case segment.ScriptDigit:
			ok = unicode.IsDigit(r)
		case segment.ScriptHan:
			ok = zhuyin.IsHan(r)
		case segment.ScriptSymbol:
			ok = !unicode.IsLetter(r) && !unicode.IsDigit(r) && zhuyin.ClassOf(r) == zhuyin.None && !isKanaRune(r)
		case segment.ScriptMixed:
			ok = true
		default:
			return fmt.Errorf("span %v 的 script %q 無法檢查", sp, script)
		}
		if !ok {
			return fmt.Errorf("span %v 的第 %d 個 rune %q 不是宣告的 %q", sp, i-sp.Start+1, r, script)
		}
	}
	return nil
}

func isKanaRune(r rune) bool {
	return unicode.In(r, unicode.Hiragana, unicode.Katakana) || r == 'ー'
}
