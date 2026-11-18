//go:build cgo && onnxruntime

package native

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/0c-biggo/0c-ime/internal/spm"
)

// RerankBackend is the native ONNX Runtime scorer for a 0c.model.manifest/1
// Japanese reranker. It tokenizes text with the package tokenizer and packs the
// fixed right-anchored deployment layout before every batch.
type RerankBackend struct {
	mu      sync.Mutex
	spec    rerankSpec
	encoder *spm.Encoder
	session *ort.DynamicAdvancedSession
	inputs  []string
	outName string
	stats   Stats
	closed  bool
}

func OpenRerank(c RerankConfig) (*RerankBackend, error) {
	if c.Threads < 1 || c.Threads > MaxThreads {
		return nil, fmt.Errorf("%w: invalid threads", ErrPackageContract)
	}
	spec, err := loadRerankSpec(c)
	if err != nil {
		return nil, err
	}
	if err := ensureEnvironment(c.LibraryPath); err != nil {
		return nil, err
	}
	infos, outputs, err := ort.GetInputOutputInfo(spec.modelPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	names := make([]string, len(infos))
	byName := map[string]bool{}
	for i := range infos {
		names[i] = infos[i].Name
		byName[infos[i].Name] = true
	}
	if !byName["input_ids"] || !byName["attention_mask"] {
		return nil, fmt.Errorf("%w: reranker inputs", ErrUnsupportedModel)
	}
	outName := ""
	for _, out := range outputs {
		if out.Name == "scores" {
			outName = out.Name
			break
		}
	}
	if outName == "" {
		return nil, fmt.Errorf("%w: reranker output", ErrUnsupportedModel)
	}
	m, err := spm.ParseModel(spec.tokenizer)
	if err != nil {
		return nil, fmt.Errorf("%w: tokenizer: %v", ErrPackageContract, err)
	}
	encoder, err := spm.NewEncoder(m)
	if err != nil {
		return nil, fmt.Errorf("%w: tokenizer: %v", ErrPackageContract, err)
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuntimeMissing, err)
	}
	defer opts.Destroy()
	if err := opts.SetIntraOpNumThreads(c.Threads); err != nil {
		return nil, err
	}
	if err := opts.SetInterOpNumThreads(1); err != nil {
		return nil, err
	}
	if err := opts.SetExecutionMode(ort.ExecutionModeSequential); err != nil {
		return nil, err
	}
	if err := opts.AddSessionConfigEntry("session.intra_op.allow_spinning", "0"); err != nil {
		return nil, err
	}
	session, err := ort.NewDynamicAdvancedSession(spec.modelPath, names, []string{outName}, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuntimeMissing, err)
	}
	return &RerankBackend{spec: spec, encoder: encoder, session: session, inputs: names, outName: outName}, nil
}

// ScoreTexts returns one relevance score per candidate. Higher is better.
func (b *RerankBackend) ScoreTexts(ctx context.Context, left string, candidates []string) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > 8 {
		return nil, ErrUnsupportedModel
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats.Calls++
	started := time.Now()
	defer func() { b.stats.TotalUS += time.Since(started).Microseconds() }()
	if b.closed {
		b.stats.Unavailable++
		return nil, ErrNativeUnavailable
	}
	ids, mask, seq := rerankBatch(b.encoder, b.spec, left, candidates)
	idT, err := ort.NewTensor(ort.NewShape(int64(len(candidates)), int64(seq)), ids)
	if err != nil {
		b.stats.Errors++
		return nil, err
	}
	defer idT.Destroy()
	maskT, err := ort.NewTensor(ort.NewShape(int64(len(candidates)), int64(seq)), mask)
	if err != nil {
		b.stats.Errors++
		return nil, err
	}
	defer maskT.Destroy()
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(int64(len(candidates))))
	if err != nil {
		b.stats.Errors++
		return nil, err
	}
	defer out.Destroy()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	byName := map[string]ort.Value{"input_ids": idT, "attention_mask": maskT}
	inputs := make([]ort.Value, 0, len(b.inputs))
	for _, name := range b.inputs {
		v, ok := byName[name]
		if !ok {
			b.stats.Errors++
			return nil, ErrUnsupportedModel
		}
		inputs = append(inputs, v)
	}
	if err := b.session.Run(inputs, []ort.Value{out}); err != nil {
		b.stats.Errors++
		return nil, err
	}
	data := out.GetData()
	if len(data) != len(candidates) {
		b.stats.Errors++
		return nil, fmt.Errorf("nonfinite_neural_score")
	}
	scores := make([]float64, len(data))
	for i, v := range data {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			b.stats.Errors++
			return nil, fmt.Errorf("nonfinite_neural_score")
		}
		scores[i] = float64(v)
	}
	b.stats.Scored++
	return scores, nil
}

func (b *RerankBackend) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stats
}

func (b *RerankBackend) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return "closed"
	}
	return "ready"
}

func (b *RerankBackend) Reason() string { return "" }

func (b *RerankBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.session != nil {
		err := b.session.Destroy()
		b.session = nil
		return err
	}
	return nil
}
