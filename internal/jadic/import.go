package jadic

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ImportSchema 是匯入回執的格式版本。
const ImportSchema = "0c-jadic-import/1"

// ImportOptions 控制把 pack 寫進產品資料目錄。
type ImportOptions struct {
	OutDir          string
	PackPath        string
	ManifestPath    string
	ConnectionsPath string // 選填；pack 沒有連接成本矩陣，需要另外提供
	Origin          string
	CostScale       float64
	MaxReadingUnits int
	Force           bool
}

// ImportReceipt 寫在輸出目錄，記錄來源 pack 的雜湊與成本換算。
// 欄位刻意不含時間戳，同一份輸入產生相同位元組。
type ImportReceipt struct {
	Schema   string          `json:"schema"`
	Pack     ReceiptPack     `json:"pack"`
	Manifest ReceiptManifest `json:"manifest"`
	Import   ReceiptImport   `json:"import"`
}

// ReceiptPack 是來源 pack 的識別與雜湊。
type ReceiptPack struct {
	Path          string `json:"path"`
	Bytes         int64  `json:"bytes"`
	SHA256        string `json:"sha256"`
	PayloadSHA256 string `json:"payload_sha256"`
}

// ReceiptManifest 是來源 manifest 的識別與雜湊。
type ReceiptManifest struct {
	Path           string   `json:"path"`
	SHA256         string   `json:"sha256"`
	Schema         string   `json:"schema"`
	Lexicon        string   `json:"lexicon"`
	Target         string   `json:"target"`
	PackFormat     string   `json:"pack_format"`
	FormatVersion  int      `json:"format_version"`
	ReadingScript  string   `json:"reading_script"`
	AllowedSources []string `json:"allowed_sources,omitempty"`
}

// ReceiptImport 是匯入參數與結果。
type ReceiptImport struct {
	Origin              string         `json:"origin"`
	CostScale           float64        `json:"cost_scale"`
	CostRule            string         `json:"cost_rule"`
	Entries             int            `json:"entries"`
	Surfaces            int            `json:"surfaces"`
	NegativeCostClamped int            `json:"negative_cost_clamped"`
	UnknownPOS          int            `json:"unknown_pos"`
	UnknownConj         int            `json:"unknown_conj"`
	MaxReadingUnits     int            `json:"max_reading_units"`
	POSCounts           map[string]int `json:"pos_counts"`
	Files               []string       `json:"files"`
	Needs               []string       `json:"needs,omitempty"`
}

// ImportResult 是匯入後的路徑與統計。
type ImportResult struct {
	WordsPath   string
	ReceiptPath string
	Stats       ConvertStats
	Receipt     ImportReceipt
}

// Import 驗證 manifest 後把 pack 寫成產品可編譯的 words.tsv。
// 輸出目錄必須由呼叫端指定；預設 CLI 指向 build/ 底下，不動 checked-in data/ja。
func Import(p *Pack, m *Manifest, opt ImportOptions) (ImportResult, error) {
	var res ImportResult
	if p == nil {
		return res, fmt.Errorf("jadic: 沒有 pack")
	}
	if err := m.Verify(p); err != nil {
		return res, err
	}
	if opt.OutDir == "" {
		return res, fmt.Errorf("jadic: 匯入需要輸出目錄")
	}
	if opt.MaxReadingUnits == 0 {
		opt.MaxReadingUnits = 64
	}
	wordsPath := filepath.Join(opt.OutDir, "lexicon", "words.tsv")
	receiptPath := filepath.Join(opt.OutDir, "jadic-import.json")
	checked := []string{wordsPath, receiptPath}
	if opt.ConnectionsPath != "" {
		checked = append(checked, filepath.Join(opt.OutDir, "connection.tsv"))
	}
	if !opt.Force {
		for _, path := range checked {
			if _, err := os.Stat(path); err == nil {
				return res, fmt.Errorf("jadic: %s 已存在；要覆寫請加 -force", path)
			}
		}
	}
	if err := os.MkdirAll(filepath.Join(opt.OutDir, "lexicon"), 0o755); err != nil {
		return res, err
	}

	tmp := wordsPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return res, err
	}
	stats, err := Convert(f, p, ConvertOptions{
		Origin:          opt.Origin,
		CostScale:       opt.CostScale,
		MaxReadingUnits: opt.MaxReadingUnits,
	})
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return res, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return res, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return res, err
	}
	if err := os.Rename(tmp, wordsPath); err != nil {
		os.Remove(tmp)
		return res, err
	}

	files := []string{"lexicon/words.tsv", "jadic-import.json"}
	needs := []string{"connection.tsv：pack 不含連接成本矩陣"}
	if opt.ConnectionsPath != "" {
		if err := copyFile(opt.ConnectionsPath, filepath.Join(opt.OutDir, "connection.tsv")); err != nil {
			return res, fmt.Errorf("jadic: 複製 connection.tsv：%w", err)
		}
		files = append(files, "connection.tsv")
		needs = nil
	}

	origin := opt.Origin
	if origin == "" {
		origin = DefaultOrigin
	}
	scale := opt.CostScale
	if scale == 0 {
		scale = DefaultCostScale
	}
	res.Stats = stats
	res.Receipt = ImportReceipt{
		Schema: ImportSchema,
		Pack: ReceiptPack{
			Path:          opt.PackPath,
			Bytes:         p.Bytes(),
			SHA256:        p.FileSHA256(),
			PayloadSHA256: p.PayloadSHA256(),
		},
		Manifest: ReceiptManifest{
			Path:           opt.ManifestPath,
			SHA256:         m.FileSHA256,
			Schema:         m.Schema,
			Lexicon:        m.Lexicon,
			Target:         m.Target,
			PackFormat:     m.PackFormat.Name,
			FormatVersion:  m.PackFormat.Version,
			ReadingScript:  m.ReadingScript,
			AllowedSources: append([]string(nil), m.Allowed...),
		},
		Import: ReceiptImport{
			Origin:              origin,
			CostScale:           scale,
			CostRule:            CostRule,
			Entries:             stats.Entries,
			Surfaces:            stats.Surfaces,
			NegativeCostClamped: stats.NegativeCostClamped,
			UnknownPOS:          stats.UnknownPOS,
			UnknownConj:         stats.UnknownConj,
			MaxReadingUnits:     stats.MaxReadingUnits,
			POSCounts:           stats.POSCounts,
			Files:               files,
			Needs:               needs,
		},
	}
	if err := writeJSON(receiptPath, res.Receipt); err != nil {
		return res, err
	}
	res.WordsPath = wordsPath
	res.ReceiptPath = receiptPath
	return res, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
