// Package rerank 把外部候選驗證器接進日文轉換。
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
// P2-01 之後正式產品不啟動外部推論後端；外殼不建立 Reranker，日文一律
// 使用統計轉換。這個套件保留 Verifier 介面與重搜邏輯，P2-30 接上 ONNX
// reranker 時由外殼實作 Verifier 即可。core 套件不得 import internal/neural
// （core 純度測試會檢查）。權重可用 Calibrate 依 T33 匯出的樣本調整；
// ライブ変換的穩定前綴用 Tracker 取得。
package rerank
