package modelsets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultBaseURL 是 Hugging Face。測試或鏡像可換成 httptest 的 URL 或 file:// 目錄。
const DefaultBaseURL = "https://huggingface.co"

// Fetcher 從 Hugging Face 取得模型檔。CacheDir 依 <repo>/<revision>/<檔名> 保存已
// 核對的檔案；Offline 時只用快取，缺檔或雜湊不符都直接失敗，不會退回網路或舊檔。
type Fetcher struct {
	BaseURL  string
	CacheDir string
	Offline  bool
	Client   *http.Client
	Stall    time.Duration
}

// FetchedFile 是一個已下載並核對的檔案。
type FetchedFile struct {
	ArtifactID string `json:"artifact_id"`
	Format     string `json:"format"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
}

// Resolve 取得 repo 在 revision 的 name，依 want（want.Name 可空）核對大小與 SHA-256。
// 快取命中但內容不符時，線上模式會刪掉重抓；離線模式直接回傳 ErrChecksum。
func (f *Fetcher) Resolve(ctx context.Context, repository, revision, name string, want HFFile) (string, error) {
	if err := checkRemoteName(repository, revision, name); err != nil {
		return "", err
	}
	cache := f.cacheDir(repository, revision, name)
	if st, err := os.Stat(cache); err == nil && st.Mode().IsRegular() {
		if ok, got, digest := matchFile(cache, want); ok {
			return cache, nil
		} else if want.Bytes > 0 || want.SHA256 != "" {
			if f.Offline {
				return "", fmt.Errorf("%w: 快取檔案 %s 與 lock 不符（大小 %d、SHA-256 %s）", ErrChecksum, cache, got, digest)
			}
			if err := os.Remove(cache); err != nil {
				return "", err
			}
		}
	}
	if f.Offline {
		return "", fmt.Errorf("%w: 快取沒有 %s@%s/%s（離線模式不連網）", ErrOffline, repository, revision, name)
	}
	source, err := f.sourceURL(repository, revision, name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		return "", err
	}
	if err := f.download(ctx, source, cache, want); err != nil {
		return "", err
	}
	if ok, got, digest := matchFile(cache, want); !ok {
		os.Remove(cache)
		return "", fmt.Errorf("%w: %s 的大小或 SHA-256 與 lock 不符（大小 %d、SHA-256 %s）", ErrChecksum, name, got, digest)
	}
	return cache, nil
}

// FetchPackage 下載模型包在 format 的 ONNX 檔，複製到 dest 的 lock 相對路徑。
// 呼叫前必須先 pin；未 pin 的 lock 會回傳 ErrUnpinned，不做任何猜測。
func (f *Fetcher) FetchPackage(ctx context.Context, lock Lock, format, dest string) ([]FetchedFile, error) {
	pkg := lock.ModelPackage
	if pkg == nil {
		return nil, fmt.Errorf("%w: 缺少 model_package", ErrSchema)
	}
	if err := pkg.ValidateRelease(); err != nil {
		return nil, err
	}
	if format == "" {
		format = FormatInt8
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	var out []FetchedFile
	for i := range pkg.Artifacts {
		artifact := &pkg.Artifacts[i]
		local, ok := artifact.Files[format]
		if !ok {
			return nil, fmt.Errorf("%w: %s 沒有 %s 檔案", ErrSchema, artifact.ArtifactID, format)
		}
		if artifact.HF == nil {
			return nil, fmt.Errorf("%w: %s 缺少 hf 來源", ErrHFSource, artifact.ArtifactID)
		}
		remote, ok := artifact.HF.Files[format]
		if !ok {
			return nil, fmt.Errorf("%w: %s 沒有 %s 的 hf 檔案", ErrHFSource, artifact.ArtifactID, format)
		}
		remote.Name = strings.TrimSpace(remote.Name)
		if remote.Name == "" {
			return nil, fmt.Errorf("%w: %s/%s 缺少 hf 檔名", ErrHFSource, artifact.ArtifactID, format)
		}
		source, err := f.Resolve(ctx, artifact.HF.Repository, artifact.HF.Revision, remote.Name, remote)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", artifact.ArtifactID, format, err)
		}
		target, err := localTarget(dest, local.Path)
		if err != nil {
			return nil, err
		}
		if err := materialize(source, target, local); err != nil {
			return nil, err
		}
		if ok, got, digest := matchFile(target, HFFile{Bytes: local.Bytes, SHA256: local.SHA256}); !ok {
			return nil, fmt.Errorf("%w: %s 寫入後與 lock 不符（大小 %d、SHA-256 %s）", ErrChecksum, target, got, digest)
		}
		out = append(out, FetchedFile{
			ArtifactID: artifact.ArtifactID, Format: format, Name: remote.Name,
			Path: target, Bytes: local.Bytes, SHA256: local.SHA256,
		})
	}
	return out, nil
}

func (f *Fetcher) cacheDir(repository, revision, name string) string {
	root := f.CacheDir
	if root == "" {
		root = filepath.Join("build", "cache", "ja-models")
	}
	return filepath.Join(root, filepath.FromSlash(repository), revision, name)
}

func (f *Fetcher) sourceURL(repository, revision, name string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(f.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	if strings.HasPrefix(base, "file://") {
		root := strings.TrimPrefix(base, "file://")
		if root == "" {
			return "", fmt.Errorf("%w: file:// 來源缺少目錄", ErrHFSource)
		}
		path := filepath.Join(root, filepath.FromSlash(repository), "resolve", revision, name)
		return filepath.Abs(path)
	}
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		return base + "/" + repository + "/resolve/" + revision + "/" + name, nil
	}
	return "", fmt.Errorf("%w: 不支援的來源 %q", ErrHFSource, base)
}

func (f *Fetcher) download(ctx context.Context, source, target string, want HFFile) error {
	temp, err := os.CreateTemp(filepath.Dir(target), ".part-")
	if err != nil {
		return err
	}
	name := temp.Name()
	ok := false
	defer func() {
		temp.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	h := sha256.New()
	var got int64
	if strings.HasPrefix(source, "/") || filepath.IsAbs(source) {
		in, err := os.Open(source)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: 找不到 %s", ErrOffline, source)
			}
			return err
		}
		defer in.Close()
		n, err := io.Copy(io.MultiWriter(temp, h), in)
		if err != nil {
			return err
		}
		got = n
	} else {
		client := f.Client
		if client == nil {
			client = http.DefaultClient
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("下載 %s：%w", source, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("下載 %s：HTTP %d", source, resp.StatusCode)
		}
		if want.Bytes > 0 && resp.ContentLength >= 0 && resp.ContentLength != want.Bytes {
			return fmt.Errorf("%w: %s 的 Content-Length %d 與 lock %d 不符", ErrChecksum, source, resp.ContentLength, want.Bytes)
		}
		n, err := io.Copy(io.MultiWriter(temp, h), resp.Body)
		if err != nil {
			return err
		}
		got = n
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if want.Bytes > 0 && got != want.Bytes {
		return fmt.Errorf("%w: %s 下載 %d bytes，lock 記 %d", ErrChecksum, source, got, want.Bytes)
	}
	if want.SHA256 != "" && digest != want.SHA256 {
		return fmt.Errorf("%w: %s 的 SHA-256 %s 與 lock %s 不符", ErrChecksum, source, digest, want.SHA256)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, target); err != nil {
		return err
	}
	ok = true
	return nil
}

// DigestFile 回傳檔案大小與 SHA-256，pin 指令用它填入 lock。
func DigestFile(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		return 0, "", err
	}
	if !st.Mode().IsRegular() {
		return 0, "", fmt.Errorf("%s 不是一般檔案", path)
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return 0, "", err
	}
	return st.Size(), hex.EncodeToString(h.Sum(nil)), nil
}

func matchFile(path string, want HFFile) (bool, int64, string) {
	size, digest, err := DigestFile(path)
	if err != nil {
		return false, 0, ""
	}
	if want.Bytes > 0 && size != want.Bytes {
		return false, size, digest
	}
	if want.SHA256 != "" && digest != want.SHA256 {
		return false, size, digest
	}
	return true, size, digest
}

func checkRemoteName(repository, revision, name string) error {
	if repository == "" || strings.HasPrefix(repository, "/") || strings.Contains(repository, "..") {
		return fmt.Errorf("%w: repository %q 不合法", ErrHFSource, repository)
	}
	if !validRevision(revision) {
		return fmt.Errorf("%w: revision %q 不是 commit 雜湊", ErrUnpinned, revision)
	}
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: 檔名 %q 不合法", ErrHFSource, name)
	}
	return nil
}

func localTarget(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", fmt.Errorf("%w: 模型路徑 %q 不是相對路徑", ErrSchema, rel)
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	clean := filepath.Clean(target)
	if clean != target || !strings.HasPrefix(clean, filepath.Clean(root)+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: 模型路徑 %q 超出輸出目錄", ErrSchema, rel)
	}
	return clean, nil
}

func materialize(source, target string, want ArtifactFile) error {
	if filepath.Clean(source) == filepath.Clean(target) {
		if ok, got, digest := matchFile(target, HFFile{Bytes: want.Bytes, SHA256: want.SHA256}); !ok {
			return fmt.Errorf("%w: %s 的大小或 SHA-256 與 lock 不符（大小 %d、SHA-256 %s）", ErrChecksum, target, got, digest)
		}
		return nil
	}
	if ok, _, _ := matchFile(target, HFFile{Bytes: want.Bytes, SHA256: want.SHA256}); ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	os.Remove(target)
	if err := os.Link(source, target); err == nil {
		return nil
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(target)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}
