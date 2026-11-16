package rewrite

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// Symbol 是記号或顔文字辭典的一筆。
type Symbol struct {
	// Reading 是查詢用的読み，例如「きごう」「かおもじ」。
	Reading string
	// Surface 是候選詞面。
	Surface string
	// Annotation 是候選視窗顯示的註解，例如「矢印」「顔文字」。
	Annotation string
	// Category 是辭典分類，只用於整理資料，不進入候選。
	Category string
}

// Symbols 是依読み查詢的記号與顔文字辭典。
//
// 建好後不再修改，可以安全共用。零值等於空辭典。
type Symbols struct {
	byReading map[string][]Symbol
	count     int
}

// NewSymbols 建立辭典。空白読み或空白詞面的項目會被忽略。
func NewSymbols(entries []Symbol) *Symbols {
	s := &Symbols{byReading: make(map[string][]Symbol)}
	for _, e := range entries {
		s.add(e)
	}
	return s
}

func (s *Symbols) add(e Symbol) {
	if s.byReading == nil {
		s.byReading = make(map[string][]Symbol)
	}
	e.Reading = normalizeReading(e.Reading)
	if e.Reading == "" || e.Surface == "" {
		return
	}
	s.byReading[e.Reading] = append(s.byReading[e.Reading], e)
	s.count++
}

// Len 回傳辭典項目數。
func (s *Symbols) Len() int {
	if s == nil {
		return 0
	}
	return s.count
}

// Lookup 回傳読み對應的項目；読み會先轉成平假名並移除空白。
func (s *Symbols) Lookup(reading string) []Symbol {
	if s == nil {
		return nil
	}
	return s.byReading[normalizeReading(reading)]
}

// Add 追加項目；方便外殼載入使用者自訂辭典。
func (s *Symbols) Add(entries ...Symbol) {
	for _, e := range entries {
		s.add(e)
	}
}

func normalizeReading(reading string) string {
	reading = strings.ReplaceAll(reading, " ", "")
	reading = strings.ReplaceAll(reading, "\t", "")
	return ToHiragana(reading)
}

// LoadSymbols 從 TSV 讀取辭典。
//
// 每行四欄：読み、詞面、註解、分類，以 TAB 分隔；空行與 # 開頭的行忽略。
// 同一読み可以有多筆。回傳的項目保持檔案順序。
func LoadSymbols(r io.Reader) ([]Symbol, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var entries []Symbol
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("symbols: line %d: got %d fields, want 4", lineNo, len(fields))
		}
		e := Symbol{
			Reading:    strings.TrimSpace(fields[0]),
			Surface:    fields[1],
			Annotation: strings.TrimSpace(fields[2]),
			Category:   strings.TrimSpace(fields[3]),
		}
		if e.Reading == "" || e.Surface == "" {
			return nil, fmt.Errorf("symbols: line %d: empty reading or surface", lineNo)
		}
		if e.Annotation == "" {
			return nil, fmt.Errorf("symbols: line %d: empty annotation", lineNo)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// LoadSymbolsFile 讀取 fsys 裡的單一 TSV 檔。
func LoadSymbolsFile(fsys fs.FS, name string) ([]Symbol, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := LoadSymbols(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return entries, nil
}

// LoadSymbolsFS 讀取目錄下所有 .tsv 檔並合併成一份辭典。
//
// fs.Glob 依檔名排序，因此同一読み的候選順序可重現。core 不碰檔案系統，
// 外殼或測試自行提供 fs.FS，例如 os.DirFS 或打包後的資料 fs。
func LoadSymbolsFS(fsys fs.FS, dir string) (*Symbols, error) {
	names, err := fs.Glob(fsys, path.Join(dir, "*.tsv"))
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("symbols: no .tsv files in %s", dir)
	}
	var entries []Symbol
	for _, name := range names {
		part, err := LoadSymbolsFile(fsys, name)
		if err != nil {
			return nil, err
		}
		entries = append(entries, part...)
	}
	return NewSymbols(entries), nil
}
