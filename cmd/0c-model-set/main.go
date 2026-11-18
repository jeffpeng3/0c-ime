// 0c-model-set 讓安裝器或使用者選擇要安裝的日文模型組（P2-27）。
//
// 模型組定義在 models/pairing.lock.json 的 model_sets 區段：public-core 與 cc-by-sa
// 各自是一組 reranker + punctuation，不得混用。這個工具只搬運已下載或已匯出的
// ONNX，不連網、不訓練，也不會把模型檔寫進 Git。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0c-biggo/0c-ime/internal/atomicfile"
	"github.com/0c-biggo/0c-ime/internal/modelsets"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "0c-model-set:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("需要指令")
	}
	switch args[0] {
	case "list":
		return list(args[1:])
	case "show":
		return show(args[1:])
	case "validate":
		return validate(args[1:])
	case "install":
		return install(args[1:])
	case "pin":
		return pin(args[1:])
	case "fetch":
		return fetch(args[1:])
	case "check":
		return check(args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		return fmt.Errorf("未知指令 %q（可用：list、show、validate、install、pin、fetch、check）", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `用法：0c-model-set <指令> [選項]

  list      列出模型組
  show      輸出一個模型組的 manifest（JSON）
  validate  驗證模型組、字典包與檔案
  install   安裝所選模型組並寫出 active.json
  pin       把 HF repo 的 revision 與逐檔大小／SHA-256 寫進 lock
  fetch     從 HF（或快取）下載所選模型組到目錄
  check     只驗證 lock schema 與雜湊欄位，不讀模型、不連網

共同選項：
  -lock PATH          模型組清單（預設 models/pairing.lock.json）

validate／install：
  -set ID             模型組 ID
  -from DIR           模型來源根目錄（validate 指定時核對檔案；install 必填）
  -pack PATH          已安裝字典包目錄或 manifest.json
  -pack-sha256 SHA    直接指定字典包 manifest 的 SHA-256（取代 -pack）
  -format int8|fp32   要驗證／安裝的模型檔（預設 int8）
  -allow-placeholders 允許 placeholder 模型（權重未訓練，預設關閉）
  -release            正式發行模式：缺 revision／雜湊或 placeholder 直接失敗

install：
  -to DIR             安裝根目錄（寫入 active.json）
  -force              同一個模型組已存在時重新安裝

pin：
  -repo OWNER/NAME    Hugging Face repo
  -revision COMMIT    HF commit 雜湊（必填）
  -base-url URL       HF 或鏡像（預設 https://huggingface.co；可用 file://）
  -cache DIR          下載快取（預設 build/cache/ja-models）
  -offline            只用快取
  -version VERSION    同時更新模型組版本（可選）
  -updated YYYY-MM-DD 更新 model_sets.updated（預設今天）

fetch：
  -set ID、-format int8|fp32、-out DIR
  -base-url、-cache、-offline 同上

check：
  -release            缺 revision／雜湊或 placeholder 直接失敗；development 只警告
`)
}

func commonFlags(fs *flag.FlagSet, lock *string) {
	fs.StringVar(lock, "lock", "models/pairing.lock.json", "模型組清單")
}

func list(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	asJSON := fs.Bool("json", false, "輸出 JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return err
	}
	if *asJSON {
		type row struct {
			ID          string   `json:"id"`
			Variant     string   `json:"variant"`
			Version     string   `json:"version"`
			Placeholder bool     `json:"placeholder"`
			Quality     string   `json:"quality"`
			Artifacts   []string `json:"artifacts"`
		}
		rows := []row{}
		for _, id := range lock.SetIDs() {
			set, _ := lock.Set(id)
			ids := make([]string, 0, len(set.Artifacts))
			for _, artifact := range set.Artifacts {
				ids = append(ids, artifact.ArtifactID)
			}
			sort.Strings(ids)
			rows = append(rows, row{ID: id, Variant: set.Variant, Version: set.Version,
				Placeholder: set.Placeholder, Quality: set.Quality, Artifacts: ids})
		}
		return writeJSON(rows)
	}
	fmt.Printf("%-18s %-12s %-18s %-12s %s\n", "SET", "VARIANT", "VERSION", "PLACEHOLDER", "ARTIFACTS")
	for _, id := range lock.SetIDs() {
		set, _ := lock.Set(id)
		ids := make([]string, 0, len(set.Artifacts))
		for _, artifact := range set.Artifacts {
			ids = append(ids, artifact.ArtifactID)
		}
		fmt.Printf("%-18s %-12s %-18s %-12v %s\n", id, set.Variant, set.Version, set.Placeholder, strings.Join(ids, ", "))
	}
	return nil
}

func show(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	setID := fs.String("set", "", "模型組 ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *setID == "" {
		return errors.New("需要 -set")
	}
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return err
	}
	set, err := lock.Set(*setID)
	if err != nil {
		return err
	}
	return writeJSON(map[string]any{"id": set.ID, "set": set})
}

func validate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	setID := fs.String("set", "", "模型組 ID；空值驗證全部")
	from := fs.String("from", "", "模型來源根目錄（指定時一併核對檔案）")
	pack := fs.String("pack", "", "已安裝字典包目錄或 manifest.json")
	packSHA := fs.String("pack-sha256", "", "直接指定字典包 manifest SHA-256")
	format := fs.String("format", modelsets.FormatInt8, "模型檔格式：int8 或 fp32")
	allow := fs.Bool("allow-placeholders", false, "允許 placeholder 模型")
	release := fs.Bool("release", false, "正式發行模式：缺 revision／雜湊直接失敗")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return err
	}
	sha, packID, err := dictionary(*pack, *packSHA)
	if err != nil {
		return err
	}
	ids := lock.SetIDs()
	if *setID != "" {
		ids = []string{*setID}
	}
	for _, id := range ids {
		set, err := lock.Set(id)
		if err != nil {
			return err
		}
		if !*release {
			for _, warning := range set.Unpinned() {
				fmt.Fprintf(os.Stderr, "0c-model-set: development 模式警告：%s: %s\n", id, warning)
			}
		}
		result, err := set.Validate(modelsets.Options{
			AllowPlaceholders: *allow, Release: *release, InstalledDictionarySHA256: sha,
			InstalledDictionaryPackID: packID, ArtifactRoot: *from, Format: *format, VerifyFiles: *from != "",
		})
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		if err := writeJSON(result); err != nil {
			return err
		}
	}
	return nil
}

func install(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	setID := fs.String("set", "", "模型組 ID")
	from := fs.String("from", "", "模型來源根目錄")
	to := fs.String("to", "", "安裝根目錄")
	pack := fs.String("pack", "", "已安裝字典包目錄或 manifest.json")
	packSHA := fs.String("pack-sha256", "", "直接指定字典包 manifest SHA-256")
	format := fs.String("format", modelsets.FormatInt8, "模型檔格式：int8 或 fp32")
	allow := fs.Bool("allow-placeholders", false, "允許 placeholder 模型")
	release := fs.Bool("release", false, "正式發行模式：缺 revision／雜湊直接失敗")
	force := fs.Bool("force", false, "同一個模型組已存在時重新安裝")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *setID == "" || *from == "" || *to == "" {
		return errors.New("install 需要 -set、-from 與 -to")
	}
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return err
	}
	sha, packID, err := dictionary(*pack, *packSHA)
	if err != nil {
		return err
	}
	selection, err := modelsets.Install(lock, *setID, modelsets.InstallOptions{
		SourceRoot: *from, DestRoot: *to, Format: *format, AllowPlaceholders: *allow, Release: *release,
		InstalledDictionarySHA256: sha, InstalledDictionaryPackID: packID, Force: *force,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "已安裝 %s（%s，%s）到 %s\n", selection.SetID, selection.Variant, selection.Format, *to)
	return writeJSON(selection)
}

func pin(args []string) error {
	fs := flag.NewFlagSet("pin", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	repo := fs.String("repo", "", "Hugging Face repo，例如 jeffpeng3/0c-ja-reranker")
	revision := fs.String("revision", "", "HF commit 雜湊")
	base := fs.String("base-url", modelsets.DefaultBaseURL, "HF 或鏡像 URL")
	cache := fs.String("cache", filepath.Join("build", "cache", "ja-models"), "下載快取")
	offline := fs.Bool("offline", false, "只用快取")
	version := fs.String("version", "", "同時更新模型組版本")
	updated := fs.String("updated", time.Now().UTC().Format("2006-01-02"), "model_sets.updated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" || *revision == "" {
		return errors.New("pin 需要 -repo 與 -revision")
	}
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return err
	}
	bytes, files, err := modelsets.Pin(context.Background(), raw, modelsets.PinOptions{
		Repository: *repo, Revision: *revision, Version: *version, Updated: *updated,
		BaseURL: *base, CacheDir: *cache, Offline: *offline,
	})
	if err != nil {
		return err
	}
	if err := atomicfile.Write(lockPath, bytes); err != nil {
		return err
	}
	return writeJSON(map[string]any{"repository": *repo, "revision": *revision, "files": files})
}

func fetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	setID := fs.String("set", "", "模型組 ID")
	format := fs.String("format", modelsets.FormatInt8, "模型檔格式：int8 或 fp32")
	out := fs.String("out", "", "輸出目錄（依 lock 的相對路徑）")
	base := fs.String("base-url", modelsets.DefaultBaseURL, "HF 或鏡像 URL")
	cache := fs.String("cache", filepath.Join("build", "cache", "ja-models"), "下載快取")
	offline := fs.Bool("offline", false, "只用快取")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *setID == "" || *out == "" {
		return errors.New("fetch 需要 -set 與 -out")
	}
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return err
	}
	fetcher := &modelsets.Fetcher{BaseURL: *base, CacheDir: *cache, Offline: *offline}
	files, err := fetcher.FetchSet(context.Background(), lock, *setID, *format, *out)
	if err != nil {
		return err
	}
	return writeJSON(map[string]any{"set_id": *setID, "format": *format, "files": files})
}

func check(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	release := fs.Bool("release", false, "正式發行模式")
	if err := fs.Parse(args); err != nil {
		return err
	}
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return err
	}
	var outer struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return fmt.Errorf("pairing.lock 不是合法 JSON: %w", err)
	}
	if outer.Schema != 1 {
		return fmt.Errorf("pairing.lock schema=%d，應為 1", outer.Schema)
	}
	lock, err := modelsets.ParseLock(raw)
	if err != nil {
		return err
	}
	var warnings []string
	seenWarnings := map[string]bool{}
	addWarning := func(w string) {
		if !seenWarnings[w] {
			seenWarnings[w] = true
			warnings = append(warnings, w)
		}
	}
	for _, id := range lock.SetIDs() {
		set, err := lock.Set(id)
		if err != nil {
			return err
		}
		if *release {
			if err := set.ValidateRelease(); err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
		} else {
			if err := set.ValidateStructure(); err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
			for _, warning := range set.Unpinned() {
				addWarning(warning)
			}
			if set.Placeholder {
				addWarning(id + ": placeholder 模型組不得進入正式發行")
			}
		}
	}
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "0c-model-set: development 模式警告：%s\n", warning)
	}
	return writeJSON(map[string]any{
		"mode": map[bool]string{true: "release", false: "development"}[*release],
		"sets": lock.SetIDs(), "warnings": warnings,
	})
}

// dictionary 解析 -pack 或 -pack-sha256。目錄可以是 <data>（含 ja/manifest.json）或
// 日文包目錄本身。
func dictionary(pack, sha string) (string, string, error) {
	if sha != "" {
		if len(sha) != 64 || strings.ToLower(sha) != sha {
			return "", "", errors.New("-pack-sha256 必須是 64 個小寫十六進位字元")
		}
		return sha, "", nil
	}
	if pack == "" {
		return "", "", errors.New("需要 -pack 或 -pack-sha256 才能核對字典包")
	}
	manifest, err := packManifest(pack)
	if err != nil {
		return "", "", err
	}
	digest, err := modelsets.DictionarySHA256(manifest)
	if err != nil {
		return "", "", err
	}
	return digest, modelsets.DictionaryPackID(manifest), nil
}

func packManifest(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return path, nil
	}
	for _, candidate := range []string{filepath.Join(path, "manifest.json"), filepath.Join(path, "ja", "manifest.json")} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s 沒有 manifest.json", path)
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
