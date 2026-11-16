package jaimport

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// MozcOptions 控制 Mozc 詞典匯入。
type MozcOptions struct {
	// Origin 寫進詞條來源，必須是 data/sources/ledger.tsv 的 ID。
	Origin string
	// POSByID 是 id.def 的左連線 ID → 詞性名稱。沒有 comment 欄時用來補詞性。
	POSByID map[int]string
	// ConjByID 是 id.def 的左連線 ID → 活用型名稱，可為 nil。
	ConjByID map[int]string
	// CostScale 把 Mozc 的整數詞成本換算成 0c 的相對成本。0 表示 1/1000。
	// Mozc 的連接成本也在同一個尺度，換算後與 curated 詞條的 1～3 可直接比較。
	CostScale float64
}

// DefaultMozcOptions 回傳以 ja-mozc 為來源的匯入選項。
func DefaultMozcOptions() MozcOptions {
	return MozcOptions{Origin: "ja-mozc", CostScale: 1.0 / 1000.0}
}

// LoadIDDef 讀取 Mozc 的 id.def：`ID<TAB>詞性,...`。回傳 ID → 詞性名稱。
func LoadIDDef(r io.Reader) (map[int]string, error) {
	out := map[int]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idText, name, ok := strings.Cut(line, "\t")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("id.def 第 %d 行：需要 ID 與詞性名稱", ln)
		}
		id, err := strconv.Atoi(strings.TrimSpace(idText))
		if err != nil || id < 0 {
			return nil, fmt.Errorf("id.def 第 %d 行：ID %q 不合法", ln, idText)
		}
		out[id] = strings.TrimSpace(name)
	}
	return out, sc.Err()
}

// LoadMozcIDDef 讀取 Mozc 的 id.def 檔。
func LoadMozcIDDef(path string) (map[int]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadIDDef(f)
}

// ParseMozc 讀取 Mozc dictionary_oss 的 TSV：
//
//	讀音 <TAB> 左ID <TAB> 右ID <TAB> 成本 <TAB> 詞面 <TAB> MeCab 詞性 CSV
//
// 詞性 CSV 的前六欄是 POS1～POS4、活用型、活用形。沒有 CSV 時用 POSByID。
// 空白行與 # 起始行略過。任何格式錯誤都回傳行號，避免默默漏詞。
func ParseMozc(r io.Reader, opt MozcOptions) ([]Entry, error) {
	if opt.Origin == "" {
		return nil, fmt.Errorf("Mozc 匯入缺少來源 ID")
	}
	scale := opt.CostScale
	if scale == 0 {
		scale = 1.0 / 1000.0
	}
	var out []Entry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			return nil, fmt.Errorf("第 %d 行：Mozc 詞條至少需要讀音、左ID、右ID、成本、詞面五欄", ln)
		}
		reading, err := NormalizeReading(f[0])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：讀音 %q：%w", ln, f[0], err)
		}
		surface := f[4]
		if strings.TrimSpace(surface) == "" {
			return nil, fmt.Errorf("第 %d 行：詞面不可為空", ln)
		}
		rawCost, err := strconv.ParseFloat(strings.TrimSpace(f[3]), 64)
		if err != nil || rawCost < 0 {
			return nil, fmt.Errorf("第 %d 行：詞成本 %q 不合法", ln, f[3])
		}
		posName := ""
		conjName := ""
		if len(f) > 5 {
			csv := splitMeCabCSV(f[5])
			if len(csv) > 0 {
				posName = csv[0]
			}
			if len(csv) > 4 {
				conjName = csv[4]
			}
		}
		if posName == "" && opt.POSByID != nil {
			if left, ok := atoiField(f[1]); ok {
				posName = opt.POSByID[left]
				if conjName == "" {
					if opt.ConjByID != nil {
						conjName = opt.ConjByID[left]
					} else if csv := strings.Split(posName, ","); len(csv) > 4 {
						conjName = csv[4]
					}
				}
			}
		}
		pos := POSOther
		if posName != "" {
			pos = MapPOS(posName)
		}
		conj := MapConjugation(conjName)
		out = append(out, MakeEntry(surface, reading, rawCost*scale, pos, []string{opt.Origin}, conj))
	}
	return Merge(out), sc.Err()
}

// ParseMozcFile 讀取一份 Mozc 詞典檔。
func ParseMozcFile(path string, opt MozcOptions) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseMozc(f, opt)
}

func splitMeCabCSV(s string) []string {
	if !strings.Contains(s, ",") {
		return []string{s}
	}
	return strings.Split(s, ",")
}

func atoiField(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil && n >= 0
}
