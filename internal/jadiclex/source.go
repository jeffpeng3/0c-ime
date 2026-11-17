// Package jadiclex 把 0c-jadic 詞典 pack 轉成 core/ja/lexicon.Source。
//
// pack 本身是詞典建置流程的產物，產品執行期仍以 data/ja 編譯出的詞典為主。
// 這個轉接層讓沒有預先編譯資料的環境（例如參考 pack 測試、離線工具）可以直接
// 用同一份 pack 產生かな→漢字候選，不必先寫出 words.tsv 再載入。
//
// 詞條轉換規則與 internal/jadic 的匯入契約一致：讀音逐字轉成 reading.Sequence，
// 詞成本是 pack 的 signed i16 乘上 0.001，詞性走 jaimport.MapPOS，未知詞性落回
// other。走訪順序固定，因此同一份 pack 在每個行程都產生相同查詢結果。
package jadiclex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jadic"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

// DefaultCostScale 與 jadic.DefaultCostScale 相同：pack 成本乘上 0.001。
const DefaultCostScale = jadic.DefaultCostScale

// Source 是讀音索引。同一個讀音的所有詞條共用一份 reading.Sequence。
type Source struct {
	byReading map[string][]lexicon.Entry
	keys      []string // 排序後的讀音鍵，供 Prefixed／HasPrefix 穩定走訪
	longest   int
	entries   int
}

var _ lexicon.Source = (*Source)(nil)

// Load 讀取 pack 檔並建立讀音索引。
func Load(path string) (*Source, error) {
	p, err := jadic.Load(path)
	if err != nil {
		return nil, err
	}
	return New(p)
}

// New 從已解析的 pack 建立讀音索引。詞條依 pack 順序加入，同一讀音內維持
// 原始順序；查詢端只依分數與詞面排序，因此結果可重現。
func New(p *jadic.Pack) (*Source, error) {
	if p == nil {
		return nil, fmt.Errorf("jadiclex: 沒有 pack")
	}
	s := &Source{byReading: make(map[string][]lexicon.Entry, p.SurfaceCount())}
	err := p.ForEach(func(e jadic.Entry) error {
		seq, err := jaimport.NormalizeReading(e.Reading)
		if err != nil {
			return fmt.Errorf("jadiclex: %q 的讀音 %q：%w", e.Surface, e.Reading, err)
		}
		if len(seq) == 0 {
			return fmt.Errorf("jadiclex: %q 的讀音是空字串", e.Surface)
		}
		key := reading.Key(seq)
		entries := s.byReading[key]
		if len(entries) == 0 {
			seq = append(reading.Sequence(nil), seq...)
			s.keys = append(s.keys, key)
		}
		entries = append(entries, lexicon.Entry{
			Surface: e.Surface,
			Reading: seq,
			Cost:    float64(e.Cost) * DefaultCostScale,
			POS:     lexicon.POS(jaimport.MapPOS(e.POS)),
		})
		s.byReading[key] = entries
		s.entries++
		if len(seq) > s.longest {
			s.longest = len(seq)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(s.keys)
	return s, nil
}

// Entries 回傳索引內的詞條數。
func (s *Source) Entries() int {
	if s == nil {
		return 0
	}
	return s.entries
}

// LongestWord 回傳最長的讀音單位數。
func (s *Source) LongestWord() int {
	if s == nil {
		return 0
	}
	return s.longest
}

// Exact 回傳讀音恰為 seq 的詞條。
func (s *Source) Exact(seq reading.Sequence) []lexicon.Entry {
	if s == nil {
		return nil
	}
	return s.byReading[reading.Key(seq)]
}

// Prefixed 回傳讀音以 seq 開頭的詞條，依讀音鍵排序。
func (s *Source) Prefixed(seq reading.Sequence) []lexicon.Entry {
	if s == nil {
		return nil
	}
	prefix := reading.Key(seq)
	i := sort.SearchStrings(s.keys, prefix)
	var out []lexicon.Entry
	for ; i < len(s.keys) && strings.HasPrefix(s.keys[i], prefix); i++ {
		out = append(out, s.byReading[s.keys[i]]...)
	}
	return out
}

// HasPrefix 回報是否有讀音以 seq 開頭。
func (s *Source) HasPrefix(seq reading.Sequence) bool {
	if s == nil {
		return false
	}
	prefix := reading.Key(seq)
	i := sort.SearchStrings(s.keys, prefix)
	return i < len(s.keys) && strings.HasPrefix(s.keys[i], prefix)
}

// Matches 走訪 seq[start:] 的每個讀音前綴，回報結束位置與詞條。
func (s *Source) Matches(seq reading.Sequence, start int, fn func(end int, e lexicon.Entry)) {
	if s == nil || start < 0 || start > len(seq) {
		return
	}
	var b strings.Builder
	for end := start; end < len(seq) && end-start < s.longest; end++ {
		if end > start {
			b.WriteString(reading.KeySeparator)
		}
		b.WriteString(string(seq[end]))
		for _, e := range s.byReading[b.String()] {
			fn(end+1, e)
		}
	}
}
