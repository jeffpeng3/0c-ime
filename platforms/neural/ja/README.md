# 日文神經 runtime（已移除）

P2-01 起，正式產品不再 import 或啟動日文 llama.cpp 後端，也不讀取 GGUF
模型。日文轉換目前一律使用 `core/ja/decode` 的統計轉換；候選重排介面
`core/ja/rerank` 保留，等 P2-30 接上 ONNX 模型。

T14 的 Python worker、mock、manifest 範例與 smoke 文件已從 `platforms/`
移除，避免正式安裝路徑誤用。T03 的實驗 spike 保留在
`scripts/spike/ja-zenz/`，只供歷史參考，不是產品路徑。

正式的日文模型會由 P2-30 以 ONNX Runtime 重新接上；在那之前沒有模型檔、
沒有 worker，也沒有需要核對的 manifest。
