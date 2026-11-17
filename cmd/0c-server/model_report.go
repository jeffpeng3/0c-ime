package main

import (
	"encoding/json"
	"io"
	"time"

	"github.com/0c-biggo/0c-ime/internal/neural"
)

// residencyReport 是 `0c-server -model-report` 的輸出契約（P2-33）。resident_bytes 來自
// internal/neural.Manager.Residency 的記帳；process_tree_rss_bytes 是引擎與 Python worker
// 的實際 RSS，兩者一起看才能把模型檔與 Python runtime 基線分開。
type residencyReport struct {
	Schema            int                  `json:"schema"`
	Mode              neural.Mode          `json:"mode"`
	PrimaryLanguage   string               `json:"primary_language"`
	AllowPlaceholders bool                 `json:"allow_placeholders"`
	BudgetMB          int                  `json:"budget_mb"`
	BudgetBytes       int64                `json:"budget_bytes"`
	BudgetSource      string               `json:"budget_source"`
	ResidentBytes     int64                `json:"resident_bytes"`
	PeakBytes         int64                `json:"peak_bytes"`
	WithinBudget      bool                 `json:"within_budget"`
	RemainingBytes    int64                `json:"remaining_bytes"`
	Loaded            int                  `json:"loaded"`
	Loading           int                  `json:"loading"`
	Failed            int                  `json:"failed"`
	Models            []neural.ModelStatus `json:"models"`
	// ModelSet 是 P2-27 的日文模型組使用中狀態；-model-report 一定會帶此區段，
	// 沒有選定時 active 為 false。
	ModelSet        *modelSetReport `json:"model_set,omitempty"`
	ProcessRSSBytes int64           `json:"process_rss_bytes"`
	WorkerRSSBytes  int64           `json:"worker_rss_bytes"`
	ProcessTreeRSS  int64           `json:"process_tree_rss_bytes"`
	// OverheadBytes 是實際 RSS 減去模型檔常駐量；包含 Go 引擎、Python、numpy 與 onnxruntime。
	OverheadBytes int64 `json:"overhead_bytes"`
}

const residencyReportSchema = 1

func buildResidencyReport(mode neural.Mode, primary string, allowPlaceholders bool, budgetMB int, budgetSource string,
	r neural.Residency) residencyReport {
	self, workers := processTreeRSS()
	report := residencyReport{
		Schema: residencyReportSchema, Mode: mode, PrimaryLanguage: primary, AllowPlaceholders: allowPlaceholders,
		BudgetMB: budgetMB, BudgetBytes: int64(budgetMB) << 20, BudgetSource: budgetSource,
		ResidentBytes: r.ResidentBytes, PeakBytes: r.PeakBytes, Loaded: r.Loaded, Loading: r.Loading, Failed: r.Failed,
		Models: r.Models, ProcessRSSBytes: self, WorkerRSSBytes: workers, ProcessTreeRSS: self + workers,
	}
	limit := report.BudgetBytes
	if limit > 0 {
		report.WithinBudget = report.ResidentBytes <= limit
		report.RemainingBytes = limit - report.ResidentBytes
		if report.RemainingBytes < 0 {
			report.RemainingBytes = 0
		}
	} else {
		report.WithinBudget = true
	}
	if report.Models == nil {
		report.Models = []neural.ModelStatus{}
	}
	report.OverheadBytes = report.ProcessTreeRSS - report.ResidentBytes
	if report.OverheadBytes < 0 {
		report.OverheadBytes = 0
	}
	return report
}

func writeResidencyReport(w io.Writer, report residencyReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// waitForResidency 等 eager 載入結束（成功或退回），最多等 timeout。延遲載入的模型保持 idle，
// 不為了報告而觸發載入。載入失敗的模型已進入 fallback，Loading 會歸零。
func waitForResidency(m *neural.Manager, timeout time.Duration) neural.Residency {
	deadline := time.Now().Add(timeout)
	for {
		r := m.Residency()
		if r.Loading == 0 || time.Now().After(deadline) {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
}
