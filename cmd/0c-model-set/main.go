// 0c-model-set 安裝與檢查唯一的日文模型包（P2-27）。
//
// 模型包定義在 models/pairing.lock.json 的 model_package 區段：CC BY-SA reranker
// 加 MIT punctuation，綁定一個日文詞典包。產品沒有模型組可選；這個工具只搬運
// 已下載或已匯出的 ONNX，不連網、不訓練，也不會把模型檔寫進 Git。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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

  list      列出日文模型包
  show      輸出模型包的 manifest（JSON）
  validate  驗證模型包、字典包與檔案
  install   安裝模型包並寫出 active.json
  pin       把 HF repo 的 revision 與逐檔大小／SHA-256 寫進 lock
  fetch     從 HF（或快取）下載模型包到目錄
  check     只驗證 lock schema 與雜湊欄位，不讀模型、不連網

共同選項：
  -lock PATH          模型包清單（預設 models/pairing.lock.json）

validate／install：
  -from DIR           模型來源根目錄（validate 指定時核對檔案；install 必填）
  -pack PATH          已安裝字典包目錄或 manifest.json
  -pack-sha256 SHA    直接指定字典包 manifest 的 SHA-256（取代 -pack）
  -format int8|fp32   要驗證／安裝的模型檔（預設 int8）
  -allow-placeholders 允許 placeholder 模型（權重未訓練，預設關閉）
  -release            正式發行模式：缺 revision／雜湊或 placeholder 直接失敗

install：
  -to DIR             安裝根目錄（寫入 active.json）
  -force              模型包已存在時重新安裝

pin：
  -repo OWNER/NAME    Hugging Face repo
  -revision COMMIT    HF commit 雜湊（必填）
  -base-url URL       HF 或鏡像（預設 https://huggingface.co；可用 file://）
  -cache DIR          下載快取（預設 build/cache/ja-models）
  -offline            只用快取
  -version VERSION    同時更新模型包版本（可選）
  -updated YYYY-MM-DD 更新 model_package.updated（預設今天）

fetch：
  -format int8|fp32、-out DIR
  -base-url、-cache、-offline 同上

check：
  -release            缺 revision／雜湊或 placeholder 直接失敗；development 只警告
`)
}

func commonFlags(fs *flag.FlagSet, lock *string) {
	fs.StringVar(lock, "lock", "models/pairing.lock.json", "模型包清單")
}

func loadPackage(lockPath string) (*modelsets.Package, error) {
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return nil, err
	}
	if lock.ModelPackage == nil {
		return nil, fmt.Errorf("%w: 缺少 model_package", modelsets.ErrSchema)
	}
	return lock.ModelPackage, nil
}

func list(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	asJSON := fs.Bool("json", false, "輸出 JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pkg, err := loadPackage(lockPath)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(map[string]any{
			"version": pkg.Version, "placeholder": pkg.Placeholder, "quality": pkg.Quality,
			"production_eligible": pkg.ProductionEligible, "dictionary": pkg.Dictionary,
			"artifacts": artifactIDs(pkg),
		})
	}
	fmt.Printf("MODEL PACKAGE  %s\n", pkg.Version)
	fmt.Printf("PLACEHOLDER    %v\n", pkg.Placeholder)
	fmt.Printf("ARTIFACTS      %s\n", strings.Join(artifactIDs(pkg), ", "))
	return nil
}

func artifactIDs(pkg *modelsets.Package) []string {
	ids := make([]string, 0, len(pkg.Artifacts))
	for _, artifact := range pkg.Artifacts {
		ids = append(ids, artifact.ArtifactID)
	}
	return ids
}

func show(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	if err := fs.Parse(args); err != nil {
		return err
	}
	pkg, err := loadPackage(lockPath)
	if err != nil {
		return err
	}
	return writeJSON(pkg)
}

func validate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	from := fs.String("from", "", "模型來源根目錄（指定時一併核對檔案）")
	pack := fs.String("pack", "", "已安裝字典包目錄或 manifest.json")
	packSHA := fs.String("pack-sha256", "", "直接指定字典包 manifest SHA-256")
	format := fs.String("format", modelsets.FormatInt8, "模型檔格式：int8 或 fp32")
	allow := fs.Bool("allow-placeholders", false, "允許 placeholder 模型")
	release := fs.Bool("release", false, "正式發行模式：缺 revision／雜湊直接失敗")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pkg, err := loadPackage(lockPath)
	if err != nil {
		return err
	}
	sha, packID, err := dictionary(*pack, *packSHA)
	if err != nil {
		return err
	}
	if !*release {
		for _, warning := range pkg.Unpinned() {
			fmt.Fprintf(os.Stderr, "0c-model-set: development 模式警告：%s\n", warning)
		}
	}
	result, err := pkg.Validate(modelsets.Options{
		AllowPlaceholders: *allow, Release: *release, InstalledDictionarySHA256: sha,
		InstalledDictionaryPackID: packID, ArtifactRoot: *from, Format: *format, VerifyFiles: *from != "",
	})
	if err != nil {
		return err
	}
	return writeJSON(result)
}

func install(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	from := fs.String("from", "", "模型來源根目錄")
	to := fs.String("to", "", "安裝根目錄")
	pack := fs.String("pack", "", "已安裝字典包目錄或 manifest.json")
	packSHA := fs.String("pack-sha256", "", "直接指定字典包 manifest SHA-256")
	format := fs.String("format", modelsets.FormatInt8, "模型檔格式：int8 或 fp32")
	allow := fs.Bool("allow-placeholders", false, "允許 placeholder 模型")
	release := fs.Bool("release", false, "正式發行模式：缺 revision／雜湊直接失敗")
	force := fs.Bool("force", false, "模型包已存在時重新安裝")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return errors.New("install 需要 -from 與 -to")
	}
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return err
	}
	sha, packID, err := dictionary(*pack, *packSHA)
	if err != nil {
		return err
	}
	selection, err := modelsets.Install(lock, modelsets.InstallOptions{
		SourceRoot: *from, DestRoot: *to, Format: *format, AllowPlaceholders: *allow, Release: *release,
		InstalledDictionarySHA256: sha, InstalledDictionaryPackID: packID, Force: *force,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "已安裝模型包 %s（%s）到 %s\n", selection.Version, selection.Format, *to)
	return writeJSON(selection)
}

func pin(args []string) error {
	fs := flag.NewFlagSet("pin", flag.ContinueOnError)
	var lockPath string
	commonFlags(fs, &lockPath)
	repo := fs.String("repo", "", "Hugging Face repo，例如 jeffpeng3/0c-ja-reranker-extra")
	revision := fs.String("revision", "", "HF commit 雜湊")
	base := fs.String("base-url", modelsets.DefaultBaseURL, "HF 或鏡像 URL")
	cache := fs.String("cache", filepath.Join("build", "cache", "ja-models"), "下載快取")
	offline := fs.Bool("offline", false, "只用快取")
	version := fs.String("version", "", "同時更新模型包版本")
	updated := fs.String("updated", time.Now().UTC().Format("2006-01-02"), "model_package.updated")
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
	format := fs.String("format", modelsets.FormatInt8, "模型檔格式：int8 或 fp32")
	out := fs.String("out", "", "輸出目錄（依 lock 的相對路徑）")
	base := fs.String("base-url", modelsets.DefaultBaseURL, "HF 或鏡像 URL")
	cache := fs.String("cache", filepath.Join("build", "cache", "ja-models"), "下載快取")
	offline := fs.Bool("offline", false, "只用快取")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("fetch 需要 -out")
	}
	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		return err
	}
	fetcher := &modelsets.Fetcher{BaseURL: *base, CacheDir: *cache, Offline: *offline}
	files, err := fetcher.FetchPackage(context.Background(), lock, *format, *out)
	if err != nil {
		return err
	}
	return writeJSON(map[string]any{"format": *format, "files": files})
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
	pkg := lock.ModelPackage
	var warnings []string
	seenWarnings := map[string]bool{}
	addWarning := func(w string) {
		if !seenWarnings[w] {
			seenWarnings[w] = true
			warnings = append(warnings, w)
		}
	}
	if *release {
		if err := pkg.ValidateRelease(); err != nil {
			return err
		}
	} else {
		if err := pkg.ValidateStructure(); err != nil {
			return err
		}
		for _, warning := range pkg.Unpinned() {
			addWarning(warning)
		}
		if pkg.Placeholder {
			addWarning("placeholder 模型包不得進入正式發行")
		}
	}
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "0c-model-set: development 模式警告：%s\n", warning)
	}
	return writeJSON(map[string]any{
		"mode":    map[bool]string{true: "release", false: "development"}[*release],
		"package": pkg.Version, "warnings": warnings,
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
