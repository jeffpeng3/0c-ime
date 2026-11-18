// Package lm 是日文轉換執行期的詞語言模型：詞面 unigram 與詞面 bigram 成本。
//
// 成本是負對數機率，越低越好。模型不含讀音、詞性與 backoff；查詢鍵是詞面字串，
// 與 jaimport.LM 編譯出的 lm.bin 格式相同。這個套件只解碼與查詢，讀檔由宿主
// 負責，core 不碰檔案系統。
package lm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const magic = "0CJLM001"

// Model 是解碼後的語言模型。未登錄的詞面成本為 0，代表沒有 LM 證據，
// 不額外懲罰；沒有 bigram 時只回傳 unigram。
type Model struct {
	unigram map[string]float64
	bigram  map[[2]string]float64
	left    map[string]struct{}
	encoded int
}

// Load 從 r 讀取 lm.bin。格式錯誤回傳錯誤，不嘗試修補。
func Load(r io.Reader) (*Model, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

// Decode 解碼 lm.bin。格式錯誤回傳錯誤，不嘗試修補。
func Decode(data []byte) (*Model, error) {
	r := bytes.NewReader(data)
	head := make([]byte, len(magic))
	if n, err := io.ReadFull(r, head); err != nil || n != len(head) || string(head) != magic {
		return nil, fmt.Errorf("不是 0c 日文語言模型檔")
	}
	n, err := binary.ReadUvarint(r)
	if err != nil || n > uint64(r.Len())/8 {
		return nil, fmt.Errorf("unigram 數量不合法")
	}
	m := &Model{
		unigram: make(map[string]float64, int(n)),
		bigram:  map[[2]string]float64{},
		left:    map[string]struct{}{},
	}
	for i := uint64(0); i < n; i++ {
		k, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("unigram 詞面：%w", err)
		}
		v, err := readCost(r)
		if err != nil {
			return nil, fmt.Errorf("unigram %q 成本：%w", k, err)
		}
		m.unigram[k] = v
	}
	bn, err := binary.ReadUvarint(r)
	if err != nil || bn > uint64(r.Len())/10 {
		return nil, fmt.Errorf("bigram 數量不合法")
	}
	m.bigram = make(map[[2]string]float64, int(bn))
	for i := uint64(0); i < bn; i++ {
		a, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("bigram 左詞：%w", err)
		}
		b, err := readString(r)
		if err != nil {
			return nil, fmt.Errorf("bigram 右詞：%w", err)
		}
		v, err := readCost(r)
		if err != nil {
			return nil, fmt.Errorf("bigram %q→%q 成本：%w", a, b, err)
		}
		m.bigram[[2]string{a, b}] = v
		m.left[a] = struct{}{}
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("語言模型檔尾端有 %d 位元組未使用", r.Len())
	}
	m.encoded = len(data)
	return m, nil
}

// Len 回傳 unigram 詞數。
func (m *Model) Len() int {
	if m == nil {
		return 0
	}
	return len(m.unigram)
}

// BigramLen 回傳 bigram 筆數。
func (m *Model) BigramLen() int {
	if m == nil {
		return 0
	}
	return len(m.bigram)
}

// EncodedSize 是 lm.bin 的位元組數。
func (m *Model) EncodedSize() int {
	if m == nil {
		return 0
	}
	return m.encoded
}

// RuntimeBytesEstimate 是解碼後常駐記憶體的保守估計，供載入器與報告使用。
// map 的 bucket、字串標頭與配置器捨入都算在內；實際值仍以量測為準。
func (m *Model) RuntimeBytesEstimate() int {
	if m == nil {
		return 0
	}
	return m.encoded*2 + len(m.unigram)*64 + len(m.bigram)*96 + len(m.left)*32
}

// UnigramCost 回傳詞面 w 的 unigram 成本；未登錄回傳 0。
func (m *Model) UnigramCost(w string) float64 {
	if m == nil {
		return 0
	}
	return m.unigram[w]
}

// BigramCost 回傳 left→w 的 bigram 成本；未登錄回傳 0。
func (m *Model) BigramCost(left, w string) float64 {
	if m == nil || left == "" {
		return 0
	}
	return m.bigram[[2]string{left, w}]
}

// Cost 回傳 left 之後接 w 的 LM 成本，等於 unigram + bigram。
// left 為空表示句首，只算 unigram。
func (m *Model) Cost(left, w string) float64 {
	if m == nil {
		return 0
	}
	return m.UnigramCost(w) + m.BigramCost(left, w)
}

// HasLeft 回報 left 是否為任何 bigram 的左詞。Viterbi 用它決定是否需要把
// 前一個詞面放進狀態；不是左詞的歷史在 bigram 上完全等價。
func (m *Model) HasLeft(left string) bool {
	if m == nil || left == "" {
		return false
	}
	_, ok := m.left[left]
	return ok
}

func readString(r *bytes.Reader) (string, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return "", err
	}
	if n > uint64(r.Len()) {
		return "", fmt.Errorf("字串長度 %d 超過剩餘位元組", n)
	}
	b := make([]byte, int(n))
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

// readCost 讀取成本。unigram 可能來自負的詞成本，因此允許有限負值；
// NaN 與 Inf 會破壞排序，一律拒絕。
func readCost(r *bytes.Reader) (float64, error) {
	var tmp [8]byte
	if _, err := io.ReadFull(r, tmp[:]); err != nil {
		return 0, err
	}
	v := math.Float64frombits(binary.LittleEndian.Uint64(tmp[:]))
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("成本 %v 不合法", v)
	}
	return v, nil
}
