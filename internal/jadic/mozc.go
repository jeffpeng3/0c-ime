package jadic

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

// MozcImportSchema 是 Mozc OSS 詞典匯入回執的格式版本。
const MozcImportSchema = "0c-jadic-mozc/1"

// DefaultMozcOrigin 是 Mozc OSS 詞典寫進 words.tsv 的來源 ID。
const DefaultMozcOrigin = "ja-mozc"

// MozcImportOptions 控制把 Mozc OSS dictionary_oss 轉成執行期資料目錄。
type MozcImportOptions struct {
	// PackageDir 是 dictionary_oss 目錄，需含 dictionary00.txt～dictionary09.txt、
	// id.def 與 connection_single_column.txt。
	PackageDir string
	// OutDir 是執行期資料目錄；不可指向 checked-in data/。
	OutDir string
	// Origin 寫進 words.tsv 第五欄；空白時使用 DefaultMozcOrigin。
	Origin string
	// CostScale 把 Mozc 的整數詞成本與連接成本換算成引擎單位；0 使用 0.001。
	CostScale float64
	// MaxReadingUnits 大於 0 時，超過上限的詞條跳過；預設 32（引擎 MaxWordUnits）。
	MaxReadingUnits int
	// ConnectionMode 是 POS 對聚合方式：median、mean、min、max。空白時用 median。
	ConnectionMode string
	// SourceRepo、SourceCommit 只寫進回執，不影響輸出資料。
	SourceRepo   string
	SourceCommit string
	// Force 允許覆寫既有輸出。
	Force bool
}

// MozcPOS 是 id.def 的一列。Name 是完整 MeCab 詞性 CSV，POS 是對應的引擎詞性。
type MozcPOS struct {
	ID   int
	Name string
	POS  jaimport.POS
	POS1 string
	POS2 string
	Conj string
	Form string
}

// MozcImportStats 是匯入結果統計。
type MozcImportStats struct {
	RawEntries          int
	FilteredEntries     int
	AddedAuxEntries     int
	SymbolEntries       int
	FilteredSymbols     int
	FilteredNonJapanese int
	FilteredReading     int
	FilteredLongReading int
	Entries             int
	Surfaces            int
	DuplicateEntries    int
	NegativeCosts       int
	MaxReadingUnits     int
	POSCounts           map[string]int
	UnknownPOS          int
	UnknownConj         int
}

// MozcSourceFile 是回執中的來源檔雜湊。
type MozcSourceFile struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// MozcImportReceipt 寫在輸出目錄，記錄來源與匯入參數。
type MozcImportReceipt struct {
	Schema        string            `json:"schema"`
	Repository    string            `json:"repository,omitempty"`
	Commit        string            `json:"commit,omitempty"`
	PackageDir    string            `json:"package_dir"`
	CostScale     float64           `json:"cost_scale"`
	CostRule      string            `json:"cost_rule"`
	Connection    string            `json:"connection_aggregation"`
	SourceFiles   []MozcSourceFile  `json:"source_files"`
	Counts        map[string]int    `json:"counts"`
	POSByID       map[string]string `json:"pos_by_id"`
	Files         []string          `json:"files"`
	FilteredNotes []string          `json:"filtered_notes,omitempty"`
}

// MozcImportResult 是匯入後的路徑與統計。
type MozcImportResult struct {
	WordsPath      string
	SymbolsPath    string
	ConnectionPath string
	ReceiptPath    string
	Stats          MozcImportStats
	Receipt        MozcImportReceipt
}

// mozcRow 是一筆通過過濾、準備寫進 words.tsv 的 Mozc 詞條。
type mozcRow struct {
	surface string
	reading string
	pos     jaimport.POS
	conj    jaimport.Conjugation
	cost    float64
	leftID  uint16
	rightID uint16
}

// mozcRaw 是 dictionary0*.txt 的原始一列。
type mozcRaw struct {
	reading string
	surface string
	lid     int
	rid     int
	cost    int
}

// mozcAux 是 aux_dictionary.tsv 的一列。
type mozcAux struct {
	key        string
	value      string
	baseKey    string
	baseValue  string
	costOffset int
}

// ImportMozc 讀取 Mozc OSS dictionary_oss，寫出引擎執行期布局：
// lexicon/words.tsv、symbols/jadic.tsv、connection.tsv 與 mozc-import.json。
//
// 原始來源不進輸出目錄，也不進 Git；回執只記檔名、大小與 SHA-256。
func ImportMozc(opt MozcImportOptions) (MozcImportResult, error) {
	var res MozcImportResult
	if opt.PackageDir == "" {
		return res, fmt.Errorf("jadic: Mozc 匯入需要來源目錄")
	}
	if opt.OutDir == "" {
		return res, fmt.Errorf("jadic: Mozc 匯入需要輸出目錄")
	}
	if isRepoDataDir(opt.OutDir) {
		return res, fmt.Errorf("jadic: 輸出目錄 %q 指向 checked-in data/；請改用 build/ 或 repo 外的目錄", opt.OutDir)
	}
	if opt.Origin == "" {
		opt.Origin = DefaultMozcOrigin
	}
	if strings.ContainsAny(opt.Origin, "\t\r\n") {
		return res, fmt.Errorf("jadic: 來源 ID %q 含非法字元", opt.Origin)
	}
	if opt.CostScale == 0 {
		opt.CostScale = DefaultCostScale
	}
	if math.IsNaN(opt.CostScale) || math.IsInf(opt.CostScale, 0) || opt.CostScale < 0 {
		return res, fmt.Errorf("jadic: 成本換算係數 %v 不合法", opt.CostScale)
	}
	mode := strings.ToLower(strings.TrimSpace(opt.ConnectionMode))
	if mode == "" {
		mode = "median"
	}
	switch mode {
	case "median", "mean", "min", "max":
	default:
		return res, fmt.Errorf("jadic: 未知連接成本聚合方式 %q", opt.ConnectionMode)
	}
	if opt.MaxReadingUnits == 0 {
		// 引擎的 lang.DefaultLimits().MaxWordUnits 是 32；超過的詞條永遠不會進 lattice。
		opt.MaxReadingUnits = 32
	}

	dictFiles, err := mozcDictionaryFiles(opt.PackageDir)
	if err != nil {
		return res, err
	}
	idDefPath := filepath.Join(opt.PackageDir, "id.def")
	connPath := filepath.Join(opt.PackageDir, "connection_single_column.txt")
	auxPath := filepath.Join(opt.PackageDir, "aux_dictionary.tsv")
	filterPath := filepath.Join(opt.PackageDir, "dictionary_filter.tsv")

	posByID, err := loadMozcIDDefFile(idDefPath)
	if err != nil {
		return res, err
	}
	if len(posByID) == 0 {
		return res, fmt.Errorf("jadic: %s 沒有詞性", idDefPath)
	}
	aux, err := loadMozcAuxFile(auxPath)
	if err != nil {
		return res, err
	}
	filters, err := loadMozcFilters(filterPath)
	if err != nil {
		return res, err
	}

	wordsPath := filepath.Join(opt.OutDir, "lexicon", "words.tsv")
	symbolsPath := filepath.Join(opt.OutDir, "symbols", "jadic.tsv")
	connOutPath := filepath.Join(opt.OutDir, "connection.tsv")
	connSourcePath := filepath.Join(opt.OutDir, "connection.source")
	receiptPath := filepath.Join(opt.OutDir, "mozc-import.json")
	if !opt.Force {
		for _, path := range []string{wordsPath, symbolsPath, connOutPath, connSourcePath, receiptPath} {
			if _, err := os.Stat(path); err == nil {
				return res, fmt.Errorf("jadic: %s 已存在；要覆寫請加 -force", path)
			}
		}
	}
	for _, dir := range []string{filepath.Join(opt.OutDir, "lexicon"), filepath.Join(opt.OutDir, "symbols")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return res, err
		}
	}

	// 先收集 aux 需要的基準詞條，再串流解析 dictionary 檔案。aux 的語意是
	// 複製基準詞條的左右 ID 與成本，因此必須保留原始 ID；POS 聚合仍只用詞性。
	auxBaseKeys := map[string]bool{}
	for _, a := range aux {
		auxBaseKeys[a.baseKey+"\x00"+a.baseValue] = true
	}
	rawByBase := map[string][]mozcRaw{}
	var raws []mozcRaw
	for _, path := range dictFiles {
		if err := scanMozcDictionary(path, filters, func(r mozcRaw) {
			raws = append(raws, r)
			key := r.reading + "\x00" + r.surface
			if auxBaseKeys[key] {
				rawByBase[key] = append(rawByBase[key], r)
			}
		}); err != nil {
			return res, err
		}
	}
	stats := MozcImportStats{POSCounts: map[string]int{}, RawEntries: len(raws)}
	for _, a := range aux {
		base := rawByBase[a.baseKey+"\x00"+a.baseValue]
		for _, b := range base {
			// 已存在的同 key/value/lid/rid 不覆寫，和 Mozc gen_aux_dictionary.py 相同。
			exists := false
			for _, r := range raws {
				if r.reading == a.key && r.surface == a.value && r.lid == b.lid && r.rid == b.rid {
					exists = true
					break
				}
			}
			if exists {
				continue
			}
			raws = append(raws, mozcRaw{
				reading: a.key,
				surface: a.value,
				lid:     b.lid,
				rid:     b.rid,
				cost:    b.cost + a.costOffset,
			})
			stats.AddedAuxEntries++
		}
	}

	type rowKey struct {
		surface string
		reading string
		pos     jaimport.POS
		leftID  uint16
		rightID uint16
	}
	rows := make(map[rowKey]mozcRow, len(raws)/2)
	order := make([]rowKey, 0, len(raws)/2)
	symbolSeen := map[string]bool{}
	var symbols []symbolRow
	for _, r := range raws {
		info, ok := posByID[r.lid]
		if !ok {
			return res, fmt.Errorf("jadic: 詞條 %q 的左 ID %d 不在 id.def", r.surface, r.lid)
		}
		cost := float64(r.cost) * opt.CostScale
		if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < -32.767 || cost > 32.767 {
			return res, fmt.Errorf("jadic: 詞條 %q 成本 %d 換算後 %v 超出容許範圍", r.surface, r.cost, cost)
		}
		if info.POS == jaimport.POSSymbol {
			stats.FilteredSymbols++
			if r.cost < 0 {
				continue
			}
			desc := mozcSymbolDescription(info.Name)
			key := r.reading + "\x00" + r.surface + "\x00" + desc
			if !symbolSeen[key] {
				symbolSeen[key] = true
				symbols = append(symbols, symbolRow{reading: r.reading, surface: r.surface, description: desc})
				stats.SymbolEntries++
			}
			continue
		}
		if !containsJapanese(r.surface) {
			stats.FilteredNonJapanese++
			continue
		}
		seq, err := jaimport.NormalizeReading(r.reading)
		if err != nil {
			// Mozc 的數字、記号與「～」讀音不是かな輸入；lattice 永遠不會查到，
			// 因此跳過並留下計數，不讓少數非かな列中斷整份詞典。
			stats.FilteredReading++
			continue
		}
		readingText, err := mozcReadingTSV(seq)
		if err != nil {
			return res, fmt.Errorf("jadic: 詞條 %q 的讀音：%w", r.surface, err)
		}
		if n := len(seq); n > stats.MaxReadingUnits {
			stats.MaxReadingUnits = n
		}
		if opt.MaxReadingUnits > 0 && len(seq) > opt.MaxReadingUnits {
			stats.FilteredLongReading++
			continue
		}
		conj := jaimport.MapConjugation(info.Conj)
		if info.Conj != "" && info.Conj != "*" && conj == jaimport.ConjUnknown {
			stats.UnknownConj++
		}
		if info.POS == jaimport.POSOther {
			stats.UnknownPOS++
		}
		if cost < 0 {
			stats.NegativeCosts++
		}
		k := rowKey{
			surface: r.surface, reading: readingText, pos: info.POS,
			leftID: uint16(r.lid), rightID: uint16(r.rid),
		}
		old, ok := rows[k]
		if !ok {
			rows[k] = mozcRow{
				surface: r.surface, reading: readingText, pos: info.POS, conj: conj, cost: cost,
				leftID: uint16(r.lid), rightID: uint16(r.rid),
			}
			order = append(order, k)
			continue
		}
		stats.DuplicateEntries++
		if cost < old.cost {
			old.cost = cost
			old.conj = conj
			old.leftID = uint16(r.lid)
			old.rightID = uint16(r.rid)
			rows[k] = old
		}
	}

	sort.Slice(order, func(i, j int) bool {
		a, b := rows[order[i]], rows[order[j]]
		if a.surface != b.surface {
			return a.surface < b.surface
		}
		if a.cost != b.cost {
			return a.cost < b.cost
		}
		if a.reading != b.reading {
			return a.reading < b.reading
		}
		if a.pos != b.pos {
			return string(a.pos) < string(b.pos)
		}
		return a.conj < b.conj
	})

	wordsTmp := wordsPath + ".tmp"
	wf, err := os.Create(wordsTmp)
	if err != nil {
		return res, err
	}
	bw := bufio.NewWriterSize(wf, 1<<20)
	if _, err := fmt.Fprintln(bw, "# 由 Mozc OSS dictionary_oss 匯入；詞面\t讀音\t成本\t詞性\t來源\t活用型"); err != nil {
		wf.Close()
		os.Remove(wordsTmp)
		return res, err
	}
	seenSurface := ""
	for _, k := range order {
		r := rows[k]
		if r.surface != seenSurface {
			stats.Surfaces++
			seenSurface = r.surface
		}
		stats.Entries++
		stats.POSCounts[string(r.pos)]++
		if _, err := fmt.Fprintf(bw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
			r.surface, r.reading, formatCost(r.cost), r.pos, opt.Origin, r.conj, r.leftID, r.rightID); err != nil {
			wf.Close()
			os.Remove(wordsTmp)
			return res, err
		}
	}
	if err := bw.Flush(); err != nil {
		wf.Close()
		os.Remove(wordsTmp)
		return res, err
	}
	if err := wf.Sync(); err != nil {
		wf.Close()
		os.Remove(wordsTmp)
		return res, err
	}
	if err := wf.Close(); err != nil {
		os.Remove(wordsTmp)
		return res, err
	}
	if err := os.Rename(wordsTmp, wordsPath); err != nil {
		os.Remove(wordsTmp)
		return res, err
	}

	symbolsTmp := symbolsPath + ".tmp"
	if err := writeMozcSymbols(symbolsTmp, symbols); err != nil {
		os.Remove(symbolsTmp)
		return res, err
	}
	if err := os.Rename(symbolsTmp, symbolsPath); err != nil {
		os.Remove(symbolsTmp)
		return res, err
	}

	contextOutPath := filepath.Join(opt.OutDir, "connection.bin")
	connTmp := connOutPath + ".tmp"
	contextTmp := contextOutPath + ".tmp"
	if err := writeMozcConnections(connTmp, contextTmp, connPath, posByID, opt.CostScale, mode); err != nil {
		os.Remove(connTmp)
		os.Remove(contextTmp)
		return res, err
	}
	if err := os.Rename(connTmp, connOutPath); err != nil {
		os.Remove(connTmp)
		os.Remove(contextTmp)
		return res, err
	}
	if err := os.Rename(contextTmp, contextOutPath); err != nil {
		os.Remove(contextTmp)
		return res, err
	}

	if err := os.WriteFile(connSourcePath, []byte(opt.Origin+"\n"), 0o644); err != nil {
		return res, err
	}
	files := []string{"lexicon/words.tsv", "symbols/jadic.tsv", "connection.tsv", "connection.bin", "connection.source", "mozc-import.json"}
	receipt := MozcImportReceipt{
		Schema:      MozcImportSchema,
		Repository:  opt.SourceRepo,
		Commit:      opt.SourceCommit,
		PackageDir:  opt.PackageDir,
		CostScale:   opt.CostScale,
		CostRule:    CostRule,
		Connection:  mode,
		SourceFiles: nil,
		Counts: map[string]int{
			"raw_entries":           stats.RawEntries,
			"added_aux_entries":     stats.AddedAuxEntries,
			"symbol_entries":        stats.SymbolEntries,
			"filtered_symbols":      stats.FilteredSymbols,
			"filtered_non_japanese": stats.FilteredNonJapanese,
			"filtered_reading":      stats.FilteredReading,
			"filtered_long_reading": stats.FilteredLongReading,
			"entries":               stats.Entries,
			"surfaces":              stats.Surfaces,
			"duplicate_entries":     stats.DuplicateEntries,
			"negative_costs":        stats.NegativeCosts,
			"max_reading_units":     stats.MaxReadingUnits,
		},
		POSByID: map[string]string{},
		Files:   files,
	}
	for _, path := range append(append([]string{}, dictFiles...), idDefPath, connPath, auxPath, filterPath) {
		f, err := describeMozcSource(path)
		if err != nil {
			return res, err
		}
		receipt.SourceFiles = append(receipt.SourceFiles, f)
	}
	for id, info := range posByID {
		receipt.POSByID[strconv.Itoa(id)] = info.Name
	}
	if err := writeJSON(receiptPath, receipt); err != nil {
		return res, err
	}

	res.WordsPath = wordsPath
	res.SymbolsPath = symbolsPath
	res.ConnectionPath = connOutPath
	res.ReceiptPath = receiptPath
	res.Stats = stats
	res.Receipt = receipt
	return res, nil
}

func isRepoDataDir(out string) bool {
	clean := filepath.Clean(out)
	if filepath.IsAbs(clean) {
		return false
	}
	return clean == "data" || strings.HasPrefix(clean, "data"+string(os.PathSeparator))
}

// mozcDictionaryFiles 回傳 dictionary00.txt～dictionary09.txt；缺少任何一檔都報錯，
// 避免只匯入部分詞典卻看起來成功。
func mozcDictionaryFiles(dir string) ([]string, error) {
	var out []string
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, fmt.Sprintf("dictionary%02d.txt", i))
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("jadic: 缺少 Mozc 詞典檔 %s：%w", path, err)
		}
		out = append(out, path)
	}
	return out, nil
}

func scanMozcDictionary(path string, filters []*regexp.Regexp, fn func(mozcRaw)) error {
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
		if matchesMozcFilter(line, filters) {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			return fmt.Errorf("jadic: %s 第 %d 行：需要讀音、左 ID、右 ID、成本、詞面五欄", path, ln)
		}
		lid, err1 := strconv.Atoi(strings.TrimSpace(fields[1]))
		rid, err2 := strconv.Atoi(strings.TrimSpace(fields[2]))
		cost, err3 := strconv.Atoi(strings.TrimSpace(fields[3]))
		if err1 != nil || err2 != nil || err3 != nil || lid < 0 || rid < 0 {
			return fmt.Errorf("jadic: %s 第 %d 行：左 ID、右 ID 或成本不合法", path, ln)
		}
		surface := fields[4]
		if surface == "" {
			return fmt.Errorf("jadic: %s 第 %d 行：詞面不可為空", path, ln)
		}
		fn(mozcRaw{reading: fields[0], surface: surface, lid: lid, rid: rid, cost: cost})
	}
	return sc.Err()
}

// loadMozcIDDefFile 讀取 Mozc id.def。格式是「ID<空白>詞性 CSV」，
// 不是 Tab；詞性 CSV 的前六欄是 POS1～POS4、活用型、活用形。
func loadMozcIDDefFile(path string) (map[int]MozcPOS, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadMozcIDDef(f)
}

// LoadMozcIDDef 解析 Mozc id.def。
func LoadMozcIDDef(r io.Reader) (map[int]MozcPOS, error) {
	out := map[int]MozcPOS{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idText, name, ok := strings.Cut(line, " ")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("jadic: id.def 第 %d 行：需要 ID 與詞性 CSV", ln)
		}
		id, err := strconv.Atoi(strings.TrimSpace(idText))
		if err != nil || id < 0 {
			return nil, fmt.Errorf("jadic: id.def 第 %d 行：ID %q 不合法", ln, idText)
		}
		name = strings.TrimSpace(name)
		parts := strings.Split(name, ",")
		info := MozcPOS{ID: id, Name: name, POS: MapMozcPOS(name)}
		if len(parts) > 0 {
			info.POS1 = strings.TrimSpace(parts[0])
		}
		if len(parts) > 1 {
			info.POS2 = strings.TrimSpace(parts[1])
		}
		if len(parts) > 4 {
			info.Conj = strings.TrimSpace(parts[4])
		}
		if len(parts) > 5 {
			info.Form = strings.TrimSpace(parts[5])
		}
		out[id] = info
	}
	return out, sc.Err()
}

// MapMozcPOS 把 Mozc/MeCab 的完整詞性 CSV 對到 core/ja/lexicon 的 POS。
//
// 第一層不足以表達 IME 需要的差異，因此先看「POS1,POS2」：
//   - 名詞,形容動詞語幹 -> adjectival_noun
//   - 名詞,数 -> number
//   - 名詞,接尾 -> suffix
//   - 動詞,接尾、形容詞,接尾 -> suffix
//   - 動詞,非自立 -> auxiliary（補助動詞，依附前一個文節）
//   - フィラー、その他,間投 -> interjection
//
// 其餘才退回第一層：名詞→noun、動詞→verb、形容詞→adjective、
// 助動詞→auxiliary、助詞→particle、接頭詞→prefix、副詞→adverb、
// 連体詞→determiner、接続詞→conjunction、感動詞→interjection、
// 記号→symbol。沒有新增 POS 值，因為上表已覆蓋會影響連接與文節的差異。
func MapMozcPOS(name string) jaimport.POS {
	parts := strings.Split(name, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	first := ""
	second := ""
	if len(parts) > 0 {
		first = parts[0]
	}
	if len(parts) > 1 {
		second = parts[1]
	}
	switch first + "," + second {
	case "名詞,形容動詞語幹":
		return jaimport.POSAdjectivalNoun
	case "名詞,数":
		return jaimport.POSNumber
	case "名詞,接尾":
		return jaimport.POSSuffix
	case "動詞,接尾", "形容詞,接尾":
		return jaimport.POSSuffix
	case "動詞,非自立":
		return jaimport.POSAuxiliary
	case "フィラー,*", "その他,間投":
		return jaimport.POSInterjection
	}
	switch first {
	case "名詞", "名詞句":
		return jaimport.POSNoun
	case "動詞":
		return jaimport.POSVerb
	case "形容詞":
		return jaimport.POSAdjective
	case "助動詞":
		return jaimport.POSAuxiliary
	case "助詞":
		return jaimport.POSParticle
	case "接頭詞", "接頭辞", "接頭語":
		return jaimport.POSPrefix
	case "副詞":
		return jaimport.POSAdverb
	case "連体詞":
		return jaimport.POSDeterminer
	case "接続詞":
		return jaimport.POSConjunction
	case "感動詞", "間投詞":
		return jaimport.POSInterjection
	case "記号", "補助記号":
		return jaimport.POSSymbol
	case "フィラー":
		return jaimport.POSInterjection
	case "BOS/EOS":
		return jaimport.POSBOS
	}
	return jaimport.POSOther
}

func mozcSymbolDescription(name string) string {
	parts := strings.Split(name, ",")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "*" {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) > 1 && kept[0] == "記号" {
		kept = kept[1:]
	}
	desc := strings.Join(kept, "・")
	if desc == "" {
		desc = SymbolCategory
	}
	return strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(desc)
}

func mozcReadingTSV(seq reading.Sequence) (string, error) {
	parts := make([]string, len(seq))
	for i, u := range seq {
		parts[i] = string(u)
	}
	return strings.Join(parts, " "), nil
}

func writeMozcSymbols(path string, rows []symbolRow) error {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].reading != rows[j].reading {
			return rows[i].reading < rows[j].reading
		}
		if rows[i].surface != rows[j].surface {
			return rows[i].surface < rows[j].surface
		}
		return rows[i].description < rows[j].description
	})
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<16)
	if _, err := fmt.Fprintln(bw, "# 由 Mozc OSS dictionary_oss 匯入；讀音\t詞面\t描述\t分類"); err != nil {
		f.Close()
		return err
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(bw, "%s\t%s\t%s\t%s\n", r.reading, r.surface, r.description, SymbolCategory); err != nil {
			f.Close()
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func loadMozcAuxFile(path string) ([]mozcAux, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []mozcAux
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			return nil, fmt.Errorf("jadic: aux_dictionary.tsv 第 %d 行：需要五欄", ln)
		}
		offset, err := strconv.Atoi(strings.TrimSpace(fields[4]))
		if err != nil {
			return nil, fmt.Errorf("jadic: aux_dictionary.tsv 第 %d 行：cost_offset 不合法", ln)
		}
		out = append(out, mozcAux{
			key: fields[0], value: fields[1], baseKey: fields[2], baseValue: fields[3], costOffset: offset,
		})
	}
	return out, sc.Err()
}

func loadMozcFilters(path string) ([]*regexp.Regexp, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []*regexp.Regexp
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("jadic: dictionary_filter.tsv 第 %d 行：需要 key 與 value", ln)
		}
		// 與 Mozc gen_filtered_dictionary.py 相同的全行比對。
		re, err := regexp.Compile(`^` + key + `\t\d+\t\d+\t\d+\t` + value + `(\t.*)?$`)
		if err != nil {
			return nil, fmt.Errorf("jadic: dictionary_filter.tsv 第 %d 行：%w", ln, err)
		}
		out = append(out, re)
	}
	return out, sc.Err()
}

func matchesMozcFilter(line string, filters []*regexp.Regexp) bool {
	for _, re := range filters {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// writeMozcConnections 讀取 connection_single_column.txt，把 2672×2672 的
// context ID 矩陣依 POS 對聚合成引擎目前使用的 POS 連接成本。
func writeMozcConnections(outPath, contextPath, srcPath string, posByID map[int]MozcPOS, scale float64, mode string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	if !sc.Scan() {
		return fmt.Errorf("jadic: %s 是空檔", srcPath)
	}
	n, err := strconv.Atoi(strings.TrimSpace(sc.Text()))
	if err != nil || n <= 0 {
		return fmt.Errorf("jadic: %s 的矩陣大小 %q 不合法", srcPath, sc.Text())
	}
	if len(posByID) != n {
		return fmt.Errorf("jadic: id.def 有 %d 個 ID，連接矩陣是 %d×%d", len(posByID), n, n)
	}
	order := connectionPOSOrder()
	posIndex := make(map[jaimport.POS]int, len(order))
	for i, p := range order {
		posIndex[p] = i
	}
	// 同一個 id 0 在左側代表 BOS、在右側代表 EOS，因此左右索引分開。
	leftIndex := make([]int, n)
	rightIndex := make([]int, n)
	for id, info := range posByID {
		if id < 0 || id >= n {
			return fmt.Errorf("jadic: id.def 的 ID %d 超出連接矩陣範圍", id)
		}
		lp := info.POS
		rp := lp
		if id == 0 {
			lp = jaimport.POSBOS
			rp = jaimport.POSEOS
		}
		li, ok := posIndex[lp]
		if !ok {
			return fmt.Errorf("jadic: 詞性 %q 不在連接矩陣詞性表", lp)
		}
		ri, ok := posIndex[rp]
		if !ok {
			return fmt.Errorf("jadic: 詞性 %q 不在連接矩陣詞性表", rp)
		}
		leftIndex[id] = li
		rightIndex[id] = ri
	}
	ctxMatrix, err := jalexicon.NewContextMatrix(n, scale)
	if err != nil {
		return err
	}
	buckets := make([][]uint16, len(order)*len(order))
	for rid := 0; rid < n; rid++ {
		lp := leftIndex[rid]
		for lid := 0; lid < n; lid++ {
			if !sc.Scan() {
				return fmt.Errorf("jadic: %s 的連接成本不足 %d×%d 筆", srcPath, n, n)
			}
			text := strings.TrimSpace(sc.Text())
			cost, err := strconv.Atoi(text)
			if err != nil || cost < 0 || cost > 65535 {
				return fmt.Errorf("jadic: %s 第 %d 筆連接成本 %q 不合法", srcPath, rid*n+lid+2, text)
			}
			idx := lp*len(order) + rightIndex[lid]
			buckets[idx] = append(buckets[idx], uint16(cost))
			ctxMatrix.Set(uint16(rid), uint16(lid), uint16(cost))
		}
	}
	if sc.Scan() {
		return fmt.Errorf("jadic: %s 的連接成本多於 %d×%d 筆", srcPath, n, n)
	}
	if err := sc.Err(); err != nil {
		return err
	}

	f, err = os.Create(outPath)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<16)
	if _, err := fmt.Fprintf(bw, "# 由 Mozc connection_single_column.txt 聚合；左詞性\t右詞性\t成本（%s，scale %s）\n", mode, formatCost(scale)); err != nil {
		f.Close()
		return err
	}
	posOrder := connectionPOSOrder()
	for left, lp := range posOrder {
		for right, rp := range posOrder {
			bucket := buckets[left*len(order)+right]
			cost, ok := aggregateMozcCost(bucket, mode)
			if !ok {
				continue
			}
			if _, err := fmt.Fprintf(bw, "%s\t%s\t%s\n", lp, rp, formatCost(float64(cost)*scale)); err != nil {
				f.Close()
				return err
			}
		}
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cf, err := os.Create(contextPath)
	if err != nil {
		return err
	}
	if err := jalexicon.EncodeContextMatrix(cf, ctxMatrix); err != nil {
		cf.Close()
		return err
	}
	if err := cf.Sync(); err != nil {
		cf.Close()
		return err
	}
	return cf.Close()
}

// connectionPOSOrder 是連接矩陣的固定列舉順序。固定順序讓同一份輸入產生相同位元組。
func connectionPOSOrder() []jaimport.POS {
	return []jaimport.POS{
		jaimport.POSBOS, jaimport.POSEOS,
		jaimport.POSOther, jaimport.POSNoun, jaimport.POSVerb, jaimport.POSAdjective,
		jaimport.POSAdjectivalNoun, jaimport.POSAdverb, jaimport.POSParticle,
		jaimport.POSAuxiliary, jaimport.POSConjunction, jaimport.POSPrefix,
		jaimport.POSSuffix, jaimport.POSDeterminer, jaimport.POSInterjection,
		jaimport.POSNumber, jaimport.POSSymbol,
	}
}

func aggregateMozcCost(costs []uint16, mode string) (int, bool) {
	if len(costs) == 0 {
		return 0, false
	}
	switch mode {
	case "min":
		min := costs[0]
		for _, c := range costs[1:] {
			if c < min {
				min = c
			}
		}
		return int(min), true
	case "max":
		max := costs[0]
		for _, c := range costs[1:] {
			if c > max {
				max = c
			}
		}
		return int(max), true
	case "mean":
		var sum uint64
		for _, c := range costs {
			sum += uint64(c)
		}
		return int(math.Round(float64(sum) / float64(len(costs)))), true
	}
	// median：排序後取中位數；偶數筆取兩中位數平均（四捨五入）。
	sort.Slice(costs, func(i, j int) bool { return costs[i] < costs[j] })
	mid := len(costs) / 2
	if len(costs)%2 == 1 {
		return int(costs[mid]), true
	}
	return int(math.Round((float64(costs[mid-1]) + float64(costs[mid])) / 2)), true
}

func describeMozcSource(path string) (MozcSourceFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return MozcSourceFile{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return MozcSourceFile{}, err
	}
	return MozcSourceFile{
		Name:   filepath.Base(path),
		Bytes:  n,
		SHA256: hex.EncodeToString(h.Sum(nil)),
	}, nil
}
