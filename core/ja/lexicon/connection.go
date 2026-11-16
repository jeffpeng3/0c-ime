package lexicon

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// DefaultConnectionCost 是沒有指定連接成本時的保底值。
//
// 正的成本代表接得普通；低於 1 表示常見搭配（複合名詞、助詞接續），
// 高於 1 表示不自然。數值本身沒有絕對意義，只拿來和詞成本相加比較。
const DefaultConnectionCost = 1.0

// Connection 回傳 left 接 right 的連接成本。越低越好。
type Connection interface {
	Cost(left, right POS) float64
}

// Matrix 是以詞性對為索引的連接成本表。
type Matrix struct {
	costs    map[[2]POS]float64
	fallback float64
}

// NewMatrix 建立連接成本表，未指定的詞性對使用 DefaultConnectionCost。
func NewMatrix() *Matrix {
	return &Matrix{costs: map[[2]POS]float64{}, fallback: DefaultConnectionCost}
}

// SetFallback 設定未指定詞性對的保底成本。
func (m *Matrix) SetFallback(cost float64) { m.fallback = cost }

// Set 設定 left 接 right 的成本。
func (m *Matrix) Set(left, right POS, cost float64) {
	m.costs[[2]POS{left, right}] = cost
}

// Cost 回傳 left 接 right 的連接成本。
func (m *Matrix) Cost(left, right POS) float64 {
	if c, ok := m.costs[[2]POS{left, right}]; ok {
		return c
	}
	return m.fallback
}

// LoadConnections 讀取連接成本 TSV：左詞性、右詞性、成本。
// 以 # 起始的行與空行略過。第一筆前詞或後詞可寫 <s> 與 </s> 表示句首、句尾。
func LoadConnections(r io.Reader) (*Matrix, error) {
	m := NewMatrix()
	sc := bufio.NewScanner(r)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			return nil, fmt.Errorf("第 %d 行：需要左詞性、右詞性、成本三欄", ln)
		}
		left, err := connectionPOS(f[0])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", ln, err)
		}
		right, err := connectionPOS(f[1])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", ln, err)
		}
		cost, err := strconv.ParseFloat(f[2], 64)
		if err != nil || cost < 0 {
			return nil, fmt.Errorf("第 %d 行：成本 %q 不合法", ln, f[2])
		}
		m.Set(left, right, cost)
	}
	return m, sc.Err()
}

func connectionPOS(s string) (POS, error) {
	switch s {
	case string(POSBOS):
		return POSBOS, nil
	case string(POSEOS):
		return POSEOS, nil
	}
	return ParsePOS(s)
}
