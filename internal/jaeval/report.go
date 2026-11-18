package jaeval

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Report aggregates a run without touching timings.
type Report struct {
	Cases         int              `json:"cases"`
	Top1          int              `json:"top1"`
	Top1Rate      float64          `json:"top1_rate"`
	AvgCandidates float64          `json:"avg_candidates"`
	LongestID     string           `json:"longest_id"`
	LongestRunes  int              `json:"longest_runes"`
	ByCategory    []CategoryReport `json:"by_category"`
}

// CategoryReport is the same view for one corpus category.
type CategoryReport struct {
	Category      string  `json:"category"`
	Cases         int     `json:"cases"`
	Top1          int     `json:"top1"`
	Top1Rate      float64 `json:"top1_rate"`
	AvgCandidates float64 `json:"avg_candidates"`
}

// BuildReport aggregates results in corpus order. Ties for the longest output
// keep the earlier case, so the value is stable for a fixed corpus.
func BuildReport(results []CaseResult) Report {
	var rep Report
	rep.Cases = len(results)
	byCat := map[string]*CategoryReport{}
	candTotal := 0
	for _, r := range results {
		candTotal += len(r.Candidates)
		if r.Rank == 1 {
			rep.Top1++
		}
		if n := utf8.RuneCountInString(r.Output); n > rep.LongestRunes {
			rep.LongestRunes = n
			rep.LongestID = r.ID
		}
		cr := byCat[r.Category]
		if cr == nil {
			cr = &CategoryReport{Category: r.Category}
			byCat[r.Category] = cr
		}
		cr.Cases++
		cr.AvgCandidates += float64(len(r.Candidates))
		if r.Rank == 1 {
			cr.Top1++
		}
	}
	if rep.Cases > 0 {
		rep.Top1Rate = float64(rep.Top1) / float64(rep.Cases)
		rep.AvgCandidates = float64(candTotal) / float64(rep.Cases)
	}
	for _, name := range sortedKeys(byCat) {
		cr := byCat[name]
		if cr.Cases > 0 {
			cr.Top1Rate = float64(cr.Top1) / float64(cr.Cases)
			cr.AvgCandidates /= float64(cr.Cases)
		}
		rep.ByCategory = append(rep.ByCategory, *cr)
	}
	sort.Slice(rep.ByCategory, func(i, j int) bool {
		return rep.ByCategory[i].Category < rep.ByCategory[j].Category
	})
	return rep
}

// FormatReport renders the aggregate statistics as a stable text block.
func FormatReport(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cases: %d\n", rep.Cases)
	if rep.Cases > 0 {
		fmt.Fprintf(&b, "expected ranked first: %d/%d (%.1f%%)\n", rep.Top1, rep.Cases, rep.Top1Rate*100)
		fmt.Fprintf(&b, "average candidates: %.2f\n", rep.AvgCandidates)
	} else {
		fmt.Fprintln(&b, "expected ranked first: 0/0")
		fmt.Fprintln(&b, "average candidates: 0.00")
	}
	fmt.Fprintf(&b, "longest conversion: %s (%d runes)\n", rep.LongestID, rep.LongestRunes)
	fmt.Fprintln(&b, "by category:")
	for _, c := range rep.ByCategory {
		fmt.Fprintf(&b, "  %-16s cases=%2d top1=%2d/%2d (%.1f%%) avg_candidates=%.2f\n",
			c.Category, c.Cases, c.Top1, c.Cases, c.Top1Rate*100, c.AvgCandidates)
	}
	return b.String()
}
