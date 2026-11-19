package lexicon

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

func TestContextMatrixRoundTrip(t *testing.T) {
	m, err := NewContextMatrix(3, 0.001)
	if err != nil {
		t.Fatal(err)
	}
	m.Set(0, 0, 10)
	m.Set(0, 1, 20)
	m.Set(1, 2, 30)
	m.Set(2, 0, 40)

	var buf bytes.Buffer
	if err := EncodeContextMatrix(&buf, m); err != nil {
		t.Fatal(err)
	}
	got, err := LoadContextMatrix(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Size() != 3 || got.Scale() != 0.001 {
		t.Fatalf("size/scale = %d/%v", got.Size(), got.Scale())
	}
	for _, tc := range []struct {
		left, right uint16
		want        float64
	}{
		{0, 0, 0.010},
		{0, 1, 0.020},
		{1, 2, 0.030},
		{2, 0, 0.040},
	} {
		if diff := math.Abs(got.ContextCost(tc.left, tc.right) - tc.want); diff > 1e-12 {
			t.Errorf("ContextCost(%d,%d) = %v, want %v", tc.left, tc.right, got.ContextCost(tc.left, tc.right), tc.want)
		}
	}
	if got.ContextCost(9, 9) != DefaultConnectionCost {
		t.Errorf("越界成本 = %v, want %v", got.ContextCost(9, 9), DefaultConnectionCost)
	}
}

func TestContextMatrixRejectsBadInput(t *testing.T) {
	for _, body := range [][]byte{
		nil,
		[]byte("not a context matrix"),
		append([]byte(ContextMatrixMagic), 0, 0, 0),
	} {
		if _, err := LoadContextMatrix(bytes.NewReader(body)); err == nil {
			t.Errorf("LoadContextMatrix(%q) 應失敗", body)
		}
	}
}

func TestConnectionsFallsBackWithoutContext(t *testing.T) {
	pos := NewMatrix()
	pos.Set(POSNoun, POSParticle, 0.3)
	conn := &Connections{POS: pos}
	if got := conn.Cost(POSNoun, POSParticle); got != 0.3 {
		t.Fatalf("POS 成本 = %v", got)
	}
	if got := conn.ContextCost(1, 2); got != DefaultConnectionCost {
		t.Fatalf("沒有 context 矩陣時 = %v, want %v", got, DefaultConnectionCost)
	}
}

func TestLoadParsesOptionalContextIDs(t *testing.T) {
	m, err := Load(strings.NewReader("明日\tあ し た\t1.0\tnoun\tsrc\t-\t12\t34\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := m.Lookup("明日")
	if len(got) != 1 {
		t.Fatalf("Lookup = %#v", got)
	}
	if !got[0].HasContext || got[0].LeftID != 12 || got[0].RightID != 34 {
		t.Fatalf("context = %#v", got[0])
	}

	if _, err := Load(strings.NewReader("明日\tあ し た\t1.0\tnoun\tsrc\t-\t12\n")); err != nil {
		t.Fatalf("只有七欄時應忽略 context：%v", err)
	}
	for _, bad := range []string{
		"明日\tあ し た\t1.0\tnoun\tsrc\t-\tx\t34\n",
		"明日\tあ し た\t1.0\tnoun\tsrc\t-\t12\t-1\n",
		"明日\tあ し た\t1.0\tnoun\tsrc\t-\t70000\t34\n",
	} {
		if _, err := Load(strings.NewReader(bad)); err == nil {
			t.Errorf("Load(%q) 應失敗", bad)
		}
	}
}
