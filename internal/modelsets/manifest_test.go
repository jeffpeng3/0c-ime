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
	files := map[string][]byte{}
	artifacts := []map[string]any{}
	for _, variant := range []string{VariantPublicCore, VariantCCBYSA} {
		for _, kind := range []string{KindReranker, KindPunctuation} {
			id := kind + "-" + variant
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
			files[rel] = body
			artifacts = append(artifacts, syntheticArtifact(id, kind, variant, rel, int64(len(body)), dictSHA))
		}
	}
	// 把同 variant 的兩個模型分成一組。
	sets := map[string]any{}
	for _, variant := range []string{VariantPublicCore, VariantCCBYSA} {
		var group []map[string]any
		for _, a := range artifacts {
			if a["variant"] == variant {
				group = append(group, a)
			}
		}
		id := "ja-" + variant
		sets[id] = syntheticSet(variant, dictSHA, group)
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

func syntheticSet(variant, dictSHA string, artifacts []map[string]any) map[string]any {
	list := make([]any, len(artifacts))
	for i, a := range artifacts {
		list[i] = a
	}
	return map[string]any{
		"variant": variant, "language": "ja", "version": "test-1", "description": "test",
		"dictionary": map[string]any{"pack_id": "ja-test", "language": "ja", "format": 2,
			"version": "test", "manifest_sha256": dictSHA, "source": "test"},
		"license": syntheticLic(variant), "attribution": []any{"legal/copyright"},
		"placeholder": true, "quality": QualityUntrained, "production_eligible": false,
		"artifacts": list,
	}
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
	if _, err := os.Stat(filepath.Join(dest, "reranker-cc-by-sa")); !errors.Is(err, os.ErrNotExist) {
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
		t.Fatalf("切換後不應留下 public-core：%v", err)
	}
	read, err = ReadSelection(dest)
	if err != nil || read.SetID != "ja-cc-by-sa" {
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

func TestDictionaryPackID(t *testing.T) {
	pack, _ := syntheticPack(t)
	if got := DictionaryPackID(pack); got != "ja-test" {
		t.Fatalf("pack id %q", got)
	}
}
