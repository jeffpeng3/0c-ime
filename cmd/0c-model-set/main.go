// 0c-model-set 讓安裝器或使用者選擇要安裝的日文模型組（P2-27）。
//
// 模型組定義在 models/pairing.lock.json 的 model_sets 區段：public-core 與 cc-by-sa
// 各自是一組 reranker + punctuation，不得混用。這個工具只搬運已下載或已匯出的
// ONNX，不連網、不訓練，也不會把模型檔寫進 Git。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		return fmt.Errorf("未知指令 %q（可用：list、show、validate、install）", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `用法：0c-model-set <指令> [選項]

  list      列出模型組
  show      輸出一個模型組的 manifest（JSON）
  validate  驗證模型組、字典包與檔案
  install   安裝所選模型組並寫出 active.json

共同選項：
  -lock PATH          模型組清單（預設 models/pairing.lock.json）

validate／install：
  -set ID             模型組 ID
  -from DIR           模型來源根目錄（validate 指定時核對檔案；install 必填）
  -pack PATH          已安裝字典包目錄或 manifest.json
  -pack-sha256 SHA    直接指定字典包 manifest 的 SHA-256（取代 -pack）
  -format int8|fp32   要驗證／安裝的模型檔（預設 int8）
  -allow-placeholders 允許 placeholder 模型（權重未訓練，預設關閉）

install：
  -to DIR             安裝根目錄（寫入 active.json）
  -force              同一個模型組已存在時重新安裝
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
		result, err := set.Validate(modelsets.Options{
			AllowPlaceholders: *allow, InstalledDictionarySHA256: sha,
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
		SourceRoot: *from, DestRoot: *to, Format: *format, AllowPlaceholders: *allow,
		InstalledDictionarySHA256: sha, InstalledDictionaryPackID: packID, Force: *force,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "已安裝 %s（%s，%s）到 %s\n", selection.SetID, selection.Variant, selection.Format, *to)
	return writeJSON(selection)
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
