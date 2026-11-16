// Package live 實作日文逐字変換（ライブ変換）的控制器。
//
// 這個套件只負責「什麼時候轉換、轉換哪一段、前次結果怎麼保留」，實際的
// かな漢字変換仍由 core/ja/decode 或任何 lang.Converter 提供。因此它不改
// 轉換演算法，也不碰 session、外殼、檔案或網路。
//
// 設計重點：
//   - 使用 debounce：每次かな增加只重設期限，安靜下來才轉換，避免每個按鍵
//     都重算造成閃爍。
//   - 只轉換「尚未確定的尾端」。已經顯示過的文節會鎖住，後續転換從鎖住
//     位置之後開始，已確定的前段不可能因尾端變化而跳動。
//   - 前次結果是下一次的 draft。防抖期間與逾時退回路徑都回傳前次結果，
//     外殼不需要先把畫面退回かな。
//   - 文節邊界一次最多變動 Config.MaxBoundaryChanges 個；超過就先保留前次
//     結果，最多 HoldLimit 次，之後仍會接受新結果，不會永久卡住。
//   - 預設關閉。關閉時 Input 不做任何事，外殼照一般変換流程處理。
package live

import (
	"errors"
	"time"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// ErrSpan 表示候選範圍超出目前的讀音。
var ErrSpan = errors.New("live: span outside reading")

// Config 是ライブ変換的行為開關。
//
// 零值等於關閉。要開啟時從 DefaultConfig 開始改，或至少把 Enabled 設為 true。
type Config struct {
	// Enabled 是總開關，預設 false；關閉時行為與一般変換完全相同。
	Enabled bool
	// Debounce 是最後一次かな輸入後要等多久才轉換。0 表示立即轉換（測試用）。
	Debounce time.Duration
	// Budget 是單次轉換的時間預算；超過就退回前次結果，不鎖定新結果。
	// 0 表示不檢查。
	Budget time.Duration
	// MaxBoundaryChanges 是單次轉換允許改變的文節邊界數，預設 1。
	// 負值表示不限制。
	MaxBoundaryChanges int
	// HoldLimit 是邊界變動過大時最多保留前次結果幾次，預設 2。
	// 0 表示不保留，直接接受新結果。
	HoldLimit int
	// MinUnits 是開始ライブ変換所需的最少讀音單位，預設 1。
	MinUnits int
	// Clock 是時間來源；nil 使用 time.Now。測試可注入虛擬時鐘。
	Clock func() time.Time
}

// DefaultConfig 回傳建議值。Enabled 保持 false，符合「預設關閉」。
func DefaultConfig() Config {
	return Config{
		Enabled:            false,
		Debounce:           120 * time.Millisecond,
		Budget:             100 * time.Millisecond,
		MaxBoundaryChanges: 1,
		HoldLimit:          2,
		MinUnits:           1,
	}
}

// Plan 是一次 Input 或 Tick 之後，控制器給外殼的決定。
type Plan struct {
	// Convert 表示 Result 是這次新轉換的結果，可以直接顯示。
	Convert bool
	// Preview 表示 Result 是前次結果（draft），本次沒有新結果。
	Preview bool
	// Pending 表示 debounce 還在等；Deadline 到了再呼叫 Tick。
	Pending bool
	// Result 是目前可顯示的轉換結果。沒有結果時為零值。
	Result lang.Result
	// Deadline 是 debounce 到期時間，只在 Pending 時有意義。
	Deadline time.Time
	// BoundaryChanges 是本次候選結果相對前次尾端的文節邊界變動數。
	BoundaryChanges int
	// TimedOut 表示這次轉換超過 Config.Budget，已退回前次結果。
	TimedOut bool
}

// Stats 是控制器累計的執行統計，供效能測試與外殼診斷使用。
type Stats struct {
	Conversions         int
	Holds               int
	Timeouts            int
	LastDuration        time.Duration
	LastBoundaryChanges int
}

// Controller 是ライブ変換的狀態機。零值不可用，請用 New 建立。
//
// Controller 不是併發安全的；語言／session 本身也是單執行緒處理按鍵，
// 呼叫端不需要額外鎖。
type Controller struct {
	cfg  Config
	conv lang.Converter

	seq reading.Sequence
	ctx lang.Context

	// cut 是已鎖定的讀音前綴長度；locked 是對應的文節，lockedSeq 是比對用
	// 的讀音快照。tail 是最近一次轉換中尚未鎖定的文節。
	cut       int
	locked    []lang.Clause
	lockedSeq reading.Sequence
	tail      []lang.Clause

	draft    lang.Result
	hasDraft bool

	pending bool
	due     time.Time
	held    int

	stats Stats
}

// New 建立控制器。conv 為 nil 或 Config.Enabled 為 false 時控制器維持停用，
// Input 與 Tick 都不會呼叫 conv。
func New(conv lang.Converter, cfg Config) *Controller {
	if cfg.MaxBoundaryChanges == 0 {
		cfg.MaxBoundaryChanges = 1
	}
	if cfg.HoldLimit < 0 {
		cfg.HoldLimit = 0
	}
	if cfg.MinUnits < 0 {
		cfg.MinUnits = 0
	}
	return &Controller{cfg: cfg, conv: conv}
}

// Enabled 回報ライブ変換是否開啟。
func (c *Controller) Enabled() bool {
	return c != nil && c.cfg.Enabled && c.conv != nil
}

// SetConfig 更新設定。任何變更都會清掉暫態，避免用舊設定產生的前綴繼續
// 被當成已確定內容；外殼在設定變更後重新送出目前讀音即可。
func (c *Controller) SetConfig(cfg Config) {
	if c == nil {
		return
	}
	if cfg.MaxBoundaryChanges == 0 {
		cfg.MaxBoundaryChanges = 1
	}
	c.cfg = cfg
	c.Reset()
}

// SetEnabled 只切換總開關。
func (c *Controller) SetEnabled(on bool) {
	if c == nil {
		return
	}
	cfg := c.cfg
	cfg.Enabled = on
	c.SetConfig(cfg)
}

// Stats 回傳累計統計。
func (c *Controller) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	return c.stats
}

// Draft 回傳前次可顯示的結果。
func (c *Controller) Draft() (lang.Result, bool) {
	if c == nil || !c.hasDraft {
		return lang.Result{}, false
	}
	return c.draft, true
}

// Pending 回報 debounce 是否還在等待。
func (c *Controller) Pending() bool { return c != nil && c.pending }

// Input 收到一次かな增加（或任何讀音更新）。
//
// 每次呼叫都會重新計算 debounce 期限；Config.Debounce 為 0 時立刻轉換。
// 若新的讀音不再是已鎖定前綴的延伸（例如倒退鍵），會先清掉舊的鎖定與
// draft，再從頭轉換，避免把不同讀音的前綴接在一起。
func (c *Controller) Input(seq reading.Sequence, ctx lang.Context) Plan {
	if !c.Enabled() {
		return Plan{}
	}
	if !c.prefixIntact(seq) {
		c.Reset()
	}
	c.seq = append(c.seq[:0], seq...)
	c.ctx = ctx

	if len(seq) < c.cfg.MinUnits {
		c.pending = false
		c.due = time.Time{}
		return c.previewPlan()
	}
	if c.cfg.Debounce > 0 {
		c.pending = true
		c.due = c.now().Add(c.cfg.Debounce)
		p := c.previewPlan()
		p.Pending = true
		p.Deadline = c.due
		return p
	}
	c.pending = false
	c.due = time.Time{}
	return c.convert()
}

// Tick 在 debounce 到期時執行轉換。回傳的第二個值表示這次是否真的做了
// 轉換（或逾時退回）；期限未到、沒有待處理輸入或功能關閉時回傳 false。
func (c *Controller) Tick() (Plan, bool) {
	if !c.Enabled() || !c.pending {
		return Plan{}, false
	}
	if c.now().Before(c.due) {
		return Plan{}, false
	}
	c.pending = false
	c.due = time.Time{}
	return c.convert(), true
}

// CancelPending 取消尚未執行的 debounce，保留目前已顯示的結果。
// 回傳是否有東西被取消。
func (c *Controller) CancelPending() bool {
	if c == nil || !c.pending {
		return false
	}
	c.pending = false
	c.due = time.Time{}
	return true
}

// Reset 清掉所有組字、鎖定與 draft 狀態，但保留累計統計。
func (c *Controller) Reset() {
	if c == nil {
		return
	}
	c.resetState()
	c.seq = nil
	c.ctx = lang.Context{}
}

// Candidates 回傳目前讀音指定範圍的候選。
//
// 候選仍由原本的 converter 產生，只是把已鎖定的前綴放進 Context.Left，
// 讓尾端候選有正確的左文脈。功能關閉時回傳 nil，外殼走一般変換。
func (c *Controller) Candidates(span lang.Span) ([]lang.Candidate, error) {
	if !c.Enabled() {
		return nil, nil
	}
	if span.Start < 0 || span.Length < 0 || span.End() > len(c.seq) {
		return nil, ErrSpan
	}
	return c.conv.Candidates(c.seq, span, c.convertContext())
}

func (c *Controller) resetState() {
	c.cut = 0
	c.locked = nil
	c.lockedSeq = nil
	c.tail = nil
	c.draft = lang.Result{}
	c.hasDraft = false
	c.pending = false
	c.due = time.Time{}
	c.held = 0
}

func (c *Controller) now() time.Time {
	if c.cfg.Clock != nil {
		return c.cfg.Clock()
	}
	return time.Now()
}

func (c *Controller) previewPlan() Plan {
	p := Plan{}
	if c.hasDraft {
		p.Preview = true
		p.Result = c.draft
	}
	return p
}

func (c *Controller) prefixIntact(seq reading.Sequence) bool {
	if c.cut == 0 {
		return true
	}
	if len(seq) <= c.cut || len(c.lockedSeq) < c.cut {
		return false
	}
	for i := 0; i < c.cut; i++ {
		if seq[i] != c.lockedSeq[i] {
			return false
		}
	}
	return true
}

func (c *Controller) convertContext() lang.Context {
	ctx := c.ctx
	ctx.Left = c.lockedText() + ctx.Left
	return ctx
}

func (c *Controller) lockedText() string {
	var b []rune
	for _, cl := range c.locked {
		b = append(b, []rune(cl.Surface)...)
	}
	return string(b)
}

func (c *Controller) convert() Plan {
	if c.cut > len(c.seq) {
		c.Reset()
		return Plan{}
	}
	suffix := c.seq[c.cut:]
	if len(suffix) == 0 {
		return c.previewPlan()
	}
	start := c.now()
	res, err := c.conv.Convert(suffix, c.convertContext())
	elapsed := c.now().Sub(start)
	c.stats.Conversions++
	c.stats.LastDuration = elapsed

	timedOut := c.cfg.Budget > 0 && elapsed > c.cfg.Budget
	if timedOut {
		c.stats.Timeouts++
	}
	if err != nil || len(res.Clauses) == 0 || timedOut || clausesUnits(res.Clauses) != len(suffix) {
		p := c.previewPlan()
		p.TimedOut = timedOut
		return p
	}

	changes := boundaryChanges(c.tail, res.Clauses)
	c.stats.LastBoundaryChanges = changes
	if c.cfg.MaxBoundaryChanges > 0 && c.hasDraft &&
		changes > c.cfg.MaxBoundaryChanges && c.held < c.cfg.HoldLimit {
		c.held++
		c.stats.Holds++
		p := c.previewPlan()
		p.BoundaryChanges = changes
		return p
	}

	c.held = 0
	c.tail = cloneClauses(res.Clauses)
	c.advanceLock()
	c.draft = lang.Result{Clauses: c.allClauses(), Score: res.Score}
	c.hasDraft = true
	return Plan{Convert: true, Result: c.draft, BoundaryChanges: changes}
}

// advanceLock 把尾端除了最後一個文節以外的部分鎖住。最後一個文節仍可能
// 隨下一個かな改變，所以留在 tail；已鎖住的文節往後不會再被轉換。
func (c *Controller) advanceLock() {
	if len(c.tail) <= 1 {
		return
	}
	n := len(c.tail) - 1
	units := clausesUnits(c.tail[:n])
	if units <= 0 || c.cut+units > len(c.seq) {
		return
	}
	c.locked = append(c.locked, cloneClauses(c.tail[:n])...)
	c.lockedSeq = append(c.lockedSeq, c.seq[c.cut:c.cut+units]...)
	c.cut += units
	c.tail = cloneClauses(c.tail[n:])
}

func (c *Controller) allClauses() []lang.Clause {
	out := make([]lang.Clause, 0, len(c.locked)+len(c.tail))
	out = append(out, cloneClauses(c.locked)...)
	out = append(out, cloneClauses(c.tail)...)
	return out
}

// boundaryChanges 比較舊尾端與新結果在重疊讀音內的文節邊界差異。
// 超過重疊長度的新邊界不算變動，因為那只是尾端變長。
func boundaryChanges(old, next []lang.Clause) int {
	n := min(clausesUnits(old), clausesUnits(next))
	if n <= 0 {
		return 0
	}
	oldSet := boundaries(old, n)
	newSet := boundaries(next, n)
	changes := 0
	for p := range oldSet {
		if !newSet[p] {
			changes++
		}
	}
	for p := range newSet {
		if !oldSet[p] {
			changes++
		}
	}
	return changes
}

func boundaries(clauses []lang.Clause, limit int) map[int]bool {
	out := map[int]bool{}
	pos := 0
	for i, cl := range clauses {
		pos += len(clauseReading(cl))
		if i == len(clauses)-1 || pos >= limit {
			break
		}
		if pos > 0 && pos < limit {
			out[pos] = true
		}
	}
	return out
}

func clausesUnits(clauses []lang.Clause) int {
	n := 0
	for _, cl := range clauses {
		n += len(clauseReading(cl))
	}
	return n
}

func clauseReading(c lang.Clause) reading.Sequence {
	if len(c.Reading) > 0 {
		return c.Reading
	}
	var out reading.Sequence
	for _, seg := range c.Segments {
		out = append(out, seg.Reading...)
	}
	return out
}

func cloneClauses(in []lang.Clause) []lang.Clause {
	out := make([]lang.Clause, len(in))
	for i, c := range in {
		out[i] = c
		out[i].Reading = append(reading.Sequence(nil), c.Reading...)
		out[i].Segments = append([]lang.CandidateSegment(nil), c.Segments...)
		for j := range out[i].Segments {
			out[i].Segments[j].Reading = append(reading.Sequence(nil), c.Segments[j].Reading...)
		}
	}
	return out
}
