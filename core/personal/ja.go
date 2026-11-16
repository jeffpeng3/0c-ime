package personal

import "path/filepath"

// JaFileName 是日文個人學習檔的固定檔名。
//
// 注音與日文分檔：注音沿用原本的 user.json（合約舊稱 personal.json），
// 日文用這一個。重置、忘記或反悔日文學習都不會寫到注音檔，
// 舊注音學習檔的位元組因此保持不變。
const JaFileName = "personal.ja.json"

// JaPath 回傳與注音個人資料檔同目錄的日文學習檔路徑。
//
// 只看目錄，不看注音檔的檔名：無論呼叫端用 user.json 還是 personal.json，
// 日文檔一律叫 personal.ja.json。
func JaPath(personalPath string) string {
	return filepath.Join(filepath.Dir(personalPath), JaFileName)
}
