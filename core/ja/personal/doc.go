// Package personal 實作日文個人學習：記住使用者選過的文節、它當時的讀音與左文節，
// 下一次同一讀音、同一前文時優先出現。
//
// 學習資料存成獨立的 personal.ja.json，與注音的個人資料檔分開：
//
//   - 鍵值是 reading.Key 加上左文節正規化後的雜湊，換了前文就不會套用。
//   - 只調整排序，不改變讀音對應；加成有上限，不會壓過差距明顯的統計首選，
//     也不動來源不是轉換的候選（記号、かな表記等明確候選類型）。
//   - 反悔（Undo）、忘記（Forget）、重置（Reset）與暫停學習（SetPaused）都只作用在
//     這一份資料，注音學習檔一個位元組都不會變。
//
// core 不碰檔案系統：Store.Save/Load 使用 io.Writer/io.Reader，LoadFile 使用注入的
// fs.FS。宿主負責以暫存檔加改名的方式寫 personal.ja.json；讀到 *CorruptError 時
// 把原檔改名保留，再用空白 Store 繼續。檔名與路徑的共用慣例在 core/personal 的
// JaFileName、JaPath。
//
// 這個套件不讀時鐘、不碰 session：時間由呼叫端放進 Selection.At，
// 宿主負責把選字、改文節與接受補全轉成 Learn... 呼叫。
package personal
