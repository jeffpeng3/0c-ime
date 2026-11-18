package modelsets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticPack 建一個假的日文詞典包 manifest，回傳路徑與 SHA-256。
func syntheticPack(t *testing.T) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ja")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b := []byte("{\"format\":2,\"language\":\"ja\",\"lexicon\":\"ja-test\",\"files\":{}}\n")
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := DictionarySHA256(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, sha
}

type fixture struct {
	root    string
	lock    Lock
	lockRaw []byte
	pack    string
	dictSHA string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pack, dictSHA := syntheticPack(t)
	root := t.TempDir()
	makeArtifact := func(id, kind, variant string) map[string]any {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, id+".placeholder.int8.onnx")
		body := []byte("synthetic-onnx:" + id + "\n")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		rel := id + "/" + id + ".placeholder.int8.onnx"
		return syntheticArtifact(id, kind, variant, rel, int64(len(body)), dictSHA)
	}
	doc := map[string]any{
		"schema": 1,
		"model_package": syntheticPackage(dictSHA, []map[string]any{
			makeArtifact("reranker-extra", KindReranker, VariantCCBYSA),
			makeArtifact("punctuation-public-core", KindPunctuation, VariantPublicCore),
		}),
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{root: root, lock: lock, lockRaw: raw, pack: pack, dictSHA: dictSHA}
}

func syntheticLic(variant string) map[string]any {
	if variant == VariantPublicCore {
		return map[string]any{"id": "MIT", "scope": "public-core", "distribution": "MIT only", "notice": "legal/MIT.txt"}
	}
	return map[string]any{"id": "CC-BY-SA-4.0", "scope": "cc-by-sa", "distribution": "carry notice", "notice": "legal/CC-BY-SA-4.0.txt"}
}

func syntheticArtifact(id, kind, variant, rel string, size int64, dictSHA string) map[string]any {
	output := map[string]any{"name": "scores", "dtype": "float32", "shape": []any{"batch"}}
	if kind == KindPunctuation {
		output = map[string]any{"name": "logits", "dtype": "float32", "shape": []any{"batch", "seq", 4}}
	}
	return map[string]any{
		"manifest": id + "/manifest.json", "artifact_id": id, "kind": kind, "variant": variant,
		"version": "test-1", "parameters": 42, "placeholder": true, "quality": QualityUntrained,
		"production_eligible": false, "dictionary_manifest_sha256": dictSHA,
		"license": syntheticLic(variant), "attribution": []any{"legal/copyright"},
		"source": map[string]any{"training_data": "none (untrained placeholder)", "datasets": []any{}, "checkpoint": "", "generator": "test", "fingerprint": "f"},
		"files":  map[string]any{"int8": map[string]any{"path": rel, "bytes": size, "sha256": digestBytes([]byte("synthetic-onnx:" + id + "\n")), "quantization": "dynamic-quint8"}},
		"io": map[string]any{
			"inputs": []any{
				map[string]any{"name": "input_ids", "dtype": "int64", "shape": []any{"batch", "seq"}},
				map[string]any{"name": "attention_mask", "dtype": "int64", "shape": []any{"batch", "seq"}},
			},
			"outputs": []any{output},
		},
	}
}

func syntheticPackage(dictSHA string, artifacts []map[string]any) map[string]any {
	list := make([]any, len(artifacts))
	for i, a := range artifacts {
		list[i] = a
	}
	return map[string]any{
		"schema": Schema, "updated": "2026-11-19", "version": "test-1",
		"dictionary": map[string]any{"pack_id": "ja-test", "language": "ja", "format": 2,
			"version": "test", "manifest_sha256": dictSHA, "source": "test"},
		"deployment": map[string]any{"sequence_length": 124, "context_tokens_per_side": 60,
			"gating_top_k": 3, "format": "int8"},
		"license": syntheticLic(VariantCCBYSA), "attribution": []any{"legal/copyright"},
		"placeholder": true, "quality": QualityUntrained, "production_eligible": false,
		"artifacts": list,
	}
}

func digestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestParseLockAcceptsSyntheticPackage(t *testing.T) {
	f := newFixture(t)
	pkg := f.lock.ModelPackage
	if pkg == nil || pkg.Version != "test-1" || len(pkg.Artifacts) != 2 {
		t.Fatalf("package %+v", pkg)
	}
	if pkg.Artifacts[0].Kind != KindReranker || pkg.Artifacts[1].Kind != KindPunctuation {
		t.Fatalf("artifacts %+v", pkg.Artifacts)
	}
}

func TestParseLockRejectsWrongArtifactVariant(t *testing.T) {
	f := newFixture(t)
	var doc map[string]any
	if err := json.Unmarshal(f.lockRaw, &doc); err != nil {
		t.Fatal(err)
	}
	pkg := doc["model_package"].(map[string]any)
	artifacts := pkg["artifacts"].([]any)
	artifacts[0].(map[string]any)["variant"] = VariantPublicCore
	raw, _ := json.Marshal(doc)
	if _, err := ParseLock(raw); !errors.Is(err, ErrVariantMixed) {
		t.Fatalf("reranker 不是 cc-by-sa 應被拒絕：%v", err)
	}
}

func TestParseLockRejectsAlternativeFields(t *testing.T) {
	f := newFixture(t)
	var doc map[string]any
	if err := json.Unmarshal(f.lockRaw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["model_sets"] = map[string]any{"schema": "0c.model-sets/1", "sets": map[string]any{}}
	delete(doc, "model_package")
	raw, _ := json.Marshal(doc)
	if _, err := ParseLock(raw); !errors.Is(err, ErrSchema) {
		t.Fatalf("舊 model_sets 應被拒絕：%v", err)
	}
}

func TestValidateRejectsDictionaryMismatch(t *testing.T) {
	f := newFixture(t)
	pkg := f.lock.ModelPackage
	if _, err := pkg.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: strings.Repeat("b", 64), ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrDictionaryMismatch) {
		t.Fatalf("字典不符應被拒絕：%v", err)
	}
	if _, err := pkg.Validate(Options{AllowPlaceholders: true, ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrDictionaryUnknown) {
		t.Fatalf("沒有已安裝字典應被拒絕：%v", err)
	}
	if _, err := pkg.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA, InstalledDictionaryPackID: "other", ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrDictionaryMismatch) {
		t.Fatalf("字典包 ID 不符應被拒絕：%v", err)
	}
}

func TestValidatePlaceholderGate(t *testing.T) {
	f := newFixture(t)
	pkg := f.lock.ModelPackage
	if _, err := pkg.Validate(Options{InstalledDictionarySHA256: f.dictSHA, ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("未允許 placeholder 應被拒絕：%v", err)
	}
	result, err := pkg.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA, ArtifactRoot: f.root, VerifyFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Placeholder || result.Quality != QualityUntrained || len(result.Artifacts) != 2 || !result.DictionaryMatches {
		t.Fatalf("結果 %+v", result)
	}
}

func TestValidateRejectsFileHashMismatch(t *testing.T) {
	f := newFixture(t)
	pkg := f.lock.ModelPackage
	path := filepath.Join(f.root, "reranker-extra", "reranker-extra.placeholder.int8.onnx")
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := pkg.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA, ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrFileMismatch) {
		t.Fatalf("檔案雜湊不符應被拒絕：%v", err)
	}
}

func TestValidateRejectsTensorContract(t *testing.T) {
	f := newFixture(t)
	pkg := f.lock.ModelPackage
	pkg.Artifacts[0].IO.Outputs[0].Shape = []any{"batch", "seq"}
	if _, err := pkg.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA}); !errors.Is(err, ErrTensorContract) {
		t.Fatalf("reranker 輸出形狀應被拒絕：%v", err)
	}
}

func TestValidateRejectsUnmarkedPlaceholder(t *testing.T) {
	f := newFixture(t)
	pkg := f.lock.ModelPackage
	pkg.Placeholder = false
	pkg.Quality = "trained"
	for i := range pkg.Artifacts {
		pkg.Artifacts[i].Placeholder = false
		pkg.Artifacts[i].Quality = "trained"
	}
	if err := pkg.ValidateStructure(); !errors.Is(err, ErrPlaceholderUnmarked) {
		t.Fatalf("placeholder 標記不可移除：%v", err)
	}
}

func TestInstallWritesOnePackage(t *testing.T) {
	f := newFixture(t)
	dest := t.TempDir()
	selection, err := Install(f.lock, InstallOptions{
		SourceRoot: f.root, DestRoot: dest, AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Version != "test-1" || !selection.Placeholder || len(selection.Artifacts) != 2 {
		t.Fatalf("selection %+v", selection)
	}
	for _, path := range []string{
		"reranker-extra/reranker-extra.placeholder.int8.onnx",
		"punctuation-public-core/punctuation-public-core.placeholder.int8.onnx",
	} {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(path))); err != nil {
			t.Fatalf("缺少 %s：%v", path, err)
		}
	}
	read, err := ReadSelection(dest)
	if err != nil || read.Version != selection.Version || len(read.Artifacts) != 2 {
		t.Fatalf("active.json %+v %v", read, err)
	}
	if _, err := SelectionResult(f.lock, read, dest, f.dictSHA, true); err != nil {
		t.Fatalf("使用中的模型包應可驗證：%v", err)
	}
	// 同一個模型包重裝要 -force。
	if _, err := Install(f.lock, InstallOptions{
		SourceRoot: f.root, DestRoot: dest, AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA,
	}); !errors.Is(err, ErrSelection) {
		t.Fatalf("重裝應要求 -force：%v", err)
	}
}

func TestInstallRefusesPlaceholderWithoutSetting(t *testing.T) {
	f := newFixture(t)
	_, err := Install(f.lock, InstallOptions{
		SourceRoot: f.root, DestRoot: t.TempDir(), InstalledDictionarySHA256: f.dictSHA,
	})
	if !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("安裝 placeholder 應要求允許：%v", err)
	}
}

func TestRepositoryLockIsSinglePinnedPackage(t *testing.T) {
	lock, err := LoadLock(filepath.Join("..", "..", "models", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkg := lock.ModelPackage
	if pkg == nil || pkg.Version != "2026.11.0" || len(pkg.Artifacts) != 2 {
		t.Fatalf("正式 lock 應只有一個模型包：%+v", pkg)
	}
	if warnings := pkg.Unpinned(); len(warnings) != 0 {
		t.Fatalf("正式模型包仍有未 pin 的來源：%v", warnings)
	}
	if err := pkg.ValidateRelease(); err != nil {
		t.Fatalf("release 模式應接受已 pin 的模型包：%v", err)
	}
	deployment := pkg.Deployment
	if deployment == nil || deployment.SequenceLength != 124 || deployment.ContextTokensPerSide != 60 ||
		deployment.GatingTopK != 3 || deployment.Format != FormatInt8 {
		t.Fatalf("deployment 不符：%+v", deployment)
	}

	want := map[string]struct {
		revision, fp32, fp32SHA, int8, int8SHA string
		fp32Bytes, int8Bytes                   int64
	}{
		"reranker-extra": {
			revision: "7450e10fe8b673560044c5db08ad3006a06d7a6e",
			fp32:     "model.onnx", fp32Bytes: 135613420,
			fp32SHA: "4b74ae019b15443f2a30936e9cf53959e60716a30548f17bb1f0d8569fa978e9",
			int8:    "model.int8.onnx", int8Bytes: 34268510,
			int8SHA: "edf1afeacb89fa5da3d10462b5d9afd85392a91056634dd9ac60d41e37faeeb3",
		},
		"punctuation-public-core": {
			revision: "ff9166abc620b6716ac7de73cfa67335e6b985d2",
			fp32:     "model.onnx", fp32Bytes: 135599640,
			fp32SHA: "998b3df5af330a7f2106d6ecd4185a15760bb4ea942eb3ebf9f24b9ca7151a06",
			int8:    "model.int8.onnx", int8Bytes: 34240656,
			int8SHA: "2860267deb3da1816b3f53decf786c5f5f478a1b9d327c8c80f2428c643f051b",
		},
	}
	for _, artifact := range pkg.Artifacts {
		expected, ok := want[artifact.ArtifactID]
		if !ok {
			t.Fatalf("模型包出現未預期的 artifact %s", artifact.ArtifactID)
		}
		if artifact.Kind == KindReranker && artifact.License.ID != "CC-BY-SA-4.0" {
			t.Fatalf("reranker 授權應是 CC BY-SA：%s", artifact.License.ID)
		}
		if artifact.Kind == KindPunctuation && artifact.License.ID != "MIT" {
			t.Fatalf("punctuation 授權應是 MIT：%s", artifact.License.ID)
		}
		if artifact.HF == nil || artifact.HF.Revision != expected.revision || !artifact.HF.Pinned() {
			t.Fatalf("%s 未 pin 到 %s：%+v", artifact.ArtifactID, expected.revision, artifact.HF)
		}
		for format, file := range map[string]ArtifactFile{
			FormatFP32: artifact.Files[FormatFP32],
			FormatInt8: artifact.Files[FormatInt8],
		} {
			remote, ok := artifact.HF.Files[format]
			if !ok {
				t.Fatalf("%s 缺少 hf %s", artifact.ArtifactID, format)
			}
			name, size, digest := expected.fp32, expected.fp32Bytes, expected.fp32SHA
			if format == FormatInt8 {
				name, size, digest = expected.int8, expected.int8Bytes, expected.int8SHA
			}
			if remote.Name != name || remote.Bytes != size || remote.SHA256 != digest {
				t.Fatalf("%s/%s hf=%+v，應為 %s %d %s", artifact.ArtifactID, format, remote, name, size, digest)
			}
			if file.Bytes != size || file.SHA256 != digest {
				t.Fatalf("%s/%s 本機=%+v，應為 %d %s", artifact.ArtifactID, format, file, size, digest)
			}
		}
	}
	if got := strings.Join(artifactIDs(pkg), ","); got != "reranker-extra,punctuation-public-core" {
		t.Fatalf("artifact 順序 %q", got)
	}
	for _, artifact := range pkg.Artifacts {
		if artifact.Kind == KindReranker && !containsAll(artifact.Attribution, "data/sources/licenses/ja-wiki.txt") {
			t.Fatalf("reranker 缺少 Wikipedia attribution：%v", artifact.Attribution)
		}
	}
}

func artifactIDs(pkg *Package) []string {
	out := make([]string, 0, len(pkg.Artifacts))
	for _, artifact := range pkg.Artifacts {
		out = append(out, artifact.ArtifactID)
	}
	return out
}

func containsAll(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestReleaseRejectsRemovedRevisionOrHash(t *testing.T) {
	lock, err := LoadLock(filepath.Join("..", "..", "models", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkg := lock.ModelPackage
	if err := pkg.ValidateRelease(); err != nil {
		t.Fatalf("基準應通過：%v", err)
	}
	pkg.Artifacts[0].HF.Revision = ""
	if err := pkg.ValidateRelease(); !errors.Is(err, ErrUnpinned) {
		t.Fatalf("移除 revision 後 release 應失敗：%v", err)
	}
	pkg.Artifacts[0].HF.Revision = "7450e10fe8b673560044c5db08ad3006a06d7a6e"
	pkg.Artifacts[1].HF.Files[FormatInt8] = HFFile{Name: "model.int8.onnx"}
	if err := pkg.ValidateRelease(); err == nil {
		t.Fatal("移除檔案雜湊後 release 應失敗")
	}
}

func TestPlaceholderFixtureStillRequiresExplicitAllow(t *testing.T) {
	lock, err := LoadLock(filepath.Join("..", "..", "testdata", "models", "placeholder", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pkg := lock.ModelPackage
	if pkg == nil || !pkg.Placeholder {
		t.Fatal("placeholder fixture 應保留 placeholder 標記")
	}
	if _, err := pkg.Validate(Options{InstalledDictionarySHA256: pkg.Dictionary.ManifestSHA256}); !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("未允許 placeholder 應被拒絕：%v", err)
	}
}

func TestDictionaryPackID(t *testing.T) {
	pack, _ := syntheticPack(t)
	if got := DictionaryPackID(pack); got != "ja-test" {
		t.Fatalf("pack id %q", got)
	}
}
