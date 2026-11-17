package jadic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ManifestSchema 是 manifest.json 支援的 schema。
const ManifestSchema = "0c-lexicon-manifest/1"

// Manifest 是 pack 旁邊 manifest.json 的必要欄位。只解碼驗證與配對需要的部分，
// 其餘欄位保留不影響讀取。
type Manifest struct {
	Schema        string          `json:"schema"`
	Lexicon       string          `json:"lexicon"`
	Target        string          `json:"target"`
	PackFormat    PackFormat      `json:"pack_format"`
	ReadingScript string          `json:"reading_script"`
	Counts        ManifestCounts  `json:"counts"`
	Outputs       ManifestOutputs `json:"outputs"`
	Allowed       []string        `json:"allowed_sources"`

	// FileSHA256 是 manifest.json 本身的雜湊，供匯入回執記錄。
	FileSHA256 string `json:"-"`
}

// PackFormat 識別 pack 格式名稱與版本。
type PackFormat struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// ManifestCounts 是 manifest 宣告的數量，必須與 pack 檔頭一致。
type ManifestCounts struct {
	Entries   int64 `json:"entries"`
	Surfaces  int64 `json:"surfaces"`
	POS       int64 `json:"pos"`
	ConjTypes int64 `json:"conj_types"`
	ConjForms int64 `json:"conj_forms"`
}

// ManifestOutputs 是 manifest 的輸出區塊。
type ManifestOutputs struct {
	Pack ManifestPack `json:"pack"`
}

// ManifestPack 是 manifest 對 pack 檔的宣告。
type ManifestPack struct {
	Path          string `json:"path"`
	Bytes         int64  `json:"bytes"`
	SHA256        string `json:"sha256"`
	PayloadSHA256 string `json:"payload_sha256"`
}

// LoadManifest 讀取並記錄 manifest.json 的雜湊。
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s：manifest JSON 解析失敗：%w", path, err)
	}
	sum := sha256.Sum256(data)
	m.FileSHA256 = hex.EncodeToString(sum[:])
	return &m, nil
}

// Verify 檢查 manifest 與已解析 pack 是否配對。
// 會驗證 schema、pack_format、數量、整檔 SHA-256、payload SHA-256 與檔案大小。
func (m *Manifest) Verify(p *Pack) error {
	if m == nil {
		return fmt.Errorf("jadic: 缺少 manifest")
	}
	if m.Schema != ManifestSchema {
		return fmt.Errorf("jadic: manifest schema 為 %q，預期 %q", m.Schema, ManifestSchema)
	}
	if m.PackFormat.Name != "0c-jadic" {
		return fmt.Errorf("jadic: manifest 的 pack_format.name 為 %q，預期 0c-jadic", m.PackFormat.Name)
	}
	if m.PackFormat.Version != FormatVersion {
		return fmt.Errorf("jadic: manifest 的 pack_format.version 為 %d，本版只支援 %d", m.PackFormat.Version, FormatVersion)
	}
	switch m.ReadingScript {
	case "hiragana", "katakana":
	default:
		return fmt.Errorf("jadic: manifest 的 reading_script 為 %q，只支援 hiragana 或 katakana", m.ReadingScript)
	}
	if m.Counts.Entries != int64(p.hdr.EntryCount) {
		return fmt.Errorf("jadic: manifest counts.entries=%d 與 pack entry_count=%d 不符", m.Counts.Entries, p.hdr.EntryCount)
	}
	if m.Counts.Surfaces != int64(p.hdr.SurfaceCount) {
		return fmt.Errorf("jadic: manifest counts.surfaces=%d 與 pack surface_count=%d 不符", m.Counts.Surfaces, p.hdr.SurfaceCount)
	}
	if m.Counts.POS != int64(p.hdr.POSCount) {
		return fmt.Errorf("jadic: manifest counts.pos=%d 與 pack pos_count=%d 不符", m.Counts.POS, p.hdr.POSCount)
	}
	if m.Counts.ConjTypes != int64(p.hdr.ConjTypeCount) {
		return fmt.Errorf("jadic: manifest counts.conj_types=%d 與 pack conj_type_count=%d 不符", m.Counts.ConjTypes, p.hdr.ConjTypeCount)
	}
	if m.Counts.ConjForms != int64(p.hdr.ConjFormCount) {
		return fmt.Errorf("jadic: manifest counts.conj_forms=%d 與 pack conj_form_count=%d 不符", m.Counts.ConjForms, p.hdr.ConjFormCount)
	}
	out := m.Outputs.Pack
	if out.Bytes != int64(len(p.data)) {
		return fmt.Errorf("jadic: manifest outputs.pack.bytes=%d 與實際檔案 %d 位元組不符", out.Bytes, len(p.data))
	}
	if !strings.EqualFold(out.SHA256, p.FileSHA256()) {
		return fmt.Errorf("jadic: 整檔 SHA-256 不符（manifest %s，實際 %s）", out.SHA256, p.FileSHA256())
	}
	if !strings.EqualFold(out.PayloadSHA256, p.PayloadSHA256()) {
		return fmt.Errorf("jadic: payload SHA-256 不符（manifest %s，trailer %s）", out.PayloadSHA256, p.PayloadSHA256())
	}
	return nil
}
