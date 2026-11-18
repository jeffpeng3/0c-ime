package decode_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	jalm "github.com/0c-biggo/0c-ime/core/ja/lm"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

func testLM(t *testing.T, uni map[string]float64, bi map[[2]string]float64) *jalm.Model {
	t.Helper()
	m, err := jalm.Decode(jaimport.NewLM(uni, bi).Encode())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func surfaceFor(t *testing.T, dec *decode.Decoder, seq reading.Sequence) string {
	t.Helper()
	res, err := dec.Convert(seq, lang.Context{})
	if err != nil {
		t.Fatal(err)
	}
	return decode.ResultSurface(res.Clauses)
}

func TestLMWeightZeroKeepsBaseline(t *testing.T) {
	base := testDecoder(t)
	lat := *base
	lat.LM = testLM(t, map[string]float64{"明日": 50}, map[[2]string]float64{{"明日", "届く"}: 100})
	lat.LMWeight = 0

	for _, tc := range loadCorpus(t) {
		want := surfaceFor(t, base, tc.reading)
		got := surfaceFor(t, &lat, tc.reading)
		if got != want {
			t.Errorf("%s：LM 權重 0 的輸出 %q，want baseline %q", tc.surface, got, want)
		}
	}
}

func TestLMBigramChangesOrder(t *testing.T) {
	dec := testDecoder(t)
	seq := units("はしを")
	if got := surfaceFor(t, dec, seq); got != "橋を" {
		t.Fatalf("baseline = %q，want 橋を", got)
	}

	// 橋→を 的 bigram 成本很高時，Viterbi 應改選成本較高的 箸。
	withBi := *dec
	withBi.LM = testLM(t, nil, map[[2]string]float64{{"橋", "を"}: 100})
	withBi.LMWeight = 1
	if got := surfaceFor(t, &withBi, seq); got != "箸を" {
		t.Fatalf("bigram 重罰後 = %q，want 箸を", got)
	}

	// 同一個模型乘上 0 必須回到 baseline，證明權重是唯一開關。
	withBi.LMWeight = 0
	if got := surfaceFor(t, &withBi, seq); got != "橋を" {
		t.Fatalf("權重 0 = %q，want 橋を", got)
	}

	// 反向 bigram 不應影響這條路徑，結果仍是 baseline。
	other := *dec
	other.LM = testLM(t, nil, map[[2]string]float64{{"箸", "を"}: 100})
	other.LMWeight = 1
	if got := surfaceFor(t, &other, seq); got != "橋を" {
		t.Fatalf("無關 bigram = %q，want 橋を", got)
	}
}

func TestLMIsDeterministic(t *testing.T) {
	dec := testDecoder(t)
	dec.LM = testLM(t, map[string]float64{"明日": 1}, map[[2]string]float64{{"明日", "届く"}: 1})
	dec.LMWeight = 0.5
	seq := units("あしたとどく")
	first := surfaceFor(t, dec, seq)
	for i := 0; i < 10; i++ {
		if got := surfaceFor(t, dec, seq); got != first {
			t.Fatalf("第 %d 次輸出 %q，want %q", i, got, first)
		}
	}
}
