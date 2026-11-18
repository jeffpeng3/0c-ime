//go:build cgo && onnxruntime

package native

import (
	"context"
	"fmt"
	"sync"
	"time"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/0c-biggo/0c-ime/core/punct"
	"github.com/0c-biggo/0c-ime/internal/spm"
)

// PunctBackend is a native ONNX Runtime scorer for a punctuation package. It
// accepts both the zh worker package (zuin-punct-runtime-v1) and the trained
// Japanese 0c.model.manifest/1 punctuation model.
type PunctBackend struct {
	mu      sync.Mutex
	spec    punctuationSpec
	session *ort.DynamicAdvancedSession
	inputs  []string
	outName string
	classes int
	encoder *spm.Encoder

	stats  Stats
	closed bool
}

// OpenPunct verifies a punctuation package and creates its ONNX session.
func OpenPunct(c Config) (*PunctBackend, error) {
	if c.Threads < 1 || c.Threads > MaxThreads {
		return nil, fmt.Errorf("%w: invalid threads", ErrPackageContract)
	}
	spec, err := loadPunctuationSpec(c)
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
	var encoder *spm.Encoder
	switch spec.kind {
	case punctuationTokens:
		if !byName["input_ids"] || !byName["attention_mask"] {
			return nil, fmt.Errorf("%w: punctuation model inputs", ErrUnsupportedModel)
		}
		m, err := spm.ParseModel(spec.tokenizer)
		if err != nil {
			return nil, fmt.Errorf("%w: tokenizer: %v", ErrPackageContract, err)
		}
		encoder, err = spm.NewEncoder(m)
		if err != nil {
			return nil, fmt.Errorf("%w: tokenizer: %v", ErrPackageContract, err)
		}
	case punctuationChars:
		if !byName["ids"] {
			return nil, fmt.Errorf("%w: character punctuation model inputs", ErrUnsupportedModel)
		}
		if !spec.bidirectional && !byName["types"] {
			return nil, fmt.Errorf("%w: echo punctuation model needs ids and types", ErrUnsupportedModel)
		}
	}
	if len(outputs) == 0 {
		return nil, fmt.Errorf("%w: model has no outputs", ErrUnsupportedModel)
	}
	outName := outputs[0].Name
	for _, out := range outputs {
		if out.Name == "logits" || out.Name == "probs" {
			outName = out.Name
			break
		}
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
	return &PunctBackend{spec: spec, session: session, inputs: names, outName: outName,
		classes: len(spec.labels), encoder: encoder}, nil
}

// Punctuate returns insertions for text. It never edits or removes an
// original character; applying the edits can only add punctuation.
func (b *PunctBackend) Punctuate(text string) ([]punct.Edit, error) {
	return b.PunctuateContext(context.Background(), text)
}

func (b *PunctBackend) PunctuateContext(ctx context.Context, text string) ([]punct.Edit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
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
	if text == "" {
		return nil, nil
	}
	if len([]rune(text)) > b.spec.maxText {
		b.stats.Errors++
		return nil, fmt.Errorf("unsupported_text")
	}
	var edits []punct.Edit
	var err error
	switch b.spec.kind {
	case punctuationChars:
		edits, err = b.charAt(ctx, text)
	case punctuationTokens:
		edits, err = b.tokenAt(ctx, text)
	default:
		err = ErrUnsupportedModel
	}
	if err != nil {
		b.stats.Errors++
		return nil, err
	}
	b.stats.Scored++
	return edits, nil
}

// TokenLogits runs one packed token window. It is used by the parity harness
// so the Go and Python sides can compare the same model output.
func (b *PunctBackend) TokenLogits(ids, mask []int64) ([][]float32, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrNativeUnavailable
	}
	if b.spec.kind != punctuationTokens {
		return nil, ErrUnsupportedModel
	}
	if len(ids) == 0 || len(ids) != len(mask) {
		return nil, fmt.Errorf("invalid token window")
	}
	return b.runTokens(ids, mask)
}

// Window and Stride expose the package's sliding-window contract.
func (b *PunctBackend) Window() int { return b.spec.window }
func (b *PunctBackend) Stride() int { return b.spec.stride }

// PunctuateText applies the edits to the original string. The second result is
// false when the model inserted nothing (the original text is returned).
func (b *PunctBackend) PunctuateText(text string) (string, error) {
	edits, err := b.Punctuate(text)
	if err != nil {
		return text, err
	}
	out, _ := applyEdits(text, edits)
	return out, nil
}

func (b *PunctBackend) charAt(ctx context.Context, text string) ([]punct.Edit, error) {
	allowed := eligibleBoundaries(text)
	anyAllowed := false
	for i := 0; i+1 < len(allowed); i++ {
		if allowed[i] {
			anyAllowed = true
			break
		}
	}
	if !anyAllowed {
		return nil, nil
	}
	runes := []rune(text)
	n := len(runes)
	starts := windowStarts(n, b.spec.window, b.spec.stride)
	rows := make([][][]float32, 0, len(starts))
	for _, start := range starts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(start+b.spec.window, n)
		window, err := b.runChars(runes[start:end])
		if err != nil {
			return nil, err
		}
		rows = append(rows, window)
	}
	probs := mergeWindowRows(n, b.spec.window, starts, rows)
	return charEdits(text, probs, allowed, b.spec.marks, b.spec.threshold), nil
}

func (b *PunctBackend) tokenAt(ctx context.Context, text string) ([]punct.Edit, error) {
	tokens, norm := b.encoder.EncodeTokens(text)
	if len(tokens) == 0 {
		return nil, nil
	}
	_, mapping := b.encoder.NormalizeMap(text)
	n := len(tokens)
	starts := windowStarts(n, b.spec.window, b.spec.stride)
	rows := make([][][]float32, 0, len(starts))
	for _, start := range starts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(start+b.spec.window, n)
		windowIDs := make([]int64, b.spec.window)
		mask := make([]int64, b.spec.window)
		for i := start; i < end; i++ {
			windowIDs[i-start] = int64(tokens[i].ID)
			mask[i-start] = 1
		}
		window, err := b.runTokens(windowIDs, mask)
		if err != nil {
			return nil, err
		}
		rows = append(rows, window)
	}
	merged := mergeWindowRows(n, b.spec.window, starts, rows)
	decisions := make([]int, n)
	for i := range decisions {
		decisions[i], _ = decision(merged[i], b.spec.marks)
	}
	return tokenEdits(norm, tokens, mapping, b.spec.marks, decisions), nil
}

func (b *PunctBackend) runTokens(ids, mask []int64) ([][]float32, error) {
	seq := len(ids)
	idT, err := ort.NewTensor(ort.NewShape(1, int64(seq)), ids)
	if err != nil {
		return nil, err
	}
	defer idT.Destroy()
	maskT, err := ort.NewTensor(ort.NewShape(1, int64(seq)), mask)
	if err != nil {
		return nil, err
	}
	defer maskT.Destroy()
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(seq), int64(b.classes)))
	if err != nil {
		return nil, err
	}
	defer out.Destroy()
	byName := map[string]ort.Value{"input_ids": idT, "attention_mask": maskT}
	inputs := make([]ort.Value, 0, len(b.inputs))
	for _, name := range b.inputs {
		v, ok := byName[name]
		if !ok {
			return nil, ErrUnsupportedModel
		}
		inputs = append(inputs, v)
	}
	if err := b.session.Run(inputs, []ort.Value{out}); err != nil {
		return nil, err
	}
	data := out.GetData()
	rows := make([][]float32, seq)
	for i := 0; i < seq; i++ {
		row := make([]float32, b.classes)
		copy(row, data[i*b.classes:(i+1)*b.classes])
		rows[i] = row
	}
	return rows, nil
}

func (b *PunctBackend) runChars(src []rune) ([][]float32, error) {
	ids := make([]int64, len(src))
	for i, r := range src {
		id, ok := b.spec.chars[r]
		if !ok {
			id = 4
		}
		ids[i] = id
	}
	var seq int
	var feed []int64
	var types []int64
	if b.spec.bidirectional {
		seq = len(ids) + 2
		feed = make([]int64, 0, seq)
		feed = append(feed, 1, 7)
		feed = append(feed, ids...)
	} else {
		seq = 2*len(ids) + 3
		feed = make([]int64, 0, seq)
		feed = append(feed, 1, 7)
		feed = append(feed, ids...)
		feed = append(feed, 2)
		feed = append(feed, ids...)
		types = make([]int64, seq)
		for i := len(ids) + 3; i < seq; i++ {
			types[i] = 2
		}
	}
	idT, err := ort.NewTensor(ort.NewShape(1, int64(seq)), feed)
	if err != nil {
		return nil, err
	}
	defer idT.Destroy()
	byName := map[string]ort.Value{"ids": idT}
	tensors := []ort.Value{idT}
	if !b.spec.bidirectional {
		typeT, err := ort.NewTensor(ort.NewShape(1, int64(seq)), types)
		if err != nil {
			return nil, err
		}
		defer typeT.Destroy()
		tensors = append(tensors, typeT)
		byName["types"] = typeT
	}
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(seq), int64(b.classes)))
	if err != nil {
		return nil, err
	}
	defer out.Destroy()
	inputs := make([]ort.Value, 0, len(b.inputs))
	for _, name := range b.inputs {
		v, ok := byName[name]
		if !ok {
			return nil, ErrUnsupportedModel
		}
		inputs = append(inputs, v)
	}
	if err := b.session.Run(inputs, []ort.Value{out}); err != nil {
		return nil, err
	}
	data := out.GetData()
	first := 2
	if !b.spec.bidirectional {
		first = len(ids) + 3
	}
	rows := make([][]float32, len(ids))
	for i := range rows {
		off := (first + i) * b.classes
		row := make([]float32, b.classes)
		copy(row, data[off:off+b.classes])
		rows[i] = row
	}
	return rows, nil
}

func (b *PunctBackend) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stats
}

func (b *PunctBackend) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return "closed"
	}
	return "ready"
}

func (b *PunctBackend) Reason() string { return "" }

func (b *PunctBackend) Close() error {
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
