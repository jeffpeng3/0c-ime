package lexicon

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
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

// ContextConnection 是選填的 context ID 連接成本介面。詞條帶有 Mozc 的
// left/right ID 時，解碼器優先使用它；否則退回 POS 連接成本。
type ContextConnection interface {
	ContextCost(leftRightID, rightLeftID uint16) float64
}

// ContextMatrixMagic 是 context 連接矩陣二進位格式的檔頭。
const ContextMatrixMagic = "0CJCON01"

// ContextMatrix 是以來源 context ID 對為索引的連接成本表。
//
// 成本以 uint16 原值儲存，ContextCost 再乘上 Scale。Mozc 的原始成本是整數，
// scale 與詞成本相同（預設 0.001），因此 context 與 POS 兩種路徑共用同一尺度。
type ContextMatrix struct {
	size  int
	scale float64
	costs []uint16
}

// NewContextMatrix 建立 size×size 的空矩陣。
func NewContextMatrix(size int, scale float64) (*ContextMatrix, error) {
	if size <= 0 || size > 65536 {
		return nil, fmt.Errorf("context 矩陣大小 %d 不合法", size)
	}
	return &ContextMatrix{size: size, scale: scale, costs: make([]uint16, size*size)}, nil
}

// Size 回傳矩陣邊長。
func (m *ContextMatrix) Size() int {
	if m == nil {
		return 0
	}
	return m.size
}

// Scale 回傳成本換算係數。
func (m *ContextMatrix) Scale() float64 {
	if m == nil {
		return 0
	}
	return m.scale
}

// Set 設定 left 接 right 的原始整數成本。
func (m *ContextMatrix) Set(leftRightID, rightLeftID uint16, cost uint16) {
	if m == nil || int(leftRightID) >= m.size || int(rightLeftID) >= m.size {
		return
	}
	m.costs[int(leftRightID)*m.size+int(rightLeftID)] = cost
}

// ContextCost 回傳 left 接 right 的換算成本。越界時回傳 POS 矩陣的保底值。
func (m *ContextMatrix) ContextCost(leftRightID, rightLeftID uint16) float64 {
	if m == nil || int(leftRightID) >= m.size || int(rightLeftID) >= m.size {
		return DefaultConnectionCost
	}
	return float64(m.costs[int(leftRightID)*m.size+int(rightLeftID)]) * m.scale
}

// EncodeContextMatrix 把矩陣寫成可攜的二進位格式：
// magic（8 bytes）、uint32 邊長、float64 scale、size×size 個 little-endian uint16。
func EncodeContextMatrix(w io.Writer, m *ContextMatrix) error {
	if m == nil {
		return fmt.Errorf("沒有 context 矩陣")
	}
	if len(m.costs) != m.size*m.size {
		return fmt.Errorf("context 矩陣資料長度 %d 與 %d×%d 不符", len(m.costs), m.size, m.size)
	}
	bw := bufio.NewWriterSize(w, 1<<20)
	if _, err := bw.WriteString(ContextMatrixMagic); err != nil {
		return err
	}
	var header [12]byte
	binary.LittleEndian.PutUint32(header[0:4], uint32(m.size))
	binary.LittleEndian.PutUint64(header[4:12], math.Float64bits(m.scale))
	if _, err := bw.Write(header[:]); err != nil {
		return err
	}
	var buf [4096]byte
	for i := 0; i < len(m.costs); {
		n := 0
		for n < len(buf)/2 && i < len(m.costs) {
			binary.LittleEndian.PutUint16(buf[n*2:], m.costs[i])
			n++
			i++
		}
		if _, err := bw.Write(buf[:n*2]); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// LoadContextMatrix 讀取 EncodeContextMatrix 的格式。長度不足或檔頭錯誤都回報錯誤。
func LoadContextMatrix(r io.Reader) (*ContextMatrix, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	magic := make([]byte, len(ContextMatrixMagic))
	if _, err := io.ReadFull(br, magic); err != nil {
		return nil, err
	}
	if string(magic) != ContextMatrixMagic {
		return nil, fmt.Errorf("context 矩陣 magic %q 不符", magic)
	}
	var header [12]byte
	if _, err := io.ReadFull(br, header[:]); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint32(header[0:4]))
	scale := math.Float64frombits(binary.LittleEndian.Uint64(header[4:12]))
	if size <= 0 || size > 65536 {
		return nil, fmt.Errorf("context 矩陣大小 %d 不合法", size)
	}
	if math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0 {
		return nil, fmt.Errorf("context 矩陣 scale %v 不合法", scale)
	}
	m := &ContextMatrix{size: size, scale: scale, costs: make([]uint16, size*size)}
	var buf [4096]byte
	for i := 0; i < len(m.costs); {
		n := len(buf) / 2
		if remain := len(m.costs) - i; remain < n {
			n = remain
		}
		if _, err := io.ReadFull(br, buf[:n*2]); err != nil {
			return nil, err
		}
		for j := 0; j < n; j++ {
			m.costs[i+j] = binary.LittleEndian.Uint16(buf[j*2:])
		}
		i += n
	}
	return m, nil
}

// Connections 同時提供 POS 與 context 兩種連接成本。
// Decoder 使用 context；文節合併與只認 POS 的呼叫端仍使用 POS 矩陣。
type Connections struct {
	POS     *Matrix
	Context *ContextMatrix
}

// Cost 回傳 POS 對的連接成本。
func (c *Connections) Cost(left, right POS) float64 {
	if c == nil || c.POS == nil {
		return DefaultConnectionCost
	}
	return c.POS.Cost(left, right)
}

// ContextCost 回傳 context ID 對的連接成本；沒有 context 矩陣時退回 POS 保底值。
func (c *Connections) ContextCost(leftRightID, rightLeftID uint16) float64 {
	if c == nil || c.Context == nil {
		return DefaultConnectionCost
	}
	return c.Context.ContextCost(leftRightID, rightLeftID)
}
