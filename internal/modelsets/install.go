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

// SelectionSchema 是安裝後 active.json 的格式。它只記錄哪一組是使用中，以及實際安裝的
// 檔案；模型檔本身不進 Git。
const SelectionSchema = "0c.model-set.selection/1"

// Selection 是 models/active.json（或個人模型目錄的 active.json）的內容。
type Selection struct {
	Schema      string              `json:"schema"`
	SetID       string              `json:"set_id"`
	Variant     string              `json:"variant"`
	Version     string              `json:"version"`
	Format      string              `json:"format"`
	Placeholder bool                `json:"placeholder"`
	Quality     string              `json:"quality"`
	Dictionary  Dictionary          `json:"dictionary"`
	Artifacts   []SelectionArtifact `json:"artifacts"`
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

// InstallOptions 是安裝一組模型的參數。SourceRoot 是已下載／匯出的模型根目錄（含
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

// ReadSelection 讀取使用中的模型組。檔案不存在時回傳 os.ErrNotExist。
func ReadSelection(root string) (Selection, error) {
	var s Selection
	b, err := os.ReadFile(SelectionPath(root))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%w: %v", ErrSelection, err)
	}
	if s.Schema != SelectionSchema || s.SetID == "" || len(s.Artifacts) == 0 {
		return s, fmt.Errorf("%w: active.json 格式不符", ErrSelection)
	}
	return s, nil
}

// Install 驗證並安裝一個模型組。只會放入所選 variant 的模型檔；清單中其他模型組的
// 目錄會先移除，避免 public-core 與 cc-by-sa 檔案留在同一個安裝根目錄。
//
// 回傳的 Selection 已寫入 <DestRoot>/active.json。模型檔一律由呼叫端提供，不從網路
// 下載；repo 不保存 .onnx。
func Install(lock Lock, setID string, opts InstallOptions) (Selection, error) {
	set, err := lock.Set(setID)
	if err != nil {
		return Selection{}, err
	}
	format := opts.Format
	if format == "" {
		format = FormatInt8
	}
	_, err = set.Validate(Options{
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
	stage, err := os.MkdirTemp(opts.DestRoot, ".model-set-")
	if err != nil {
		return Selection{}, err
	}
	defer os.RemoveAll(stage)

	selection := Selection{
		Schema: SelectionSchema, SetID: set.ID, Variant: set.Variant, Version: set.Version,
		Format: format, Placeholder: set.Placeholder, Quality: set.Quality, Dictionary: set.Dictionary,
		Artifacts: make([]SelectionArtifact, 0, len(set.Artifacts)),
	}
	for i := range set.Artifacts {
		artifact := &set.Artifacts[i]
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
	}

	// 同一模型組重裝時需要 -force；切換 variant 則直接移除另一組的目錄。
	for _, other := range lock.SetIDs() {
		if other == set.ID {
			continue
		}
		otherSet := lock.ModelSets.Sets[other]
		for _, artifact := range otherSet.Artifacts {
			for _, file := range artifact.Files {
				if top := topDir(file.Path); top != "" {
					if err := os.RemoveAll(filepath.Join(opts.DestRoot, filepath.FromSlash(top))); err != nil {
						return Selection{}, err
					}
				}
			}
		}
	}
	moved := map[string]bool{}
	for _, artifact := range set.Artifacts {
		file := artifact.Files[format]
		top := topDir(file.Path)
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
	if err := atomicfile.WriteJSON(SelectionPath(opts.DestRoot), selection); err != nil {
		return Selection{}, err
	}
	// 重新核對安裝後的大小與雜湊；避免複製失敗留下看似可用的檔案。
	for _, artifact := range set.Artifacts {
		file := artifact.Files[format]
		if _, err := verifyFile(opts.DestRoot, file); err != nil {
			return Selection{}, err
		}
	}
	return selection, nil
}

// SelectionResult 驗證 active.json 指向的模型組仍與目前 lock、字典包及實際檔案一致。
func SelectionResult(lock Lock, selection Selection, root, installedDictionarySHA string, allowPlaceholders bool) (Result, error) {
	set, err := lock.Set(selection.SetID)
	if err != nil {
		return Result{}, err
	}
	if selection.Variant != set.Variant {
		return Result{}, fmt.Errorf("%w: active.json 的 variant=%q，清單是 %q", ErrVariantMixed, selection.Variant, set.Variant)
	}
	if selection.Dictionary.ManifestSHA256 != "" && selection.Dictionary.ManifestSHA256 != set.Dictionary.ManifestSHA256 {
		return Result{}, fmt.Errorf("%w: active.json 綁定的字典與清單不同", ErrDictionaryMismatch)
	}
	return set.Validate(Options{
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
