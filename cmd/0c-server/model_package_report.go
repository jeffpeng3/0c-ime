package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0c-biggo/0c-ime/internal/modelsets"
)

// modelPackageReport 是 `0c-server -model-report` 的日文模型包區段（P2-27）。它重用
// P2-33 的報告輸出：常駐量與 placeholder 設定不變，另外說明模型包版本、字典包是否
// 相符，以及模型檔是否通過大小與 SHA-256 核對。產品只有一個模型包，沒有可選的組。
type modelPackageReport struct {
	Active            bool                       `json:"active"`
	Source            string                     `json:"source,omitempty"`
	Version           string                     `json:"version,omitempty"`
	Format            string                     `json:"format,omitempty"`
	Placeholder       bool                       `json:"placeholder"`
	Quality           string                     `json:"quality,omitempty"`
	Dictionary        modelPackageDictionary     `json:"dictionary"`
	DictionaryMatches bool                       `json:"dictionary_matches"`
	FilesVerified     bool                       `json:"files_verified"`
	Valid             bool                       `json:"valid"`
	Reason            string                     `json:"reason,omitempty"`
	Artifacts         []modelsets.ArtifactStatus `json:"artifacts"`
}

type modelPackageDictionary struct {
	PackID                  string `json:"pack_id,omitempty"`
	Version                 string `json:"version,omitempty"`
	ManifestSHA256          string `json:"manifest_sha256,omitempty"`
	InstalledManifestSHA256 string `json:"installed_manifest_sha256,omitempty"`
}

// buildModelPackageReport 解析使用中的模型包：讀取 <root>/active.json，沒有 active.json
// 時回報 not_configured。任何錯誤只寫進報告，不讓引擎啟動失敗。root 是安裝模型包的
// 根目錄（0c-model-set install -to 的位置）。
func buildModelPackageReport(lockPath, root, dataDir string, allowPlaceholders bool) *modelPackageReport {
	report := &modelPackageReport{Artifacts: []modelsets.ArtifactStatus{}}
	if root == "" {
		report.Reason = "not_configured"
		return report
	}
	selection, err := modelsets.ReadSelection(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			report.Reason = "not_configured"
		} else {
			report.Reason = "active.json：" + err.Error()
		}
		return report
	}
	report.Active, report.Source = true, "active.json"

	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		report.Reason = "模型包清單：" + err.Error()
		return report
	}
	pkg := lock.ModelPackage
	if pkg == nil {
		report.Reason = "模型包清單缺少 model_package"
		return report
	}
	report.Version = pkg.Version
	report.Placeholder, report.Quality = pkg.Placeholder, pkg.Quality
	report.Dictionary = modelPackageDictionary{
		PackID: pkg.Dictionary.PackID, Version: pkg.Dictionary.Version, ManifestSHA256: pkg.Dictionary.ManifestSHA256,
	}
	format := selection.Format
	if format == "" {
		format = modelsets.FormatInt8
	}
	report.Format = format

	installed, err := installedJADictionary(dataDir)
	if err != nil {
		report.Reason = err.Error()
		return report
	}
	report.Dictionary.InstalledManifestSHA256 = installed.SHA256
	report.DictionaryMatches = installed.SHA256 == pkg.Dictionary.ManifestSHA256 &&
		(installed.PackID == "" || installed.PackID == pkg.Dictionary.PackID)

	result, err := pkg.Validate(modelsets.Options{
		AllowPlaceholders:         allowPlaceholders,
		InstalledDictionarySHA256: installed.SHA256,
		InstalledDictionaryPackID: installed.PackID,
		ArtifactRoot:              root,
		Format:                    format,
		VerifyFiles:               true,
	})
	if err != nil {
		report.Reason = err.Error()
		return report
	}
	report.Valid, report.FilesVerified = true, true
	report.Artifacts = result.Artifacts
	return report
}

type installedDictionary struct {
	SHA256 string
	PackID string
}

// installedJADictionary 找引擎資料目錄下的日文包 manifest：<data>/ja/manifest.json
// 優先，其次 <data>/manifest.json（只有它真的是日文包時才採用）。找不到時回報錯誤，
// 報告仍然可以說明模型包本身。
func installedJADictionary(dataDir string) (installedDictionary, error) {
	var out installedDictionary
	if dataDir == "" {
		return out, fmt.Errorf("沒有資料包目錄，無法核對字典")
	}
	for i, manifest := range []string{
		filepath.Join(dataDir, "ja", "manifest.json"),
		filepath.Join(dataDir, "manifest.json"),
	} {
		b, err := os.ReadFile(manifest)
		if err != nil {
			continue
		}
		var m struct {
			Language string `json:"language"`
			Lexicon  string `json:"lexicon"`
			PackID   string `json:"pack_id"`
		}
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		// <data>/manifest.json 只有在明示 language=ja 時才算日文包；<data>/ja/manifest.json
		// 允許舊包只寫 lexicon。
		if m.Language != "ja" && (i > 0 || m.Lexicon == "") {
			continue
		}
		sha, err := modelsets.DictionarySHA256(manifest)
		if err != nil {
			return out, err
		}
		out.SHA256 = sha
		out.PackID = modelsets.DictionaryPackID(manifest)
		return out, nil
	}
	return out, fmt.Errorf("找不到日文詞典包 manifest（%s）", filepath.Join(dataDir, "ja", "manifest.json"))
}
