package eval

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/0c-biggo/0c-ime/core/fuzzy"
	jainput "github.com/0c-biggo/0c-ime/core/ja/input"
	"github.com/0c-biggo/0c-ime/core/layout"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// P2-62 模糊輸入題庫：從已凍結的注音 golden 與日文單語言題庫衍生。
//
// 每個來源案例只做一種擾動，讓 top-1／top-5 的分母可以直接對回擾動種類：
//
//   - wrong-key：把一個實體按鍵換成相鄰鍵（標準注音排列／QWERTY）。
//   - missing-key：刪掉一個按鍵。
//   - doubled-key：重複一個按鍵。
//   - near-sound：注音限定，把一個音節換成 core/fuzzy 的近音替代，聲調不變。
//
// 題庫不載入任何模型，也不改產品排序預設。日文取樣固定用 seed 與案例 ID 的
// SHA-256 排序，不依 map 走訪或時間。

const (
	// FuzzySchema 是題庫、manifest 與報告的格式版本。
	FuzzySchema = 1
	// FuzzySeed 是 P2-62 題庫的固定種子。
	FuzzySeed = 62
	// FuzzyGenerator 記錄寫出題庫的工具。
	FuzzyGenerator = "cmd/0c-eval-fuzzy build"
	// FuzzyJASample 是從日文單語言題庫保留切片取樣的案例數。
	FuzzyJASample = 200
	// FuzzyQuality 說明這些數字的定位；不是上線門檻，也不是模型品質。
	FuzzyQuality = "observational fuzzy-input robustness on synthetic typos; references are the frozen source corpora, not human annotations"
)

// 擾動種類。值會寫進題庫與報告，必須穩定。
const (
	FuzzyWrongKey   = "wrong-key"
	FuzzyMissingKey = "missing-key"
	FuzzyDoubledKey = "doubled-key"
	FuzzyNearSound  = "near-sound"
)

// 題庫語言代號。
const (
	FuzzyLangZh = "zh"
	FuzzyLangJa = "ja"
)

// 輸入表示法。
const (
	FuzzyInputZhuyinKeys      = "zhuyin-keys"
	FuzzyInputZhuyinSyllables = "zhuyin-syllables"
	FuzzyInputRomaji          = "romaji"
)

// FuzzyKinds 依固定順序列出所有擾動種類。
func FuzzyKinds() []string {
	return []string{FuzzyWrongKey, FuzzyMissingKey, FuzzyDoubledKey, FuzzyNearSound}
}

// FuzzyLanguages 依固定順序列出題庫語言。
func FuzzyLanguages() []string { return []string{FuzzyLangZh, FuzzyLangJa} }

// FuzzyMutation 記錄一筆擾動的位置與前後值；doubled-key 的 From 與 To 相同。
type FuzzyMutation struct {
	At   int    `json:"at"`
	From string `json:"from"`
	To   string `json:"to"`
}

// FuzzyItem 是一個模糊輸入案例。Reading 是來源讀音，Surface 是預期文字；
// Input 是實際送進輸入法的按鍵或音節，OriginalInput 是未擾動的同一種表示。
type FuzzyItem struct {
	ID            string        `json:"id"`
	Language      string        `json:"language"`
	Kind          string        `json:"kind"`
	SourceID      string        `json:"source_id"`
	Reading       string        `json:"reading"`
	Surface       string        `json:"surface"`
	OriginalInput string        `json:"original_input"`
	Input         string        `json:"input"`
	InputKind     string        `json:"input_kind"`
	NoisyReading  string        `json:"noisy_reading,omitempty"`
	Pending       string        `json:"pending,omitempty"`
	Mutation      FuzzyMutation `json:"mutation"`
}

// FuzzySourcePin 是一份來源題庫的 identity。
type FuzzySourcePin struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Count  int    `json:"count"`
}

// FuzzyCorpus 是 testdata/fuzzy/fuzzy-input.json。
type FuzzyCorpus struct {
	Schema    int              `json:"schema"`
	Name      string           `json:"name"`
	License   string           `json:"license"`
	Source    string           `json:"source"`
	Notes     string           `json:"notes,omitempty"`
	Seed      int              `json:"seed"`
	Generator string           `json:"generator"`
	Sources   []FuzzySourcePin `json:"sources"`
	Count     int              `json:"count"`
	Items     []FuzzyItem      `json:"items"`
}

// FuzzyModelPin 固定「沒有模型參與」這件事。P2-62 不載入任何模型；
// 參考資料夾的 placeholder 不得出現在這裡。
type FuzzyModelPin struct {
	Used bool   `json:"used"`
	Note string `json:"note"`
}

// FuzzyManifest 是 testdata/fuzzy/fuzzy-input.manifest.json。
type FuzzyManifest struct {
	Schema    int              `json:"schema"`
	Corpus    string           `json:"corpus"`
	Path      string           `json:"path"`
	SHA256    string           `json:"sha256"`
	Count     int              `json:"count"`
	Seed      int              `json:"seed"`
	Generator string           `json:"generator"`
	Sources   []FuzzySourcePin `json:"sources"`
	Kinds     map[string]int   `json:"kinds"`
	Languages map[string]int   `json:"languages"`
	Models    FuzzyModelPin    `json:"models"`
	Quality   string           `json:"quality"`
}

// FuzzyBuildOptions 是產生題庫的固定參數。
type FuzzyBuildOptions struct {
	Seed     int
	JASample int
}

// DefaultFuzzyBuildOptions 回傳 P2-62 凍結的參數。
func DefaultFuzzyBuildOptions() FuzzyBuildOptions {
	return FuzzyBuildOptions{Seed: FuzzySeed, JASample: FuzzyJASample}
}

// BuildFuzzyCorpus 從兩份凍結題庫重新產生模糊輸入題庫。同一份來源、同一組
// 參數會產生逐位元相同的 JSON；測試會比對提交檔。
func BuildFuzzyCorpus(zhPath, jaPath string, opts FuzzyBuildOptions) (*FuzzyCorpus, error) {
	if opts.Seed <= 0 {
		return nil, fmt.Errorf("fuzzy corpus: seed 必須大於 0")
	}
	if opts.JASample <= 0 {
		return nil, fmt.Errorf("fuzzy corpus: ja_sample 必須大於 0")
	}
	zh, zhSHA, err := LoadZhGoldenCorpus(zhPath)
	if err != nil {
		return nil, err
	}
	ja, jaSHA, err := LoadJALexCorpus(jaPath)
	if err != nil {
		return nil, err
	}
	c := &FuzzyCorpus{
		Schema:    FuzzySchema,
		Name:      "fuzzy-input",
		License:   "CC0-1.0 for the project-owned zhuyin fixtures; Japanese readings and surfaces retain the ja-lexicon-ambiguous license and attribution.",
		Source:    "testdata/eval/zhuyin-golden.json and testdata/eval/ja-lexicon-ambiguous.json",
		Notes:     "Synthetic typos derived deterministically from frozen corpora. One perturbation per case; no model is loaded and no product ranking default is changed. Japanese cases are a fixed-seed holdout sample and are scored only when a reference lexicon pack is configured.",
		Seed:      opts.Seed,
		Generator: FuzzyGenerator,
		Sources: []FuzzySourcePin{
			{Name: zh.Name, Path: filepath.Base(filepathSlash(zhPath)), SHA256: zhSHA, Count: zhSegmentCount(zh)},
			{Name: ja.Name, Path: filepath.Base(filepathSlash(jaPath)), SHA256: jaSHA, Count: opts.JASample},
		},
	}

	zhItems, err := buildZhFuzzyItems(zh, opts.Seed)
	if err != nil {
		return nil, err
	}
	jaItems, err := buildJAFuzzyItems(ja, opts.Seed, opts.JASample)
	if err != nil {
		return nil, err
	}
	c.Sources[1].Count = uniqueFuzzySources(jaItems)
	c.Items = append(append(c.Items, zhItems...), jaItems...)
	sort.Slice(c.Items, func(i, j int) bool {
		if c.Items[i].Language != c.Items[j].Language {
			return c.Items[i].Language < c.Items[j].Language
		}
		if c.Items[i].Kind != c.Items[j].Kind {
			return c.Items[i].Kind < c.Items[j].Kind
		}
		return c.Items[i].ID < c.Items[j].ID
	})
	c.Count = len(c.Items)
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func uniqueFuzzySources(items []FuzzyItem) int {
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.SourceID] = true
	}
	return len(seen)
}

func zhSegmentCount(c *ZhGoldenCorpus) int {
	n := 0
	for _, item := range c.Items {
		n += len(item.Segments)
	}
	return n
}

func buildZhFuzzyItems(c *ZhGoldenCorpus, seed int) ([]FuzzyItem, error) {
	var out []FuzzyItem
	for _, item := range c.Items {
		for i, seg := range item.Segments {
			sourceID := item.ID + "/" + strconv.Itoa(i)
			keys := zhKeysForReading(seg.Reading)
			if keys == "" {
				return nil, fmt.Errorf("%s: empty zhuyin keys", sourceID)
			}
			base := FuzzyItem{
				SourceID:      sourceID,
				Language:      FuzzyLangZh,
				Reading:       seg.Reading,
				Surface:       seg.Surface,
				OriginalInput: keys,
				InputKind:     FuzzyInputZhuyinKeys,
				NoisyReading:  zhKeysToReading(keys),
			}
			for _, kind := range []string{FuzzyWrongKey, FuzzyMissingKey, FuzzyDoubledKey} {
				it, ok := buildZhKeyItem(base, kind, seed)
				if ok {
					out = append(out, it)
				}
			}
			if it, ok := buildZhNearSoundItem(base, seed); ok {
				out = append(out, it)
			}
		}
	}
	return out, nil
}

func buildZhKeyItem(base FuzzyItem, kind string, seed int) (FuzzyItem, bool) {
	keys := []rune(base.OriginalInput)
	if len(keys) == 0 {
		return FuzzyItem{}, false
	}
	at := fuzzyIndex(seed, "zh-"+kind, base.SourceID, len(keys))
	mutated := append([]rune(nil), keys...)
	var from, to rune
	switch kind {
	case FuzzyWrongKey:
		replaced := false
		for off := 0; off < len(keys) && !replaced; off++ {
			i := (at + off) % len(keys)
			current, ok := layout.Standard.Symbol(keys[i])
			if !ok {
				continue
			}
			for _, alt := range adjacentKeys(keys[i]) {
				sym, ok := layout.Standard.Symbol(alt)
				if !ok || sym == current {
					continue
				}
				mutated[i], from, to = alt, keys[i], alt
				at = i
				replaced = true
				break
			}
		}
		if !replaced {
			return FuzzyItem{}, false
		}
	case FuzzyMissingKey:
		if len(keys) < 2 {
			return FuzzyItem{}, false
		}
		from = keys[at]
		mutated = append(append([]rune(nil), keys[:at]...), keys[at+1:]...)
	case FuzzyDoubledKey:
		from = keys[at]
		to = from
		next := make([]rune, 0, len(mutated)+1)
		next = append(next, mutated[:at+1]...)
		next = append(next, keys[at])
		next = append(next, mutated[at+1:]...)
		mutated = next
	default:
		return FuzzyItem{}, false
	}
	it := base
	it.ID = "zh/" + base.SourceID + "/" + kind
	it.Kind = kind
	it.Input = string(mutated)
	it.NoisyReading = zhKeysToReading(it.Input)
	it.Mutation = FuzzyMutation{At: at, From: string(from), To: runeString(to)}
	return it, true
}

func buildZhNearSoundItem(base FuzzyItem, seed int) (FuzzyItem, bool) {
	cfg := fuzzySelectAll()
	syls := zhuyin.ParseReading(base.Reading)
	var eligible []int
	for i, syl := range syls {
		if len(cfg.Alternatives(syl)) > 0 {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 {
		return FuzzyItem{}, false
	}
	at := eligible[fuzzyIndex(seed, "zh-near", base.SourceID, len(eligible))]
	alts := cfg.Alternatives(syls[at])
	alt := alts[fuzzyIndex(seed, "zh-near-alt", base.SourceID, len(alts))]
	noisy := append([]zhuyin.Syllable(nil), syls...)
	noisy[at] = alt.Syl
	it := base
	it.ID = "zh/" + base.SourceID + "/" + FuzzyNearSound
	it.Kind = FuzzyNearSound
	it.OriginalInput = strings.Join(syllableStrings(syls), " ")
	it.InputKind = FuzzyInputZhuyinSyllables
	it.Input = zhuyin.Key(noisy)
	it.NoisyReading = it.Input
	it.Mutation = FuzzyMutation{At: at, From: string(syls[at]), To: string(alt.Syl)}
	return it, true
}

func buildJAFuzzyItems(c *JALexCorpus, seed, sample int) ([]FuzzyItem, error) {
	var candidates []JALexItem
	for _, item := range c.Items {
		if !item.Holdout {
			continue
		}
		keys, ok := kanaToRomaji(item.Reading)
		if !ok || len(keys) < 2 {
			continue
		}
		units, pending, ok := composeRomaji(keys)
		if !ok || pending != "" || units != item.Reading {
			continue
		}
		candidates = append(candidates, item)
	}
	sort.Slice(candidates, func(i, j int) bool {
		a := sampledKey(seed, "fuzzy-ja-sample", candidates[i].ID)
		b := sampledKey(seed, "fuzzy-ja-sample", candidates[j].ID)
		if a != b {
			return a < b
		}
		return candidates[i].ID < candidates[j].ID
	})
	if len(candidates) > sample {
		candidates = candidates[:sample]
	}
	var out []FuzzyItem
	for _, item := range candidates {
		keys, _ := kanaToRomaji(item.Reading)
		base := FuzzyItem{
			SourceID:      item.ID,
			Language:      FuzzyLangJa,
			Reading:       item.Reading,
			Surface:       item.Reference,
			OriginalInput: keys,
			InputKind:     FuzzyInputRomaji,
			NoisyReading:  item.Reading,
		}
		for _, kind := range []string{FuzzyWrongKey, FuzzyMissingKey, FuzzyDoubledKey} {
			it, ok := buildJAKeyItem(base, kind, seed)
			if ok {
				out = append(out, it)
			}
		}
	}
	return out, nil
}

func buildJAKeyItem(base FuzzyItem, kind string, seed int) (FuzzyItem, bool) {
	keys := []rune(base.OriginalInput)
	if len(keys) == 0 {
		return FuzzyItem{}, false
	}
	at := fuzzyIndex(seed, "ja-"+kind, base.SourceID, len(keys))
	mutated := append([]rune(nil), keys...)
	var from, to rune
	switch kind {
	case FuzzyWrongKey:
		replaced := false
		for off := 0; off < len(keys) && !replaced; off++ {
			i := (at + off) % len(keys)
			for _, alt := range adjacentKeys(keys[i]) {
				if !romajiKey(alt) || alt == keys[i] {
					continue
				}
				mutated[i], from, to = alt, keys[i], alt
				at = i
				replaced = true
				break
			}
		}
		if !replaced {
			return FuzzyItem{}, false
		}
	case FuzzyMissingKey:
		if len(keys) < 2 {
			return FuzzyItem{}, false
		}
		from = keys[at]
		mutated = append(append([]rune(nil), keys[:at]...), keys[at+1:]...)
	case FuzzyDoubledKey:
		from = keys[at]
		to = from
		next := make([]rune, 0, len(mutated)+1)
		next = append(next, mutated[:at+1]...)
		next = append(next, keys[at])
		next = append(next, mutated[at+1:]...)
		mutated = next
	default:
		return FuzzyItem{}, false
	}
	units, pending, _ := composeRomaji(string(mutated))
	it := base
	it.ID = "ja/" + base.SourceID + "/" + kind
	it.Kind = kind
	it.Input = string(mutated)
	it.NoisyReading = units
	it.Pending = pending
	it.Mutation = FuzzyMutation{At: at, From: string(from), To: runeString(to)}
	return it, true
}

func fuzzySelectAll() fuzzy.Config {
	cfg := fuzzy.Config{}
	for _, pair := range fuzzy.Pairs() {
		cfg = cfg.With(pair, fuzzy.Select)
	}
	return cfg
}

func syllableStrings(syls []zhuyin.Syllable) []string {
	out := make([]string, len(syls))
	for i, s := range syls {
		out[i] = string(s)
	}
	return out
}

func zhKeysForReading(reading string) string {
	var b strings.Builder
	for _, syl := range zhuyin.ParseReading(reading) {
		b.WriteString(layout.Standard.Keys(syl))
	}
	return b.String()
}

// zhKeysToReading 用標準排列重播按鍵，回傳組出來的音節；未完成的音節也保留
// （沒有聲調），讓「漏鍵」案例在報告裡看得出停在哪裡。
func zhKeysToReading(keys string) string {
	var c zhuyin.Composer
	var out []string
	for _, key := range keys {
		next, sym, ok := layout.Standard.Press(c, key)
		if !ok {
			continue
		}
		c = next
		if zhuyin.ClassOf(sym) != zhuyin.Tone {
			continue
		}
		if c.Empty() {
			continue
		}
		done, ok := c.Add(sym)
		if ok {
			out = append(out, string(done))
		}
		c.Reset()
	}
	if !c.Empty() {
		out = append(out, c.String())
	}
	return strings.Join(out, " ")
}

// keyboardRows 是產生相鄰鍵用的實體列；不是注音或羅馬字對照表。
var keyboardRows = []string{
	"1234567890-=",
	"qwertyuiop[]",
	"asdfghjkl;'",
	"zxcvbnm,./",
}

// adjacentKeys 回傳與 key 實體相鄰的按鍵，順序固定。上下列取同欄與左右一欄，
// 近似一般鍵盤的交錯排列；空白鍵只與下排中間相鄰。
func adjacentKeys(key rune) []rune {
	seen := map[rune]bool{key: true}
	var out []rune
	add := func(r, c int) {
		if r < 0 || r >= len(keyboardRows) || c < 0 || c >= len(keyboardRows[r]) {
			return
		}
		x := rune(keyboardRows[r][c])
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	found := false
	for r, row := range keyboardRows {
		c := strings.IndexRune(row, key)
		if c < 0 {
			continue
		}
		found = true
		add(r, c-1)
		add(r, c+1)
		for _, dr := range []int{-1, 1} {
			for _, dc := range []int{-1, 0, 1} {
				add(r+dr, c+dc)
			}
		}
	}
	if key == ' ' {
		found = true
		for _, x := range "xcvbnm" {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	} else if found && strings.ContainsRune("xcvbnm", key) {
		out = append(out, ' ')
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func romajiKey(r rune) bool {
	return r >= 'a' && r <= 'z' || r == '-'
}

func runeString(r rune) string {
	if r == 0 {
		return ""
	}
	return string(r)
}

// fuzzyIndex 由 seed、用途與案例 ID 決定位置；不用 map 走訪或時間。
func fuzzyIndex(seed int, salt, id string, n int) int {
	if n <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", seed, salt, id)))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(n))
}

func filepathSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

// LoadFuzzyCorpus 讀取題庫並驗證結構。
func LoadFuzzyCorpus(path string) (*FuzzyCorpus, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var c FuzzyCorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	return &c, hex.EncodeToString(sum[:]), nil
}

func (c *FuzzyCorpus) validate() error {
	if c.Schema != FuzzySchema {
		return fmt.Errorf("schema %d, want %d", c.Schema, FuzzySchema)
	}
	if c.Name == "" || c.License == "" || c.Source == "" || c.Generator == "" {
		return fmt.Errorf("name, license, source and generator are required")
	}
	if c.Seed <= 0 {
		return fmt.Errorf("seed must be positive")
	}
	if c.Count != len(c.Items) || c.Count == 0 {
		return fmt.Errorf("count %d does not match %d items", c.Count, len(c.Items))
	}
	if len(c.Sources) != 2 {
		return fmt.Errorf("want exactly two source pins, got %d", len(c.Sources))
	}
	for _, src := range c.Sources {
		if src.Name == "" || src.Path == "" || len(src.SHA256) != 64 || src.Count <= 0 {
			return fmt.Errorf("invalid source pin %+v", src)
		}
	}
	seen := map[string]bool{}
	for _, item := range c.Items {
		if item.ID == "" || item.SourceID == "" || item.Surface == "" || item.Reading == "" || item.Input == "" {
			return fmt.Errorf("item has an empty id, source, surface, reading or input")
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate item %q", item.ID)
		}
		seen[item.ID] = true
		if item.Language != FuzzyLangZh && item.Language != FuzzyLangJa {
			return fmt.Errorf("%s: unknown language %q", item.ID, item.Language)
		}
		switch item.Kind {
		case FuzzyWrongKey, FuzzyMissingKey, FuzzyDoubledKey:
		case FuzzyNearSound:
			if item.Language != FuzzyLangZh {
				return fmt.Errorf("%s: near-sound is zhuyin-only", item.ID)
			}
		default:
			return fmt.Errorf("%s: unknown kind %q", item.ID, item.Kind)
		}
		switch item.InputKind {
		case FuzzyInputZhuyinKeys, FuzzyInputZhuyinSyllables:
			if item.Language != FuzzyLangZh {
				return fmt.Errorf("%s: zhuyin input on a %s item", item.ID, item.Language)
			}
		case FuzzyInputRomaji:
			if item.Language != FuzzyLangJa {
				return fmt.Errorf("%s: romaji input on a %s item", item.ID, item.Language)
			}
		default:
			return fmt.Errorf("%s: unknown input kind %q", item.ID, item.InputKind)
		}
		if item.Kind == FuzzyNearSound && item.Input == item.OriginalInput {
			return fmt.Errorf("%s: near-sound case did not change the input", item.ID)
		}
	}
	return nil
}

// FuzzyManifestFor 建立題庫 manifest；raw 是實際寫出的 JSON bytes。
func FuzzyManifestFor(corpus *FuzzyCorpus, path string, raw []byte) *FuzzyManifest {
	m := &FuzzyManifest{
		Schema:    FuzzySchema,
		Corpus:    corpus.Name,
		Path:      filepathSlash(path),
		SHA256:    shaHex(raw),
		Count:     corpus.Count,
		Seed:      corpus.Seed,
		Generator: corpus.Generator,
		Sources:   append([]FuzzySourcePin(nil), corpus.Sources...),
		Kinds:     map[string]int{},
		Languages: map[string]int{},
		Models: FuzzyModelPin{
			Used: false,
			Note: "no model is loaded; placeholder int8 ONNX files are never used by this corpus or runner",
		},
		Quality: FuzzyQuality,
	}
	for _, item := range corpus.Items {
		m.Kinds[item.Kind]++
		m.Languages[item.Language]++
	}
	return m
}

func shaHex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// LoadFuzzyManifest 讀取並檢查 manifest。
func LoadFuzzyManifest(path string) (*FuzzyManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m FuzzyManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.Schema != FuzzySchema || m.Corpus == "" || m.Path == "" || len(m.SHA256) != 64 || m.Count <= 0 || m.Seed <= 0 {
		return nil, fmt.Errorf("%s: invalid manifest", path)
	}
	if m.Models.Used {
		return nil, fmt.Errorf("%s: manifest says models are used; P2-62 only accepts model-free runs", path)
	}
	return &m, nil
}

// FuzzyCorpusProblems 比對題庫、manifest 與來源 pin。任何不符都代表跑到的
// 不是同一份題庫；不因回退或缺少來源而放行。
func FuzzyCorpusProblems(corpus *FuzzyCorpus, corpusSHA string, manifest *FuzzyManifest) []string {
	var problems []string
	if manifest.Corpus != corpus.Name {
		problems = append(problems, fmt.Sprintf("manifest corpus %q does not match %q", manifest.Corpus, corpus.Name))
	}
	if manifest.SHA256 != corpusSHA {
		problems = append(problems, fmt.Sprintf("manifest sha256 %s does not match corpus %s", manifest.SHA256, corpusSHA))
	}
	if manifest.Count != corpus.Count {
		problems = append(problems, fmt.Sprintf("manifest count %d does not match corpus %d", manifest.Count, corpus.Count))
	}
	if manifest.Seed != corpus.Seed {
		problems = append(problems, fmt.Sprintf("manifest seed %d does not match corpus %d", manifest.Seed, corpus.Seed))
	}
	if len(manifest.Sources) != len(corpus.Sources) {
		problems = append(problems, fmt.Sprintf("manifest has %d source pins, corpus has %d", len(manifest.Sources), len(corpus.Sources)))
	} else {
		for i := range corpus.Sources {
			want, got := corpus.Sources[i], manifest.Sources[i]
			if want != got {
				problems = append(problems, fmt.Sprintf("source pin %d differs: manifest %+v corpus %+v", i, got, want))
			}
		}
	}
	return problems
}

// composeRomaji 用日文羅馬字組字器重播 keys；回傳完成的かな與未完成按鍵。
func composeRomaji(keys string) (units, pending string, ok bool) {
	c := jainput.NewRomajiComposer()
	for _, key := range keys {
		if _, accepted := c.Insert(string(key)); !accepted {
			return readingUnitsString(c.Units()), c.Pending(), false
		}
	}
	c.Flush()
	return readingUnitsString(c.Units()), c.Pending(), true
}

func readingUnitsString(units reading.Sequence) string {
	var b strings.Builder
	for _, u := range units {
		b.WriteString(string(u))
	}
	return b.String()
}

// kanaToRomaji 產生可由內建羅馬字表完整組回同一個讀音的按鍵。これは
// keyboard input 的來源表示，不是新的かな漢字変換規則。
func kanaToRomaji(reading string) (string, bool) {
	rs := []rune(reading)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		if i+1 < len(rs) {
			if keys, ok := kanaRomajiDigraphs[string(rs[i:i+2])]; ok {
				b.WriteString(keys)
				i++
				continue
			}
		}
		if rs[i] == 'ん' {
			next := rune(0)
			if i+1 < len(rs) {
				next = rs[i+1]
			}
			if strings.ContainsRune("あいうえおやゆよ", next) {
				b.WriteString("n'")
			} else {
				b.WriteString("nn")
			}
			continue
		}
		keys, ok := kanaRomaji[rs[i]]
		if !ok {
			return "", false
		}
		b.WriteString(keys)
	}
	return b.String(), true
}

var kanaRomajiDigraphs = map[string]string{
	"きゃ": "kya", "きゅ": "kyu", "きょ": "kyo",
	"ぎゃ": "gya", "ぎゅ": "gyu", "ぎょ": "gyo",
	"しゃ": "sha", "しゅ": "shu", "しょ": "sho",
	"じゃ": "ja", "じゅ": "ju", "じょ": "jo",
	"ちゃ": "cha", "ちゅ": "chu", "ちょ": "cho",
	"ぢゃ": "dya", "ぢゅ": "dyu", "ぢょ": "dyo",
	"にゃ": "nya", "にゅ": "nyu", "にょ": "nyo",
	"ひゃ": "hya", "ひゅ": "hyu", "ひょ": "hyo",
	"びゃ": "bya", "びゅ": "byu", "びょ": "byo",
	"ぴゃ": "pya", "ぴゅ": "pyu", "ぴょ": "pyo",
	"みゃ": "mya", "みゅ": "myu", "みょ": "myo",
	"りゃ": "rya", "りゅ": "ryu", "りょ": "ryo",
	"ふぁ": "fa", "ふぃ": "fi", "ふぇ": "fe", "ふぉ": "fo",
	"ゔぁ": "va", "ゔぃ": "vi", "ゔぇ": "ve", "ゔぉ": "vo",
}

var kanaRomaji = map[rune]string{
	'あ': "a", 'い': "i", 'う': "u", 'え': "e", 'お': "o",
	'か': "ka", 'き': "ki", 'く': "ku", 'け': "ke", 'こ': "ko",
	'が': "ga", 'ぎ': "gi", 'ぐ': "gu", 'げ': "ge", 'ご': "go",
	'さ': "sa", 'し': "shi", 'す': "su", 'せ': "se", 'そ': "so",
	'ざ': "za", 'じ': "ji", 'ず': "zu", 'ぜ': "ze", 'ぞ': "zo",
	'た': "ta", 'ち': "chi", 'つ': "tsu", 'て': "te", 'と': "to",
	'だ': "da", 'ぢ': "di", 'づ': "du", 'で': "de", 'ど': "do",
	'な': "na", 'に': "ni", 'ぬ': "nu", 'ね': "ne", 'の': "no",
	'は': "ha", 'ひ': "hi", 'ふ': "fu", 'へ': "he", 'ほ': "ho",
	'ば': "ba", 'び': "bi", 'ぶ': "bu", 'べ': "be", 'ぼ': "bo",
	'ぱ': "pa", 'ぴ': "pi", 'ぷ': "pu", 'ぺ': "pe", 'ぽ': "po",
	'ま': "ma", 'み': "mi", 'む': "mu", 'め': "me", 'も': "mo",
	'や': "ya", 'ゆ': "yu", 'よ': "yo",
	'ら': "ra", 'り': "ri", 'る': "ru", 'れ': "re", 'ろ': "ro",
	'わ': "wa", 'を': "wo", 'ゔ': "vu",
	'ぁ': "xa", 'ぃ': "xi", 'ぅ': "xu", 'ぇ': "xe", 'ぉ': "xo",
	'ゃ': "xya", 'ゅ': "xyu", 'ょ': "xyo", 'っ': "xtu", 'ゎ': "xwa",
	'ゕ': "xka", 'ゖ': "xke", 'ー': "-",
}
