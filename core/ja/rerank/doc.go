// Package rerank 把 T14 的神經驗證器接進日文轉換。
//
// 流程是 Zenzai 式的 draft、verify、限制式重搜：
//
//  1. 統計轉換先產生 draft 與數條候選。
//  2. 驗證器回報每條候選的分數、是否全部 token 都是 top-1，以及高機率前綴。
//  3. 取最佳候選的高機率前綴當限制，重建 lattice 後重搜被前綴固定的其餘路徑。
//  4. 達到驗證次數上限、時間預算用完，或最佳候選通過就停。
//
// 逾時、模型不可用或協定錯誤時原樣回傳統計結果。所有候選都從同一條讀音
// lattice 產生，因此神經層不會改變讀音對應。
//
// core 套件不得 import internal/neural（core 純度測試會檢查）。外殼用
// VerifierFunc 把 T14 runtime 轉成這裡的 Verifier；neural_bridge_test.go
// 有完整的 T14 mock 轉接與端到端測試。權重可用 Calibrate 依 T33 匯出的
// 樣本調整；ライブ変換的穩定前綴用 Tracker 取得。
package rerank
