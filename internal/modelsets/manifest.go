// Package modelsets 管理日文模型組（P2-27）：產品側的模型 manifest、public-core／cc-by-sa
// 版本分離、字典包綁定、placeholder 判定與安裝選擇。
//
// 一份模型組由一個 reranker 與一個 punctuation 模型組成，兩者必須同 variant；模型組另外
// 綁定一個日文詞典包的 manifest SHA-256。public-core 與 cc-by-sa 不得混用，placeholder
// 模型只有在明確允許時才能驗證或安裝。
package modelsets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Schema 是產品側 model_sets 區段的格式版本。外層 models/pairing.lock.json 仍是既有
// 字典配對清單的 schema 1，model_sets 是向後相容的附加區段。
const Schema = "0c.model-sets/1"

// 模型種類與 variant。名稱固定，不得用別名。
const (
	KindReranker    = "reranker"
	KindPunctuation = "punctuation"

	VariantPublicCore = "public-core"
	VariantCCBYSA     = "cc-by-sa"

	FormatInt8 = "int8"
	FormatFP32 = "fp32"

	QualityUntrained = "untrained"
)

// 驗證錯誤分類。呼叫端用 errors.Is 判斷，訊息另外帶上可讀的中文細節。
var (
	ErrSchema              = errors.New("model set schema mismatch")
	ErrVariantMixed        = errors.New("model set mixes variants")
	ErrIncompleteSet       = errors.New("model set is incomplete")
	ErrDictionaryMismatch  = errors.New("model set dictionary mismatch")
	ErrDictionaryUnknown   = errors.New("installed dictionary pack is unknown")
	ErrPlaceholder         = errors.New("placeholder model set is not allowed")
	ErrPlaceholderUnmarked = errors.New("placeholder model set is not marked")
	ErrTensorContract      = errors.New("model set tensor contract mismatch")
	ErrFileMismatch        = errors.New("model set file mismatch")
	ErrLicenseMixed        = errors.New("model set license does not match variant")
	ErrSelection           = errors.New("model set selection is invalid")
)

// Lock 是 models/pairing.lock.json 中 P2-27 讀取的部分。ModelSets 以外的鍵（既有
// 注音字典配對）由 internal/models 解讀，這裡不碰。
type Lock struct {
	Schema    int      `json:"schema"`
	ModelSets *Section `json:"model_sets"`
}

// Section 是 model_sets 區段。
type Section struct {
	Schema       string          `json:"schema"`
	Updated      string          `json:"updated"`
	MixingPolicy string          `json:"mixing_policy"`
	Sets         map[string]*Set `json:"sets"`
}

// Set 是一個可安裝的模型組：一個 reranker、一個 punctuation，同一 variant，綁定同一字典包。
type Set struct {
	ID                 string     `json:"-"`
	Variant            string     `json:"variant"`
	Language           string     `json:"language"`
	Version            string     `json:"version"`
	Description        string     `json:"description"`
	Dictionary         Dictionary `json:"dictionary"`
	License            License    `json:"license"`
	Attribution        []string   `json:"attribution"`
	Placeholder        bool       `json:"placeholder"`
	Quality            string     `json:"quality"`
	ProductionEligible bool       `json:"production_eligible"`
	Artifacts          []Artifact `json:"artifacts"`
}

// Dictionary 是模型組綁定的字典包。ManifestSHA256 是安裝後 <data>/ja/manifest.json 的
// SHA-256；驗證與安裝時必須與實際安裝的包相同。
type Dictionary struct {
	PackID         string `json:"pack_id"`
	Language       string `json:"language"`
	Format         int    `json:"format"`
	Version        string `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Source         string `json:"source"`
}

// License 是模型組或模型檔的授權與散布邊界。Notice 是 repo 內的授權全文參考；
// Attribution 另列姓名標示與來源帳本參考。
type License struct {
	ID           string `json:"id"`
	Scope        string `json:"scope"`
	Distribution string `json:"distribution"`
	Notice       string `json:"notice"`
}

// Source 記錄模型的來源與可重現資訊。
type Source struct {
	TrainingData string   `json:"training_data"`
	Datasets     []string `json:"datasets"`
	Checkpoint   string   `json:"checkpoint"`
	Generator    string   `json:"generator"`
	Fingerprint  string   `json:"fingerprint"`
}

// Artifact 是模型組內的一個模型檔集合。Files 以 fp32／int8 為鍵。
type Artifact struct {
	Manifest                 string                  `json:"manifest"`
	ArtifactID               string                  `json:"artifact_id"`
	Kind                     string                  `json:"kind"`
	Variant                  string                  `json:"variant"`
	Version                  string                  `json:"version"`
	Parameters               int64                   `json:"parameters"`
	Placeholder              bool                    `json:"placeholder"`
	Quality                  string                  `json:"quality"`
	ProductionEligible       bool                    `json:"production_eligible"`
	DictionaryManifestSHA256 string                  `json:"dictionary_manifest_sha256"`
	License                  License                 `json:"license"`
	Attribution              []string                `json:"attribution"`
	Source                   Source                  `json:"source"`
	Files                    map[string]ArtifactFile `json:"files"`
	IO                       TensorIO                `json:"io"`
}

// ArtifactFile 是一個實際的 ONNX 檔。Path 相對於模型根目錄。
type ArtifactFile struct {
	Path         string `json:"path"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	Quantization string `json:"quantization"`
}

// TensorIO 是模型檔的輸入輸出契約。
type TensorIO struct {
	Inputs  []Tensor `json:"inputs"`
	Outputs []Tensor `json:"outputs"`
}

// Tensor 是一個輸入或輸出張量。Shape 的元素是字串（batch、seq）或固定整數。
type Tensor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DType       string `json:"dtype"`
	Shape       []any  `json:"shape"`
}

// Options 控制驗證範圍。InstalledDictionarySHA256 必須是實際安裝的字典包 manifest 雜湊。
type Options struct {
	AllowPlaceholders         bool
	InstalledDictionarySHA256 string
	InstalledDictionaryPackID string
	ArtifactRoot              string
	Format                    string
	VerifyFiles               bool
}

// ArtifactStatus 是驗證通過（或安裝）後回報的模型檔摘要。
type ArtifactStatus struct {
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

// Result 是一個模型組的驗證結果。
type Result struct {
	SetID             string           `json:"set_id"`
	Variant           string           `json:"variant"`
	Version           string           `json:"version"`
	Placeholder       bool             `json:"placeholder"`
	Quality           string           `json:"quality"`
	Dictionary        Dictionary       `json:"dictionary"`
	DictionaryMatches bool             `json:"dictionary_matches"`
	InstalledSHA256   string           `json:"installed_dictionary_sha256"`
	Artifacts         []ArtifactStatus `json:"artifacts"`
}

// ParseLock 解析 models/pairing.lock.json 的 model_sets 區段，並做不需要檔案與已安裝
// 字典包的結構驗證。格式錯誤、混用 variant、缺模型、placeholder 標記不完整、張量契約
// 不符都會在這裡被拒絕。
func ParseLock(b []byte) (Lock, error) {
	var lock Lock
	if err := json.Unmarshal(b, &lock); err != nil {
		return Lock{}, fmt.Errorf("%w: %v", ErrSchema, err)
	}
	if lock.ModelSets == nil {
		return Lock{}, fmt.Errorf("%w: 缺少 model_sets", ErrSchema)
	}
	if lock.ModelSets.Schema != Schema {
		return Lock{}, fmt.Errorf("%w: model_sets.schema=%q", ErrSchema, lock.ModelSets.Schema)
	}
	if len(lock.ModelSets.Sets) == 0 {
		return Lock{}, fmt.Errorf("%w: 沒有模型組", ErrSchema)
	}
	seen := map[string]string{}
	for id, set := range lock.ModelSets.Sets {
		if set == nil {
			return Lock{}, fmt.Errorf("%w: %s 是空的模型組", ErrSchema, id)
		}
		set.ID = id
		if err := set.ValidateStructure(); err != nil {
			return Lock{}, fmt.Errorf("%s: %w", id, err)
		}
		for _, artifact := range set.Artifacts {
			if other, ok := seen[artifact.ArtifactID]; ok {
				return Lock{}, fmt.Errorf("%w: %s 同時出現在 %s 與 %s", ErrVariantMixed, artifact.ArtifactID, other, id)
			}
			seen[artifact.ArtifactID] = id
		}
	}
	return lock, nil
}

// LoadLock 讀取並解析模型組清單。
func LoadLock(path string) (Lock, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Lock{}, err
	}
	return ParseLock(b)
}

// SetIDs 回傳排序後的模型組 ID。
func (l Lock) SetIDs() []string {
	if l.ModelSets == nil {
		return nil
	}
	ids := make([]string, 0, len(l.ModelSets.Sets))
	for id := range l.ModelSets.Sets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Set 依 ID 取得模型組。
func (l Lock) Set(id string) (*Set, error) {
	if l.ModelSets == nil {
		return nil, fmt.Errorf("%w: 缺少 model_sets", ErrSchema)
	}
	set, ok := l.ModelSets.Sets[id]
	if !ok || set == nil {
		return nil, fmt.Errorf("%w: 沒有模型組 %q（可用：%s）", ErrSchema, id, strings.Join(l.SetIDs(), ", "))
	}
	return set, nil
}

// ValidateStructure 只驗證 manifest 本身：variant 分離、模型組完整、授權與張量契約。
// 不讀取已安裝字典包或 ONNX 檔案。
func (s *Set) ValidateStructure() error {
	if s.ID == "" {
		return fmt.Errorf("%w: 模型組沒有 ID", ErrSchema)
	}
	switch s.Variant {
	case VariantPublicCore, VariantCCBYSA:
	default:
		return fmt.Errorf("%w: variant=%q", ErrSchema, s.Variant)
	}
	if s.Language != "ja" {
		return fmt.Errorf("%w: language=%q", ErrSchema, s.Language)
	}
	if s.Version == "" {
		return fmt.Errorf("%w: 缺少 version", ErrSchema)
	}
	if err := validateDictionary(s.Dictionary); err != nil {
		return err
	}
	if err := checkLicense(s.Variant, s.License); err != nil {
		return err
	}
	if len(s.Attribution) == 0 {
		return fmt.Errorf("%w: 缺少 attribution 參考", ErrSchema)
	}
	if len(s.Artifacts) == 0 {
		return fmt.Errorf("%w: 沒有模型", ErrIncompleteSet)
	}

	kinds := map[string]int{}
	ids := map[string]bool{}
	placeholder := s.Artifacts[0].Placeholder
	for i := range s.Artifacts {
		artifact := &s.Artifacts[i]
		if artifact.Variant != s.Variant {
			return fmt.Errorf("%w: %s 是 %s，模型組是 %s", ErrVariantMixed, artifact.ArtifactID, artifact.Variant, s.Variant)
		}
		if artifact.ArtifactID == "" || ids[artifact.ArtifactID] {
			return fmt.Errorf("%w: 模型 ID 空白或重複：%q", ErrSchema, artifact.ArtifactID)
		}
		ids[artifact.ArtifactID] = true
		switch artifact.Kind {
		case KindReranker, KindPunctuation:
		default:
			return fmt.Errorf("%w: %s 的 kind=%q", ErrSchema, artifact.ArtifactID, artifact.Kind)
		}
		kinds[artifact.Kind]++
		if artifact.Version == "" {
			return fmt.Errorf("%w: %s 缺少 version", ErrSchema, artifact.ArtifactID)
		}
		if err := checkLicense(s.Variant, artifact.License); err != nil {
			return fmt.Errorf("%s: %w", artifact.ArtifactID, err)
		}
		if len(artifact.Attribution) == 0 {
			return fmt.Errorf("%w: %s 缺少 attribution 參考", ErrSchema, artifact.ArtifactID)
		}
		if artifact.DictionaryManifestSHA256 != s.Dictionary.ManifestSHA256 {
			return fmt.Errorf("%w: %s 記的字典 %s 與模型組 %s 不符", ErrDictionaryMismatch,
				artifact.ArtifactID, artifact.DictionaryManifestSHA256, s.Dictionary.ManifestSHA256)
		}
		if artifact.Placeholder != placeholder {
			return fmt.Errorf("%w: %s 的 placeholder=%v 與模型組 %v 不一致", ErrPlaceholderUnmarked,
				artifact.ArtifactID, artifact.Placeholder, placeholder)
		}
		if artifact.Placeholder {
			if artifact.Quality != QualityUntrained || artifact.ProductionEligible {
				return fmt.Errorf("%w: %s 是 placeholder 但 quality=%q production_eligible=%v",
					ErrPlaceholderUnmarked, artifact.ArtifactID, artifact.Quality, artifact.ProductionEligible)
			}
		}
		if err := validateFiles(artifact); err != nil {
			return fmt.Errorf("%s: %w", artifact.ArtifactID, err)
		}
		if err := validateIO(artifact.Kind, artifact.IO); err != nil {
			return fmt.Errorf("%s: %w", artifact.ArtifactID, err)
		}
		if artifact.Source.TrainingData == "" || artifact.Source.Generator == "" {
			return fmt.Errorf("%w: %s 缺少 source", ErrSchema, artifact.ArtifactID)
		}
	}
	if s.Placeholder != placeholder {
		return fmt.Errorf("%w: 模型組 placeholder=%v 與模型 %v 不一致", ErrPlaceholderUnmarked, s.Placeholder, placeholder)
	}
	if s.Placeholder && s.Quality != QualityUntrained {
		return fmt.Errorf("%w: placeholder 模型組 quality=%q", ErrPlaceholderUnmarked, s.Quality)
	}
	if s.Placeholder && s.ProductionEligible {
		return fmt.Errorf("%w: placeholder 模型組不得 production_eligible", ErrPlaceholderUnmarked)
	}
	if kinds[KindReranker] != 1 || kinds[KindPunctuation] != 1 {
		return fmt.Errorf("%w: 需要各一個 reranker 與 punctuation，目前 reranker=%d punctuation=%d",
			ErrIncompleteSet, kinds[KindReranker], kinds[KindPunctuation])
	}
	return nil
}

// Validate 驗證模型組可以安裝或成為使用中的模型組：結構、已安裝字典包雜湊、placeholder
// 設定，以及（可選）實際檔案大小與 SHA-256。
func (s *Set) Validate(opts Options) (Result, error) {
	if err := s.ValidateStructure(); err != nil {
		return Result{}, err
	}
	if opts.InstalledDictionarySHA256 == "" {
		return Result{}, fmt.Errorf("%w: 無法取得已安裝字典包的 manifest 雜湊", ErrDictionaryUnknown)
	}
	if opts.InstalledDictionarySHA256 != s.Dictionary.ManifestSHA256 {
		return Result{}, fmt.Errorf("%w: 模型組記的是 %s，已安裝字典包是 %s",
			ErrDictionaryMismatch, s.Dictionary.ManifestSHA256, opts.InstalledDictionarySHA256)
	}
	if opts.InstalledDictionaryPackID != "" && opts.InstalledDictionaryPackID != s.Dictionary.PackID {
		return Result{}, fmt.Errorf("%w: 模型組要 %q，已安裝字典包是 %q",
			ErrDictionaryMismatch, s.Dictionary.PackID, opts.InstalledDictionaryPackID)
	}
	if s.Placeholder && !opts.AllowPlaceholders {
		return Result{}, fmt.Errorf("%w: %s 的權重未訓練（quality=%s），設定未允許 placeholder",
			ErrPlaceholder, s.ID, s.Quality)
	}
	format := opts.Format
	if format == "" {
		format = FormatInt8
	}
	result := Result{
		SetID: s.ID, Variant: s.Variant, Version: s.Version, Placeholder: s.Placeholder,
		Quality: s.Quality, Dictionary: s.Dictionary, DictionaryMatches: true,
		InstalledSHA256: opts.InstalledDictionarySHA256,
		Artifacts:       make([]ArtifactStatus, 0, len(s.Artifacts)),
	}
	for i := range s.Artifacts {
		artifact := &s.Artifacts[i]
		file, ok := artifact.Files[format]
		if !ok {
			return Result{}, fmt.Errorf("%w: %s 沒有 %s 檔案", ErrSchema, artifact.ArtifactID, format)
		}
		status := ArtifactStatus{
			ArtifactID: artifact.ArtifactID, Kind: artifact.Kind, Variant: artifact.Variant,
			Format: format, Path: file.Path, Bytes: file.Bytes, SHA256: file.SHA256,
			Placeholder: artifact.Placeholder, Quality: artifact.Quality,
		}
		if opts.VerifyFiles {
			if opts.ArtifactRoot == "" {
				return Result{}, fmt.Errorf("%w: 沒有模型根目錄", ErrFileMismatch)
			}
			actual, err := verifyFile(opts.ArtifactRoot, file)
			if err != nil {
				return Result{}, fmt.Errorf("%s: %w", artifact.ArtifactID, err)
			}
			status.Bytes, status.SHA256 = actual.Bytes, actual.SHA256
		}
		result.Artifacts = append(result.Artifacts, status)
	}
	return result, nil
}

// DictionarySHA256 計算字典包 manifest 的雜湊。path 可以是 manifest.json 本身，或含
// manifest.json 的目錄。日文包目錄也可以直接傳 <data>/ja。
func DictionarySHA256(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: 沒有字典包路徑", ErrDictionaryUnknown)
	}
	manifest := path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		manifest = filepath.Join(path, "manifest.json")
	}
	b, err := os.ReadFile(manifest)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDictionaryUnknown, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// DictionaryPackID 讀取字典包 manifest 的 `lexicon` 或 `pack_id` 欄位；沒有時回傳空字串。
func DictionaryPackID(path string) string {
	if path == "" {
		return ""
	}
	manifest := path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		manifest = filepath.Join(path, "manifest.json")
	}
	b, err := os.ReadFile(manifest)
	if err != nil {
		return ""
	}
	var m struct {
		Lexicon string `json:"lexicon"`
		PackID  string `json:"pack_id"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	if m.PackID != "" {
		return m.PackID
	}
	return m.Lexicon
}

func validateDictionary(d Dictionary) error {
	if d.PackID == "" || d.Language != "ja" || d.Format <= 0 || d.Version == "" || !validHex(d.ManifestSHA256) {
		return fmt.Errorf("%w: dictionary=%+v", ErrSchema, d)
	}
	return nil
}

func checkLicense(variant string, l License) error {
	if l.ID == "" || l.Scope == "" || l.Distribution == "" || l.Notice == "" {
		return fmt.Errorf("%w: license=%+v", ErrSchema, l)
	}
	id := strings.ToUpper(l.ID)
	scope := strings.ToLower(l.Scope)
	switch variant {
	case VariantPublicCore:
		if scope != VariantPublicCore {
			return fmt.Errorf("%w: public-core 模型組含 scope=%q 的授權", ErrLicenseMixed, l.Scope)
		}
		if strings.Contains(id, "-SA") || strings.Contains(id, "-NC") {
			return fmt.Errorf("%w: public-core 模型組不得使用 %s", ErrLicenseMixed, l.ID)
		}
	case VariantCCBYSA:
		if scope != VariantCCBYSA {
			return fmt.Errorf("%w: cc-by-sa 模型組含 scope=%q 的授權", ErrLicenseMixed, l.Scope)
		}
		if !strings.Contains(id, "CC-BY-SA") {
			return fmt.Errorf("%w: cc-by-sa 模型組的授權應為 CC-BY-SA，得到 %s", ErrLicenseMixed, l.ID)
		}
	}
	return nil
}

func validateFiles(a *Artifact) error {
	if len(a.Files) == 0 {
		return fmt.Errorf("%w: 沒有檔案", ErrSchema)
	}
	if _, ok := a.Files[FormatInt8]; !ok {
		return fmt.Errorf("%w: 缺少 %s 檔案", ErrSchema, FormatInt8)
	}
	for format, file := range a.Files {
		if format != FormatInt8 && format != FormatFP32 {
			return fmt.Errorf("%w: 未知格式 %q", ErrSchema, format)
		}
		if err := validateFile(file); err != nil {
			return err
		}
		if strings.Contains(file.Path, ".placeholder.") && !a.Placeholder {
			return fmt.Errorf("%w: %s 的檔名標示 placeholder，但 manifest 沒有標記", ErrPlaceholderUnmarked, file.Path)
		}
	}
	return nil
}

func validateFile(f ArtifactFile) error {
	if f.Path == "" || filepath.IsAbs(f.Path) || !filepath.IsLocal(filepath.FromSlash(f.Path)) {
		return fmt.Errorf("%w: 路徑 %q 不是模型根目錄內的相對路徑", ErrSchema, f.Path)
	}
	if filepath.Ext(f.Path) != ".onnx" {
		return fmt.Errorf("%w: %q 不是 ONNX 檔", ErrSchema, f.Path)
	}
	if f.Bytes <= 0 || !validHex(f.SHA256) {
		return fmt.Errorf("%w: %q 的大小或 SHA-256 不正確", ErrSchema, f.Path)
	}
	return nil
}

func validateIO(kind string, io TensorIO) error {
	if len(io.Inputs) != 2 {
		return fmt.Errorf("%w: 需要 input_ids 與 attention_mask 兩個輸入，得到 %d", ErrTensorContract, len(io.Inputs))
	}
	want := map[string]bool{"input_ids": false, "attention_mask": false}
	for _, in := range io.Inputs {
		if _, ok := want[in.Name]; !ok || want[in.Name] {
			return fmt.Errorf("%w: 未知或重複的輸入 %q", ErrTensorContract, in.Name)
		}
		want[in.Name] = true
		if in.DType != "int64" || !shapeIs(in.Shape, "batch", "seq") {
			return fmt.Errorf("%w: %s 必須是 int64 [batch, seq]", ErrTensorContract, in.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			return fmt.Errorf("%w: 缺少輸入 %s", ErrTensorContract, name)
		}
	}
	if len(io.Outputs) != 1 {
		return fmt.Errorf("%w: 需要一個輸出，得到 %d", ErrTensorContract, len(io.Outputs))
	}
	out := io.Outputs[0]
	switch kind {
	case KindReranker:
		if out.Name != "scores" || out.DType != "float32" || !shapeIs(out.Shape, "batch") {
			return fmt.Errorf("%w: reranker 輸出必須是 float32 scores [batch]", ErrTensorContract)
		}
	case KindPunctuation:
		if out.Name != "logits" || out.DType != "float32" || !shapeIs(out.Shape, "batch", "seq", 4) {
			return fmt.Errorf("%w: punctuation 輸出必須是 float32 logits [batch, seq, 4]", ErrTensorContract)
		}
	}
	return nil
}

func shapeIs(got []any, want ...any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		switch w := want[i].(type) {
		case string:
			s, ok := got[i].(string)
			if !ok || s != w {
				return false
			}
		case int:
			n, ok := got[i].(float64)
			if !ok || int(n) != w || n != float64(w) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func verifyFile(root string, f ArtifactFile) (ArtifactFile, error) {
	path := filepath.Join(root, filepath.FromSlash(f.Path))
	info, err := os.Stat(path)
	if err != nil {
		return ArtifactFile{}, fmt.Errorf("%w: %v", ErrFileMismatch, err)
	}
	if !info.Mode().IsRegular() {
		return ArtifactFile{}, fmt.Errorf("%w: %s 不是一般檔案", ErrFileMismatch, f.Path)
	}
	if info.Size() != f.Bytes {
		return ArtifactFile{}, fmt.Errorf("%w: %s 大小 %d，清單記 %d", ErrFileMismatch, f.Path, info.Size(), f.Bytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return ArtifactFile{}, fmt.Errorf("%w: %v", ErrFileMismatch, err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return ArtifactFile{}, fmt.Errorf("%w: %v", ErrFileMismatch, err)
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if digest != f.SHA256 {
		return ArtifactFile{}, fmt.Errorf("%w: %s SHA-256 %s，清單記 %s", ErrFileMismatch, f.Path, digest, f.SHA256)
	}
	return ArtifactFile{Path: f.Path, Bytes: info.Size(), SHA256: digest, Quantization: f.Quantization}, nil
}

func validHex(s string) bool {
	if len(s) != 64 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
