package native

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0c-biggo/0c-ime/internal/spm"
)

// RerankConfig describes one 0c.model.manifest/1 Japanese reranker package.
// The layout matches the exported artifact: <bos> left <eos> candidate <eos>
// right, packed to the deployment sequence length. The product has no right
// context; the candidate slot is kept fixed so the model always reads the same
// trained pooling position.
type RerankConfig struct {
	Package     string
	Tokenizer   string
	Threads     int
	LibraryPath string
}

type rerankArtifactFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type rerankArtifactManifest struct {
	Schema       string                        `json:"schema"`
	Kind         string                        `json:"kind"`
	ArtifactID   string                        `json:"artifact_id"`
	Files        map[string]rerankArtifactFile `json:"files"`
	IO           rerankIO                      `json:"io"`
	Architecture rerankArchitecture            `json:"architecture"`
	Datasets     []punctuationDataset          `json:"datasets"`
	Placeholder  bool                          `json:"placeholder"`
	Quality      string                        `json:"quality"`
	Trained      bool                          `json:"trained"`
}

type rerankIO struct {
	Inputs     []punctuationTensor `json:"inputs"`
	Outputs    []punctuationTensor `json:"outputs"`
	Deployment rerankDeployment    `json:"deployment"`
}

type rerankDeployment struct {
	SeqLen               int `json:"seq_len"`
	CandidateSlot        int `json:"candidate_slot"`
	CandidatePosition    int `json:"candidate_position"`
	ContextTokensPerSide int `json:"deployment_context_tokens_per_side"`
}

type rerankArchitecture struct {
	Kind      string `json:"kind"`
	VocabSize int    `json:"vocab_size"`
	BosID     int64  `json:"bos_id"`
	EosID     int64  `json:"eos_id"`
	PaddingID int64  `json:"padding_id"`
	PoolIndex int    `json:"pool_index"`
}

// rerankSpec is the verified, executor-independent layout of a reranker.
type rerankSpec struct {
	modelPath   string
	modelSHA    string
	modelSize   int64
	tokenizer   string
	seq         int
	slot        int
	context     int
	bos         int64
	eos         int64
	pad         int64
	vocab       int
	placeholder bool
	quality     string
}

func loadRerankSpec(c RerankConfig) (rerankSpec, error) {
	raw, err := os.ReadFile(filepath.Join(c.Package, "manifest.json"))
	if err != nil {
		return rerankSpec{}, err
	}
	var m rerankArtifactManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return rerankSpec{}, fmt.Errorf("%w: %v", ErrPackageCorrupt, err)
	}
	if m.Schema != artifactManifest || m.Kind != "reranker" {
		return rerankSpec{}, ErrPackageContract
	}
	if err := checkRerankIO(m.IO); err != nil {
		return rerankSpec{}, err
	}
	file, ok := m.Files["int8"]
	if !ok || file.Path == "" {
		file, ok = m.Files["fp32"]
	}
	if !ok || file.Path == "" {
		return rerankSpec{}, fmt.Errorf("%w: no model file", ErrPackageContract)
	}
	modelPath, err := resolvePunctPath(file.Path, c.Package)
	if err != nil {
		return rerankSpec{}, err
	}
	modelSHA, err := verifyPunctFile(modelPath, file.SHA256)
	if err != nil {
		return rerankSpec{}, err
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return rerankSpec{}, err
	}
	tokenizer, err := verifiedTokenizer(c.Package, c.Tokenizer, m.Datasets)
	if err != nil {
		return rerankSpec{}, err
	}

	deployment := m.IO.Deployment
	seq := deployment.SeqLen
	if seq <= 0 {
		seq = 124
	}
	slot := deployment.CandidateSlot
	if slot <= 0 {
		slot = 62
	}
	context := deployment.ContextTokensPerSide
	if context <= 0 {
		context = 60
	}
	if m.Architecture.BosID == 0 {
		m.Architecture.BosID = 2
	}
	if m.Architecture.EosID == 0 {
		m.Architecture.EosID = 3
	}
	if slot <= 0 || slot >= seq-1 || context <= 0 || context > slot {
		return rerankSpec{}, ErrPackageContract
	}
	if m.Architecture.VocabSize <= 0 {
		m.Architecture.VocabSize = 32768
	}
	return rerankSpec{
		modelPath: modelPath, modelSHA: modelSHA, modelSize: info.Size(), tokenizer: tokenizer,
		seq: seq, slot: slot, context: context,
		bos: m.Architecture.BosID, eos: m.Architecture.EosID, pad: m.Architecture.PaddingID,
		vocab: m.Architecture.VocabSize, placeholder: m.Placeholder, quality: m.Quality,
	}, nil
}

func checkRerankIO(io rerankIO) error {
	if len(io.Inputs) != 2 || len(io.Outputs) != 1 {
		return fmt.Errorf("%w: expected input_ids/attention_mask and scores", ErrUnsupportedModel)
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
	out := io.Outputs[0]
	if out.Name != "scores" || out.DType != "float32" || len(out.Shape) != 1 {
		return fmt.Errorf("%w: reranker output must be float32 scores [batch]", ErrUnsupportedModel)
	}
	return nil
}

// verifiedTokenizer finds the SentencePiece model and, when the manifest names
// a tokenizer dataset with a hash, verifies it before the model is loaded.
func verifiedTokenizer(pkg, explicit string, datasets []punctuationDataset) (string, error) {
	path := findTokenizer(pkg, explicit, datasets)
	if path == "" {
		return "", fmt.Errorf("%w: tokenizer model is not installed", ErrPackageContract)
	}
	for _, d := range datasets {
		if d.Role != "tokenizer" || d.SHA256 == "" {
			continue
		}
		got, err := verifyPunctFile(path, d.SHA256)
		if err != nil {
			return "", err
		}
		if got != d.SHA256 {
			return "", ErrPackageHash
		}
	}
	return path, nil
}

// rerankBatch assembles the fixed deployment layout for each candidate:
//
//	<bos> [pad... left] <eos> candidate <eos> [right][pad]
//
// Left context is right-aligned so the last real token is adjacent to the
// separator and the candidate always starts at the trained slot. There is no
// lookahead in an IME, so right context is empty and only padding follows.
func rerankBatch(enc *spm.Encoder, spec rerankSpec, left string, candidates []string) (ids, mask []int64, seq int) {
	seq = spec.seq
	ids = make([]int64, len(candidates)*seq)
	mask = make([]int64, len(candidates)*seq)
	leftIDs := capIDs(enc.Encode(left), spec.context)
	for ci, text := range candidates {
		row := ids[ci*seq : (ci+1)*seq]
		maskRow := mask[ci*seq : (ci+1)*seq]
		row[0], maskRow[0] = spec.bos, 1
		// Right-align the left context in slots 1..context.
		start := 1 + spec.context - len(leftIDs)
		for i, id := range leftIDs {
			pos := start + i
			row[pos], maskRow[pos] = int64(id), 1
		}
		row[spec.slot-1], maskRow[spec.slot-1] = spec.eos, 1
		cand := capIDs(enc.Encode(text), spec.seq-spec.slot-1)
		pos := spec.slot
		for _, id := range cand {
			row[pos], maskRow[pos] = int64(id), 1
			pos++
		}
		if pos < seq {
			row[pos], maskRow[pos] = spec.eos, 1
			pos++
		}
	}
	return ids, mask, seq
}

func capIDs(ids []int32, limit int) []int32 {
	if limit <= 0 {
		return nil
	}
	if len(ids) > limit {
		return ids[:limit]
	}
	return ids
}

func rerankSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
