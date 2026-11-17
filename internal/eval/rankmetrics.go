package eval

// RankMetrics 是「一個參考答案在候選清單中的名次」的聚合。
//
// 名次從 1 開始；0 表示候選清單裡沒有參考答案。MRR 對缺少的候選記 0，
// 不會把它從分母移除，否則大量漏掉參考答案反而會讓 MRR 變好看。
type RankMetrics struct {
	N        int     `json:"n"`
	Top1     int     `json:"top1"`
	Top1Rate float64 `json:"top1_rate"`
	MRR      float64 `json:"mrr"`
	Misses   int     `json:"misses"`

	finalized bool
}

// Add 加入一個名次。rank <= 0 視為沒找到。
func (m *RankMetrics) Add(rank int) {
	m.N++
	if rank == 1 {
		m.Top1++
	}
	if rank > 0 {
		m.MRR += 1 / float64(rank)
	} else {
		m.Misses++
	}
}

// Finalize 把累計值換算成比率。重複呼叫的結果相同；Add 必須在 Finalize 前完成。
func (m *RankMetrics) Finalize() {
	if m.finalized {
		return
	}
	m.Top1Rate = ratio(m.Top1, m.N)
	if m.N > 0 {
		m.MRR /= float64(m.N)
	}
	m.finalized = true
}

// Ranked 回報一個參考答案在候選順序中的名次；找不到回 0。
func Ranked(candidates []string, reference string) int {
	for i, candidate := range candidates {
		if candidate == reference {
			return i + 1
		}
	}
	return 0
}
