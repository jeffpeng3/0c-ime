package rerank

import (
	"sync"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// Stability 是一次ライブ変換更新的穩定度資料。StableSurface 是目前輸入
// 前綴下已經連續不變的文節；外殼可以只重畫不穩定的尾端，減少閃爍。
type Stability struct {
	Surface       string
	StableSurface string
	StableClauses int
	StableUnits   int
	Changed       bool
	Extended      bool
}

// Tracker 記錄同一段組字的相鄰轉換結果，供ライブ変換判斷穩定前綴。
// 每個 session 使用一個 Tracker；它不保存詞面到檔案，也不跨 session 共用。
type Tracker struct {
	mu      sync.Mutex
	key     string
	surface string
	clauses []lang.Clause
}

// NewTracker 建立空的穩定度追蹤器。
func NewTracker() *Tracker { return &Tracker{} }

// Reset 清除上一筆結果。使用者取消或送出後應呼叫。
func (t *Tracker) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.key, t.surface, t.clauses = "", "", nil
	t.mu.Unlock()
}

// Observe 比較新的轉換結果與上一次結果，回傳穩定前綴。
//
// 只有新讀音是舊讀音的延伸時才沿用上一次結果；使用者刪字或改動讀音會
// 讓穩定前綴歸零。文節的讀音鍵與表記都相同才算穩定。
func (t *Tracker) Observe(seq reading.Sequence, res lang.Result) Stability {
	if t == nil {
		return Stability{Surface: surfaceOf(res)}
	}
	key := reading.Key(seq)
	surface := surfaceOf(res)
	t.mu.Lock()
	defer t.mu.Unlock()
	extended := t.key != "" && (key == t.key || readingPrefix(t.key, key))
	if !extended {
		t.key, t.surface, t.clauses = "", "", nil
	}
	base := t.clauses
	if t.key != key && !readingPrefix(t.key, key) {
		base = nil
	}
	stable := leadingStable(base, res.Clauses)
	st := Stability{Surface: surface, Changed: surface != t.surface, Extended: extended}
	for _, c := range stable {
		st.StableSurface += c.Surface
		st.StableUnits += len(c.Reading)
	}
	st.StableClauses = len(stable)
	t.key = key
	t.surface = surface
	t.clauses = append([]lang.Clause(nil), res.Clauses...)
	return st
}

func surfaceOf(res lang.Result) string {
	var s string
	for _, c := range res.Clauses {
		s += c.Surface
	}
	return s
}

// readingPrefix 回報 a 是否為 b 的完整單位前綴。reading.Key 以單一空格
// 分隔，避免「は」被誤判成「はし」的前綴。
func readingPrefix(a, b string) bool {
	if a == "" {
		return true
	}
	if a == b {
		return true
	}
	return len(b) > len(a) && b[:len(a)] == a && b[len(a)] == ' '
}

func leadingStable(old, cur []lang.Clause) []lang.Clause {
	n := len(old)
	if len(cur) < n {
		n = len(cur)
	}
	var out []lang.Clause
	for i := 0; i < n; i++ {
		if old[i].Surface != cur[i].Surface || reading.Key(old[i].Reading) != reading.Key(cur[i].Reading) {
			break
		}
		out = append(out, cur[i])
	}
	return out
}
