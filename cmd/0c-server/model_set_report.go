package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0c-biggo/0c-ime/internal/modelsets"
)

// modelSetReport 是 `0c-server -model-report` 的日文模型組區段（P2-27）。它重用
// P2-33 的報告輸出：常駐量與 placeholder 設定不變，另外說明哪一組模型是使用中的、
// variant、字典包是否相符，以及模型檔是否通過大小與 SHA-256 核對。
type modelSetReport struct {
	Active            bool                       `json:"active"`
	Source            string                     `json:"source,omitempty"`
	SetID             string                     `json:"set_id,omitempty"`
	Variant           string                     `json:"variant,omitempty"`
	Version           string                     `json:"version,omitempty"`
	Format            string                     `json:"format,omitempty"`
	Placeholder       bool                       `json:"placeholder"`
	Quality           string                     `json:"quality,omitempty"`
	Dictionary        modelSetDictionaryReport   `json:"dictionary"`
	DictionaryMatches bool                       `json:"dictionary_matches"`
	FilesVerified     bool                       `json:"files_verified"`
	Valid             bool                       `json:"valid"`
	Reason            string                     `json:"reason,omitempty"`
	Artifacts         []modelsets.ArtifactStatus `json:"artifacts"`
}

type modelSetDictionaryReport struct {
	PackID                  string `json:"pack_id,omitempty"`
	Version                 string `json:"version,omitempty"`
	ManifestSHA256          string `json:"manifest_sha256,omitempty"`
	InstalledManifestSHA256 string `json:"installed_manifest_sha256,omitempty"`
}

// buildModelSetReport 解析使用中的模型組：setID 非空時以旗標為準，否則讀取
// <root>/active.json。任何錯誤只寫進報告，不讓引擎啟動失敗。root 是安裝模型組的
// 根目錄（0c-model-set install -to 的位置）。
func buildModelSetReport(setID, lockPath, root, dataDir string, allowPlaceholders bool) *modelSetReport {
	report := &modelSetReport{Artifacts: []modelsets.ArtifactStatus{}}
	selection := modelsets.Selection{}
	switch {
	case setID != "":
		report.Source = "flag"
	case root != "":
		read, err := modelsets.ReadSelection(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				report.Reason = "not_configured"
			} else {
				report.Reason = "active.json：" + err.Error()
			}
			return report
		}
		selection, setID = read, read.SetID
		report.Source = "active.json"
	default:
		report.Reason = "not_configured"
		return report
	}
	report.Active, report.SetID = true, setID

	lock, err := modelsets.LoadLock(lockPath)
	if err != nil {
		report.Reason = "模型組清單：" + err.Error()
		return report
	}
	set, err := lock.Set(setID)
	if err != nil {
		report.Reason = err.Error()
		return report
	}
	report.Variant, report.Version = set.Variant, set.Version
	report.Placeholder, report.Quality = set.Placeholder, set.Quality
	report.Dictionary = modelSetDictionaryReport{
		PackID: set.Dictionary.PackID, Version: set.Dictionary.Version, ManifestSHA256: set.Dictionary.ManifestSHA256,
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
	report.DictionaryMatches = installed.SHA256 == set.Dictionary.ManifestSHA256 &&
		(installed.PackID == "" || installed.PackID == set.Dictionary.PackID)

	result, err := set.Validate(modelsets.Options{
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
// 報告仍然可以說明模型組本身。
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
