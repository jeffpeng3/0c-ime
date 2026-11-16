package live_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/ja/live"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

type fakeConverter struct {
	clock    *fakeClock
	cost     time.Duration
	err      error
	split    func(reading.Sequence) []lang.Clause
	calls    int
	contexts []lang.Context
	spans    []lang.Span
}

func (f *fakeConverter) Convert(seq reading.Sequence, ctx lang.Context) (lang.Result, error) {
	f.calls++
	f.contexts = append(f.contexts, ctx)
	if f.clock != nil {
		f.clock.Advance(f.cost)
	}
	if f.err != nil {
		return lang.Result{}, f.err
	}
	return lang.Result{Clauses: f.split(seq)}, nil
}

func (f *fakeConverter) Candidates(seq reading.Sequence, span lang.Span, ctx lang.Context) ([]lang.Candidate, error) {
	f.spans = append(f.spans, span)
	return []lang.Candidate{{Surface: "候補", Reading: append(reading.Sequence(nil), seq[span.Start:span.End()]...)}}, nil
}

func (f *fakeConverter) Prefixes(reading.Sequence) []lang.Candidate { return nil }

// splitEvery 每 n 個かな切成一個文節，詞面帶出讀音，方便觀察前綴是否穩定。
func splitEvery(n int) func(reading.Sequence) []lang.Clause {
	return func(seq reading.Sequence) []lang.Clause {
		var out []lang.Clause
		for i := 0; i < len(seq); i += n {
			end := min(i+n, len(seq))
			part := append(reading.Sequence(nil), seq[i:end]...)
			out = append(out, lang.Clause{Reading: part, Surface: "[" + text(part) + "]"})
		}
		return out
	}
}

func units(s string) reading.Sequence {
	out := make(reading.Sequence, 0, len([]rune(s)))
	for _, r := range s {
		out = append(out, reading.Unit(r))
	}
	return out
}

func text(seq reading.Sequence) string {
	var b strings.Builder
	for _, u := range seq {
		b.WriteString(string(u))
	}
	return b.String()
}

func resultText(res lang.Result) string {
	var b strings.Builder
	for _, cl := range res.Clauses {
		b.WriteString(cl.Surface)
	}
	return b.String()
}

// stableText 是結果中「除了最後一個文節」的字串；ライブ変換承諾這一段不變。
func stableText(res lang.Result) string {
	if len(res.Clauses) <= 1 {
		return ""
	}
	return resultText(lang.Result{Clauses: res.Clauses[:len(res.Clauses)-1]})
}

func testConfig(clock *fakeClock) live.Config {
	cfg := live.DefaultConfig()
	cfg.Enabled = true
	cfg.Clock = clock.Now
	return cfg
}

func TestDisabledByDefault(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	c := live.New(conv, live.DefaultConfig())
	if c.Enabled() {
		t.Fatal("DefaultConfig 必須預設關閉")
	}
	if p := c.Input(units("きょう"), lang.Context{}); p.Convert || p.Preview || p.Pending {
		t.Fatalf("關閉時 Input 不得有動作：%+v", p)
	}
	if _, ok := c.Tick(); ok {
		t.Fatal("關閉時 Tick 不得轉換")
	}
	if cands, err := c.Candidates(lang.Span{Start: 0, Length: 1}); err != nil || cands != nil {
		t.Fatalf("關閉時不得產生候選：%v %v", cands, err)
	}
	if conv.calls != 0 {
		t.Fatalf("關閉時呼叫了 converter %d 次", conv.calls)
	}
	// 關閉時正常変換仍由原本的 converter 完成，結果不受ライブ変換影響。
	res, err := conv.Convert(units("きょう"), lang.Context{Left: "前"})
	if err != nil || resultText(res) != "[きょ][う]" {
		t.Fatalf("一般変換結果 = %q %v", resultText(res), err)
	}
}

func TestDebounceAndTick(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 100 * time.Millisecond
	c := live.New(conv, cfg)

	start := clock.Now()
	p := c.Input(units("きょう"), lang.Context{})
	if !p.Pending || p.Convert || p.Preview {
		t.Fatalf("第一次輸入應只排程：%+v", p)
	}
	if !p.Deadline.Equal(start.Add(100 * time.Millisecond)) {
		t.Fatalf("deadline = %v", p.Deadline)
	}
	clock.Advance(50 * time.Millisecond)
	if _, ok := c.Tick(); ok {
		t.Fatal("期限未到不得轉換")
	}
	clock.Advance(50 * time.Millisecond)
	p, ok := c.Tick()
	if !ok || !p.Convert || p.Preview || p.Pending {
		t.Fatalf("到期應轉換：%+v ok=%v", p, ok)
	}
	if conv.calls != 1 {
		t.Fatalf("converter 呼叫 %d 次，want 1", conv.calls)
	}
	if _, ok := c.Tick(); ok {
		t.Fatal("轉換後不得重複執行")
	}
}

func TestDebounceRestartsAndCancel(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 100 * time.Millisecond
	c := live.New(conv, cfg)

	c.Input(units("き"), lang.Context{})
	clock.Advance(80 * time.Millisecond)
	c.Input(units("きょ"), lang.Context{}) // 重設期限
	clock.Advance(30 * time.Millisecond)
	if _, ok := c.Tick(); ok {
		t.Fatal("重設後不應在舊期限轉換")
	}
	clock.Advance(70 * time.Millisecond)
	if p, ok := c.Tick(); !ok || !p.Convert {
		t.Fatalf("新期限到應轉換：%+v ok=%v", p, ok)
	}

	c.Input(units("きょう"), lang.Context{})
	if !c.Pending() || !c.CancelPending() {
		t.Fatal("CancelPending 應取消待執行轉換")
	}
	if c.Pending() {
		t.Fatal("取消後仍在 pending")
	}
	clock.Advance(time.Second)
	if _, ok := c.Tick(); ok {
		t.Fatal("取消後不得轉換")
	}
	if calls := conv.calls; calls != 1 {
		t.Fatalf("converter 呼叫 %d 次，want 1", calls)
	}
}

func TestPreviousResultIsDraftWhilePending(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 100 * time.Millisecond
	c := live.New(conv, cfg)

	c.Input(units("きょう"), lang.Context{})
	clock.Advance(100 * time.Millisecond)
	first, ok := c.Tick()
	if !ok || !first.Convert {
		t.Fatalf("第一次轉換失敗：%+v", first)
	}
	draft := resultText(first.Result)

	p := c.Input(units("きょうは"), lang.Context{})
	if !p.Pending || !p.Preview || p.Convert {
		t.Fatalf("等待期間應顯示前次結果：%+v", p)
	}
	if got := resultText(p.Result); got != draft {
		t.Fatalf("draft = %q，want %q", got, draft)
	}
	if d, ok := c.Draft(); !ok || resultText(d) != draft {
		t.Fatalf("Draft() = %q ok=%v", resultText(d), ok)
	}
}

func TestLockedPrefixStableAcrossTwentyKana(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 30 * time.Millisecond
	c := live.New(conv, cfg)

	const keys = "あいうえおかきくけこさしすせそたちつてと"
	runes := []rune(keys)
	locked := ""
	conversions := 0
	var snapshots []string

	for i := range runes {
		clock.Advance(10 * time.Millisecond)
		seq := units(string(runes[:i+1]))
		p := c.Input(seq, lang.Context{})
		if p.Convert {
			conversions++
		}
		// 每三個かな模擬一次停頓，讓 debounce 到期。
		if (i+1)%3 == 0 {
			clock.Advance(40 * time.Millisecond)
			for {
				tick, ok := c.Tick()
				if !ok {
					break
				}
				if !tick.Convert {
					t.Fatalf("第 %d 字轉換失敗：%+v", i+1, tick)
				}
				conversions++
				got := stableText(tick.Result)
				if !strings.HasPrefix(got, locked) {
					t.Fatalf("第 %d 字已確定前段改變：%q 不再是 %q 的延伸", i+1, got, locked)
				}
				locked = got
				snapshots = append(snapshots, resultText(tick.Result))
			}
		}
	}
	// 最後再停頓一次，確認尾端也能完成。
	clock.Advance(time.Second)
	if tick, ok := c.Tick(); ok && tick.Convert {
		conversions++
		if got := stableText(tick.Result); !strings.HasPrefix(got, locked) {
			t.Fatalf("最後前段改變：%q 不再是 %q 的延伸", got, locked)
		}
	}
	if conversions == 0 {
		t.Fatal("20 字內完全沒有轉換")
	}
	if conversions > 20 {
		t.Fatalf("轉換 %d 次，超過按鍵數", conversions)
	}
	if len(snapshots) == 0 {
		t.Fatal("沒有轉換快照")
	}
	for _, s := range snapshots {
		if s == "" {
			t.Fatal("轉換結果不得為空")
		}
	}
}

func TestConversionUsesLockedPrefixAsContext(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 0
	c := live.New(conv, cfg)

	p := c.Input(units("きょう"), lang.Context{Left: "前"})
	if !p.Convert {
		t.Fatalf("立即轉換失敗：%+v", p)
	}
	// 首次轉換沒有鎖定前綴，只帶入呼叫端的 Context.Left。
	if got := conv.contexts[0].Left; got != "前" {
		t.Fatalf("首次 Context.Left = %q", got)
	}
	locked := stableText(p.Result)
	if locked == "" {
		t.Fatal("應至少鎖定一個文節")
	}
	c.Input(units("きょうは"), lang.Context{Left: "前"})
	if len(conv.contexts) != 2 {
		t.Fatalf("converter 呼叫 %d 次，want 2", len(conv.contexts))
	}
	if got := conv.contexts[1].Left; got != locked+"前" {
		t.Fatalf("第二次 Context.Left = %q，want %q", got, locked+"前")
	}
	// 第二次只転換未鎖定的尾端。
	if got := conv.contexts[1]; got.Left == "" {
		t.Fatal("尾端轉換沒有左文脈")
	}
}

func TestBackspaceDropsLockedPrefix(t *testing.T) {
	clock := newFakeClock()
	var seen []reading.Sequence
	conv := &fakeConverter{clock: clock, split: func(seq reading.Sequence) []lang.Clause {
		seen = append(seen, append(reading.Sequence(nil), seq...))
		return splitEvery(2)(seq)
	}}
	cfg := testConfig(clock)
	cfg.Debounce = 0
	c := live.New(conv, cfg)

	c.Input(units("きょうは"), lang.Context{Left: "前"})
	// 倒退到比鎖定前綴更短，必須從頭転換新的讀音。
	c.Input(units("きょ"), lang.Context{Left: "前"})
	if len(seen) != 2 || text(seen[1]) != "きょ" {
		t.Fatalf("倒退後轉換讀音 = %q，want きょ", text(seen[len(seen)-1]))
	}
	if got := conv.contexts[len(conv.contexts)-1].Left; got != "前" {
		t.Fatalf("倒退後 Context.Left = %q，已被舊前綴污染", got)
	}
}

func TestBudgetFallsBackToDraft(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 0
	cfg.Budget = 100 * time.Millisecond
	c := live.New(conv, cfg)

	first := c.Input(units("きょう"), lang.Context{})
	if !first.Convert {
		t.Fatalf("第一次転換失敗：%+v", first)
	}
	draft := resultText(first.Result)

	conv.cost = 150 * time.Millisecond
	p := c.Input(units("きょうは"), lang.Context{})
	if !p.TimedOut || p.Convert || !p.Preview {
		t.Fatalf("逾時應退回前次結果：%+v", p)
	}
	if got := resultText(p.Result); got != draft {
		t.Fatalf("逾時顯示 %q，want draft %q", got, draft)
	}
	if st := c.Stats(); st.Timeouts != 1 {
		t.Fatalf("Timeouts = %d，want 1", st.Timeouts)
	}
}

func TestBoundaryChangesAreCapped(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: func(seq reading.Sequence) []lang.Clause {
		if len(seq) == 5 {
			return splitPattern(seq, 2, 3)
		}
		return splitPattern(seq, 1, 1, 1, len(seq)-3)
	}}
	cfg := testConfig(clock)
	cfg.Debounce = 0
	cfg.MaxBoundaryChanges = 1
	cfg.HoldLimit = 1
	c := live.New(conv, cfg)

	first := c.Input(units("あいうえお"), lang.Context{})
	if !first.Convert {
		t.Fatalf("第一次転換失敗：%+v", first)
	}
	locked := stableText(first.Result)
	shown := resultText(first.Result)

	p := c.Input(units("あいうえおか"), lang.Context{})
	if p.Convert || !p.Preview {
		t.Fatalf("邊界變動過大時應先保留前次結果：%+v", p)
	}
	if p.BoundaryChanges != 2 {
		t.Fatalf("BoundaryChanges = %d，want 2", p.BoundaryChanges)
	}
	if got := resultText(p.Result); got != shown {
		t.Fatalf("保留結果 = %q，want %q", got, shown)
	}
	if st := c.Stats(); st.Holds != 1 {
		t.Fatalf("Holds = %d，want 1", st.Holds)
	}
	if got := stableText(p.Result); got != locked {
		t.Fatalf("保留時前段 = %q，want %q", got, locked)
	}

	// HoldLimit 用完後仍要接受新結果，不能永久卡住。
	p = c.Input(units("あいうえおか"), lang.Context{})
	if !p.Convert {
		t.Fatalf("HoldLimit 之後應接受新結果：%+v", p)
	}
	if got := stableText(p.Result); !strings.HasPrefix(got, locked) {
		t.Fatalf("接受新結果後已確定前段改變：%q 不再是 %q 的延伸", got, locked)
	}
}

func TestConversionErrorKeepsDraft(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 0
	c := live.New(conv, cfg)

	first := c.Input(units("きょう"), lang.Context{})
	draft := resultText(first.Result)
	conv.err = errors.New("boom")
	p := c.Input(units("きょうは"), lang.Context{})
	if p.Convert || !p.Preview || resultText(p.Result) != draft {
		t.Fatalf("転換錯誤應保留 draft：%+v", p)
	}
}

func TestCandidatesAndInvalidSpan(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 0
	c := live.New(conv, cfg)
	c.Input(units("きょう"), lang.Context{Left: "前"})

	cands, err := c.Candidates(lang.Span{Start: 1, Length: 2})
	if err != nil || len(cands) != 1 {
		t.Fatalf("Candidates = %v %v", cands, err)
	}
	if _, err := c.Candidates(lang.Span{Start: 3, Length: 9}); !errors.Is(err, live.ErrSpan) {
		t.Fatalf("越界應回 ErrSpan：%v", err)
	}
}

func TestSetEnabledResetsState(t *testing.T) {
	clock := newFakeClock()
	conv := &fakeConverter{clock: clock, split: splitEvery(2)}
	cfg := testConfig(clock)
	cfg.Debounce = 0
	c := live.New(conv, cfg)
	if !c.Input(units("きょう"), lang.Context{}).Convert {
		t.Fatal("転換失敗")
	}
	c.SetEnabled(false)
	if c.Enabled() {
		t.Fatal("SetEnabled(false) 沒有關閉")
	}
	if _, ok := c.Draft(); ok {
		t.Fatal("關閉後不得保留 draft")
	}
	c.SetEnabled(true)
	if p := c.Input(units("きょう"), lang.Context{}); !p.Convert {
		t.Fatalf("重新開啟後應可轉換：%+v", p)
	}
}

func TestZeroConfigIsDisabled(t *testing.T) {
	c := live.New(&fakeConverter{split: splitEvery(2)}, live.Config{})
	if c.Enabled() {
		t.Fatal("零值 Config 必須關閉")
	}
	if p := c.Input(units("き"), lang.Context{}); p.Convert || p.Pending {
		t.Fatalf("零值 Config 不得轉換：%+v", p)
	}
}

func splitPattern(seq reading.Sequence, sizes ...int) []lang.Clause {
	var out []lang.Clause
	at := 0
	for _, n := range sizes {
		if n <= 0 || at >= len(seq) {
			break
		}
		end := min(at+n, len(seq))
		part := append(reading.Sequence(nil), seq[at:end]...)
		out = append(out, lang.Clause{Reading: part, Surface: "[" + text(part) + "]"})
		at = end
	}
	if at < len(seq) {
		part := append(reading.Sequence(nil), seq[at:]...)
		out = append(out, lang.Clause{Reading: part, Surface: "[" + text(part) + "]"})
	}
	return out
}
