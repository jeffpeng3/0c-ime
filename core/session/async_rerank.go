package session

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/0c-biggo/0c-ime/core/decode"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// asyncCandidate 是一個候選視窗的背景評分。session 只把不可變的請求交給
// goroutine，結果經由 result 回到宿主執行緒；背景 goroutine 不碰 session。
type asyncCandidate struct {
	token  asyncToken
	cancel context.CancelFunc
	result chan asyncResult
}

// asyncToken 記下請求當時的狀態。結果回來時逐項比對；任何一項不同就丟棄，
// 不改動目前顯示的候選。
type asyncToken struct {
	rev     int
	cursor  int
	pathSig string
	start   int
	length  int
	reading string
	cand    *candState
	indices []int // 參與評分的候選在 cand.items 的位置（由小到大）
	rows    []decode.Hypothesis
	weight  float64
	margin  float64
}

type asyncResult struct {
	scores []float64
	err    error
}

// startAsyncCandidates 在候選清單建立後送出統計前 3 名。只有普通改字候選
// 會送模型；符號選單、中英混打、英文補字與原樣注音都不送。
func (s *Session) startAsyncCandidates() {
	async := s.Rerank.Async
	if s.DisableModelScoring || async.Scorer == nil || s.cand == nil {
		return
	}
	c := s.cand
	if c.literal || c.mixed || c.english || c.pending || c.kind != "" || c.symbols != "" || c.length <= 0 {
		return
	}
	if !s.comp.Empty() || s.private || s.paused || s.english || len(s.syls) == 0 {
		return
	}
	start, length := c.start, c.length
	if start < 0 || length <= 0 || start+length > len(s.syls) {
		return
	}
	limit := len(c.items)
	if c.split > 0 && c.split < limit {
		limit = c.split // 混合清單接在後面的單字不在這次評分範圍
	}
	topK := async.AsyncTopK()
	var rows []decode.Hypothesis
	var indices []int
	for i := 0; i < limit && len(rows) < topK; i++ {
		if c.raw && i == len(c.items)-1 {
			continue // 原樣注音不是模型候選
		}
		if _, ok := c.keyLiterals[i]; ok {
			continue // 原始按鍵候選保留給使用者，不交給模型
		}
		surface := c.items[i].Entry.Surface
		if surface == "" || len([]rune(surface)) != length {
			continue
		}
		rows = append(rows, decode.Hypothesis{
			Text:     surface,
			LogScore: c.items[i].Score,
			Segments: []decode.Seg{{Word: surface, Start: 0, End: length}},
		})
		indices = append(indices, i)
	}
	if len(rows) < 2 {
		return
	}
	opt := s.Rerank
	context := ""
	if opt.ContextRunes > 0 {
		context = lastRunes(s.neuralContext()+s.textBefore(start), min(opt.ContextRunes, rerank.MaxContextRunes))
	}
	req := rerank.Request{
		AllowPartial: opt.AllowPartial,
		Revision:     s.rev + 1,
		Context:      context,
		Reading:      slices.Clone(s.syls[start : start+length]),
		Candidates:   rows,
	}
	weight, margin := opt.Weight, opt.Margin
	if length == 1 {
		req.Mode, weight, margin = rerank.ModeText, opt.TextWeight, opt.TextMargin
	}
	if !(weight > 0) || !rerank.Supported(req) {
		return
	}
	token := asyncToken{
		rev:     s.rev + 1, // Handle 會在這次請求結尾把 rev 加一
		cursor:  s.cursor,
		pathSig: s.pathSig,
		start:   start,
		length:  length,
		reading: zhuyin.Key(req.Reading),
		cand:    c,
		indices: indices,
		rows:    rows,
		weight:  weight,
		margin:  margin,
	}
	s.launchAsync(async, req, token)
}

// textBefore 是候選位置之前已顯示的文字；與 rerankSegment 的前文拼接方式一致。
func (s *Session) textBefore(pos int) string {
	var b strings.Builder
	for _, g := range s.path {
		if g.End <= pos {
			b.WriteString(g.Word)
		}
	}
	return b.String()
}

func (s *Session) launchAsync(async rerank.AsyncOptions, req rerank.Request, token asyncToken) {
	ctx, cancel := context.WithCancel(context.Background())
	pending := &asyncCandidate{token: token, cancel: cancel, result: make(chan asyncResult, 1)}
	s.asyncPending = pending
	timeout := async.AsyncTimeout()
	go func() {
		done := make(chan asyncResult, 1)
		go func() {
			scores, err := async.Scorer.ScoreContext(ctx, req)
			done <- asyncResult{scores: scores, err: err}
		}()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		var out asyncResult
		select {
		case out = <-done:
		case <-timer.C:
			out.err = rerank.ErrAsyncTimeout
			cancel()
		case <-ctx.Done():
			out.err = ctx.Err()
		}
		select {
		case pending.result <- out:
		default:
		}
	}()
}

// Poll 套用已經回來且仍然有效的背景評分，回傳更新後的快照。沒有結果、結果
// 逾時／失敗或請求已過期時回 false，畫面維持原狀。宿主可在自己的計時器上呼叫；
// 不在 Handle 內等待模型。
func (s *Session) Poll() (proto.Snapshot, bool) {
	if !s.drainAsync() {
		return proto.Snapshot{}, false
	}
	s.rev++
	snap := s.snapshot()
	snap.Handled = true
	s.status(&snap)
	return snap, true
}

// drainAsync 取出一筆背景結果。成功且 token 仍相符時重排候選並回 true。
func (s *Session) drainAsync() bool {
	pending := s.asyncPending
	if pending == nil {
		return false
	}
	select {
	case out := <-pending.result:
		s.asyncPending = nil
		pending.cancel()
		if out.err != nil || !s.asyncTokenCurrent(pending.token) {
			return false
		}
		return s.applyAsyncScores(pending.token, out.scores)
	default:
		return false
	}
}

func (s *Session) asyncTokenCurrent(t asyncToken) bool {
	if s.cand == nil || s.cand != t.cand || s.rev != t.rev || s.cursor != t.cursor || s.pathSig != t.pathSig {
		return false
	}
	if t.start < 0 || t.length <= 0 || t.start+t.length > len(s.syls) {
		return false
	}
	if s.cand.start != t.start || s.cand.length != t.length {
		return false
	}
	if len(t.indices) > len(s.cand.items) {
		return false
	}
	return zhuyin.Key(s.syls[t.start:t.start+t.length]) == t.reading
}

// applyAsyncScores 依融合分數重排參與評分的前 3 名。第一名沒有超過 margin
// 時保持統計順序；其餘候選不動。候選、近音附註、顯示原因與原始按鍵一起移動。
func (s *Session) applyAsyncScores(t asyncToken, scores []float64) bool {
	if len(scores) != len(t.rows) {
		return false
	}
	winner := rerank.Choose(t.rows, scores, t.weight, t.margin)
	if winner <= 0 {
		return false
	}
	fused := make([]float64, len(t.rows))
	for i, row := range t.rows {
		fused[i] = row.LogScore + t.weight*scores[i]
		if !finite(fused[i]) {
			return false
		}
	}
	order := make([]int, len(t.rows))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case fused[a] > fused[b]:
			return -1
		case fused[a] < fused[b]:
			return 1
		default:
			return 0
		}
	})
	c := s.cand
	oldItems := slices.Clone(c.items)
	oldMatched := slices.Clone(c.matched)
	oldLabels := slices.Clone(c.labels)
	newIndex := map[int]int{}
	for slot, src := range order {
		dst := t.indices[slot]
		srcAt := t.indices[src]
		c.items[dst] = oldItems[srcAt]
		if dst < len(c.matched) && srcAt < len(oldMatched) {
			c.matched[dst] = oldMatched[srcAt]
		}
		if dst < len(c.labels) && srcAt < len(oldLabels) {
			c.labels[dst] = oldLabels[srcAt]
		}
		newIndex[srcAt] = dst
	}
	if len(c.keyLiterals) > 0 {
		keys := make(map[int]string, len(c.keyLiterals))
		for at, key := range c.keyLiterals {
			if moved, ok := newIndex[at]; ok {
				at = moved
			}
			keys[at] = key
		}
		c.keyLiterals = keys
	}
	return true
}

// cancelAsync 取消進行中的背景評分（如果有的話）。
func (s *Session) cancelAsync() {
	if s.asyncPending == nil {
		return
	}
	s.asyncPending.cancel()
	s.asyncPending = nil
}

// cancelStaleAsync 在請求結束後取消已不符合目前片段、游標或版本的背景評分。
func (s *Session) cancelStaleAsync() {
	if p := s.asyncPending; p != nil && (s.DisableModelScoring || !s.asyncTokenCurrent(p.token)) {
		s.cancelAsync()
	}
}

// Close 取消這個 session 尚未完成的背景評分。宿主關閉 session 時呼叫。
func (s *Session) Close() {
	s.cancelAsync()
}
