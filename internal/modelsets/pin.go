package modelsets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// PinOptions 是 0c-model-set pin 的參數。Revision 必須是 HF commit 雜湊；pin 會依
// repo 內每個 format 的檔名下載一次、計算大小與 SHA-256，再寫回 lock 的 hf.files
// 與 files。Offline 時只接受 cache 內已存在的檔案。
type PinOptions struct {
	Repository string
	Revision   string
	Version    string
	Updated    string
	BaseURL    string
	CacheDir   string
	Offline    bool
	Client     *http.Client
}

// PinnedFile 是 pin 的結果摘要。
type PinnedFile struct {
	ArtifactID string `json:"artifact_id"`
	Format     string `json:"format"`
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
}

// Pin 把一個 HF repo 的 revision 與逐檔大小／SHA-256 寫進 pairing.lock.json。
// 只更新 repository 相符的 artifacts；找不到 repo、revision 不合法或下載失敗都直接
// 回傳錯誤，不會留下半套 lock。
func Pin(ctx context.Context, raw []byte, opts PinOptions) ([]byte, []PinnedFile, error) {
	repository := strings.TrimSpace(opts.Repository)
	revision := strings.TrimSpace(opts.Revision)
	if repository == "" {
		return nil, nil, fmt.Errorf("%w: 需要 --repo", ErrHFSource)
	}
	if !validRevision(revision) {
		return nil, nil, fmt.Errorf("%w: --revision %q 必須是 HF commit 雜湊", ErrUnpinned, revision)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrSchema, err)
	}
	modelPackage, ok := doc["model_package"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%w: 缺少 model_package", ErrSchema)
	}
	artifacts, _ := modelPackage["artifacts"].([]any)
	if len(artifacts) == 0 {
		return nil, nil, fmt.Errorf("%w: 沒有模型", ErrSchema)
	}

	fetcher := &Fetcher{BaseURL: opts.BaseURL, CacheDir: opts.CacheDir, Offline: opts.Offline, Client: opts.Client}
	var pinned []PinnedFile
	found := false
	for _, artifactValue := range artifacts {
		artifact, ok := artifactValue.(map[string]any)
		if !ok {
			continue
		}
		hf, ok := artifact["hf"].(map[string]any)
		if !ok || hf["repository"] != repository {
			continue
		}
		found = true
		artifactID, _ := artifact["artifact_id"].(string)
		hfFiles, ok := hf["files"].(map[string]any)
		if !ok || len(hfFiles) == 0 {
			return nil, nil, fmt.Errorf("%w: %s 缺少 hf.files", ErrHFSource, artifactID)
		}
		localFiles, _ := artifact["files"].(map[string]any)
		if localFiles == nil {
			localFiles = map[string]any{}
			artifact["files"] = localFiles
		}
		for format, fileValue := range hfFiles {
			entry, ok := fileValue.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("%w: %s/%s 不是物件", ErrHFSource, artifactID, format)
			}
			name, _ := entry["name"].(string)
			if name == "" || filepath.Base(name) != name {
				return nil, nil, fmt.Errorf("%w: %s/%s 缺少合法檔名", ErrHFSource, artifactID, format)
			}
			if !opts.Offline {
				// 重新 pin 時不要沿用舊快取，避免把損壞或不同 revision 的檔案寫進 lock。
				os.Remove(fetcher.cacheDir(repository, revision, name))
			}
			source, err := fetcher.Resolve(ctx, repository, revision, name, HFFile{Name: name})
			if err != nil {
				return nil, nil, fmt.Errorf("%s/%s: %w", artifactID, format, err)
			}
			size, digest, err := DigestFile(source)
			if err != nil {
				return nil, nil, err
			}
			entry["bytes"] = size
			entry["sha256"] = digest
			local, _ := localFiles[format].(map[string]any)
			if local == nil {
				local = map[string]any{"path": artifactID + "/" + name, "quantization": format}
				localFiles[format] = local
			}
			local["bytes"] = size
			local["sha256"] = digest
			pinned = append(pinned, PinnedFile{ArtifactID: artifactID, Format: format, Name: name, Bytes: size, SHA256: digest})
		}
		hf["revision"] = revision
		if opts.Version != "" {
			artifact["version"] = opts.Version
			modelPackage["version"] = opts.Version
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("%w: lock 沒有 repository %q 的模型", ErrHFSource, repository)
	}
	if opts.Updated != "" {
		modelPackage["updated"] = opts.Updated
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	out = append(out, '\n')
	if _, err := ParseLock(out); err != nil {
		return nil, nil, fmt.Errorf("pin 後驗證失敗: %w", err)
	}
	return out, pinned, nil
}
