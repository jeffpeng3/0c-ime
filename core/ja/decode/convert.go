package decode

import (
	"sort"

	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

var _ lang.Converter = (*Decoder)(nil)

// Convert 回傳整段讀音的最佳轉換結果。
//
// 有驗證器時保留數條統計候選讓驗證器重排；沒有驗證器時只算最佳路徑。
func (d *Decoder) Convert(seq reading.Sequence, ctx lang.Context) (lang.Result, error) {
	if len(seq) == 0 {
		return lang.Result{}, nil
	}
	k := 1
	if d.Verifier != nil || d.Reranker != nil {
		k = d.desiredNBest()
	}
	results := d.results(seq, ctx, k)
	if len(results) == 0 {
		return lang.Result{}, nil
	}
	return results[0], nil
}

// NBest 回傳前 n 條轉換結果，依分數由高到低排序。
func (d *Decoder) NBest(seq reading.Sequence, ctx lang.Context, n int) ([]lang.Result, error) {
	if n <= 0 || len(seq) == 0 {
		return nil, nil
	}
	return d.results(seq, ctx, n), nil
}

func (d *Decoder) results(seq reading.Sequence, ctx lang.Context, k int) []lang.Result {
	finals := d.search(seq, ctx, k)
	out := make([]lang.Result, 0, len(finals))
	seen := map[string]bool{}
	for _, sr := range finals {
		res := d.result(sr, seq)
		surface := ResultSurface(res.Clauses)
		if seen[surface] {
			continue
		}
		seen[surface] = true
		if d.Verifier != nil {
			res.Score += d.Verifier.Score(seq, res, ctx)
		}
		out = append(out, res)
	}
	sortResults(out)
	if d.Reranker != nil && len(out) > 0 {
		if reranked := d.Reranker.Rerank(seq, out, ctx); len(reranked) > 0 {
			out = reranked
		}
	}
	return out
}

// Candidates 回傳指定讀音範圍的候選，依分數由高到低排序。
//
// 範圍不合法時回傳空清單，不視為錯誤：外殼可能在游標移動後送出舊範圍。
func (d *Decoder) Candidates(seq reading.Sequence, span lang.Span, ctx lang.Context) ([]lang.Candidate, error) {
	if span.Start < 0 || span.Length <= 0 || span.End() > len(seq) {
		return nil, nil
	}
	limit := d.desiredCandidates()
	sub := append(reading.Sequence(nil), seq[span.Start:span.End()]...)
	results := d.results(sub, ctx, limit)

	cands := make([]lang.Candidate, 0, limit)
	seen := map[string]bool{}
	for _, res := range results {
		surface := ResultSurface(res.Clauses)
		if surface == "" || seen[surface] {
			continue
		}
		seen[surface] = true
		cands = append(cands, lang.Candidate{
			Surface: surface,
			Reading: append(reading.Sequence(nil), sub...),
			Clauses: res.Clauses,
			Score:   res.Score,
			Source:  lang.SourceConvert,
		})
	}
	// 轉換候選只涵蓋前 N 條路徑；同讀音的詞典詞條即使沒有進前 N 名也要能選。
	if d.Lex != nil {
		for _, e := range d.Lex.Exact(sub) {
			if seen[e.Surface] {
				continue
			}
			seen[e.Surface] = true
			cands = append(cands, lang.Candidate{
				Surface: e.Surface,
				Reading: append(reading.Sequence(nil), sub...),
				Clauses: []lang.Clause{{
					Reading:  append(reading.Sequence(nil), e.Reading...),
					Surface:  e.Surface,
					Segments: []lang.CandidateSegment{{Reading: append(reading.Sequence(nil), e.Reading...), Surface: e.Surface}},
					Score:    -d.candidateCost(e),
				}},
				Score:  -d.candidateCost(e),
				Source: lang.SourceConvert,
			})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].Surface < cands[j].Surface
	})
	if d.Reranker != nil && len(cands) > 0 {
		cands = d.rerankCandidates(sub, cands, ctx)
	}
	if len(cands) > limit {
		cands = cands[:limit]
	}
	return cands, nil
}

// rerankCandidates applies the same draft/verify/rerank hook to the candidate
// window. results() already reranks the N-best before dictionary exact entries
// are appended; reranking again after the merge keeps the final candidate list
// in the model's order instead of letting the statistical score sort them back.
func (d *Decoder) rerankCandidates(seq reading.Sequence, cands []lang.Candidate, ctx lang.Context) []lang.Candidate {
	drafts := make([]lang.Result, 0, len(cands))
	bySurface := make(map[string]lang.Candidate, len(cands))
	for _, cand := range cands {
		if cand.Surface == "" {
			continue
		}
		clauses := cand.Clauses
		if len(clauses) == 0 {
			clauses = []lang.Clause{{Reading: append(reading.Sequence(nil), cand.Reading...), Surface: cand.Surface}}
		}
		drafts = append(drafts, lang.Result{Clauses: clauses, Score: cand.Score})
		bySurface[cand.Surface] = cand
	}
	if len(drafts) == 0 {
		return cands
	}
	reranked := d.Reranker.Rerank(seq, drafts, ctx)
	if len(reranked) == 0 {
		return cands
	}
	out := make([]lang.Candidate, 0, len(cands))
	seen := make(map[string]bool, len(cands))
	for _, res := range reranked {
		surface := ResultSurface(res.Clauses)
		cand, ok := bySurface[surface]
		if !ok || seen[surface] {
			continue
		}
		seen[surface] = true
		out = append(out, cand)
	}
	for _, cand := range cands {
		if !seen[cand.Surface] {
			out = append(out, cand)
		}
	}
	return out
}

// Prefixes 回傳讀音以 seq 開頭的較長詞條，供預測補全。
func (d *Decoder) Prefixes(seq reading.Sequence) []lang.Candidate {
	if len(seq) == 0 || d.Lex == nil {
		return nil
	}
	limit := d.desiredCandidates()
	var cands []lang.Candidate
	seen := map[string]bool{}
	for _, e := range d.Lex.Prefixed(seq) {
		if len(e.Reading) <= len(seq) || seen[e.Surface] {
			continue
		}
		seen[e.Surface] = true
		cands = append(cands, lang.Candidate{
			Surface: e.Surface,
			Reading: append(reading.Sequence(nil), e.Reading...),
			Score:   -e.Cost,
			Source:  lang.SourcePredict,
		})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		if cands[i].Surface != cands[j].Surface {
			return cands[i].Surface < cands[j].Surface
		}
		return reading.Key(cands[i].Reading) < reading.Key(cands[j].Reading)
	})
	if len(cands) > limit {
		cands = cands[:limit]
	}
	return cands
}

func entryCost(conn lexicon.Connection, e lexicon.Entry) float64 {
	return e.Cost + connectionCost(conn, lexicon.POSBOS, e.POS) + connectionCost(conn, e.POS, lexicon.POSEOS)
}

// candidateCost 是單一詞條候選的分數。句首沒有左詞，因此只加 unigram；
// 沒有 LM 時與 entryCost 相同。
func (d *Decoder) candidateCost(e lexicon.Entry) float64 {
	return entryCost(d.Conn, e) + d.lmCost("", e.Surface)
}
