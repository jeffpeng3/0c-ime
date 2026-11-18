package modelsets

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// P2-25P placeholder 測試 fixture（一個模型包：placeholder reranker 加 punctuation）。
// 預設跳過；設定 OC_PLACEHOLDER_MODELS=/path/to/phase2/reference/models 後，會用
// testdata/models/placeholder/pairing.lock.json 核對每個檔案的大小、SHA-256、
// 張量契約與 placeholder 標記。placeholder 不在正式 lock 內。
func TestPlaceholderModelPackage(t *testing.T) {
	root := os.Getenv("OC_PLACEHOLDER_MODELS")
	if root == "" {
		t.Skip("set OC_PLACEHOLDER_MODELS to the placeholder reference/models directory")
	}
	lock, err := LoadLock(filepath.Join("..", "..", "testdata", "models", "placeholder", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkg := lock.ModelPackage
	result, err := pkg.Validate(Options{
		AllowPlaceholders:         true,
		InstalledDictionarySHA256: pkg.Dictionary.ManifestSHA256,
		ArtifactRoot:              root,
		Format:                    FormatInt8,
		VerifyFiles:               true,
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !result.Placeholder || result.Quality != QualityUntrained || len(result.Artifacts) != 2 {
		t.Fatalf("placeholder metadata: %+v", result)
	}
	kinds := map[string]bool{}
	for _, artifact := range result.Artifacts {
		kinds[artifact.Kind] = true
		if artifact.Format != FormatInt8 || artifact.Bytes < 50<<20 {
			t.Fatalf("artifact %+v", artifact)
		}
		t.Logf("%s: %d bytes %s", artifact.ArtifactID, artifact.Bytes, artifact.SHA256[:12])
	}
	if !kinds[KindReranker] || !kinds[KindPunctuation] {
		t.Fatalf("kinds %v", kinds)
	}
	checkPlaceholderManifests(t, root, pkg)
}

// 真實 placeholder 沒有 allow_placeholders 時必須被拒。
func TestPlaceholderModelPackageRefusesWithoutSetting(t *testing.T) {
	root := os.Getenv("OC_PLACEHOLDER_MODELS")
	if root == "" {
		t.Skip("set OC_PLACEHOLDER_MODELS to the placeholder reference/models directory")
	}
	lock, err := LoadLock(filepath.Join("..", "..", "testdata", "models", "placeholder", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkg := lock.ModelPackage
	if _, err := pkg.Validate(Options{InstalledDictionarySHA256: pkg.Dictionary.ManifestSHA256, ArtifactRoot: root, Format: FormatInt8, VerifyFiles: true}); !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("placeholder 應被拒絕：%v", err)
	}
}

// 真實 placeholder 的 manifest 與 lock 必須一致；把 variant 換錯或改雜湊要失敗。
func TestPlaceholderModelPackageRejectsBadVariantAndTamperedFiles(t *testing.T) {
	root := os.Getenv("OC_PLACEHOLDER_MODELS")
	if root == "" {
		t.Skip("set OC_PLACEHOLDER_MODELS to the placeholder reference/models directory")
	}
	lock, err := LoadLock(filepath.Join("..", "..", "testdata", "models", "placeholder", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkg := lock.ModelPackage

	wrong := *pkg
	wrong.Artifacts = append([]Artifact(nil), pkg.Artifacts...)
	wrong.Artifacts[0].Variant = VariantPublicCore
	if err := wrong.ValidateStructure(); !errors.Is(err, ErrVariantMixed) {
		t.Fatalf("reranker variant 錯誤應被拒絕：%v", err)
	}

	tampered := *pkg
	tampered.Artifacts = append([]Artifact(nil), pkg.Artifacts...)
	tampered.Artifacts[0].Files = map[string]ArtifactFile{}
	for format, file := range pkg.Artifacts[0].Files {
		file.SHA256 = "0" + file.SHA256[1:]
		tampered.Artifacts[0].Files[format] = file
	}
	if _, err := tampered.Validate(Options{AllowPlaceholders: true,
		InstalledDictionarySHA256: pkg.Dictionary.ManifestSHA256, ArtifactRoot: root, Format: FormatInt8, VerifyFiles: true}); !errors.Is(err, ErrFileMismatch) {
		t.Fatalf("檔案雜湊不符應被拒絕：%v", err)
	}
}

type placeholderManifest struct {
	ArtifactID  string                  `json:"artifact_id"`
	Kind        string                  `json:"kind"`
	Variant     string                  `json:"variant"`
	Placeholder bool                    `json:"placeholder"`
	Quality     string                  `json:"quality"`
	Files       map[string]ArtifactFile `json:"files"`
	IO          TensorIO                `json:"io"`
}

func checkPlaceholderManifests(t *testing.T, root string, pkg *Package) {
	t.Helper()
	for _, artifact := range pkg.Artifacts {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifact.Manifest)))
		if err != nil {
			t.Fatalf("%s: %v", artifact.ArtifactID, err)
		}
		var m placeholderManifest
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", artifact.ArtifactID, err)
		}
		if m.ArtifactID != artifact.ArtifactID || m.Kind != artifact.Kind || m.Variant != artifact.Variant ||
			!m.Placeholder || m.Quality != QualityUntrained {
			t.Fatalf("%s manifest 與 lock 不一致：%+v", artifact.ArtifactID, m)
		}
		for format, file := range artifact.Files {
			got, ok := m.Files[format]
			if !ok || got.Bytes != file.Bytes || got.SHA256 != file.SHA256 || got.Path != file.Path {
				t.Fatalf("%s/%s 檔案資訊不一致：lock=%+v manifest=%+v", artifact.ArtifactID, format, file, got)
			}
		}
		if len(m.IO.Inputs) != 2 || len(m.IO.Outputs) != 1 {
			t.Fatalf("%s IO %+v", artifact.ArtifactID, m.IO)
		}
		for i := range artifact.IO.Inputs {
			if m.IO.Inputs[i].Name != artifact.IO.Inputs[i].Name || m.IO.Inputs[i].DType != artifact.IO.Inputs[i].DType {
				t.Fatalf("%s 輸入 %d 與 manifest 不同", artifact.ArtifactID, i)
			}
		}
		out, want := m.IO.Outputs[0], artifact.IO.Outputs[0]
		if out.Name != want.Name || out.DType != want.DType {
			t.Fatalf("%s 輸出 %+v 與 lock %+v 不同", artifact.ArtifactID, out, want)
		}
	}
}
