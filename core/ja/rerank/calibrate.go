package rerank

// Sample 是一條候選的校準資料。Stats 是統計分數，Neural 是驗證器的長度
// 正規化分數；Draft 表示它是統計第一名；Correct 表示它是正解或可接受答案。
type Sample struct {
	Stats   float64
	Neural  float64
	Draft   bool
	Correct bool
}

// SampleGroup 是同一次轉換的候選。Calibrate 只在同一組內比較，因為統計
// 分數的尺度會隨句長與詞典而變。
type SampleGroup struct {
	Candidates []Sample
}

// Calibrate 用 T33 匯出的 (統計分數, 模型分數, 正解) 資料網格搜尋權重。
// 回傳在給定 grid 中首選正確率最高的權重；同分時取較早出現者，確保
// 結果可重現。沒有可比較的資料時回傳 DefaultConfig 的權重。
//
// 這個函式不讀題庫、不連模型；呼叫端負責用凍結的 T33 資料產生樣本。
func Calibrate(groups []SampleGroup, grid []Weights) Weights {
	if len(groups) == 0 || len(grid) == 0 {
		return DefaultConfig().Weights
	}
	best := grid[0]
	bestHits := -1
	for _, w := range grid {
		hits := 0
		for _, group := range groups {
			if len(group.Candidates) == 0 {
				continue
			}
			min, max := group.Candidates[0].Stats, group.Candidates[0].Stats
			for _, c := range group.Candidates[1:] {
				if c.Stats < min {
					min = c.Stats
				}
				if c.Stats > max {
					max = c.Stats
				}
			}
			span := max - min
			top := 0
			topScore := 0.0
			for i, c := range group.Candidates {
				norm := 0.5
				if span > 0 {
					norm = (c.Stats - min) / span
				}
				score := w.Neural*c.Neural + w.Stats*norm
				if c.Draft {
					score += w.Draft
				}
				if i == 0 || score > topScore {
					top = i
					topScore = score
				}
			}
			if group.Candidates[top].Correct {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = w, hits
		}
	}
	return best
}
