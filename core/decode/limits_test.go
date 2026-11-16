package decode

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/input"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// 詞長上限由語言 limits 決定，不是固定 8。詞庫本身可以收更長的詞（#142），
// 但明確的 MaxWordUnits 是硬上限。
func TestLimitsControlWordLength(t *testing.T) {
	const units = 10
	reading := "ㄚ ㄇㄨˇ ㄙ ㄊㄜˋ ㄌㄤˇ ㄏㄨㄟˊ ㄒㄩㄢˊ ㄐㄧㄚ ㄙㄨˋ ㄆㄣ"
	word := "阿姆斯特朗回旋加速噴"
	syls := zhuyin.ParseReading(reading)
	if len(syls) != units {
		t.Fatalf("測試讀音有 %d 單位，want %d", len(syls), units)
	}

	lex := lexicon.NewMem()
	lex.Add(lexicon.Entry{Surface: word, Reading: zhuyin.ReadingUnits(syls), PRW: 1})
	lat := input.Exact{}.Expand(syls)

	if got := MaxLenFor(lex, lang.Limits{MaxWordUnits: 12}); got != 12 {
		t.Errorf("MaxLenFor = %d, want 12", got)
	}
	if got := MaxLenFor(lex, lang.Limits{}); got != units {
		t.Errorf("沒有語言上限時應沿用詞庫長度 %d，got %d", units, got)
	}

	wide := &Decoder{Lex: lex, Score: uni{word: 1}, Limits: lang.Limits{MaxWordUnits: 12}}
	if got := text(wide.Decode(lat, nil, false)); got != word {
		t.Errorf("上限 12：got %q, want %q", got, word)
	}

	narrow := &Decoder{Lex: lex, Score: uni{word: 1}, Limits: lang.Limits{MaxWordUnits: 8}}
	if got := text(narrow.Decode(lat, nil, false)); got == word {
		t.Errorf("上限 8 不應解出 %d 單位的詞", units)
	}
}
