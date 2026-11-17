package score

import (
	"context"
	"errors"
	"fmt"
	"math"
	"unicode"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/decode"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// ZhNeutralScore 是中文 adapter 對非中文候選回報的中性分數。
//
// 同一次請求可以混合多種語言；每個 scorer 只評自己的語言，其他語言的候選
// 必須保留在原順序，也不能被當成模型不可用。0 是未經校準的中性值，仲裁器
// 仍要用片段語言與其他特徵決定排序。
const ZhNeutralScore = 0

// ZhAdapter 把既有中文 ONNX reranker 接到多語言 Scorer 介面。
//
// 這個 adapter 只轉換資料形狀：lattice 的語言標記候選轉成 core/rerank 的
// 注音候選，再把模型分數放回原本的候選順序。它不重訓、不改模型權重、不改
// vocab 或 manifest，也不改變注音 session 既有的 rerank 路徑。
//
// 同一個請求可以含中文、日文與英文候選；只有中文候選會送進中文模型，
// 其他候選拿 ZhNeutralScore。若完全沒有可評分的中文候選，或模型回報任何
// 錯誤，Score 回傳 ErrUnavailable，呼叫端保留統計轉換結果。
type ZhAdapter struct {
	// Scorer 是既有中文模型（core/rerank.Scorer）。必填。
	Scorer rerank.Scorer
	// AllowPartial 是否允許等長前綴候選；預設 false，與既有中文 reranker 相同。
	AllowPartial bool
	// ContextRunes 是送給模型的前文上限。0 表示使用 rerank.MaxContextRunes，
	// 負值表示完全不送前文。超過上限時保留最右側文字，與模型訓練窗口一致。
	ContextRunes int
	// TextMode 允許單音節中文候選在有前文時使用 text task。
	//
	// 中文模型套件若沒有宣告 text_weight，worker 會回報不支援，adapter 再退回
	// ErrUnavailable。預設 false，避免把原本不評分的單音節片段送進模型。
	TextMode bool
}

var _ Scorer = (*ZhAdapter)(nil)

// NewZhAdapter 建立中文 adapter。Scorer 可以是 internal/neural 的 worker，
// 也可以是測試替身；模型不可用時由 Score 回報 ErrUnavailable。
func NewZhAdapter(scorer rerank.Scorer) *ZhAdapter {
	return &ZhAdapter{Scorer: scorer, ContextRunes: rerank.MaxContextRunes}
}

// Languages 回報中文。零值語言標籤仍視為中文，保留舊呼叫端相容性。
func (a *ZhAdapter) Languages() []lang.ID { return []lang.ID{lang.Zh} }

// Score 依 req.Candidates 的順序回傳等長分數。
//
// 中文候選會依讀音分組，同一組一次送進既有中文模型；同詞面的重複候選只送
// 一次，再把同一個分數套回所有重複項。非中文候選、形狀不完整或無法轉成注音
// 的候選保留 ZhNeutralScore。任何一組模型呼叫失敗都不回傳部分結果，整筆請求
// 一律以 ErrUnavailable 退回。
func (a *ZhAdapter) Score(ctx context.Context, req Request) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if a == nil || a.Scorer == nil {
		return nil, ErrUnavailable
	}
	scores := make([]float64, len(req.Candidates))
	if len(req.Candidates) == 0 {
		return scores, nil
	}
	if !utf8.ValidString(req.Context) {
		return nil, fmt.Errorf("%w: invalid context", ErrUnavailable)
	}

	groups := zhGroups(req.Candidates)
	if len(groups) == 0 {
		return nil, ErrUnavailable
	}

	context := a.context(req.Context)
	scored := false
	for _, group := range groups {
		if len(group.unique) < 2 {
			// 既有中文模型一次至少評兩個候選；單一候選留給統計轉換。
			continue
		}
		if len(group.unique) > rerank.MaxCandidates {
			return nil, fmt.Errorf("%w: %d chinese candidates exceed model limit %d", ErrUnavailable, len(group.unique), rerank.MaxCandidates)
		}

		request := a.request(group, req.Candidates, context)
		if !rerank.Supported(request) {
			// 單音節在沒有 text task 時不支援；這是形狀限制，不是模型故障。
			continue
		}
		raw, err := a.Scorer.Score(request)
		if err != nil {
			return nil, errors.Join(ErrUnavailable, err)
		}
		if len(raw) != len(group.unique) {
			return nil, fmt.Errorf("%w: model returned %d scores for %d candidates", ErrUnavailable, len(raw), len(group.unique))
		}
		for i, value := range raw {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("%w: non-finite model score", ErrUnavailable)
			}
			surface := req.Candidates[group.unique[i]].Surface
			for _, index := range group.indexes[surface] {
				scores[index] = value
			}
		}
		scored = true
	}
	if !scored {
		return nil, ErrUnavailable
	}
	return scores, nil
}

// zhGroup 是同一個注音讀音下的候選。unique 保留第一次出現的候選索引，
// indexes 把詞面映射到所有原始候選索引，供重複詞面套用同一個分數。
type zhGroup struct {
	reading []zhuyin.Syllable
	unique  []int
	indexes map[string][]int
}

// zhGroups 依注音讀音分組，並跳過非中文或無法轉換的候選。
// 回傳順序依讀音第一次出現的位置，讓模型呼叫順序穩定。
func zhGroups(candidates []lattice.Candidate) []*zhGroup {
	var groups []*zhGroup
	byReading := map[string]*zhGroup{}
	for i, candidate := range candidates {
		reading, ok := zhReading(candidate)
		if !ok {
			continue
		}
		key := zhuyin.Key(reading)
		group := byReading[key]
		if group == nil {
			group = &zhGroup{
				reading: reading,
				indexes: map[string][]int{},
			}
			byReading[key] = group
			groups = append(groups, group)
		}
		if _, seen := group.indexes[candidate.Surface]; !seen {
			group.unique = append(group.unique, i)
		}
		group.indexes[candidate.Surface] = append(group.indexes[candidate.Surface], i)
	}
	return groups
}

// zhReading 回報候選是否是可送進中文模型的注音候選。
//
// 零值語言標籤視為中文；明確標成其他語言，或輸出變體屬於其他語言時跳過。
// 詞面必須是合法 UTF-8，且每個音節對應一個字。注音結構由 zhuyin.Syllable
// 檢查，かな與其他讀音不會被誤送進中文模型。
func zhReading(candidate lattice.Candidate) ([]zhuyin.Syllable, bool) {
	if !candidate.Valid() {
		return nil, false
	}
	if id := candidate.Language.Language; id != lang.Und && id != lang.Zh {
		return nil, false
	}
	if candidate.Variant != "" && candidate.Variant.Language() != lang.Zh {
		return nil, false
	}
	if candidate.Surface == "" || !utf8.ValidString(candidate.Surface) {
		return nil, false
	}
	if len(candidate.Reading) == 0 || len(candidate.Reading) != utf8.RuneCountInString(candidate.Surface) {
		return nil, false
	}
	for _, r := range candidate.Surface {
		if unicode.IsControl(r) {
			return nil, false
		}
	}
	reading := make([]zhuyin.Syllable, len(candidate.Reading))
	for i, unit := range candidate.Reading {
		syllable := zhuyin.Syllable(unit)
		if !syllable.Valid() {
			return nil, false
		}
		reading[i] = syllable
	}
	return reading, true
}

// request 把一組中文候選轉成既有 rerank.Request。
//
// lattice 候選只有一個片段，因此轉換時補一個覆蓋整個詞面的 Seg；模型只用
// 詞面與讀音，這個分段不改變分數。LogScore 固定為 0，模型分數由 adapter
// 原樣回傳，不與詞典分數混在一起。
func (a *ZhAdapter) request(group *zhGroup, candidates []lattice.Candidate, context string) rerank.Request {
	rows := make([]decode.Hypothesis, len(group.unique))
	for i, index := range group.unique {
		text := candidates[index].Surface
		rows[i] = decode.Hypothesis{
			Text: text,
			Segments: []decode.Seg{{
				Word:  text,
				Start: 0,
				End:   utf8.RuneCountInString(text),
			}},
		}
	}
	mode := ""
	if a.TextMode && len(group.reading) == 1 && context != "" {
		mode = rerank.ModeText
	}
	return rerank.Request{
		AllowPartial: a.AllowPartial,
		Mode:         mode,
		Context:      context,
		Reading:      append([]zhuyin.Syllable(nil), group.reading...),
		Candidates:   rows,
	}
}

// context 依 adapter 設定截取前文。模型只看最後 ContextRunes 字，因此截掉
// 較舊的文字不會改變候選分數，也避免超過 rerank.MaxContextRunes 而上層退回。
func (a *ZhAdapter) context(text string) string {
	limit := a.ContextRunes
	switch {
	case limit < 0:
		return ""
	case limit == 0:
		limit = rerank.MaxContextRunes
	case limit > rerank.MaxContextRunes:
		limit = rerank.MaxContextRunes
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[len(runes)-limit:])
}
