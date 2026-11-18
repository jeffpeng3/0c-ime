package modelsets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRepo = "jeffpeng3/0c-ja-test"
const testRevision = "0123456789abcdef0123456789abcdef01234567"

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func testLockDoc(t *testing.T, pinned bool, bodies map[string][]byte) []byte {
	t.Helper()
	dict := strings.Repeat("a", 64)
	revision := ""
	if pinned {
		revision = testRevision
	}
	artifacts := []any{}
	for i, spec := range []struct{ id, kind, name string }{
		{"reranker-test", KindReranker, "reranker.int8.onnx"},
		{"punctuation-test", KindPunctuation, "punctuation.int8.onnx"},
	} {
		body := bodies[spec.name]
		entry := map[string]any{"name": spec.name, "bytes": 0, "sha256": ""}
		local := map[string]any{"path": spec.id + "/model.int8.onnx", "bytes": 0, "sha256": "", "quantization": "dynamic-quint8"}
		if pinned {
			entry["bytes"], entry["sha256"] = len(body), sum(body)
			local["bytes"], local["sha256"] = len(body), sum(body)
		}
		output := map[string]any{"name": "scores", "dtype": "float32", "shape": []any{"batch"}}
		if i == 1 {
			output = map[string]any{"name": "logits", "dtype": "float32", "shape": []any{"batch", "seq", 4}}
		}
		artifacts = append(artifacts, map[string]any{
			"manifest": spec.id + "/manifest.json", "artifact_id": spec.id, "kind": spec.kind,
			"variant": VariantPublicCore, "version": "test-1", "parameters": 42,
			"placeholder": false, "quality": "trained", "production_eligible": true,
			"dictionary_manifest_sha256": dict,
			"license":                    map[string]any{"id": "MIT", "scope": "public-core", "distribution": "test", "notice": "legal/MIT.txt"},
			"attribution":                []any{"legal/copyright"},
			"source":                     map[string]any{"training_data": "test", "datasets": []any{}, "checkpoint": "", "generator": "test", "fingerprint": ""},
			"files":                      map[string]any{"int8": local},
			"hf": map[string]any{
				"repository": testRepo, "revision": revision, "license": "MIT",
				"files": map[string]any{"int8": entry},
			},
			"io": map[string]any{
				"inputs": []any{
					map[string]any{"name": "input_ids", "dtype": "int64", "shape": []any{"batch", "seq"}},
					map[string]any{"name": "attention_mask", "dtype": "int64", "shape": []any{"batch", "seq"}},
				},
				"outputs": []any{output},
			},
		})
	}
	doc := map[string]any{
		"schema": 1,
		"model_sets": map[string]any{
			"schema": Schema, "updated": "2026-11-18", "mixing_policy": "never mix",
			"sets": map[string]any{
				"ja-public-core": map[string]any{
					"variant": VariantPublicCore, "language": "ja", "version": "test-1",
					"dictionary": map[string]any{"pack_id": "ja-test", "language": "ja", "format": 2,
						"version": "test", "manifest_sha256": dict, "source": "test"},
					"license":     map[string]any{"id": "MIT", "scope": "public-core", "distribution": "test", "notice": "legal/MIT.txt"},
					"attribution": []any{"legal/copyright"}, "placeholder": false, "quality": "trained",
					"production_eligible": true, "artifacts": artifacts,
				},
			},
		},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func fakeHub(t *testing.T, bodies map[string][]byte) *httptest.Server {
	t.Helper()
	hub := http.NewServeMux()
	for name, body := range bodies {
		body := body
		hub.HandleFunc("/"+testRepo+"/resolve/"+testRevision+"/"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Write(body)
		})
	}
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	return server
}

func TestFetchSetDownloadsVerifiesCachesAndSupportsOffline(t *testing.T) {
	bodies := map[string][]byte{
		"reranker.int8.onnx":    []byte("reranker-onnx"),
		"punctuation.int8.onnx": []byte("punctuation-onnx"),
	}
	server := fakeHub(t, bodies)
	cache, out := t.TempDir(), t.TempDir()
	lock, err := ParseLock(testLockDoc(t, true, bodies))
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &Fetcher{BaseURL: server.URL, CacheDir: cache, Client: server.Client()}
	files, err := fetcher.FetchSet(t.Context(), lock, "ja-public-core", FormatInt8, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files %+v", files)
	}
	for _, file := range files {
		got, err := os.ReadFile(file.Path)
		if err != nil {
			t.Fatal(err)
		}
		if sum(got) != file.SHA256 {
			t.Fatalf("%s 寫入內容不符", file.Path)
		}
	}
	// 快取命中後離線仍可再產生一份。
	offline := &Fetcher{BaseURL: "https://invalid.example", CacheDir: cache, Offline: true}
	again := t.TempDir()
	if _, err := offline.FetchSet(t.Context(), lock, "ja-public-core", FormatInt8, again); err != nil {
		t.Fatalf("離線快取應可用：%v", err)
	}
}

func TestFetchRejectsHashMismatchAndOfflineMiss(t *testing.T) {
	good := map[string][]byte{
		"reranker.int8.onnx":    []byte("reranker-onnx"),
		"punctuation.int8.onnx": []byte("punctuation-onnx"),
	}
	lock, err := ParseLock(testLockDoc(t, true, good))
	if err != nil {
		t.Fatal(err)
	}
	bad := map[string][]byte{
		"reranker.int8.onnx":    []byte("tampered"),
		"punctuation.int8.onnx": good["punctuation.int8.onnx"],
	}
	server := fakeHub(t, bad)
	fetcher := &Fetcher{BaseURL: server.URL, CacheDir: t.TempDir(), Client: server.Client()}
	if _, err := fetcher.FetchSet(t.Context(), lock, "ja-public-core", FormatInt8, t.TempDir()); err == nil || !containsErr(err, ErrChecksum) {
		t.Fatalf("雜湊不符應失敗：%v", err)
	}

	offline := &Fetcher{CacheDir: t.TempDir(), Offline: true}
	if _, err := offline.FetchSet(t.Context(), lock, "ja-public-core", FormatInt8, t.TempDir()); err == nil || !containsErr(err, ErrOffline) {
		t.Fatalf("離線缺檔應失敗：%v", err)
	}
}

func TestFetchFromFileBaseURL(t *testing.T) {
	bodies := map[string][]byte{
		"reranker.int8.onnx":    []byte("reranker-onnx"),
		"punctuation.int8.onnx": []byte("punctuation-onnx"),
	}
	root := t.TempDir()
	for name, body := range bodies {
		path := filepath.Join(root, filepath.FromSlash(testRepo), "resolve", testRevision, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := ParseLock(testLockDoc(t, true, bodies))
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &Fetcher{BaseURL: "file://" + root, CacheDir: t.TempDir()}
	if _, err := fetcher.FetchSet(t.Context(), lock, "ja-public-core", FormatInt8, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestPinTransitionsFromDevelopmentToRelease(t *testing.T) {
	bodies := map[string][]byte{
		"reranker.int8.onnx":    []byte("reranker-onnx"),
		"punctuation.int8.onnx": []byte("punctuation-onnx"),
	}
	raw := testLockDoc(t, false, bodies)
	lock, err := ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := lock.Set("ja-public-core")
	if err := set.ValidateRelease(); !containsErr(err, ErrUnpinned) {
		t.Fatalf("pin 前 release 應失敗：%v", err)
	}
	server := fakeHub(t, bodies)
	pinned, files, err := Pin(t.Context(), raw, PinOptions{
		Repository: testRepo, Revision: testRevision, Updated: "2026-11-18",
		BaseURL: server.URL, CacheDir: t.TempDir(), Client: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("pin files %+v", files)
	}
	lock, err = ParseLock(pinned)
	if err != nil {
		t.Fatal(err)
	}
	set, _ = lock.Set("ja-public-core")
	if err := set.ValidateRelease(); err != nil {
		t.Fatalf("pin 後 release 應通過：%v", err)
	}
	for i := range set.Artifacts {
		artifact := &set.Artifacts[i]
		if artifact.HF.Revision != testRevision || !artifact.HF.Pinned() {
			t.Fatalf("artifact 未 pin：%+v", artifact.HF)
		}
		if artifact.Files[FormatInt8].SHA256 != artifact.HF.Files[FormatInt8].SHA256 {
			t.Fatalf("本機與 HF 雜湊不同：%+v", artifact.Files)
		}
	}
	// 再跑一次 pin 不會改變結果。
	again, _, err := Pin(t.Context(), pinned, PinOptions{
		Repository: testRepo, Revision: testRevision, Updated: "2026-11-18",
		BaseURL: server.URL, CacheDir: t.TempDir(), Client: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	json.Unmarshal(pinned, &a)
	json.Unmarshal(again, &b)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatal("重複 pin 的結果不一致")
	}
}

func containsErr(err error, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
