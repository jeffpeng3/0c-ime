package compile

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

// JAOptions 控制日文資料包的編譯。與注音 Options 分開，避免日文欄位影響既有注音流程。
type JAOptions struct {
	Public             bool     `json:"public"`
	ExpandConjugations bool     `json:"expand_conjugations"`
	InputMethods       []string `json:"input_methods"`
	OutputVariants     []string `json:"output_variants"`
	Limits             JALimits `json:"limits"`
	MozcCostScale      float64  `json:"mozc_cost_scale"`
}

// JALimits 對應 manifest 的 limits，引擎載入後作為轉換上限。
type JALimits struct {
	MaxReadingUnits int `json:"max_reading_units"`
	MaxWordUnits    int `json:"max_word_units"`
	MaxCandidates   int `json:"max_candidates"`
}

// DefaultJAOptions 回傳日文資料包的預設值，與 core/lang.DefaultLimits 相同。
func DefaultJAOptions() JAOptions {
	return JAOptions{
		ExpandConjugations: false,
		InputMethods:       []string{"romaji", "kana"},
		OutputVariants:     []string{"ja"},
		Limits:             JALimits{MaxReadingUnits: 64, MaxWordUnits: 32, MaxCandidates: 16},
		MozcCostScale:      1.0 / 1000.0,
	}
}

// JAData 是 data/ja 目錄讀進來的全部日文來源。
type JAData struct {
	Sources     map[string]Source
	Entries     []jaimport.Entry
	Connections []jaimport.Connection
	// Counts 是來源 ID → 詞面 → 次數，用來估算 unigram 成本。
	Counts map[string]map[string]float64
	// Bigrams 是詞面配對 → P(右|左)；BigramOrigins 是同一配對的來源 ID。
	Bigrams       map[[2]string]float64
	BigramOrigins map[[2]string]string
	// Inputs 是實際讀取的來源檔相對路徑 → 雜湊與大小，供 manifest 與可重現建置檢查。
	Inputs map[string]FileInfo
}

// JAReport 是日文資料包的編譯報告。
type JAReport struct {
	Entries          int
	Readings         int
	Surfaces         int
	LongestReading   int
	Connections      int
	Bigrams          int
	Conjugated       int
	MaxSingleReading string
	MaxSingleEntries int
	LexiconBytes     int
	LMBytes          int
	TSVBytes         int
	RuntimeEstimate  int
}

// String 產生人類可讀的報告。
func (r JAReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "日文詞條（詞面×讀音×詞性）：%d\n", r.Entries)
	fmt.Fprintf(&b, "詞面：%d；讀音種類：%d；最長讀音：%d 單位\n", r.Surfaces, r.Readings, r.LongestReading)
	fmt.Fprintf(&b, "候選最多的讀音：%s（%d 筆）\n", r.MaxSingleReading, r.MaxSingleEntries)
	fmt.Fprintf(&b, "活用形新增：%d\n", r.Conjugated)
	fmt.Fprintf(&b, "連接成本：%d 對；bigram：%d\n", r.Connections, r.Bigrams)
	fmt.Fprintf(&b, "lexicon.bin：%d bytes；lm.bin：%d bytes；lexicon.tsv：%d bytes\n", r.LexiconBytes, r.LMBytes, r.TSVBytes)
	fmt.Fprintf(&b, "執行期記憶體估計：%d bytes（%.1f MiB）\n", r.RuntimeEstimate, float64(r.RuntimeEstimate)/(1<<20))
	return b.String()
}

// LoadJA 讀取 data/ja 與共用來源帳本。dir 是日文資料目錄，root 是 data/ 根目錄。
// Mozc 詞成本使用預設換算（1/1000）。
func LoadJA(dir, root string) (*JAData, []Problem, error) {
	return LoadJAWithMozcScale(dir, root, 1.0/1000.0)
}

// LoadJAWithMozcScale 與 LoadJA 相同，但可指定 Mozc 詞成本換算係數。
func LoadJAWithMozcScale(dir, root string, mozcScale float64) (*JAData, []Problem, error) {
	d := &JAData{
		Sources:       map[string]Source{},
		Counts:        map[string]map[string]float64{},
		Bigrams:       map[[2]string]float64{},
		BigramOrigins: map[[2]string]string{},
		Inputs:        map[string]FileInfo{},
	}
	var errs []Problem

	if err := loadJALedger(filepath.Join(root, "sources", "ledger.tsv"), d); err != nil {
		return nil, nil, err
	}

	words := filepath.Join(dir, "lexicon", "words.tsv")
	if !fileExists(words) {
		return nil, nil, fmt.Errorf("日文詞庫不存在：%s", words)
	}
	entries, err := parseJAWords(words)
	if err != nil {
		errs = append(errs, Problem{words, err.Error()})
	} else {
		d.Entries = append(d.Entries, entries...)
		addJAInput(d, words, root)
	}

	// import/ 下的來源檔名稱決定來源 ID：<origin>.mozc.tsv、<origin>.sudachi.csv。
	importDir := filepath.Join(dir, "import")
	if paths, _ := filepath.Glob(filepath.Join(importDir, "*")); len(paths) > 0 {
		sort.Strings(paths)
		for _, p := range paths {
			base := filepath.Base(p)
			if strings.HasSuffix(base, ".id.def") || strings.HasSuffix(base, ".def.tsv") {
				continue
			}
			origin, format, ok := jaImportOrigin(base)
			if !ok {
				continue
			}
			if _, ok := d.Sources[origin]; !ok {
				errs = append(errs, Problem{p, fmt.Sprintf("來源 %q 不在帳本內", origin)})
				continue
			}
			var imported []jaimport.Entry
			switch format {
			case "mozc":
				opt := jaimport.DefaultMozcOptions()
				opt.Origin = origin
				opt.CostScale = mozcScale
				idDef := filepath.Join(importDir, origin+".id.def")
				if fileExists(idDef) {
					pos, err := jaimport.LoadMozcIDDef(idDef)
					if err != nil {
						errs = append(errs, Problem{idDef, err.Error()})
						continue
					}
					opt.POSByID = pos
					addJAInput(d, idDef, root)
				}
				imported, err = jaimport.ParseMozcFile(p, opt)
				if err != nil {
					errs = append(errs, Problem{p, err.Error()})
					continue
				}
			case "sudachi":
				opt := jaimport.DefaultSudachiOptions()
				opt.Origin = origin
				imported, err = jaimport.ParseSudachiFile(p, opt)
				if err != nil {
					errs = append(errs, Problem{p, err.Error()})
					continue
				}
			}
			d.Entries = append(d.Entries, imported...)
			addJAInput(d, p, root)
		}
	}

	connPath := filepath.Join(dir, "connection.tsv")
	if fileExists(connPath) {
		conns, err := jaimport.ParseConnectionsFile(connPath)
		if err != nil {
			errs = append(errs, Problem{connPath, err.Error()})
		} else {
			d.Connections = conns
			addJAInput(d, connPath, root)
		}
	}

	if p := filepath.Join(dir, "lm", "unigram.tsv"); fileExists(p) {
		if err := parseJAUnigram(p, d); err != nil {
			errs = append(errs, Problem{p, err.Error()})
		} else {
			addJAInput(d, p, root)
		}
	}
	if p := filepath.Join(dir, "lm", "bigram.tsv"); fileExists(p) {
		if err := parseJABigram(p, d); err != nil {
			errs = append(errs, Problem{p, err.Error()})
		} else {
			addJAInput(d, p, root)
		}
	}
	return d, errs, nil
}

// JAResult 是日文資料包的編譯結果。
type JAResult struct {
	Lexicon *jaimport.Lexicon
	LM      *jaimport.LM
	Errors  []Problem
	Report  JAReport
	Sources []Source
	Inputs  map[string]FileInfo
}

// BuildJA 合併、驗證並產生日文資料包內容。Errors 非空時不得出版。
func BuildJA(d *JAData, o JAOptions) *JAResult {
	res := &JAResult{Inputs: map[string]FileInfo{}}
	for k, v := range d.Inputs {
		res.Inputs[k] = v
	}
	fail := func(loc, format string, a ...any) {
		res.Errors = append(res.Errors, Problem{loc, fmt.Sprintf(format, a...)})
	}
	used := map[string]bool{}
	checkOrigins := func(loc string, origins []string) {
		for _, id := range origins {
			if strings.HasPrefix(id, "rule:") {
				continue
			}
			if _, ok := d.Sources[id]; !ok {
				fail(loc, "來源 %q 不在帳本內", id)
			} else {
				used[id] = true
			}
		}
	}

	entries := append([]jaimport.Entry(nil), d.Entries...)
	for _, e := range entries {
		checkOrigins("ja:"+e.Surface, e.Origins)
	}
	if o.ExpandConjugations {
		var extra []jaimport.Entry
		for _, e := range entries {
			extra = append(extra, jaimport.ExpandConjugations(e)...)
		}
		entries = append(entries, extra...)
	}
	entries = jaimport.Merge(entries)
	if len(entries) == 0 {
		fail("ja", "沒有日文詞條")
	}
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			fail("ja:"+e.Surface, "%v", err)
			continue
		}
		if o.Limits.MaxWordUnits > 0 && len(e.Reading) > o.Limits.MaxWordUnits {
			fail("ja:"+e.Surface, "讀音 %d 單位超過上限 %d", len(e.Reading), o.Limits.MaxWordUnits)
		}
	}

	lex, err := jaimport.NewLexicon(entries, d.Connections)
	if err != nil {
		fail("lexicon", "%v", err)
		return res
	}
	res.Lexicon = lex

	total := 0.0
	combined := map[string]float64{}
	for _, counts := range d.Counts {
		for w, n := range counts {
			combined[w] += n
			total += n
		}
	}
	uni := map[string]float64{}
	for _, e := range entries {
		cost := e.Cost
		if n := combined[e.Surface]; n > 0 && total > 0 {
			cost = 1 + math.Log(total/n)
		}
		if old, ok := uni[e.Surface]; !ok || cost < old {
			uni[e.Surface] = cost
		}
	}
	bi := map[[2]string]float64{}
	for pair, p := range d.Bigrams {
		if !(p > 0 && p <= 1) {
			fail("ja:bigram", "%q→%q 的機率 %v 不合法", pair[0], pair[1], p)
			continue
		}
		if origin := d.BigramOrigins[pair]; origin != "" {
			checkOrigins("ja:bigram", []string{origin})
		}
		if _, ok := uni[pair[0]]; !ok {
			continue // 左詞不在詞庫，bigram 不進模型
		}
		if _, ok := uni[pair[1]]; !ok {
			continue
		}
		bi[pair] = -math.Log(p)
	}
	res.LM = jaimport.NewLM(uni, bi)

	if len(d.Connections) > 0 {
		if origin := connectionOrigin(d); origin != "" {
			checkOrigins("ja:connection", []string{origin})
		}
	}
	for origin := range d.Counts {
		checkOrigins("ja:unigram", []string{origin})
	}
	for _, origin := range d.BigramOrigins {
		checkOrigins("ja:bigram", []string{origin})
	}
	for id := range used {
		res.Sources = append(res.Sources, d.Sources[id])
	}
	sort.Slice(res.Sources, func(i, j int) bool { return res.Sources[i].ID < res.Sources[j].ID })
	if len(res.Errors) == 0 {
		res.Report.fill(lex, res.LM)
	}
	return res
}

func connectionOrigin(d *JAData) string {
	// 連接成本沒有逐列來源；優先標成 curated，沒有 curated 時標成第一個實際使用的來源。
	if _, ok := d.Sources["curated"]; ok {
		return "curated"
	}
	ids := make([]string, 0, len(d.Sources))
	for id := range d.Sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func (r *JAReport) fill(lex *jaimport.Lexicon, lm *jaimport.LM) {
	r.Entries = lex.Len()
	r.LongestReading = lex.LongestWord()
	r.Connections = len(lex.Matrix().Connections())
	r.Bigrams = lm.BigramLen()
	surfaces := map[string]bool{}
	readings := map[string]int{}
	for _, e := range lex.Entries() {
		surfaces[e.Surface] = true
		k := reading.Key(e.Reading)
		readings[k]++
		if readings[k] > r.MaxSingleEntries || readings[k] == r.MaxSingleEntries && k < r.MaxSingleReading {
			r.MaxSingleEntries, r.MaxSingleReading = readings[k], k
		}
		if len(e.Origins) > 0 {
			for _, x := range e.Origins {
				if x == "rule:conjugation" {
					r.Conjugated++
					break
				}
			}
		}
	}
	r.Surfaces = len(surfaces)
	r.Readings = len(readings)
	lex.Encode()
	lm.Encode()
	r.LexiconBytes = lex.EncodedSize()
	r.LMBytes = lm.EncodedSize()
	r.TSVBytes = len(jaimport.TSV(lex.Entries()))
	r.RuntimeEstimate = lex.RuntimeBytesEstimate() + lm.RuntimeBytesEstimate()
}

// Write 把日文編譯結果寫成資料包目錄。公開模式先檢查來源帳本與授權全文，
// 失敗時不建立或改寫輸出。manifest 不含時間戳，同一份輸入會得到相同位元組。
func (r *JAResult) Write(out, licenseDir string, o JAOptions) error {
	if len(r.Errors) > 0 {
		return fmt.Errorf("有 %d 個錯誤，不出版", len(r.Errors))
	}
	if o.Public {
		if err := checkJAPublicSources(r.Sources, licenseDir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	lexBytes := r.Lexicon.Encode()
	lmBytes := r.LM.Encode()
	tsvBytes := jaimport.TSV(r.Lexicon.Entries())
	files := map[string][]byte{
		"lexicon.bin": lexBytes,
		"lm.bin":      lmBytes,
		"lexicon.tsv": tsvBytes,
	}
	m := JAManifest{
		Format:         2,
		Language:       "ja",
		InputMethods:   append([]string(nil), o.InputMethods...),
		OutputVariants: append([]string(nil), o.OutputVariants...),
		Limits:         o.Limits,
		Files:          map[string]FileInfo{},
		Counts: map[string]int{
			"entries": r.Report.Entries, "surfaces": r.Report.Surfaces,
			"readings": r.Report.Readings, "connections": r.Report.Connections,
			"bigrams": r.Report.Bigrams, "conjugated": r.Report.Conjugated,
		},
		Options:     o,
		Sources:     nil,
		Inputs:      map[string]FileInfo{},
		SourceHash:  sourceHash(r.Inputs),
		RuntimeHint: map[string]int{"encoded_bytes": len(lexBytes) + len(lmBytes), "estimated_bytes": r.Report.RuntimeEstimate},
	}
	for name, b := range files {
		sum := sha256.Sum256(b)
		m.Files[name] = FileInfo{SHA256: hex.EncodeToString(sum[:]), Size: len(b)}
		if err := writeAtomic(filepath.Join(out, name), b); err != nil {
			return err
		}
	}
	for _, s := range r.Sources {
		m.Sources = append(m.Sources, SourceInfo{s.ID, s.Name, s.URL, s.Version, s.License, s.Attribution, s.Distributable})
	}
	inputs := make([]string, 0, len(r.Inputs))
	for p := range r.Inputs {
		inputs = append(inputs, p)
	}
	sort.Strings(inputs)
	for _, p := range inputs {
		m.Inputs[p] = r.Inputs[p]
	}
	var notice strings.Builder
	notice.WriteString("靈犀注音（0c）日文資料包使用下列來源，各來源依其授權條款使用。\n")
	notice.WriteString("Mozc 詞典屬混合授權，不得整體標成單一 BSD-3-Clause。\n")
	for _, s := range r.Sources {
		fmt.Fprintf(&notice, "\n== %s（%s）==\n授權：%s\n版本：%s\n網址：%s\n", s.Name, s.ID, s.License, s.Version, s.URL)
		if s.Attribution != "" && s.Attribution != "-" {
			fmt.Fprintf(&notice, "姓名標示：%s\n", s.Attribution)
		}
		if b, err := os.ReadFile(filepath.Join(licenseDir, s.ID+".txt")); err == nil {
			fmt.Fprintf(&notice, "\n%s\n", strings.TrimSpace(string(b)))
		}
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	for name, b := range map[string][]byte{
		"manifest.json": mb,
		"NOTICE.txt":    []byte(notice.String()),
		"report.txt":    []byte(r.Report.String()),
	} {
		if err := writeAtomic(filepath.Join(out, name), b); err != nil {
			return err
		}
	}
	return nil
}

// JAManifest 是日文資料包的 manifest v2。
type JAManifest struct {
	Format         int                 `json:"format"`
	Language       string              `json:"language"`
	InputMethods   []string            `json:"input_methods"`
	OutputVariants []string            `json:"output_variants"`
	Limits         JALimits            `json:"limits"`
	Files          map[string]FileInfo `json:"files"`
	Counts         map[string]int      `json:"counts"`
	Options        JAOptions           `json:"options"`
	Sources        []SourceInfo        `json:"sources"`
	Inputs         map[string]FileInfo `json:"inputs"`
	SourceHash     string              `json:"source_hash"`
	RuntimeHint    map[string]int      `json:"runtime_hint"`
}

// ManifestLanguage 回傳資料包語言。沒有 language 或 format 1 一律視為注音包。
func ManifestLanguage(mb []byte) (string, error) {
	var m struct {
		Format   int    `json:"format"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(mb, &m); err != nil {
		return "", fmt.Errorf("manifest.json：%w", err)
	}
	if m.Format <= 1 || strings.TrimSpace(m.Language) == "" {
		return "zh", nil
	}
	return m.Language, nil
}

func checkJAPublicSources(sources []Source, licenseDir string) error {
	if len(sources) == 0 {
		return fmt.Errorf("公開日文包沒有來源紀錄")
	}
	for _, s := range sources {
		if s.ID == "" || s.ID == "." || s.ID == ".." || strings.ContainsAny(s.ID, `/\\`) {
			return fmt.Errorf("公開包來源 ID 無效：%q", s.ID)
		}
		if !s.Distributable {
			return fmt.Errorf("公開包來源 %s 標示為不可散布", s.ID)
		}
		for _, field := range []struct{ name, value string }{
			{"名稱", s.Name}, {"網址", s.URL}, {"版本", s.Version},
			{"授權", s.License}, {"署名", s.Attribution},
		} {
			value := strings.TrimSpace(field.value)
			if value == "" || value == "-" || value == "未定" || strings.EqualFold(value, "unknown") {
				return fmt.Errorf("公開包來源 %s 缺少%s", s.ID, field.name)
			}
		}
		b, err := os.ReadFile(filepath.Join(licenseDir, s.ID+".txt"))
		if err != nil || len(strings.TrimSpace(string(b))) == 0 {
			return fmt.Errorf("公開包來源 %s 缺少授權聲明：%s.txt", s.ID, s.ID)
		}
	}
	return nil
}

func loadJALedger(path string, d *JAData) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 6 {
			return fmt.Errorf("%s:%d：來源帳本至少需要 6 欄", path, ln)
		}
		dist := strings.TrimSpace(f[5])
		if dist != "yes" && dist != "no" {
			return fmt.Errorf("%s:%d：可散布欄須為 yes 或 no", path, ln)
		}
		attr := ""
		if len(f) > 6 {
			attr = strings.TrimSpace(f[6])
		}
		s := Source{ID: strings.TrimSpace(f[0]), Name: f[1], URL: f[2], Version: f[3], License: f[4], Distributable: dist == "yes", Attribution: attr}
		if _, dup := d.Sources[s.ID]; dup {
			return fmt.Errorf("%s:%d：來源 ID %q 重複", path, ln, s.ID)
		}
		d.Sources[s.ID] = s
	}
	return sc.Err()
}

func parseJAWords(path string) ([]jaimport.Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []jaimport.Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) < 5 {
			return nil, fmt.Errorf("第 %d 行：日文詞條需要詞面、讀音、成本、詞性、來源五欄", ln)
		}
		reading, err := jaimport.NormalizeReading(c[1])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", ln, err)
		}
		cost, err := strconv.ParseFloat(strings.TrimSpace(c[2]), 64)
		if err != nil || math.IsNaN(cost) || math.IsInf(cost, 0) || cost < jaimport.MinWordCost || cost > jaimport.MaxWordCost {
			return nil, fmt.Errorf("第 %d 行：成本 %q 不合法；容許範圍 %v～%v", ln, c[2], jaimport.MinWordCost, jaimport.MaxWordCost)
		}
		pos, err := jaimport.ParsePOS(strings.TrimSpace(c[3]))
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", ln, err)
		}
		origins := splitJAList(c[4])
		if len(origins) == 0 {
			return nil, fmt.Errorf("第 %d 行：缺少來源", ln)
		}
		conj := jaimport.ConjNone
		if len(c) > 5 {
			conj = jaimport.MapConjugation(c[5])
		}
		out = append(out, jaimport.MakeEntry(strings.TrimSpace(c[0]), reading, cost, pos, origins, conj))
	}
	return out, sc.Err()
}

func parseJAUnigram(path string, d *JAData) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) < 3 {
			return fmt.Errorf("第 %d 行：詞頻需要詞面、次數、來源三欄", ln)
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(c[1]), 64)
		if err != nil || n < 0 {
			return fmt.Errorf("第 %d 行：次數 %q 不合法", ln, c[1])
		}
		origin := strings.TrimSpace(c[2])
		if d.Counts[origin] == nil {
			d.Counts[origin] = map[string]float64{}
		}
		d.Counts[origin][strings.TrimSpace(c[0])] += n
	}
	return sc.Err()
}

func parseJABigram(path string, d *JAData) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) < 3 {
			return fmt.Errorf("第 %d 行：bigram 需要左詞、右詞、機率三欄", ln)
		}
		p, err := strconv.ParseFloat(strings.TrimSpace(c[2]), 64)
		if err != nil || !(p > 0 && p <= 1) {
			return fmt.Errorf("第 %d 行：機率 %q 不合法", ln, c[2])
		}
		pair := [2]string{strings.TrimSpace(c[0]), strings.TrimSpace(c[1])}
		d.Bigrams[pair] = p
		if len(c) > 3 && strings.TrimSpace(c[3]) != "" {
			d.BigramOrigins[pair] = strings.TrimSpace(c[3])
		}
	}
	return sc.Err()
}

// jaImportOrigin 由檔名取出來源 ID 與格式：<origin>.mozc.tsv、<origin>.sudachi.csv。
func jaImportOrigin(base string) (origin, format string, ok bool) {
	for _, x := range []struct {
		suffix string
		format string
	}{
		{".mozc.tsv", "mozc"}, {".mozc.txt", "mozc"},
		{".sudachi.csv", "sudachi"}, {".sudachi.tsv", "sudachi"},
	} {
		if strings.HasSuffix(base, x.suffix) {
			return strings.TrimSuffix(base, x.suffix), x.format, true
		}
	}
	return "", "", false
}

func addJAInput(d *JAData, path, root string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	key := path
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		key = rel
	}
	sum := sha256.Sum256(b)
	d.Inputs[filepath.ToSlash(key)] = FileInfo{SHA256: hex.EncodeToString(sum[:]), Size: len(b)}
}

func sourceHash(inputs map[string]FileInfo) string {
	values := make([]string, 0, len(inputs))
	for _, v := range inputs {
		values = append(values, fmt.Sprintf("%s %d", v.SHA256, v.Size))
	}
	sort.Strings(values)
	h := sha256.New()
	for _, v := range values {
		fmt.Fprintln(h, v)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func splitJAList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
