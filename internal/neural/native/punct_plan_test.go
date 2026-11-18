package native

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/punct"
	"github.com/0c-biggo/0c-ime/internal/spm"
)

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestLoadTokenPunctuationManifest(t *testing.T) {
	dir := t.TempDir()
	model := []byte("fake-onnx")
	if err := os.WriteFile(filepath.Join(dir, "model.onnx"), model, 0o600); err != nil {
		t.Fatal(err)
	}
	tokenizer := filepath.Join(dir, "tokenizer.model")
	if err := os.WriteFile(tokenizer, []byte("fake-spm"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema": "0c.model.manifest/1", "kind": "punctuation", "artifact_id": "punct-test",
		"files": map[string]any{"int8": map[string]any{"path": "model.onnx", "bytes": len(model), "sha256": digest(model)}},
		"io": map[string]any{
			"inputs": []any{
				map[string]any{"name": "input_ids", "dtype": "int64", "shape": []any{"batch", "seq"}},
				map[string]any{"name": "attention_mask", "dtype": "int64", "shape": []any{"batch", "seq"}},
			},
			"outputs": []any{map[string]any{"name": "logits", "dtype": "float32", "shape": []any{"batch", "seq", 4}}},
		},
		"architecture": map[string]any{
			"labels": []string{"none", "comma", "period", "question"},
			"marks":  []string{"", "、", "。", "？"}, "max_positions": 512,
		},
		"training": map[string]any{"seq_len": 192},
		"datasets": []any{map[string]any{"path": tokenizer, "role": "tokenizer"}},
	}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := loadPunctuationSpec(Config{Package: dir})
	if err != nil {
		t.Fatal(err)
	}
	if spec.kind != punctuationTokens || spec.window != 192 || spec.stride != 96 ||
		spec.tokenizer != tokenizer || !reflect.DeepEqual(spec.marks, []string{"", "、", "。", "？"}) {
		t.Fatalf("spec %+v", spec)
	}

	// A wrong output width must be rejected before ONNX Runtime is touched.
	manifest["io"].(map[string]any)["outputs"] = []any{
		map[string]any{"name": "logits", "dtype": "float32", "shape": []any{"batch", "seq", 7}},
	}
	raw, _ = json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600)
	if _, err := loadPunctuationSpec(Config{Package: dir}); err == nil {
		t.Fatal("wrong output shape accepted")
	}
}

func TestLoadCharacterPunctuationManifest(t *testing.T) {
	dir := t.TempDir()
	model := []byte("fake-onnx")
	vocab := []byte(`{"characters":["我","想","吃"]}`)
	os.WriteFile(filepath.Join(dir, "model.onnx"), model, 0o600)
	os.WriteFile(filepath.Join(dir, "vocab.json"), vocab, 0o600)
	manifest := map[string]any{
		"schema": "zuin-punct-runtime-v1", "labels": punctWorkerLabels,
		"threshold": 0.63, "window": 128, "stride": 64, "input": "bidirectional",
		"files": map[string]string{"model.onnx": digest(model), "vocab.json": digest(vocab)},
	}
	raw, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600)
	spec, err := loadPunctuationSpec(Config{Package: dir})
	if err != nil {
		t.Fatal(err)
	}
	if spec.kind != punctuationChars || !spec.bidirectional || spec.window != 128 || spec.threshold != 0.63 ||
		spec.chars['我'] != 8 || spec.chars['吃'] != 10 {
		t.Fatalf("spec %+v", spec)
	}
}

func TestWindowStartsMatchesWorker(t *testing.T) {
	for _, tc := range []struct {
		n, window, stride int
		want              []int
	}{
		{0, 4, 2, nil},
		{3, 4, 2, []int{0}},
		{4, 4, 2, []int{0}},
		{5, 4, 2, []int{0, 1}},
		{10, 4, 2, []int{0, 2, 4, 6}},
		{10, 4, 3, []int{0, 3, 6}},
		{256, 256, 128, []int{0}},
		{257, 256, 128, []int{0, 1}},
	} {
		if got := windowStarts(tc.n, tc.window, tc.stride); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("windowStarts(%d,%d,%d) = %v, want %v", tc.n, tc.window, tc.stride, got, tc.want)
		}
	}
}

func TestMergeWindowRowsPrefersFarthestFromEdge(t *testing.T) {
	// Two overlapping 4-token windows over 6 positions. The middle position
	// must come from the window with the larger edge margin.
	rows := [][][]float32{
		{{1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}},
		{{0, 1, 0, 0}, {0, 1, 0, 0}, {0, 1, 0, 0}, {0, 1, 0, 0}},
	}
	got := mergeWindowRows(6, 4, []int{0, 2}, rows)
	want := []int{0, 0, 0, 1, 1, 1}
	for i := range got {
		label, _ := decision(got[i], []string{"", "、", "。", "？"})
		if label != want[i] {
			t.Fatalf("position %d label %d, want %d (%v)", i, label, want[i], got)
		}
	}
}

func TestTruncationCoversEveryPosition(t *testing.T) {
	const n, window, stride = 601, 256, 128
	starts := windowStarts(n, window, stride)
	covered := make([]bool, n)
	for _, s := range starts {
		for i := s; i < min(s+window, n); i++ {
			covered[i] = true
		}
	}
	for i, ok := range covered {
		if !ok {
			t.Fatalf("position %d is not covered by %v", i, starts)
		}
	}
	if starts[len(starts)-1] != n-window {
		t.Fatalf("last window start %d, want %d", starts[len(starts)-1], n-window)
	}
}

func TestTokenEditsInsertAfterOriginalRunes(t *testing.T) {
	text := "今日は晴れです"
	// Normalisation adds the dummy prefix; each token covers one original rune.
	tokens := []spm.Token{
		{ID: 9, Start: 0, End: 1},
		{ID: 10, Start: 1, End: 2},
		{ID: 11, Start: 2, End: 3},
		{ID: 12, Start: 3, End: 4},
		{ID: 13, Start: 4, End: 5},
		{ID: 14, Start: 5, End: 6},
		{ID: 15, Start: 6, End: 7},
		{ID: 16, Start: 7, End: 8},
	}
	norm := "▁今日は晴れです"
	mapping := []int{-1, 0, 1, 2, 3, 4, 5, 6}
	decisions := []int{0, 0, 0, 1, 0, 0, 0, 2}
	edits := tokenEdits(norm, tokens, mapping, []string{"", "、", "。", "？"}, decisions)
	want := []punct.Edit{{Offset: 3, Text: "、"}, {Offset: 7, Text: "。"}}
	if !reflect.DeepEqual(edits, want) {
		t.Fatalf("edits %v, want %v", edits, want)
	}
	got, ok := applyEdits(text, edits)
	if !ok || got != "今日は、晴れです。" {
		t.Fatalf("applyEdits = %q, %v", got, ok)
	}
	// No-edit guarantee: removing the inserted marks recovers the input exactly.
	if strings.ReplaceAll(strings.ReplaceAll(got, "、", ""), "。", "") != text {
		t.Fatalf("original characters changed: %q", got)
	}
}

func TestTokenEditsSkipWhitespaceAndMaskedPadding(t *testing.T) {
	norm := "▁今日▁は"
	tokens := []spm.Token{
		{ID: 1, Start: 0, End: 1}, // dummy prefix
		{ID: 2, Start: 1, End: 2}, // 今
		{ID: 3, Start: 2, End: 3}, // 日
		{ID: 4, Start: 3, End: 4}, // ▁ space only
		{ID: 5, Start: 4, End: 5}, // は
	}
	mapping := []int{-1, 0, 1, -1, 3}
	// Marks on the dummy/space tokens must not appear; only the real token at
	// the end gets a period. Padding positions are not in tokens/decisions.
	decisions := []int{1, 0, 0, 1, 2}
	edits := tokenEdits(norm, tokens, mapping, []string{"", "、", "。", "？"}, decisions)
	want := []punct.Edit{{Offset: 4, Text: "。"}}
	if !reflect.DeepEqual(edits, want) {
		t.Fatalf("edits %v, want %v", edits, want)
	}
}

func TestEmptyInputAndEmptyWindow(t *testing.T) {
	if got := windowStarts(0, 4, 2); got != nil {
		t.Fatalf("empty text windows %v", got)
	}
	if got := charEdits("", nil, nil, punctWorkerLabels, 0.5); got != nil {
		t.Fatalf("empty char edits %v", got)
	}
	if out, ok := applyEdits("", nil); out != "" || ok {
		t.Fatalf("empty apply = %q, %v", out, ok)
	}
	if out, ok := applyEdits("今日", []punct.Edit{{Offset: 0, Text: "、"}}); out != "今日" || ok {
		t.Fatalf("mark before the first rune must be a no-op: %q, %v", out, ok)
	}
}

func TestCharacterWorkerDecisionRule(t *testing.T) {
	text := "我想吃"
	probs := [][]float32{
		{0.1, 0.9, 0, 0, 0, 0, 0},
		{0.1, 0, 0, 0, 0, 0.8, 0},
		{0.1, 0, 0.9, 0, 0, 0, 0},
	}
	allowed := []bool{true, true, true}
	edits := charEdits(text, probs, allowed, punctWorkerLabels, 0.5)
	want := []punct.Edit{{Offset: 1, Text: "，"}, {Offset: 2, Text: "、"}}
	if !reflect.DeepEqual(edits, want) {
		t.Fatalf("edits %v, want %v", edits, want)
	}
	// The final character is never punctuated by the character worker.
	if len(edits) != 2 {
		t.Fatalf("tail edit appeared: %v", edits)
	}
	// Below the package threshold nothing is inserted.
	if got := charEdits(text, probs, allowed, punctWorkerLabels, 0.95); len(got) != 0 {
		t.Fatalf("threshold ignored: %v", got)
	}
	// A leading 一 must not become 一、.
	enum := [][]float32{{0, 0, 0, 0, 0, 0.9, 0}, {0.9, 0, 0, 0, 0, 0, 0}}
	if got := charEdits("一本", enum, []bool{true, true}, punctWorkerLabels, 0.5); len(got) != 0 {
		t.Fatalf("enumerator mark: %v", got)
	}
}

func TestEligibleBoundariesProtectsURLsAndNonHan(t *testing.T) {
	text := "今日はhttps://example.com/一二三です"
	allowed := eligibleBoundaries(text)
	runes := []rune(text)
	if !allowed[0] {
		t.Fatal("first Han before the URL must be eligible")
	}
	for i, r := range runes[3:26] {
		if allowed[3+i] {
			t.Fatalf("URL rune %q must be protected", string(r))
		}
	}
	plain := eligibleBoundaries("abc一二三")
	for i, r := range []rune("abc一二三") {
		if isHan(r) && !plain[i] {
			t.Fatalf("plain Han %q must be eligible", string(r))
		}
	}
}
