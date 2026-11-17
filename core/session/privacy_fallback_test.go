package session

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/0c-biggo/0c-ime/core/layout"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/rerank"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// TestSecureInputNeverScoresOrLearns：密碼框／安全輸入狀態由宿主宣告一次，
// 之後的每個請求都不能送模型、不能學入個人資料，即使宿主忘了逐鍵帶 Private。
func TestSecureInputNeverScoresOrLearns(t *testing.T) {
	calls := 0
	s := newSession()
	s.Rerank = rerank.Options{Weight: .1, Scorer: rankFunc(func(r rerank.Request) ([]float64, error) {
		calls++
		return make([]float64, len(r.Candidates)), nil
	})}
	s.SecureInput = true

	// 先打一組真的有多個候選、平常會呼叫模型的音節，證明安全欄位讓模型完全不被呼叫。
	typeSyl(s, "ㄨㄛˇ")
	typeSyl(s, "ㄐㄧㄥˇ")
	if calls != 0 {
		t.Fatalf("secure input called model %d times, want 0", calls)
	}
	key(s, proto.KeyEnter)

	typeSyl(s, "ㄇㄚˊ")
	typeSyl(s, "ㄈㄢˊ")
	if snap := key(s, proto.KeyEnter); snap.Commit != "麻煩" {
		t.Fatalf("private commit = %q, want 麻煩", snap.Commit)
	}
	if calls != 0 {
		t.Fatalf("secure input called model %d times, want 0", calls)
	}
	if got := s.model.Evidence(lm.BOS, "麻煩"); got != 0 {
		t.Fatalf("secure input learned %g, want 0", got)
	}

	// 離開安全欄位後，同一條輸入照常學習；再打一組有多個候選的音節，
	// 確認模型路徑本身是通的（上面兩音節的「麻煩」只有一個候選，不會呼叫模型）。
	s.SecureInput = false
	typeSyl(s, "ㄇㄚˊ")
	typeSyl(s, "ㄈㄢˊ")
	if snap := key(s, proto.KeyEnter); snap.Commit != "麻煩" {
		t.Fatalf("public commit = %q, want 麻煩", snap.Commit)
	}
	if got := s.model.Evidence(lm.BOS, "麻煩"); got <= 0 {
		t.Fatalf("public input learned %g, want positive", got)
	}
	typeSyl(s, "ㄨㄛˇ")
	typeSyl(s, "ㄐㄧㄥˇ")
	if calls == 0 {
		t.Fatal("public input never called model")
	}
}

// TestPerRequestPrivateNeverScores 確認舊外殼逐鍵帶的 Private 仍然有效。
func TestPerRequestPrivateNeverScores(t *testing.T) {
	calls := 0
	s := newSession()
	s.Rerank = rerank.Options{Weight: .1, Scorer: rankFunc(func(r rerank.Request) ([]float64, error) {
		calls++
		return make([]float64, len(r.Candidates)), nil
	})}
	var snap proto.Snapshot
	for _, syl := range []string{"ㄇㄚˊ", "ㄈㄢˊ"} {
		for _, k := range layout.Standard.Keys(zhuyin.Syllable(syl)) {
			snap = s.Handle(proto.Request{Type: proto.Key, Key: string(k), Private: true})
		}
	}
	snap = s.Handle(proto.Request{Type: proto.Key, Key: proto.KeyEnter, Private: true})
	if snap.Commit != "麻煩" {
		t.Fatalf("private commit = %q, want 麻煩", snap.Commit)
	}
	if calls != 0 {
		t.Fatalf("private request called model %d times, want 0", calls)
	}
	if got := s.model.Evidence(lm.BOS, "麻煩"); got != 0 {
		t.Fatalf("private request learned %g, want 0", got)
	}
}

func playZhuyin(s *Session, syls []string) []string {
	var out []string
	record := func(snap proto.Snapshot) { out = append(out, snap.Text()+"\x00"+snap.Commit) }
	for _, syl := range syls {
		for _, k := range layout.Standard.Keys(zhuyin.Syllable(syl)) {
			record(s.Handle(proto.Request{Type: proto.Key, Key: string(k)}))
		}
	}
	record(s.Handle(proto.Request{Type: proto.Key, Key: proto.KeyEnter}))
	return out
}

// TestModelDisabledSessionMatchesStatisticalZhuyin：模型缺席時整個 session 的
// 注音輸出必須逐鍵與沒有模型時相同；錯誤與無效分數都不能改字或掉候選。
func TestModelDisabledSessionMatchesStatisticalZhuyin(t *testing.T) {
	syls := []string{"ㄨㄛˇ", "ㄒㄧㄤˇ", "ㄔ", "ㄇㄚˊ", "ㄈㄢˊ", "ㄐㄧㄥˇ", "ㄈㄥ"}
	want := playZhuyin(newSession(), syls)

	cases := []struct {
		name  string
		score func(rerank.Request) ([]float64, error)
	}{
		{
			name: "error",
			score: func(rerank.Request) ([]float64, error) {
				return nil, errors.New("model disabled")
			},
		},
		{
			name: "invalid scores",
			score: func(r rerank.Request) ([]float64, error) {
				scores := make([]float64, len(r.Candidates))
				scores[0] = math.NaN()
				return scores, nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := newSession()
			s.Rerank = rerank.Options{Weight: .1, Scorer: rankFunc(func(r rerank.Request) ([]float64, error) {
				calls++
				return tc.score(r)
			})}
			got := playZhuyin(s, syls)
			if calls == 0 {
				t.Fatal("disabled model was never called; fallback path not exercised")
			}
			if !reflect.DeepEqual(got, want) {
				for i := range want {
					if i == len(got) || got[i] != want[i] {
						t.Fatalf("snapshot %d = %q, want %q\n got=%q\nwant=%q", i, at(got, i), want[i], got, want)
					}
				}
				t.Fatalf("outputs differ:\n got=%q\nwant=%q", got, want)
			}
		})
	}
}

func at(values []string, i int) string {
	if i < 0 || i >= len(values) {
		return "<missing>"
	}
	return values[i]
}
