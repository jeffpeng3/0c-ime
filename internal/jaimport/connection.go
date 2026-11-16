package jaimport

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Connection 是一筆詞性連接成本：Left 接 Right 的成本，越低越好。
type Connection struct {
	Left, Right POS
	Cost        float64
}

// DefaultConnectionCost 是沒有指定詞性對時的保底成本，與 core/ja/lexicon 相同。
const DefaultConnectionCost = 1.0

// ParseConnections 讀取連接成本 TSV：左詞性、右詞性、成本。
// 前詞或後詞可寫 <s>、</s> 表示句首、句尾。空白行與 # 起始行略過。
func ParseConnections(r io.Reader) ([]Connection, error) {
	var out []Connection
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			return nil, fmt.Errorf("第 %d 行：連接成本需要左詞性、右詞性、成本三欄", ln)
		}
		left, err := connectionPOS(f[0])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", ln, err)
		}
		right, err := connectionPOS(f[1])
		if err != nil {
			return nil, fmt.Errorf("第 %d 行：%w", ln, err)
		}
		cost, err := strconv.ParseFloat(strings.TrimSpace(f[2]), 64)
		if err != nil || math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
			return nil, fmt.Errorf("第 %d 行：成本 %q 不合法", ln, f[2])
		}
		out = append(out, Connection{Left: left, Right: right, Cost: cost})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return NormalizeConnections(out), nil
}

// ParseConnectionsFile 讀取連接成本檔。
func ParseConnectionsFile(path string) ([]Connection, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseConnections(f)
}

func connectionPOS(s string) (POS, error) {
	s = strings.TrimSpace(s)
	switch s {
	case string(POSBOS):
		return POSBOS, nil
	case string(POSEOS):
		return POSEOS, nil
	}
	return ParsePOS(s)
}

// NormalizeConnections 排序並除重；同一詞性對重複時後者覆蓋前者。
func NormalizeConnections(in []Connection) []Connection {
	byKey := map[[2]POS]float64{}
	for _, c := range in {
		byKey[[2]POS{c.Left, c.Right}] = c.Cost
	}
	out := make([]Connection, 0, len(byKey))
	for k, v := range byKey {
		out = append(out, Connection{Left: k[0], Right: k[1], Cost: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Left != out[j].Left {
			return out[i].Left < out[j].Left
		}
		return out[i].Right < out[j].Right
	})
	return out
}

// Matrix 是編譯後的連接成本表。未指定的詞性對使用 fallback。
type Matrix struct {
	costs    map[[2]POS]float64
	fallback float64
}

// NewMatrix 建立空連接成本表。
func NewMatrix() *Matrix {
	return &Matrix{costs: map[[2]POS]float64{}, fallback: DefaultConnectionCost}
}

// BuildMatrix 由連接成本清單建立矩陣。
func BuildMatrix(conns []Connection) *Matrix {
	m := NewMatrix()
	for _, c := range conns {
		m.Set(c.Left, c.Right, c.Cost)
	}
	return m
}

// Set 設定 left 接 right 的成本。
func (m *Matrix) Set(left, right POS, cost float64) {
	m.costs[[2]POS{left, right}] = cost
}

// Cost 回傳 left 接 right 的成本。
func (m *Matrix) Cost(left, right POS) float64 {
	if c, ok := m.costs[[2]POS{left, right}]; ok {
		return c
	}
	return m.fallback
}

// SetFallback 設定未指定詞性對的保底成本。
func (m *Matrix) SetFallback(cost float64) { m.fallback = cost }

// Connections 回傳排序後的連接成本，供編碼。
func (m *Matrix) Connections() []Connection {
	out := make([]Connection, 0, len(m.costs))
	for k, v := range m.costs {
		out = append(out, Connection{Left: k[0], Right: k[1], Cost: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Left != out[j].Left {
			return out[i].Left < out[j].Left
		}
		return out[i].Right < out[j].Right
	})
	return out
}

// Len 回傳已設定的詞性對數量。
func (m *Matrix) Len() int { return len(m.costs) }
