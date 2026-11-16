package zhuyin

// 這個檔是注音音節與核心讀音單位之間的轉接。
//
// core/reading 為了讓既有注音呼叫端零轉換接上讀音泛化，把 Unit 定成
// Syllable 的型別別名，因此 core/reading 會 import 本套件。這個檔不能
// 反向 import core/reading，否則形成循環；改以型別別名表示相同的型別。

// ReadingSequence 與 reading.Sequence 是相同的型別。
type ReadingSequence = []Syllable

// ReadingUnit 回傳音節對應的核心讀音單位。一個注音音節就是一個單位。
func (s Syllable) ReadingUnit() Syllable { return s }

// SyllableFromUnit 把核心讀音單位轉回注音音節。
// 呼叫端負責確認單位確實是注音音節；這個函式不做結構檢查。
func SyllableFromUnit(u Syllable) Syllable { return u }

// ReadingUnits 把音節序列轉成核心讀音序列。
// 空序列維持 nil，讓 reading.Key 與 Key 產生相同結果。
func ReadingUnits(syls []Syllable) ReadingSequence {
	if syls == nil {
		return nil
	}
	seq := make(ReadingSequence, len(syls))
	copy(seq, syls)
	return seq
}

// Syllables 把核心讀音序列轉回音節序列。
func Syllables(seq ReadingSequence) []Syllable {
	if seq == nil {
		return nil
	}
	syls := make([]Syllable, len(seq))
	copy(syls, seq)
	return syls
}
