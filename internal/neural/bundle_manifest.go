package neural

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ArtifactManifest 是 P2-25／P2-25P 匯出的 0c.model.manifest/1。它與產品內
// 的 sequence/punct 套件 manifest 不同：檔案以 fp32／int8 分開列出，並帶
// placeholder、quality、production_eligible 等出貨判定欄位。
//
// P2-30 只讀它來核對檔案、回報常駐大小與 placeholder 狀態；真正的文字
// adapter 由 P2-31 接上。placeholder 模型永遠不得被當成已訓練模型。
type ArtifactManifest struct {
	Schema             string                  `json:"schema"`
	ArtifactID         string                  `json:"artifact_id"`
	Kind               string                  `json:"kind"`
	Variant            string                  `json:"variant"`
	Quality            string                  `json:"quality"`
	Placeholder        bool                    `json:"placeholder"`
	ProductionEligible bool                    `json:"production_eligible"`
	Trained            bool                    `json:"trained"`
	Files              map[string]ArtifactFile `json:"files"`
	IO                 ArtifactIO              `json:"io"`
}

type ArtifactFile struct {
	Path         string `json:"path"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	Quantization string `json:"quantization"`
}

type ArtifactIO struct {
	Inputs  []ArtifactTensor `json:"inputs"`
	Outputs []ArtifactTensor `json:"outputs"`
}

type ArtifactTensor struct {
	Name        string `json:"name"`
	Dtype       string `json:"dtype"`
	Description string `json:"description"`
	Shape       []any  `json:"shape"`
}

// Artifact 是核對過的 ONNX 檔案。Manifest 保留原始 metadata，讓狀態與紀錄能
// 顯示 placeholder；Path 是實際存在的 int8/fp32 檔案。
type Artifact struct {
	Manifest ArtifactManifest `json:"manifest"`
	Format   string           `json:"format"`
	Path     string           `json:"path"`
	Bytes    int64            `json:"bytes"`
	SHA256   string           `json:"sha256"`
}

// LoadArtifactManifest 讀取並解析 0c.model.manifest/1。檔案不存在回傳
// os.ErrNotExist，JSON 損壞回傳 errCorruptPackage。
func LoadArtifactManifest(path string) (ArtifactManifest, error) {
	var m ArtifactManifest
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("%w: %v", errCorruptPackage, err)
	}
	if m.Schema != "0c.model.manifest/1" || m.Kind == "" || len(m.Files) == 0 {
		return m, errPackageContract
	}
	return m, nil
}

// checkPunctuationArtifact verifies the tensor contract used by the native
// Japanese punctuation scorer before any model is loaded.
func checkPunctuationArtifact(m ArtifactManifest) error {
	if m.Kind != "punctuation" || len(m.IO.Inputs) != 2 || len(m.IO.Outputs) != 1 {
		return errPackageContract
	}
	names := map[string]bool{}
	for _, in := range m.IO.Inputs {
		if in.Dtype != "int64" || len(in.Shape) != 2 {
			return errPackageContract
		}
		names[in.Name] = true
	}
	out := m.IO.Outputs[0]
	if !names["input_ids"] || !names["attention_mask"] || out.Name != "logits" ||
		out.Dtype != "float32" || len(out.Shape) != 3 {
		return errPackageContract
	}
	if n, ok := out.Shape[2].(float64); !ok || int(n) != 4 {
		return errPackageContract
	}
	return nil
}

// checkRerankerArtifact verifies the tensor contract used by the native
// Japanese reranker before any model is loaded.
func checkRerankerArtifact(m ArtifactManifest) error {
	if m.Kind != "reranker" || len(m.IO.Inputs) != 2 || len(m.IO.Outputs) != 1 {
		return errPackageContract
	}
	names := map[string]bool{}
	for _, in := range m.IO.Inputs {
		if in.Dtype != "int64" || len(in.Shape) != 2 {
			return errPackageContract
		}
		names[in.Name] = true
	}
	out := m.IO.Outputs[0]
	if !names["input_ids"] || !names["attention_mask"] || out.Name != "scores" ||
		out.Dtype != "float32" || len(out.Shape) != 1 {
		return errPackageContract
	}
	return nil
}

// ValidateArtifact 選出 int8（沒有時用 fp32），解析相對於 manifest 目錄或
// artifact root 的路徑，並核對大小與 SHA-256。artifactRoot 可為空。
func ValidateArtifact(manifestPath, artifactRoot, format string) (Artifact, error) {
	m, err := LoadArtifactManifest(manifestPath)
	if err != nil {
		return Artifact{}, err
	}
	if format == "" {
		format = "int8"
		if _, ok := m.Files[format]; !ok {
			format = "fp32"
		}
	}
	file, ok := m.Files[format]
	if !ok {
		return Artifact{}, fmt.Errorf("%w: no %s file", errPackageContract, format)
	}
	path, err := resolveArtifactPath(file.Path, filepath.Dir(manifestPath), artifactRoot)
	if err != nil {
		return Artifact{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Artifact{}, err
	}
	if file.Bytes > 0 && info.Size() != file.Bytes {
		return Artifact{}, fmt.Errorf("%w: size %d, want %d", errPackageHashMismatch, info.Size(), file.Bytes)
	}
	digest, err := fileSHA(path)
	if err != nil {
		return Artifact{}, err
	}
	if file.SHA256 != "" && digest != file.SHA256 {
		return Artifact{}, fmt.Errorf("%w: sha256 %s, want %s", errPackageHashMismatch, digest, file.SHA256)
	}
	return Artifact{Manifest: m, Format: format, Path: path, Bytes: info.Size(), SHA256: digest}, nil
}

func resolveArtifactPath(path, manifestDir, root string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	candidates := []string{filepath.Join(manifestDir, path), filepath.Join(filepath.Dir(manifestDir), path)}
	if root != "" {
		candidates = append(candidates, filepath.Join(root, path))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: %s", os.ErrNotExist, path)
}
