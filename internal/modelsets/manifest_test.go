package modelsets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
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
	artifacts := map[string]map[string]any{}
	makeArtifact := func(id, kind, variant string) {
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
		artifacts[id] = syntheticArtifact(id, kind, variant, rel, int64(len(body)), dictSHA)
	}
	makeArtifact("reranker-public-core", KindReranker, VariantPublicCore)
	makeArtifact("punctuation-public-core", KindPunctuation, VariantPublicCore)
	makeArtifact("reranker-extra", KindReranker, VariantCCBYSA)
	sets := map[string]any{
		"ja-public-core": syntheticSet(VariantPublicCore, dictSHA, "", []map[string]any{
			artifacts["reranker-public-core"], artifacts["punctuation-public-core"],
		}),
		"ja-cc-by-sa": syntheticSet(VariantCCBYSA, dictSHA, "punctuation-public-core", []map[string]any{
			artifacts["reranker-extra"],
		}),
	}
	doc := map[string]any{
		"schema": 1,
		"model_sets": map[string]any{
			"schema": Schema, "updated": "2026-11-18",
			"mixing_policy": "never mix",
			"sets":          sets,
		},
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
		return map[string]any{"id": "CC-BY-4.0", "scope": "public-core", "distribution": "public install", "notice": "legal/CC-BY-4.0.txt"}
	}
	return map[string]any{"id": "CC-BY-SA-4.0", "scope": "cc-by-sa", "distribution": "experimental only", "notice": "legal/CC-BY-SA-4.0.txt"}
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

func syntheticSet(variant, dictSHA, shared string, artifacts []map[string]any) map[string]any {
	list := make([]any, len(artifacts))
	for i, a := range artifacts {
		list[i] = a
	}
	set := map[string]any{
		"variant": variant, "language": "ja", "version": "test-1", "description": "test",
		"dictionary": map[string]any{"pack_id": "ja-test", "language": "ja", "format": 2,
			"version": "test", "manifest_sha256": dictSHA, "source": "test"},
		"license": syntheticLic(variant), "attribution": []any{"legal/copyright"},
		"placeholder": true, "quality": QualityUntrained, "production_eligible": false,
		"artifacts": list,
	}
	if shared != "" {
		set["shared_punctuation"] = shared
	}
	return set
}

func digestBytes(b []byte) string {
	return sha256Hex(b)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestParseLockAcceptsSyntheticSets(t *testing.T) {
	f := newFixture(t)
	if got := f.lock.SetIDs(); len(got) != 2 {
		t.Fatalf("sets %v", got)
	}
	set, err := f.lock.Set("ja-public-core")
	if err != nil {
		t.Fatal(err)
	}
	if set.Variant != VariantPublicCore || len(set.Artifacts) != 2 {
		t.Fatalf("set %+v", set)
	}
}

func TestParseLockRejectsMixedVariants(t *testing.T) {
	f := newFixture(t)
	var doc map[string]any
	if err := json.Unmarshal(f.lockRaw, &doc); err != nil {
		t.Fatal(err)
	}
	sets := doc["model_sets"].(map[string]any)["sets"].(map[string]any)
	set := sets["ja-public-core"].(map[string]any)
	artifacts := set["artifacts"].([]any)
	artifacts[0].(map[string]any)["variant"] = VariantCCBYSA
	raw, _ := json.Marshal(doc)
	if _, err := ParseLock(raw); !errors.Is(err, ErrVariantMixed) {
		t.Fatalf("混用 variant 應被拒絕：%v", err)
	}
}

func TestValidateRejectsDictionaryMismatch(t *testing.T) {
	f := newFixture(t)
	set, _ := f.lock.Set("ja-public-core")
	if _, err := set.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: strings.Repeat("b", 64), ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrDictionaryMismatch) {
		t.Fatalf("字典不符應被拒絕：%v", err)
	}
	if _, err := set.Validate(Options{AllowPlaceholders: true, ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrDictionaryUnknown) {
		t.Fatalf("沒有已安裝字典應被拒絕：%v", err)
	}
	if _, err := set.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA, InstalledDictionaryPackID: "other", ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrDictionaryMismatch) {
		t.Fatalf("字典包 ID 不符應被拒絕：%v", err)
	}
}

func TestValidatePlaceholderGate(t *testing.T) {
	f := newFixture(t)
	set, _ := f.lock.Set("ja-public-core")
	if _, err := set.Validate(Options{InstalledDictionarySHA256: f.dictSHA, ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("未允許 placeholder 應被拒絕：%v", err)
	}
	result, err := set.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA, ArtifactRoot: f.root, VerifyFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Placeholder || result.Quality != QualityUntrained || len(result.Artifacts) != 2 || !result.DictionaryMatches {
		t.Fatalf("結果 %+v", result)
	}
}

func TestValidateRejectsFileHashMismatch(t *testing.T) {
	f := newFixture(t)
	set, _ := f.lock.Set("ja-public-core")
	path := filepath.Join(f.root, "reranker-public-core", "reranker-public-core.placeholder.int8.onnx")
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA, ArtifactRoot: f.root, VerifyFiles: true}); !errors.Is(err, ErrFileMismatch) {
		t.Fatalf("檔案雜湊不符應被拒絕：%v", err)
	}
}

func TestValidateRejectsTensorContract(t *testing.T) {
	f := newFixture(t)
	set, _ := f.lock.Set("ja-public-core")
	for i := range set.Artifacts {
		if set.Artifacts[i].Kind == KindReranker {
			set.Artifacts[i].IO.Outputs[0].Shape = []any{"batch", "seq"}
		}
	}
	if _, err := set.Validate(Options{AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA}); !errors.Is(err, ErrTensorContract) {
		t.Fatalf("reranker 輸出形狀應被拒絕：%v", err)
	}
}

func TestValidateRejectsUnmarkedPlaceholder(t *testing.T) {
	f := newFixture(t)
	set, _ := f.lock.Set("ja-public-core")
	set.Placeholder = false
	set.Quality = "trained"
	for i := range set.Artifacts {
		set.Artifacts[i].Placeholder = false
		set.Artifacts[i].Quality = "trained"
	}
	if err := set.ValidateStructure(); !errors.Is(err, ErrPlaceholderUnmarked) {
		t.Fatalf("placeholder 標記不可移除：%v", err)
	}
}

func TestInstallSelectsOneSet(t *testing.T) {
	f := newFixture(t)
	dest := t.TempDir()
	selection, err := Install(f.lock, "ja-public-core", InstallOptions{
		SourceRoot: f.root, DestRoot: dest, AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.SetID != "ja-public-core" || selection.Variant != VariantPublicCore || !selection.Placeholder {
		t.Fatalf("selection %+v", selection)
	}
	if _, err := os.Stat(filepath.Join(dest, "reranker-public-core", "reranker-public-core.placeholder.int8.onnx")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "reranker-extra")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("不應安裝另一 variant：%v", err)
	}
	read, err := ReadSelection(dest)
	if err != nil || read.SetID != selection.SetID || len(read.Artifacts) != 2 {
		t.Fatalf("active.json %+v %v", read, err)
	}
	if _, err := SelectionResult(f.lock, read, dest, f.dictSHA, true); err != nil {
		t.Fatalf("使用中的模型組應可驗證：%v", err)
	}

	// 切換到另一組：舊 variant 的目錄要移除，不能留下混用檔案。
	if _, err := Install(f.lock, "ja-cc-by-sa", InstallOptions{
		SourceRoot: f.root, DestRoot: dest, AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "reranker-public-core")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("切換後不應留下 public-core reranker：%v", err)
	}
	// cc-by-sa 是 reranker-only，但會安裝共用的 public-core punctuation。
	for _, path := range []string{
		"reranker-extra/reranker-extra.placeholder.int8.onnx",
		"punctuation-public-core/punctuation-public-core.placeholder.int8.onnx",
	} {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(path))); err != nil {
			t.Fatalf("切換後缺少 %s：%v", path, err)
		}
	}
	read, err = ReadSelection(dest)
	if err != nil || read.SetID != "ja-cc-by-sa" || len(read.Artifacts) != 2 {
		t.Fatalf("切換後 active.json %+v %v", read, err)
	}
	// 同組重裝要 -force。
	if _, err := Install(f.lock, "ja-cc-by-sa", InstallOptions{
		SourceRoot: f.root, DestRoot: dest, AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA,
	}); !errors.Is(err, ErrSelection) {
		t.Fatalf("同組重裝應要求 -force：%v", err)
	}
	if _, err := Install(f.lock, "ja-cc-by-sa", InstallOptions{
		SourceRoot: f.root, DestRoot: dest, AllowPlaceholders: true, InstalledDictionarySHA256: f.dictSHA, Force: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInstallRefusesPlaceholderWithoutSetting(t *testing.T) {
	f := newFixture(t)
	_, err := Install(f.lock, "ja-public-core", InstallOptions{
		SourceRoot: f.root, DestRoot: t.TempDir(), InstalledDictionarySHA256: f.dictSHA,
	})
	if !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("安裝 placeholder 應要求允許：%v", err)
	}
}

func TestRepositoryLockDefaultSetIsPinned(t *testing.T) {
	lock, err := LoadLock(filepath.Join("..", "..", "models", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	defaultID, err := lock.DefaultSetID()
	if err != nil {
		t.Fatal(err)
	}
	if defaultID != "ja-cc-by-sa" {
		t.Fatalf("default_set=%q，應為 ja-cc-by-sa", defaultID)
	}
	set, err := lock.DefaultSet()
	if err != nil {
		t.Fatal(err)
	}
	if set.Variant != VariantCCBYSA || len(set.Artifacts) != 1 ||
		set.Artifacts[0].ArtifactID != "reranker-extra" ||
		set.SharedPunctuation != "punctuation-public-core" || set.Companion == nil {
		t.Fatalf("預設模型組配置不符：%+v", set)
	}
	if set.Companion.License.ID != "MIT" || set.Artifacts[0].License.ID != "CC-BY-SA-4.0" {
		t.Fatalf("授權配置不符：extra=%s punctuation=%s", set.Artifacts[0].License.ID, set.Companion.License.ID)
	}
	if set.License.Notice != "legal/CC-BY-SA-4.0.txt" || set.Artifacts[0].License.Notice != "legal/CC-BY-SA-4.0.txt" {
		t.Fatalf("預設組必須帶 CC BY-SA notice：set=%q artifact=%q", set.License.Notice, set.Artifacts[0].License.Notice)
	}
	if warnings := set.Unpinned(); len(warnings) != 0 {
		t.Fatalf("預設組仍有未 pin 的來源：%v", warnings)
	}
	if err := lock.ValidateRelease(); err != nil {
		t.Fatalf("release 模式應接受已 pin 的預設組：%v", err)
	}
	deployment := lock.ModelSets.Deployment
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
	for _, artifact := range set.AllArtifacts() {
		expected, ok := want[artifact.ArtifactID]
		if !ok {
			t.Fatalf("預設組出現未預期的 artifact %s", artifact.ArtifactID)
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

	// MIT-only public-core 仍是替代組，但 reranker repo 尚未發布，不得是 release 候選。
	public, err := lock.Set("ja-public-core")
	if err != nil {
		t.Fatal(err)
	}
	if public.Variant != VariantPublicCore || len(public.Artifacts) != 2 || public.ProductionEligible {
		t.Fatalf("MIT-only 替代組配置不符：%+v", public)
	}
	if public.Artifacts[0].ArtifactID != "reranker-public-core" || public.Artifacts[0].License.ID != "MIT" {
		t.Fatalf("MIT-only 替代組應保留 public-core reranker：%+v", public.Artifacts[0])
	}
	if err := public.ValidateRelease(); !errors.Is(err, ErrUnpinned) {
		t.Fatalf("未發布的替代組在 release 模式應被拒絕：%v", err)
	}

	ids := make([]string, 0, 3)
	for _, id := range lock.SetIDs() {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if got, want := strings.Join(ids, ","), "ja-cc-by-sa,ja-public-core"; got != want {
		t.Fatalf("模型組清單 %q，應為 %q", got, want)
	}
	seenArtifacts := map[string]bool{}
	for _, id := range lock.SetIDs() {
		set, _ := lock.Set(id)
		for _, artifact := range set.AllArtifacts() {
			seenArtifacts[artifact.ArtifactID] = true
		}
	}
	artifacts := make([]string, 0, len(seenArtifacts))
	for id := range seenArtifacts {
		artifacts = append(artifacts, id)
	}
	sort.Strings(artifacts)
	if got, want := strings.Join(artifacts, ","), "punctuation-public-core,reranker-extra,reranker-public-core"; got != want {
		t.Fatalf("artifact 清單 %q，應為 %q", got, want)
	}
}

func TestReleaseRejectsRemovedRevision(t *testing.T) {
	lock, err := LoadLock(filepath.Join("..", "..", "models", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := lock.DefaultSet()
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.ValidateRelease(); err != nil {
		t.Fatalf("基準應通過：%v", err)
	}
	set.Artifacts[0].HF.Revision = ""
	if err := lock.ValidateRelease(); !errors.Is(err, ErrUnpinned) {
		t.Fatalf("移除預設組 revision 後 release 應失敗：%v", err)
	}
	set.Artifacts[0].HF.Revision = "7450e10fe8b673560044c5db08ad3006a06d7a6e"
	set.Companion.HF.Files[FormatInt8] = HFFile{Name: "model.int8.onnx"}
	if err := lock.ValidateRelease(); err == nil {
		t.Fatal("移除共用 punctuation 的大小／雜湊後 release 應失敗")
	}
}

func TestDefaultSetSelectionRequiresMetadata(t *testing.T) {
	f := newFixture(t)
	if _, err := f.lock.DefaultSetID(); !errors.Is(err, ErrSchema) {
		t.Fatalf("沒有 default_set 應被拒絕：%v", err)
	}
	if _, err := f.lock.DefaultSet(); !errors.Is(err, ErrSchema) {
		t.Fatalf("沒有 default_set 應被拒絕：%v", err)
	}
	f.lock.ModelSets.DefaultSet = "missing"
	if _, err := f.lock.DefaultSetID(); !errors.Is(err, ErrSchema) {
		t.Fatalf("不存在的 default_set 應被拒絕：%v", err)
	}
	f.lock.ModelSets.DefaultSet = "ja-cc-by-sa"
	set, err := f.lock.DefaultSet()
	if err != nil || set.ID != "ja-cc-by-sa" {
		t.Fatalf("default_set 選擇失敗：%+v %v", set, err)
	}
}

func TestCCBYSAArtifactCannotEnterPublicCoreSet(t *testing.T) {
	f := newFixture(t)
	public, _ := f.lock.Set("ja-public-core")
	extra, _ := f.lock.Set("ja-cc-by-sa")
	public.Artifacts = append(public.Artifacts, extra.Artifacts[0])
	if err := public.ValidateStructure(); !errors.Is(err, ErrVariantMixed) {
		t.Fatalf("CC BY-SA artifact 不得進 public-core：%v", err)
	}
}

func TestPlaceholderFixtureStillRequiresExplicitAllow(t *testing.T) {
	lock, err := LoadLock(filepath.Join("..", "..", "testdata", "models", "placeholder", "pairing.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := lock.Set("ja-public-core")
	if err != nil {
		t.Fatal(err)
	}
	if !set.Placeholder {
		t.Fatal("placeholder fixture 應保留 placeholder 標記")
	}
	if _, err := set.Validate(Options{InstalledDictionarySHA256: set.Dictionary.ManifestSHA256}); !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("未允許 placeholder 應被拒絕：%v", err)
	}
}

func TestDictionaryPackID(t *testing.T) {
	pack, _ := syntheticPack(t)
	if got := DictionaryPackID(pack); got != "ja-test" {
		t.Fatalf("pack id %q", got)
	}
}
