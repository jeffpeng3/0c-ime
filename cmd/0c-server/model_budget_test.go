package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/internal/neural"
)

func fixedLoader(size int64, placeholder bool, calls *int) neural.Loader {
	return func(context.Context, neural.ModelSpec, *neural.Prepared) (*neural.Loaded, error) {
		if calls != nil {
			*calls++
		}
		return &neural.Loaded{ResidentBytes: size, Placeholder: placeholder, Quality: "untrained"}, nil
	}
}

func modelErrorReason(t *testing.T, err error) string {
	t.Helper()
	var modelErr *neural.ModelError
	if !errors.As(err, &modelErr) {
		t.Fatalf("應是 *neural.ModelError：%v", err)
	}
	return modelErr.Reason
}

func TestBudgetLoaderRefusesBeforeLoading(t *testing.T) {
	calls := 0
	b := newBudgetLoader(100, false, fixedLoader(80, false, &calls), nil)
	first, err := b.Load(context.Background(), neural.ModelSpec{Name: "a"}, &neural.Prepared{Bytes: 80})
	if err != nil || first.ResidentBytes != 80 || calls != 1 || b.residency() != 80 {
		t.Fatalf("第一個模型應載入：%+v %v calls=%d committed=%d", first, err, calls, b.residency())
	}
	_, err = b.Load(context.Background(), neural.ModelSpec{Name: "b"}, &neural.Prepared{Bytes: 30})
	if reason := modelErrorReason(t, err); reason != ReasonModelBudgetExceeded {
		t.Fatalf("reason %q", reason)
	}
	if calls != 1 {
		t.Fatalf("超過預算的模型不應進入載入器：calls=%d", calls)
	}
	if b.residency() != 80 {
		t.Fatalf("拒絕後不應改變常駐量：%d", b.residency())
	}
}

func TestBudgetLoaderChecksActualSizeAfterLoad(t *testing.T) {
	calls := 0
	b := newBudgetLoader(200, false, fixedLoader(180, false, &calls), nil)
	if _, err := b.Load(context.Background(), neural.ModelSpec{Name: "large"}, nil); err != nil {
		t.Fatal(err)
	}
	_, err := b.Load(context.Background(), neural.ModelSpec{Name: "second"}, nil)
	if reason := modelErrorReason(t, err); reason != ReasonModelBudgetExceeded {
		t.Fatalf("reason %q", reason)
	}
	if b.residency() != 180 {
		t.Fatalf("超過預算的實際大小不應記帳：%d", b.residency())
	}
}

func TestBudgetLoaderRefusesPlaceholdersUnlessAllowed(t *testing.T) {
	calls := 0
	b := newBudgetLoader(1<<30, false, fixedLoader(1, true, &calls), nil)
	prepared := &neural.Prepared{Bytes: 1, Placeholder: true, Quality: "untrained"}
	_, err := b.Load(context.Background(), neural.ModelSpec{Name: "placeholder"}, prepared)
	if reason := modelErrorReason(t, err); reason != ReasonPlaceholderDisallowed {
		t.Fatalf("reason %q", reason)
	}
	if calls != 0 {
		t.Fatalf("不允許 placeholder 時不應載入：calls=%d", calls)
	}

	b = newBudgetLoader(1<<30, true, fixedLoader(1, true, &calls), nil)
	if _, err := b.Load(context.Background(), neural.ModelSpec{Name: "placeholder"}, prepared); err != nil {
		t.Fatalf("允許 placeholder 時應載入：%v", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestManagerBudgetRefusalFallsBackToStatistical(t *testing.T) {
	const limit = 250 << 20
	const size = 130 << 20
	loader := newBudgetLoader(limit, false, fixedLoader(size, false, nil), nil)
	m, err := neural.OpenManager(neural.ManagerConfig{Mode: neural.ModeAll, Loader: loader.Load},
		neural.ModelSpec{Name: "a", Language: "zh", Role: neural.RoleSelect},
		neural.ModelSpec{Name: "b", Language: "zh", Role: neural.RolePunct})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForModelState(t, m, func(r neural.Residency) bool { return r.Loading == 0 && r.Loaded+r.Failed == 2 })
	r := m.Residency()
	if r.Loaded != 1 || r.Failed != 1 || r.ResidentBytes != size || r.PeakBytes != size {
		t.Fatalf("預算拒絕後應只留一個模型：%+v", r)
	}
	reasons := map[string]string{}
	for _, model := range r.Models {
		reasons[model.Name] = model.Reason
	}
	if reasons["a"] != "" && reasons["b"] != "" {
		t.Fatalf("應有一個模型保留統計轉換：%v", reasons)
	}
	for _, name := range []string{"a", "b"} {
		if m.State(name) == "fallback" {
			if reason := m.Reason(name); reason != ReasonModelBudgetExceeded {
				t.Fatalf("%s fallback reason %q", name, reason)
			}
			if _, err := m.ScoreContext(context.Background(), name, rerank.Request{}); !errors.Is(err, neural.ErrUnavailable) {
				t.Fatalf("%s 應維持不可用：%v", name, err)
			}
		}
	}
}

func TestResidencyMeasurementIsStable(t *testing.T) {
	const each = 64 << 20
	m, err := neural.OpenManager(neural.ManagerConfig{Mode: neural.ModeAll, Loader: fixedLoader(each, false, nil)},
		neural.ModelSpec{Name: "a", Language: "zh", Role: neural.RoleSelect},
		neural.ModelSpec{Name: "b", Language: "ja", Role: neural.RoleRerank})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitForModelState(t, m, func(r neural.Residency) bool { return r.Loading == 0 && r.Loaded == 2 })
	want := int64(each * 2)
	for i := 0; i < 100; i++ {
		r := m.Residency()
		if r.ResidentBytes != want {
			t.Fatalf("第 %d 次量測 %d，want %d", i, r.ResidentBytes, want)
		}
		var sum int64
		for _, model := range r.Models {
			sum += model.ResidentBytes
		}
		if sum != r.ResidentBytes {
			t.Fatalf("逐模型加總 %d != 總量 %d", sum, r.ResidentBytes)
		}
	}
	report := buildResidencyReport(neural.ModeAll, "zh", false, 400, "configured", m.Residency())
	if report.ResidentBytes != want || report.BudgetBytes != 400<<20 || report.BudgetSource != "configured" ||
		len(report.Models) != 2 || report.BudgetMB != 400 || !report.WithinBudget || report.RemainingBytes != (400<<20)-want {
		t.Fatalf("報告內容不對：%+v", report)
	}
	var buf bytes.Buffer
	if err := writeResidencyReport(&buf, report); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["resident_bytes"].(float64) != float64(want) {
		t.Fatalf("JSON report: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"models"`) {
		t.Fatalf("報告應含逐模型明細：%s", buf.String())
	}
}

func waitForModelState(t *testing.T, m *neural.Manager, cond func(neural.Residency) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond(m.Residency()) {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for residency: %+v", m.Residency())
		}
		time.Sleep(time.Millisecond)
	}
}
