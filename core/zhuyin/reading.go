package zhuyin

import "github.com/0c-biggo/0c-ime/core/reading"

// ReadingUnit 回傳音節對應的核心讀音單位。一個注音音節就是一個單位。
func (s Syllable) ReadingUnit() reading.Unit { return reading.Unit(s) }

// SyllableFromUnit 把核心讀音單位轉回注音音節。
// 呼叫端負責確認單位確實是注音音節；這個函式不做結構檢查。
func SyllableFromUnit(u reading.Unit) Syllable { return Syllable(u) }

// ReadingUnits 把音節序列轉成核心讀音序列。
// 空序列維持 nil，讓 reading.Key 與 Key 產生相同結果。
func ReadingUnits(syls []Syllable) reading.Sequence {
	if syls == nil {
		return nil
	}
	seq := make(reading.Sequence, len(syls))
	for i, s := range syls {
		seq[i] = s.ReadingUnit()
	}
	return seq
}

// Syllables 把核心讀音序列轉回音節序列。
func Syllables(seq reading.Sequence) []Syllable {
	if seq == nil {
		return nil
	}
	syls := make([]Syllable, len(seq))
	for i, u := range seq {
		syls[i] = SyllableFromUnit(u)
	}
	return syls
}
