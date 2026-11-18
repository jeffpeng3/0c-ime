package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/punct"
	"github.com/0c-biggo/0c-ime/internal/spm"
)

// punctuationKind selects the tensor contract of a punctuation package.
type punctuationKind int

const (
	// punctuationChars is the zh worker package (zuin-punct-runtime-v1):
	// character ids plus a type tensor for the echo input, or ids only for the
	// bidirectional input.
	punctuationChars punctuationKind = iota + 1
	// punctuationTokens is the 0c.model.manifest/1 Japanese punctuation model:
	// SentencePiece ids, attention mask, four logits per token.
	punctuationTokens
)

// punctuationSpec is the verified, backend-independent description of a
// punctuation package. The ORT and stub builds share this parser.
type punctuationSpec struct {
	kind      punctuationKind
	schema    string
	modelPath string
	modelSHA  string
	modelSize int64
	tokenizer string

	// labels are the model's label names. marks[label] is the character that
	// is inserted after a position; label 0 is always "none" for the token
	// model and the empty string for the character worker.
	labels []string
	marks  []string

	// Character worker fields.
	chars         map[rune]int64
	threshold     float64
	bidirectional bool
	maxText       int

	// Shared window contract.
	window int
	stride int
}

type punctuationArtifactManifest struct {
	Schema       string                       `json:"schema"`
	Kind         string                       `json:"kind"`
	ArtifactID   string                       `json:"artifact_id"`
	Files        map[string]punctArtifactFile `json:"files"`
	IO           punctuationIO                `json:"io"`
	Architecture punctuationArchitecture      `json:"architecture"`
	Training     punctuationTraining          `json:"training"`
	Datasets     []punctuationDataset         `json:"datasets"`
	Placeholder  bool                         `json:"placeholder"`
	Quality      string                       `json:"quality"`
	ProductionOK bool                         `json:"production_eligible"`
}

type punctArtifactFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type punctuationIO struct {
	Inputs  []punctuationTensor `json:"inputs"`
	Outputs []punctuationTensor `json:"outputs"`
}

type punctuationTensor struct {
	Name  string `json:"name"`
	DType string `json:"dtype"`
	Shape []any  `json:"shape"`
}

type punctuationArchitecture struct {
	Labels       []string `json:"labels"`
	Marks        []string `json:"marks"`
	MaxPositions int      `json:"max_positions"`
	VocabSize    int      `json:"vocab_size"`
	PadID        int64    `json:"pad_id"`
}

type punctuationTraining struct {
	SeqLen int `json:"seq_len"`
}

type punctuationDataset struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	SHA256 string `json:"sha256,omitempty"`
}

type punctuationWorkerManifest struct {
	Schema    string            `json:"schema"`
	Labels    []string          `json:"labels"`
	Threshold float64           `json:"threshold"`
	Window    int               `json:"window"`
	Stride    int               `json:"stride"`
	Input     string            `json:"input"`
	MaxText   int               `json:"max_text_runes"`
	Files     map[string]string `json:"files"`
}

const (
	punctWorkerSchema  = "zuin-punct-runtime-v1"
	artifactManifest   = "0c.model.manifest/1"
	punctWindowDefault = 256
	punctMaxPositions  = 512
)

var punctWorkerLabels = []string{"", "，", "。", "？", "！", "、", "；"}

// loadPunctuationSpec reads the manifest and verifies the model file. It does
// not touch ONNX Runtime, so the same checks run in CGO-free unit tests.
func loadPunctuationSpec(c Config) (punctuationSpec, error) {
	raw, err := os.ReadFile(filepath.Join(c.Package, "manifest.json"))
	if err != nil {
		return punctuationSpec{}, err
	}
	var head struct {
		Schema string `json:"schema"`
		Kind   string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return punctuationSpec{}, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	switch head.Schema {
	case punctWorkerSchema:
		return loadCharacterPunctuation(c.Package, raw)
	case artifactManifest:
		if head.Kind != "punctuation" {
			return punctuationSpec{}, fmt.Errorf("%w: kind %q", ErrUnsupportedModel, head.Kind)
		}
		return loadTokenPunctuation(c, raw)
	}
	return punctuationSpec{}, fmt.Errorf("%w: schema %q", ErrPackageContract, head.Schema)
}

func loadCharacterPunctuation(pkg string, raw []byte) (punctuationSpec, error) {
	var m punctuationWorkerManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return punctuationSpec{}, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	if m.Schema != punctWorkerSchema || !sameStrings(m.Labels, punctWorkerLabels) {
		return punctuationSpec{}, ErrPackageContract
	}
	limit := 0
	switch m.Input {
	case "", "echo":
		limit = 64
	case "bidirectional":
		limit = 128
	default:
		return punctuationSpec{}, ErrPackageContract
	}
	if !(m.Threshold > 0 && m.Threshold < 1) || m.Window < 8 || m.Window > limit ||
		m.Stride <= 0 || m.Stride > m.Window {
		return punctuationSpec{}, ErrPackageContract
	}
	modelPath := filepath.Join(pkg, "model.onnx")
	if m.Files["model.onnx"] == "" || m.Files["vocab.json"] == "" {
		return punctuationSpec{}, ErrPackageContract
	}
	modelSHA, err := verifyPunctFile(modelPath, m.Files["model.onnx"])
	if err != nil {
		return punctuationSpec{}, err
	}
	vocabPath := filepath.Join(pkg, "vocab.json")
	if _, err := verifyPunctFile(vocabPath, m.Files["vocab.json"]); err != nil {
		return punctuationSpec{}, err
	}
	var vocab struct {
		Characters []string `json:"characters"`
	}
	vraw, err := os.ReadFile(vocabPath)
	if err != nil {
		return punctuationSpec{}, err
	}
	if err := json.Unmarshal(vraw, &vocab); err != nil {
		return punctuationSpec{}, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	chars := make(map[rune]int64, len(vocab.Characters))
	for i, s := range vocab.Characters {
		r := []rune(s)
		if len(r) == 1 {
			chars[r[0]] = int64(i + 8)
		}
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return punctuationSpec{}, err
	}
	return punctuationSpec{
		kind: punctuationChars, schema: m.Schema, modelPath: modelPath, modelSHA: modelSHA,
		modelSize: info.Size(), labels: append([]string(nil), m.Labels...),
		marks: append([]string(nil), m.Labels...), chars: chars, threshold: m.Threshold,
		bidirectional: m.Input == "bidirectional", window: m.Window, stride: m.Stride,
		maxText: 256,
	}, nil
}

func loadTokenPunctuation(c Config, raw []byte) (punctuationSpec, error) {
	var m punctuationArtifactManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return punctuationSpec{}, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	if m.Schema != artifactManifest || m.Kind != "punctuation" ||
		len(m.Architecture.Labels) != 4 || len(m.Architecture.Marks) != 4 ||
		m.Architecture.Labels[0] != "none" || m.Architecture.Marks[0] != "" {
		return punctuationSpec{}, ErrPackageContract
	}
	if err := checkTokenIO(m.IO); err != nil {
		return punctuationSpec{}, err
	}
	file, ok := m.Files["int8"]
	if !ok || file.Path == "" {
		file, ok = m.Files["fp32"]
	}
	if !ok || file.Path == "" {
		return punctuationSpec{}, fmt.Errorf("%w: no model file", ErrPackageContract)
	}
	modelPath, err := resolvePunctPath(file.Path, c.Package)
	if err != nil {
		return punctuationSpec{}, err
	}
	modelSHA, err := verifyPunctFile(modelPath, file.SHA256)
	if err != nil {
		return punctuationSpec{}, err
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return punctuationSpec{}, err
	}
	tokenizer := findTokenizer(c.Package, c.Tokenizer, m.Datasets)
	if tokenizer == "" {
		return punctuationSpec{}, fmt.Errorf("%w: tokenizer model is not installed", ErrPackageContract)
	}
	maxPos := m.Architecture.MaxPositions
	if maxPos <= 0 || maxPos > punctMaxPositions {
		maxPos = punctMaxPositions
	}
	window := m.Training.SeqLen
	if window < 8 || window > maxPos {
		window = min(punctWindowDefault, maxPos)
	}
	stride := window / 2
	if stride < 1 {
		stride = 1
	}
	return punctuationSpec{
		kind: punctuationTokens, schema: m.Schema, modelPath: modelPath, modelSHA: modelSHA,
		modelSize: info.Size(), tokenizer: tokenizer,
		labels: append([]string(nil), m.Architecture.Labels...),
		marks:  append([]string(nil), m.Architecture.Marks...),
		window: window, stride: stride, maxText: 1 << 20,
	}, nil
}

func checkTokenIO(io punctuationIO) error {
	if len(io.Inputs) != 2 {
		return fmt.Errorf("%w: expected input_ids and attention_mask", ErrUnsupportedModel)
	}
	seen := map[string]bool{}
	for _, in := range io.Inputs {
		if in.DType != "int64" || len(in.Shape) != 2 {
			return fmt.Errorf("%w: input %s is not int64 [batch, seq]", ErrUnsupportedModel, in.Name)
		}
		seen[in.Name] = true
	}
	if !seen["input_ids"] || !seen["attention_mask"] {
		return fmt.Errorf("%w: expected input_ids and attention_mask", ErrUnsupportedModel)
	}
	if len(io.Outputs) != 1 {
		return fmt.Errorf("%w: expected one logits output", ErrUnsupportedModel)
	}
	out := io.Outputs[0]
	if out.Name == "" || out.DType != "float32" || len(out.Shape) != 3 {
		return fmt.Errorf("%w: output is not float32 [batch, seq, 4]", ErrUnsupportedModel)
	}
	if n, ok := out.Shape[2].(float64); !ok || int(n) != 4 {
		return fmt.Errorf("%w: output is not [batch, seq, 4]", ErrUnsupportedModel)
	}
	return nil
}

func resolvePunctPath(path, pkg string) (string, error) {
	if filepath.IsAbs(path) {
		if regularFile(path) {
			return path, nil
		}
		return "", fmt.Errorf("%w: %s", os.ErrNotExist, path)
	}
	for _, base := range []string{pkg, filepath.Dir(filepath.Clean(pkg))} {
		candidate := filepath.Join(base, filepath.FromSlash(path))
		if regularFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: %s", os.ErrNotExist, path)
}

func findTokenizer(pkg, explicit string, datasets []punctuationDataset) string {
	var candidates []string
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	for _, d := range datasets {
		if d.Role != "tokenizer" || d.Path == "" {
			continue
		}
		candidates = append(candidates, d.Path,
			filepath.Join(pkg, filepath.FromSlash(d.Path)),
			filepath.Join(filepath.Dir(filepath.Clean(pkg)), filepath.FromSlash(d.Path)),
			filepath.Join(pkg, filepath.Base(d.Path)))
	}
	for _, name := range []string{"tokenizer.model", "public_core_ja.model", "spm.model", "vocab.model",
		filepath.Join("tokenizer", "public_core_ja.model")} {
		candidates = append(candidates, filepath.Join(pkg, name))
	}
	for _, candidate := range candidates {
		if regularFile(candidate) {
			return candidate
		}
	}
	return ""
}

func verifyPunctFile(path, want string) (string, error) {
	digest, err := fileSHA(path)
	if err != nil {
		return "", err
	}
	if want != "" && digest != want {
		return "", fmt.Errorf("%w: %s", ErrPackageHash, path)
	}
	return digest, nil
}

func regularFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// windowStarts mirrors the reference implementation:
//
//	[0] if n <= window, else range(0, n-window, stride) + [n-window]
func windowStarts(n, window, stride int) []int {
	if n <= 0 || window <= 0 {
		return nil
	}
	if n <= window {
		return []int{0}
	}
	if stride <= 0 {
		stride = 1
	}
	out := make([]int, 0, (n-window)/stride+2)
	for s := 0; s < n-window; s += stride {
		out = append(out, s)
	}
	return append(out, n-window)
}

// mergeWindowRows picks, for every position, the row from the window whose
// position is farthest from an edge. It is the reference margin rule.
func mergeWindowRows(n, window int, starts []int, rows [][][]float32) [][]float32 {
	out := make([][]float32, n)
	best := make([]int, n)
	for i := range best {
		best[i] = -1
	}
	for wi, start := range starts {
		if wi >= len(rows) {
			break
		}
		m := len(rows[wi])
		for j := 0; j < m && start+j < n; j++ {
			margin := 0
			if n > window {
				margin = min(j, m-1-j)
			}
			if margin > best[start+j] {
				best[start+j] = margin
				out[start+j] = rows[wi][j]
			}
		}
	}
	return out
}

// decision returns the winning label and the mark to insert. Label 0 is none.
func decision(row []float32, marks []string) (int, string) {
	if len(row) == 0 {
		return 0, ""
	}
	best := 0
	for i := 1; i < len(row); i++ {
		if row[i] > row[best] {
			best = i
		}
	}
	if best >= len(marks) {
		return 0, ""
	}
	return best, marks[best]
}

// charEdits ports Runtime.edits for the character worker. The caller supplies
// per-position probabilities (rows[0] is character 0).
func charEdits(text string, probs [][]float32, allowed []bool, marks []string, threshold float64) []punct.Edit {
	if len(text) == 0 || len(probs) != len([]rune(text)) || !sameStrings(marks, punctWorkerLabels) {
		return nil
	}
	var out []punct.Edit
	for i := 0; i < len([]rune(text))-1; i++ {
		label, mark := decision(probs[i][1:], marks[1:])
		label++
		if i == 0 && mark == "、" && isEnumerator([]rune(text)[0]) {
			continue
		}
		if i < len(allowed) && allowed[i] && probs[i][label] >= float32(threshold) {
			out = append(out, punct.Edit{Offset: i + 1, Text: mark})
		}
	}
	return out
}

var enumerators = map[rune]bool{}

func init() {
	for _, r := range "一二三四五六七八九十壹貳參肆伍陸柒捌玖拾甲乙丙丁戊己庚辛壬癸" {
		enumerators[r] = true
	}
}

func isEnumerator(r rune) bool { return enumerators[r] }

// tokenEdits maps per-token decisions back to original rune offsets. tokens
// and normMap both refer to the normalised string; only spans that map exactly
// to original runes can receive a mark.
func tokenEdits(norm string, tokens []spm.Token, normMap []int, marks []string, decisions []int) []punct.Edit {
	normRunes := []rune(norm)
	seen := map[int]bool{}
	var out []punct.Edit
	for i, tok := range tokens {
		if i >= len(decisions) || tok.Byte {
			continue
		}
		label := decisions[i]
		if label <= 0 || label >= len(marks) {
			continue
		}
		if tok.Start < 0 || tok.End > len(normRunes) || tok.Start >= tok.End {
			continue
		}
		surface := strings.ReplaceAll(string(normRunes[tok.Start:tok.End]), "▁", " ")
		if strings.TrimSpace(surface) == "" {
			continue
		}
		offset := originalOffset(tok.End, normMap)
		if offset < 0 || seen[offset] {
			continue
		}
		seen[offset] = true
		out = append(out, punct.Edit{Offset: offset, Text: marks[label]})
	}
	return out
}

func originalOffset(normEnd int, mapping []int) int {
	for i := normEnd - 1; i >= 0; i-- {
		if i < len(mapping) && mapping[i] >= 0 {
			return mapping[i] + 1
		}
	}
	return -1
}

// applyEdits inserts validated punctuation without rewriting any original
// rune. Invalid edits make the whole batch a no-op.
func applyEdits(text string, edits []punct.Edit) (string, bool) {
	if len(edits) == 0 {
		return text, false
	}
	runes := []rune(text)
	last := -1
	for _, e := range edits {
		if e.Offset <= 0 || e.Offset > len(runes) || e.Offset < last || !isMark(e.Text) {
			return text, false
		}
		last = e.Offset
	}
	var b strings.Builder
	b.Grow(len(text) + len(edits)*3)
	at := 0
	for i := 0; i <= len(runes); i++ {
		for at < len(edits) && edits[at].Offset == i {
			b.WriteString(edits[at].Text)
			at++
		}
		if i < len(runes) {
			b.WriteRune(runes[i])
		}
	}
	return b.String(), true
}

func isMark(s string) bool {
	r := []rune(s)
	if len(r) != 1 {
		return false
	}
	return unicode.IsPunct(r[0])
}

// eligibleBoundaries is a line-for-line port of the reference punctuation runtime.
func eligibleBoundaries(text string) []bool {
	protected := map[int]bool{}
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		low := strings.ToLower(string(runes[i:]))
		if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") && !strings.HasPrefix(low, "www.") {
			continue
		}
		j := i
		for j < len(runes) && !strings.ContainsRune("，。！？、；「」『』（）【】", runes[j]) &&
			!unicode.IsSpace(runes[j]) {
			j++
		}
		for k := i; k < j; k++ {
			protected[k] = true
		}
		if j > i {
			i = j - 1
		}
	}
	allowed := make([]bool, len(runes))
	for i, r := range runes {
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		nextVisible := rune(0)
		for j := i + 1; j < len(runes); j++ {
			if unicode.IsSpace(runes[j]) {
				continue
			}
			nextVisible = runes[j]
			break
		}
		allowed[i] = isHan(r) && !protected[i] &&
			(next == 0 || isHan(next) || unicode.IsSpace(next)) &&
			(nextVisible == 0 || isHan(nextVisible) || (nextVisible < 128 && (unicode.IsLetter(nextVisible) || unicode.IsDigit(nextVisible))))
	}
	return allowed
}

func isHan(r rune) bool {
	return (r >= 0x3400 && r <= 0x9FFF) || (r >= 0x20000 && r <= 0x323AF)
}
