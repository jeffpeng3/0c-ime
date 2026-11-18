package modelsets

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/0c-biggo/0c-ime/internal/atomicfile"
)

// SelectionSchema 是安裝後 active.json 的格式。它只記錄目前模型包的版本與實際安裝
// 的檔案；模型檔本身不進 Git，也沒有可切換的模型組。
const SelectionSchema = "0c.model-package.selection/1"

// Selection 是 models/active.json（或個人模型目錄的 active.json）的內容。
type Selection struct {
	Schema      string              `json:"schema"`
	Version     string              `json:"version"`
	Format      string              `json:"format"`
	Placeholder bool                `json:"placeholder"`
	Quality     string              `json:"quality"`
	Dictionary  Dictionary          `json:"dictionary"`
	Artifacts   []SelectionArtifact `json:"artifacts"`
	Assets      []SelectionAsset    `json:"assets,omitempty"`
}

// SelectionAsset records a manifest, vocabulary or tokenizer file installed
// beside the weights. Path is relative to the model root.
type SelectionAsset struct {
	ArtifactID string `json:"artifact_id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
}

// SelectionArtifact 是使用中的模型檔。Path 相對於模型根目錄。
type SelectionArtifact struct {
	ArtifactID  string `json:"artifact_id"`
	Kind        string `json:"kind"`
	Variant     string `json:"variant"`
	Format      string `json:"format"`
	Path        string `json:"path"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	Placeholder bool   `json:"placeholder"`
	Quality     string `json:"quality"`
}

// InstallOptions 是安裝模型包的參數。SourceRoot 是已下載／匯出的模型根目錄（含
// manifest.json 與 ONNX）；DestRoot 是安裝根目錄，active.json 會寫在這裡。
type InstallOptions struct {
	SourceRoot                string
	DestRoot                  string
	Format                    string
	AllowPlaceholders         bool
	Release                   bool
	InstalledDictionarySHA256 string
	InstalledDictionaryPackID string
	Force                     bool
}

// SelectionPath 回傳安裝根目錄的 active.json 路徑。
func SelectionPath(root string) string { return filepath.Join(root, "active.json") }

// ReadSelection 讀取使用中的模型包。檔案不存在時回傳 os.ErrNotExist。
func ReadSelection(root string) (Selection, error) {
	var s Selection
	b, err := os.ReadFile(SelectionPath(root))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%w: %v", ErrSelection, err)
	}
	if s.Schema != SelectionSchema || s.Version == "" || len(s.Artifacts) == 0 {
		return s, fmt.Errorf("%w: active.json 格式不符", ErrSelection)
	}
	return s, nil
}

// Install 驗證並安裝唯一的模型包。只會放入 lock 列的模型檔；不清除其他版本或語言
// 的目錄。回傳的 Selection 已寫入 <DestRoot>/active.json。模型檔一律由呼叫端提供，
// 不從網路下載；repo 不保存 .onnx。
func Install(lock Lock, opts InstallOptions) (Selection, error) {
	pkg := lock.ModelPackage
	if pkg == nil {
		return Selection{}, fmt.Errorf("%w: 缺少 model_package", ErrSchema)
	}
	format := opts.Format
	if format == "" {
		format = FormatInt8
	}
	_, err := pkg.Validate(Options{
		AllowPlaceholders:         opts.AllowPlaceholders,
		Release:                   opts.Release,
		InstalledDictionarySHA256: opts.InstalledDictionarySHA256,
		InstalledDictionaryPackID: opts.InstalledDictionaryPackID,
		ArtifactRoot:              opts.SourceRoot,
		Format:                    format,
		VerifyFiles:               true,
	})
	if err != nil {
		return Selection{}, err
	}
	if opts.SourceRoot == "" || opts.DestRoot == "" {
		return Selection{}, fmt.Errorf("%w: 需要來源與目的目錄", ErrSelection)
	}
	if err := os.MkdirAll(opts.DestRoot, 0o755); err != nil {
		return Selection{}, err
	}
	// 先複製到同目錄的暫存樹；全部成功後才動安裝目錄。
	stage, err := os.MkdirTemp(opts.DestRoot, ".model-package-")
	if err != nil {
		return Selection{}, err
	}
	defer os.RemoveAll(stage)

	selection := Selection{
		Schema: SelectionSchema, Version: pkg.Version,
		Format: format, Placeholder: pkg.Placeholder, Quality: pkg.Quality, Dictionary: pkg.Dictionary,
		Artifacts: make([]SelectionArtifact, 0, len(pkg.Artifacts)),
		Assets:    make([]SelectionAsset, 0),
	}
	for i := range pkg.Artifacts {
		artifact := &pkg.Artifacts[i]
		file := artifact.Files[format]
		from := filepath.Join(opts.SourceRoot, filepath.FromSlash(file.Path))
		to := filepath.Join(stage, filepath.FromSlash(file.Path))
		if err := copyFile(from, to, file.Bytes); err != nil {
			return Selection{}, err
		}
		selection.Artifacts = append(selection.Artifacts, SelectionArtifact{
			ArtifactID: artifact.ArtifactID, Kind: artifact.Kind, Variant: artifact.Variant,
			Format: format, Path: file.Path, Bytes: file.Bytes, SHA256: file.SHA256,
			Placeholder: artifact.Placeholder, Quality: artifact.Quality,
		})
		for _, asset := range artifact.Assets {
			from := filepath.Join(opts.SourceRoot, filepath.FromSlash(asset.Path))
			to := filepath.Join(stage, filepath.FromSlash(asset.Path))
			if err := copyFile(from, to, asset.Bytes); err != nil {
				return Selection{}, err
			}
			selection.Assets = append(selection.Assets, SelectionAsset{
				ArtifactID: artifact.ArtifactID, Name: asset.Name, Path: asset.Path,
				Bytes: asset.Bytes, SHA256: asset.SHA256,
			})
		}
	}

	moved := map[string]bool{}
	for _, artifact := range pkg.Artifacts {
		paths := []string{artifact.Files[format].Path}
		for _, asset := range artifact.Assets {
			paths = append(paths, asset.Path)
		}
		for _, path := range paths {
			top := topDir(path)
			if top == "" || moved[top] {
				continue
			}
			moved[top] = true
			target := filepath.Join(opts.DestRoot, filepath.FromSlash(top))
			if _, err := os.Stat(target); err == nil && !opts.Force {
				return Selection{}, fmt.Errorf("%w: %s 已存在（重裝請加 -force）", ErrSelection, target)
			}
			if err := os.RemoveAll(target); err != nil {
				return Selection{}, err
			}
			if err := os.Rename(filepath.Join(stage, filepath.FromSlash(top)), target); err != nil {
				return Selection{}, err
			}
		}
	}
	if err := atomicfile.WriteJSON(SelectionPath(opts.DestRoot), selection); err != nil {
		return Selection{}, err
	}
	// 重新核對安裝後的大小與雜湊；避免複製失敗留下看似可用的檔案。
	for _, artifact := range pkg.Artifacts {
		file := artifact.Files[format]
		if _, err := verifyFile(opts.DestRoot, file); err != nil {
			return Selection{}, err
		}
		for _, asset := range artifact.Assets {
			if _, err := verifyFile(opts.DestRoot, ArtifactFile{Path: asset.Path, Bytes: asset.Bytes, SHA256: asset.SHA256}); err != nil {
				return Selection{}, err
			}
		}
	}
	return selection, nil
}

// SelectionResult 驗證 active.json 指向的模型包仍與目前 lock、字典包及實際檔案一致。
func SelectionResult(lock Lock, selection Selection, root, installedDictionarySHA string, allowPlaceholders bool) (Result, error) {
	pkg := lock.ModelPackage
	if pkg == nil {
		return Result{}, fmt.Errorf("%w: 缺少 model_package", ErrSchema)
	}
	if selection.Version != "" && selection.Version != pkg.Version {
		return Result{}, fmt.Errorf("%w: active.json 的版本是 %q，清單是 %q", ErrSelection, selection.Version, pkg.Version)
	}
	if selection.Dictionary.ManifestSHA256 != "" && selection.Dictionary.ManifestSHA256 != pkg.Dictionary.ManifestSHA256 {
		return Result{}, fmt.Errorf("%w: active.json 綁定的字典與清單不同", ErrDictionaryMismatch)
	}
	return pkg.Validate(Options{
		AllowPlaceholders:         allowPlaceholders,
		InstalledDictionarySHA256: installedDictionarySHA,
		ArtifactRoot:              root,
		Format:                    selection.Format,
		VerifyFiles:               true,
	})
}

func topDir(path string) string {
	path = filepath.ToSlash(path)
	if i := strings.IndexByte(path, '/'); i > 0 {
		return path[:i]
	}
	return path
}

func copyFile(from, to string, want int64) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		dst.Close()
		if !ok {
			os.Remove(to)
		}
	}()
	n, err := io.Copy(dst, src)
	if err != nil {
		return err
	}
	if want > 0 && n != want {
		return fmt.Errorf("%w: %s 複製 %d bytes，清單記 %d", ErrFileMismatch, from, n, want)
	}
	if err := dst.Sync(); err != nil {
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}
