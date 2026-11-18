package lm_test

import (
	"math"
	"testing"

	jalm "github.com/0c-biggo/0c-ime/core/ja/lm"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

func encode(t *testing.T, uni map[string]float64, bi map[[2]string]float64) []byte {
	t.Helper()
	return jaimport.NewLM(uni, bi).Encode()
}

func TestDecodeMatchesCompilerFormat(t *testing.T) {
	data := encode(t,
		map[string]float64{"私": 1.25, "は": 0.5},
		map[[2]string]float64{{"私", "は"}: 2.5},
	)
	m, err := jalm.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Len() != 2 || m.BigramLen() != 1 || m.EncodedSize() != len(data) {
		t.Fatalf("Len/BigramLen/EncodedSize = %d/%d/%d，want 2/1/%d",
			m.Len(), m.BigramLen(), m.EncodedSize(), len(data))
	}
	if got := m.UnigramCost("私"); got != 1.25 {
		t.Errorf("UnigramCost(私) = %v", got)
	}
	if got := m.UnigramCost("知らない"); got != 0 {
		t.Errorf("未登錄 unigram = %v，want 0", got)
	}
	if got := m.BigramCost("私", "は"); got != 2.5 {
		t.Errorf("BigramCost = %v", got)
	}
	if got := m.BigramCost("は", "私"); got != 0 {
		t.Errorf("反向 bigram = %v，want 0", got)
	}
	if !m.HasLeft("私") || m.HasLeft("は") {
		t.Errorf("HasLeft = %v/%v，want true/false", m.HasLeft("私"), m.HasLeft("は"))
	}
	if got := m.Cost("私", "は"); got != 3 {
		t.Errorf("Cost = %v，want 3", got)
	}
	if got := m.Cost("", "は"); got != 0.5 {
		t.Errorf("句首 Cost = %v，want 0.5", got)
	}
}

func TestDecodeEmptyModel(t *testing.T) {
	m, err := jalm.Decode(encode(t, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if m.Len() != 0 || m.BigramLen() != 0 {
		t.Fatalf("空模型 = %d/%d", m.Len(), m.BigramLen())
	}
	if got := m.Cost("左", "右"); got != 0 {
		t.Fatalf("空模型 Cost = %v", got)
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	good := encode(t, map[string]float64{"語": 1}, map[[2]string]float64{{"語", "語"}: 2})
	cases := map[string][]byte{
		"空檔":    nil,
		"magic": []byte("NOPE0000"),
		"截斷":    good[:len(good)-1],
		"尾端":    append(append([]byte(nil), good...), 0),
	}
	for name, data := range cases {
		if _, err := jalm.Decode(data); err == nil {
			t.Errorf("%s：預期錯誤，卻解碼成功", name)
		}
	}
}

func TestNegativeCostAccepted(t *testing.T) {
	data := encode(t, map[string]float64{"x": -1.5}, nil)
	m, err := jalm.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.UnigramCost("x"); got != -1.5 {
		t.Fatalf("UnigramCost = %v，want -1.5", got)
	}
}

func TestNaNRejected(t *testing.T) {
	m := jaimport.NewLM(map[string]float64{"x": 1}, nil)
	data := m.Encode()
	// unigram 成本固定 8 bytes；改成 NaN。
	nan := math.Float64bits(math.NaN())
	for i := 0; i < 8; i++ {
		data[len(data)-8+i] = byte(nan >> (8 * i))
	}
	if _, err := jalm.Decode(data); err == nil {
		t.Fatal("NaN 成本應被拒絕")
	}
}

func TestNilModel(t *testing.T) {
	var m *jalm.Model
	if m.Len() != 0 || m.BigramLen() != 0 || m.EncodedSize() != 0 || m.Cost("a", "b") != 0 {
		t.Fatal("nil 模型必須是零值")
	}
	if m.HasLeft("a") {
		t.Fatal("nil 模型不應有 left context")
	}
}
