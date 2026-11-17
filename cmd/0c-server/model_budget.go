package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/0c-biggo/0c-ime/internal/neural"
)

// P2-33 的固定退回原因：與 internal/neural 的其他 reason 代碼一樣只含分類，不含文字或檔名。
const (
	ReasonModelBudgetExceeded   = "budget_exceeded"
	ReasonPlaceholderDisallowed = "placeholder_disallowed"
)

// budgetLoader 在 residency manager 的載入器前面加上 P2-33 的預算與 placeholder 檢查。
//
// 以 prepared.Bytes 先預留（Preload 的模型都有），再把實際 ResidentBytes 記進 committed。
// 兩個模型同時 eager 載入時，第二個會看到第一個的預留量，不會兩個都先載入才發現超過預算。
// 超過預算或 placeholder 未開放時回傳 *neural.ModelError，管理器照一般載入失敗退回統計轉換，
// 不反覆重試同一個模型。
type budgetLoader struct {
	limit             int64
	allowPlaceholders bool
	load              neural.Loader
	log               func(string, ...any)

	mu        sync.Mutex
	committed int64
	reserved  int64
}

func newBudgetLoader(limit int64, allowPlaceholders bool, load neural.Loader, log func(string, ...any)) *budgetLoader {
	if load == nil {
		load = neural.LoadModel
	}
	return &budgetLoader{limit: limit, allowPlaceholders: allowPlaceholders, load: load, log: log}
}

func (b *budgetLoader) Load(ctx context.Context, spec neural.ModelSpec, prepared *neural.Prepared) (*neural.Loaded, error) {
	planned := int64(0)
	if prepared != nil {
		if prepared.Placeholder && !b.allowPlaceholders {
			return nil, b.reject(spec, ReasonPlaceholderDisallowed,
				fmt.Errorf("模型 %s 是 placeholder（quality=%s），設定未允許", spec.Name, prepared.Quality))
		}
		planned = prepared.Bytes
	}
	if err := b.reserve(spec, planned); err != nil {
		return nil, err
	}

	loaded, err := b.load(ctx, spec, prepared)
	if err != nil {
		b.release(planned)
		return nil, err
	}
	actual := planned
	if loaded != nil && loaded.ResidentBytes > 0 {
		actual = loaded.ResidentBytes
	}
	if loaded != nil && loaded.Placeholder && !b.allowPlaceholders {
		b.release(planned)
		closeLoaded(loaded)
		return nil, b.reject(spec, ReasonPlaceholderDisallowed,
			fmt.Errorf("模型 %s 是 placeholder（quality=%s），設定未允許", spec.Name, loaded.Quality))
	}
	if err := b.commit(planned, actual); err != nil {
		closeLoaded(loaded)
		return nil, b.reject(spec, ReasonModelBudgetExceeded, err)
	}
	return loaded, nil
}

func (b *budgetLoader) reserve(spec neural.ModelSpec, planned int64) error {
	if planned <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit > 0 && b.committed+b.reserved+planned > b.limit {
		return b.reject(spec, ReasonModelBudgetExceeded,
			fmt.Errorf("模型 %s 需要 %d bytes，常駐 %d + 預留 %d 會超過預算 %d bytes",
				spec.Name, planned, b.committed, b.reserved, b.limit))
	}
	b.reserved += planned
	return nil
}

// release 釋放這次沒有成功載入的預留量。
func (b *budgetLoader) release(planned int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reserved -= planned
	if b.reserved < 0 {
		b.reserved = 0
	}
}

// commit 把預留換成實際常駐量。實際大小可能與 manifest 不同（例如延遲載入才核對），
// 因此再檢查一次；超過時不記帳，呼叫端會關掉後端。
func (b *budgetLoader) commit(planned, actual int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reserved -= planned
	if b.reserved < 0 {
		b.reserved = 0
	}
	projected := b.committed + b.reserved + actual
	if b.limit > 0 && projected > b.limit {
		return fmt.Errorf("模型實際常駐 %d bytes，總計 %d 會超過預算 %d bytes", actual, projected, b.limit)
	}
	b.committed += actual
	return nil
}

func (b *budgetLoader) residency() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.committed
}

func (b *budgetLoader) reject(spec neural.ModelSpec, reason string, err error) error {
	if b.log != nil {
		b.log("模型 %s 不載入，保留統計轉換：%v", spec.Name, err)
	}
	return &neural.ModelError{Reason: reason, Err: err}
}

func closeLoaded(loaded *neural.Loaded) {
	if loaded != nil && loaded.Close != nil {
		_ = loaded.Close()
	}
}
