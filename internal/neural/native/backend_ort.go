//go:build cgo && onnxruntime

package native

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/internal/neural/native/input"
)

// Available reports whether this binary contains the native ONNX backend.
func Available() bool { return true }

type modelMode int

const (
	modeSequence modelMode = iota + 1
	modeReranker
)

var sequenceInputs = []string{"ids", "bases", "tones", "types", "positions", "mask", "proj_pos", "target_node", "target_ids"}

var (
	envMu    sync.Mutex
	envReady bool
)

// Backend is a native ONNX Runtime scorer for one sequence/reranker package.
type Backend struct {
	mu       sync.Mutex
	manifest manifest
	maps     input.Maps
	session  *ort.DynamicAdvancedSession
	inputs   []string // model input order
	mode     modelMode
	seq      int // reranker token length
	outName  string
	stats    Stats
	closed   bool
}

func Open(c Config) (*Backend, error) {
	if c.Threads < 1 || c.Threads > MaxThreads {
		return nil, fmt.Errorf("%w: invalid threads", ErrPackageContract)
	}
	m, maps, err := loadPackage(c.Package, c.DictionarySHA256)
	if err != nil {
		return nil, err
	}
	if err := ensureEnvironment(c.LibraryPath); err != nil {
		return nil, err
	}
	modelPath := filepath.Join(c.Package, "model.onnx")
	inputs, outputs, err := modelIO(modelPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	mode, seq, err := detectMode(inputs)
	if err != nil {
		return nil, err
	}
	if len(outputs) == 0 {
		return nil, fmt.Errorf("%w: model has no outputs", ErrUnsupportedModel)
	}
	outName := outputs[0].Name
	for _, out := range outputs {
		if out.Name == "scores" {
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

	names := make([]string, len(inputs))
	for i := range inputs {
		names[i] = inputs[i].Name
	}
	session, err := ort.NewDynamicAdvancedSession(modelPath, names, []string{outName}, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuntimeMissing, err)
	}
	return &Backend{manifest: m, maps: maps, session: session, inputs: names, mode: mode, seq: seq, outName: outName}, nil
}

func ensureEnvironment(library string) error {
	envMu.Lock()
	defer envMu.Unlock()
	if envReady {
		return nil
	}
	// The C API reads these when the environment is created. The Python worker
	// sets the same two before importing onnxruntime.
	_ = os.Setenv("ORT_DISABLE_TELEMETRY", "1")
	_ = os.Setenv("ORT_RUNNING_UNIT_TESTS", "1")
	if library != "" {
		ort.SetSharedLibraryPath(library)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		return fmt.Errorf("%w: %v", ErrRuntimeMissing, err)
	}
	_ = ort.DisableTelemetry()
	envReady = true
	return nil
}

// modelIO reads the graph signature from the protobuf, which avoids loading
// the model once only to discover the layout. If the minimal reader cannot
// parse the file, fall back to onnxruntime's own loader.
func modelIO(path string) ([]onnxValue, []onnxValue, error) {
	inputs, outputs, err := readONNXIO(path)
	if err == nil {
		return inputs, outputs, nil
	}
	in, out, ortErr := ort.GetInputOutputInfo(path)
	if ortErr != nil {
		return nil, nil, err
	}
	convert := func(v []ort.InputOutputInfo) []onnxValue {
		vals := make([]onnxValue, len(v))
		for i := range v {
			vals[i] = onnxValue{Name: v[i].Name, ElemType: int32(v[i].DataType), Dims: append([]int64(nil), v[i].Dimensions...)}
		}
		return vals
	}
	return convert(in), convert(out), nil
}

func detectMode(inputs []onnxValue) (modelMode, int, error) {
	names := make(map[string]onnxValue, len(inputs))
	for _, in := range inputs {
		names[in.Name] = in
	}
	if len(inputs) == len(sequenceInputs) {
		ok := true
		for _, name := range sequenceInputs {
			if _, found := names[name]; !found {
				ok = false
				break
			}
		}
		if ok {
			return modeSequence, 0, nil
		}
	}
	if len(inputs) == 2 {
		if _, ok := names["input_ids"]; ok {
			if _, ok := names["attention_mask"]; ok {
				seq := 32
				if d := names["input_ids"].Dims; len(d) == 2 && d[1] > 0 {
					seq = int(d[1])
				}
				return modeReranker, seq, nil
			}
		}
	}
	return 0, 0, ErrUnsupportedModel
}

// Score assembles the same tensors as sequence_runtime.py and returns one
// score per candidate. Multiple branches (neutral-last) take the maximum, as
// the worker does.
func (b *Backend) Score(r rerank.Request) ([]float64, error) {
	return b.ScoreContext(context.Background(), r)
}

func (b *Backend) ScoreContext(ctx context.Context, r rerank.Request) ([]float64, error) {
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
	if b.mode != modeSequence {
		b.stats.Errors++
		return nil, ErrUnsupportedModel
	}
	if !rerank.Supported(r) || r.AllowPartial != b.manifest.AllowPartial ||
		(r.Mode == rerank.ModeText && b.manifest.TextWeight <= 0) {
		b.stats.Errors++
		return nil, input.ErrUnsupportedShape
	}
	count := len(r.Candidates)
	reading := make([]string, len(r.Reading))
	for i, y := range r.Reading {
		reading[i] = string(y)
	}
	last := ""
	if len(reading) > 0 {
		last = reading[len(reading)-1]
	}
	neutral := input.NeutralLast(r.Mode, last, b.manifest.NeutralExpand > 0, b.manifest.NeutralTolerant)
	candidates := make([]input.Candidate, count)
	for i, c := range r.Candidates {
		segs := make([]input.Segment, len(c.Segments))
		for j, s := range c.Segments {
			segs[j] = input.Segment{Start: s.Start, End: s.End, Word: s.Word, Partial: s.Partial, Correction: s.Correction}
		}
		candidates[i] = input.Candidate{Text: c.Text, LogScore: c.LogScore, Segments: segs}
	}
	arrays, err := input.Prepare(input.Request{
		Context:      r.Context,
		Reading:      reading,
		Candidates:   candidates,
		AllowPartial: r.AllowPartial,
		TextOnly:     r.Mode == rerank.ModeText,
		NeutralLast:  neutral,
	}, b.maps)
	if err != nil {
		b.stats.Errors++
		return nil, err
	}
	raw, err := b.runSequence(arrays)
	if err != nil {
		b.stats.Errors++
		return nil, err
	}
	if len(raw) == 0 || len(raw)%count != 0 {
		b.stats.Errors++
		return nil, fmt.Errorf("nonfinite_neural_score")
	}
	for _, v := range raw {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			b.stats.Errors++
			return nil, fmt.Errorf("nonfinite_neural_score")
		}
	}
	out := make([]float64, count)
	for i := range out {
		out[i] = float64(raw[i])
	}
	for row := 1; row < len(raw)/count; row++ {
		for i := 0; i < count; i++ {
			if v := float64(raw[row*count+i]); v > out[i] {
				out[i] = v
			}
		}
	}
	b.stats.Scored++
	return out, nil
}

func (b *Backend) runSequence(a *input.Arrays) ([]float32, error) {
	L := len(a.IDs)
	rows := len(a.TargetNode)
	cols := 0
	if rows > 0 {
		cols = len(a.TargetNode[0])
	}
	mask := make([]bool, L*L)
	for q := 0; q < L; q++ {
		copy(mask[q*L:(q+1)*L], a.Mask[q])
	}
	node := make([]int64, 0, rows*cols)
	ids := make([]int64, 0, rows*cols)
	for i := range a.TargetNode {
		node = append(node, a.TargetNode[i]...)
		ids = append(ids, a.TargetIDs[i]...)
	}

	var tensors []ort.Value
	defer func() {
		for _, t := range tensors {
			_ = t.Destroy()
		}
	}()

	byName := map[string]ort.Value{}
	if err := addTensor(&tensors, byName, "ids", ort.NewShape(1, int64(L)), a.IDs); err != nil {
		return nil, err
	}
	if err := addTensor(&tensors, byName, "bases", ort.NewShape(1, int64(L)), a.Bases); err != nil {
		return nil, err
	}
	if err := addTensor(&tensors, byName, "tones", ort.NewShape(1, int64(L)), a.Tones); err != nil {
		return nil, err
	}
	if err := addTensor(&tensors, byName, "types", ort.NewShape(1, int64(L)), a.Types); err != nil {
		return nil, err
	}
	if err := addTensor(&tensors, byName, "positions", ort.NewShape(1, int64(L)), a.Positions); err != nil {
		return nil, err
	}
	if err := addTensor(&tensors, byName, "mask", ort.NewShape(1, 1, int64(L), int64(L)), mask); err != nil {
		return nil, err
	}
	if err := addTensor(&tensors, byName, "proj_pos", ort.NewShape(int64(len(a.ProjPos))), a.ProjPos); err != nil {
		return nil, err
	}
	if err := addTensor(&tensors, byName, "target_node", ort.NewShape(int64(rows), int64(cols)), node); err != nil {
		return nil, err
	}
	if err := addTensor(&tensors, byName, "target_ids", ort.NewShape(int64(rows), int64(cols)), ids); err != nil {
		return nil, err
	}

	inputs := make([]ort.Value, 0, len(b.inputs))
	for _, name := range b.inputs {
		v, ok := byName[name]
		if !ok {
			return nil, ErrUnsupportedModel
		}
		inputs = append(inputs, v)
	}
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(int64(rows)))
	if err != nil {
		return nil, err
	}
	tensors = append(tensors, out)
	if err := b.session.Run(inputs, []ort.Value{out}); err != nil {
		return nil, err
	}
	return append([]float32(nil), out.GetData()...), nil
}

func addTensor[T ort.TensorData](tensors *[]ort.Value, byName map[string]ort.Value, name string, shape ort.Shape, data []T) error {
	t, err := ort.NewTensor(shape, data)
	if err != nil {
		return err
	}
	*tensors = append(*tensors, t)
	byName[name] = t
	return nil
}

// ScoreTokens runs a reranker-layout model on token ids. It exists so the
// parity harness can feed the same assembled ids to the native backend and the
// Python reference. The product sequence path uses Score.
func (b *Backend) ScoreTokens(tokens []int32, seq, vocabCap int) (float64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, ErrNativeUnavailable
	}
	if b.mode != modeReranker {
		return 0, ErrUnsupportedModel
	}
	if seq <= 0 {
		seq = b.seq
	}
	ids := make([]int64, seq)
	mask := make([]int64, seq)
	n := len(tokens)
	if n > seq {
		n = seq
	}
	for i := 0; i < n; i++ {
		id := tokens[i]
		if vocabCap > 0 {
			id %= int32(vocabCap)
		}
		ids[i] = int64(id)
		mask[i] = 1
	}
	idT, err := ort.NewTensor(ort.NewShape(1, int64(seq)), ids)
	if err != nil {
		return 0, err
	}
	defer idT.Destroy()
	maskT, err := ort.NewTensor(ort.NewShape(1, int64(seq)), mask)
	if err != nil {
		return 0, err
	}
	defer maskT.Destroy()
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1))
	if err != nil {
		return 0, err
	}
	defer out.Destroy()
	byName := map[string]ort.Value{"input_ids": idT, "attention_mask": maskT}
	inputs := make([]ort.Value, 0, len(b.inputs))
	for _, name := range b.inputs {
		v, ok := byName[name]
		if !ok {
			return 0, ErrUnsupportedModel
		}
		inputs = append(inputs, v)
	}
	if err := b.session.Run(inputs, []ort.Value{out}); err != nil {
		b.stats.Errors++
		return 0, err
	}
	return float64(out.GetData()[0]), nil
}

func (b *Backend) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stats
}

func (b *Backend) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return "closed"
	}
	return "ready"
}

func (b *Backend) Reason() string { return "" }

func (b *Backend) Close() error {
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
