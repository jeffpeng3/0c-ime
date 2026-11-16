# T03 神經轉換實測 spike

狀態：**初步結果**。品質數字在 `corpus.1.txt` 的 200 題上有效。延遲與記憶體來自修正前的混合 corpus 樣本，而且是在主機 load average 約 21（12 vCPU，兩個 CI 加上其他 agent 正在編譯）時量的，只能當上限，不能當最終數字。安靜時段要用 `--isolated` 重跑。

## 結論

1. **T14 後端選 llama.cpp，CPU 優先。** zenz-v3.1-small 的 Q5_K_M GGUF 只有 70.4 MiB，CPU 6 threads 可以載入並轉換；GPU 不是第一版必要條件。
2. **不要在每次按鍵跑完整 free generation。** 32 かな完整生成 P50 已超過 100 ms 預算。T14 要照 Zenzai 的 draft + verify 形狀，verify 只做一次 forward pass 對候選評分。
3. **100 ms 預算目前無法認證。** 完整生成在 CPU 上不可行；verify-only 可能可行，但這次沒有量到，必須在安靜時段補測。
4. **ONNX 技術上可行，但不選為第一版。** fp32 約 380 MB，int8 約 95 MB，比 GGUF 大；雖然 0C 已經打包 onnxruntime，仍要先證明品質與延遲才值得更換。
5. **Anthy 首選正確率 80.0%，zenz free greedy 47.5%。** zenz 這個數字沒有跑 Zenzai 的限制式候選驗證，不能當神經轉換的上限。Anthy 的 80.0% 也因為 `corpus.1.txt` 是它的訓練資料而明顯偏高。

## 授權修正

T02 的盤點結果：Anthy corpus 只有 `calctrans/corpus.1.txt` 標明 public domain。`corpus.2.txt`、`corpus.3.txt`、`corpus.5.txt` 沒有授權聲明，不得散布。

因此：

- fixture 只從 `corpus.1.txt` 抽 200 題，`corpus_files` 只記這一個檔案的 SHA-256。
- `tools/build_sample.py` 只讀 `ALLOWED_CORPUS_FILES = ("corpus.1.txt",)`，而且會檢查檔案前 5 行含 `public domain`，沒有就 fail closed。
- 修正前用混合 corpus 產生的 `results/cpu-6t.json` 已從目前樹移除，因為它內含 2/3/5 的逐句資料。
- 前一筆 spike commit `8ab57ae` 的歷史仍含舊 fixture 與舊結果。分支還沒 push，合併前要 squash 或丟棄那筆 commit，否則未授權文字仍會進 main 的歷史。
- README 保留的延遲聚合數字沒有 corpus 文字，只作為 preliminary 方向，不能由這次的 fixture 重現。
- 品質數字已用只含 `corpus.1.txt` 的新 fixture 重跑，逐項結果在 `results/corpus1-quality.json`。

## 量測環境

| 項目 | 值 |
|---|---|
| 平台 | WSL2，kernel 6.18.58.1-microsoft-standard-WSL2，Fedora 45 |
| CPU | AMD Ryzen 5 5600X，6 核 12 執行緒 |
| RAM | 31 GiB |
| GPU | NVIDIA RTX 4060 8 GB，WSL 內透過 Mesa Dozen（Vulkan）可見 |
| CUDA | 沒有裝 CUDA toolkit，這次沒有 CUDA 數字 |
| 主機負載 | 延遲量測時 load average 約 21 / 12 vCPU；兩個 CI 與其他 agent 同時編譯 |

延遲與記憶體數字全部標成 `contended_preliminary_not_final`。品質是 deterministic，主機負載不影響輸出。

## 版本鎖定

完整清單在 [pin.json](pin.json)。

| 元件 | 版本 |
|---|---|
| llama.cpp | `2d8a32502909ca065c41c4cf5f2fb1c427c047fc`（v1.0.0） |
| llama.cpp patch | [patches/llama-gpt2-small-japanese-char.patch](patches/llama-gpt2-small-japanese-char.patch) |
| 模型 | `Miwa-Keita/zenz-v3.1-small-gguf` revision `ddf6e44b2e05ab7ea9a3e31559c5e7948761365c` |
| 模型檔 | `ggml-model-Q5_K_M.gguf`，73,871,968 bytes，SHA-256 `4de930c06bef8c263aa1aa40684af206db4ce1b96375b3b8ed0ea508e0b14f6c` |
| 傳統 baseline | Anthy 9100h（Fedora `anthy-9100h-62.fc45`），libanthy UTF-8 |
| corpus | 只用 Anthy corpus `calctrans/corpus.1.txt`，public domain，SHA-256 在 pin.json |
| 取樣 | 200 題，seed `20261117`，[fixtures/anthy_sample200.json](fixtures/anthy_sample200.json) |

zenz GGUF 的 `tokenizer.ggml.pre` 是 `gpt2-small-japanese-char`，upstream llama.cpp v1.0.0 不認得這個名字。patch 把它接到既有的 GPT-2 byte-level BPE pre-tokenizer，分詞規則與 azooKey fork 的實作相同。

## 可重現指令

```sh
# 1. llama.cpp
git clone https://github.com/ggml-org/llama.cpp
cd llama.cpp
git checkout 2d8a32502909ca065c41c4cf5f2fb1c427c047fc
git apply /path/to/scripts/spike/ja-zenz/patches/llama-gpt2-small-japanese-char.patch
cmake -B build-cpu -DCMAKE_BUILD_TYPE=Release -DLLAMA_BUILD_TESTS=OFF
cmake --build build-cpu -j --target llama-server llama-tokenize

# 2. 模型
curl -L -o ggml-model-Q5_K_M.gguf \
  https://huggingface.co/Miwa-Keita/zenz-v3.1-small-gguf/resolve/ddf6e44b2e05ab7ea9a3e31559c5e7948761365c/ggml-model-Q5_K_M.gguf
sha256sum ggml-model-Q5_K_M.gguf   # 要等於 pin.json

# 3. 傳統 baseline
sudo dnf install anthy anthy-devel
cc -O2 -o /tmp/anthy_top1 tools/anthy_top1.c $(pkg-config --cflags --libs anthy)

# 4. 200 題樣本（只在要重建 fixture 時跑；只讀 corpus.1.txt）
python3 tools/build_sample.py --corpus-dir /path/to/anthy-9100h/calctrans \
  --out fixtures/anthy_sample200.json

# 5. 只跑品質，不量效能
python3 run_spike.py --skip-latency \
  --backend cpu-6t --threads 6 \
  --llama-bin /path/to/llama.cpp/build-cpu/bin \
  --model /path/to/ggml-model-Q5_K_M.gguf \
  --anthy-bin /tmp/anthy_top1 \
  --out results/corpus1-quality.json

# 6. 安靜時段量測；load1 超過 0.35 * CPU 數會直接拒絕
python3 run_spike.py --isolated \
  --backend cpu-6t --threads 6 \
  --llama-bin /path/to/llama.cpp/build-cpu/bin \
  --model /path/to/ggml-model-Q5_K_M.gguf \
  --anthy-bin /tmp/anthy_top1 \
  --out results/cpu-6t-isolated.json
```

`--isolated` 只擋高負載，不擋其他 llama-server。重跑前先確認沒有其他 agent 或 CI 在工作。結果 JSON 會記錄 `loadavg_start`、`loadavg_end`、`latency_status`；負載超標時 `latency_status` 是 `contended_preliminary_not_final`。`--skip-latency` 只輸出品質，不記錄時間與記憶體。

## 品質

200 題，全部來自 `corpus.1.txt`，逐題 greedy 轉換，跟 Anthy corpus 正解比對。

| 引擎 | 首選完全一致 | 首選正確率 | NFKC 後一致 | LCS recall |
|---|---:|---:|---:|---:|
| zenz-v3.1-small Q5_K_M，free greedy | 95 / 200 | 47.5% | 48.5% | 89.23% |
| Anthy 9100h，每段 candidate 0 | 160 / 200 | 80.0% | 80.0% | 97.19% |

注意事項：

- zenz 只做 `\uEE00読み\uEE01` 的 greedy 生成。Zenzai 正式流程會用轉換器產生的候選做限制式重搜與評分，這次沒有實作，所以 47.5% 是這個簡化流程的數字，不是模型上限。
- `corpus.1.txt` 是 Anthy 的訓練語料，Anthy 的 80.0% 明顯偏高，只能當「傳統轉換在同語料上的參考」。
- zenz 的完全一致與 NFKC 正規化後差 2 題，正規化只影響全角數字或空白，主要差距仍是轉換內容。
- 例：`きょうはいいてんきですね` → `今日はいい天気ですね`。
- 這個 200 題集合只涵蓋 `corpus.1.txt`，題材比原本的混合樣本窄，數字不能直接跟其他專案的 Anthy 評測比較。

## 延遲（ historical，contended，不是最終數字）

以下是修正前混合 corpus 跑程的聚合數字。原始逐項結果含未授權 corpus 文字，已移除；表格只保留聚合值，不能由這次的 fixture 重現，等安靜時段用 `--isolated` 重跑。

CPU 6 threads、Q5_K_M、ctx 512、greedy、token array prompt、無 BOS。

| 情境 | n | P50 | P95 | P99 | 備註 |
|---|---:|---:|---:|---:|---|
| 每次按鍵，prefix cache 開 | 135 | 33.74 ms | 129.46 ms | 164.29 ms | 連續輸入同一句的 1..N 前綴 |
| 每次按鍵，cache 關 | 135 | 44.27 ms | 572.82 ms | 1630.96 ms | 每次重算整段 prompt |
| 32 かな完整轉換 | 200 | 165.87 ms | 319.96 ms | 402.48 ms | 最長 32 字 |
| 200 題品質轉換 | 200 | 73.90 ms | 186.38 ms | 240.52 ms | 長度不一 |

讀法：

- 這些數字在高負載下量到，只能當上限。安靜時段應該會明顯下降。
- 就算只看 P50，32 かな完整生成 166 ms 已經超過 100 ms。**每次按鍵跑完整生成不可行。**
- cache 開的 P95 129 ms 也超過 100 ms，但這是「重跑整句生成」的成本，不是 Zenzai verify 的成本。
- Zenzai verify 只對 prompt + 單一候選做一次 forward pass。llama.cpp log 顯示 6 threads prompt eval 約 0.84 到 0.88 ms/token（同樣在高負載下），40 token 的 verify 約 35 ms。這是估算，不是量測結果。

## 載入與記憶體（ historical，contended，需重跑）

| 項目 | 值 |
|---|---:|
| 模型檔 | 70.4 MiB |
| 載入 wall time | 4,154 ms（高負載；server log 的 model load 約 0.13 s） |
| 載入後 RSS | 114 MiB |
| 跑完 600+ 次請求後 RSS | 348 MiB |
| ctx | 512 |

348 MiB 的尾端數字不能直接當常駐記憶體。它可能來自 batch/compute buffer 擴張或配置器成長，需要安靜時段逐步量。以 0C 的單語言 250 MB 預算來看，這個數字會超標，T14 必須先壓低再談常駐。

## 後端測試

| 後端 | 結果 |
|---|---|
| CPU（AVX2/VNNI，6 threads） | 可用，這次所有延遲數字都來自這裡 |
| Vulkan（Mesa Dozen，RTX 4060） | 可用，但這個 74 MB 模型更慢：smoke test 18 tokens 259 ms（15.3 ms/token），prompt 31 tokens 744 ms |
| CUDA | 環境沒有 nvcc，沒有量 |
| ONNX Runtime | 沒有量，評估見下節 |

Vulkan 的數字不能代表原生 Windows CUDA。模型太小，Vulkan/Doze 的 kernel launch 與搬移成本蓋過計算。T14 若要在 Windows 開 GPU，應直接量 CUDA EP 或 CUDA llama.cpp，不要用這次的 Doze 數字推論。

另外，`llama-server` v1.0.0 在同一次實驗用 `-b 256 -ub 256` 加 prefix cache 的一次重複請求跑程中，於 `libggml-cpu` 發生一次 SIGSEGV。預設 batch 的完整跑程沒有重現。這是 preliminary 的穩定性風險，T14 要選成熟 commit 並做 soak test，不要直接跟最新 tag。

## GPT-2 轉 ONNX 的可行性

zenz-v3.1-small 是標準 `GPT2LMHeadModel`：12 layers、768 hidden、6000 char vocab、95.1M parameters。技術上可以用 `torch.onnx` 或 `optimum` 匯出，verify 只需要一次 forward pass 加候選位置的 logits gather，不需要 generation loop。

| 項目 | 評估 |
|---|---|
| 匯出 | 可行，動態 sequence 軸，opset 17 |
| fp32 大小 | 約 380 MB（95M × 4 bytes） |
| int8 大小 | 約 95 MB，需量化與品質驗證 |
| GGUF Q5 大小 | 74 MB |
| 預期 verify 延遲 | 與 llama.cpp prompt eval 同量級；40 token 約 35 到 60 ms，但要實測 |
| 打包 | 0C 已經有 onnxruntime（select/punct），可共用 runtime；llama.cpp 要多帶一組 DLL |
| 風險 | 量化掉品質、匯出圖與 tokenizer 對齊、Windows EP 差異 |

建議：T14 第一版用 llama.cpp。只有在 Windows 打包不願多帶 llama.cpp，或量到 ONNX int8 明顯更快時，才做 ONNX 對照。屆時只要支援 verify 的單次 forward pass，不需要 ONNX generation。

## 檔案

| 路徑 | 用途 |
|---|---|
| `run_spike.py` | 啟動 llama-server、量延遲與記憶體、跑 200 題品質、呼叫 Anthy baseline；`--isolated` 與 `--skip-latency` |
| `tools/anthy_top1.c` | libanthy top-1 harness，逐行 reset，不 commit、不學習 |
| `tools/build_sample.py` | 只從 `corpus.1.txt` 抽固定 200 題，授權標記缺失時 fail closed |
| `fixtures/anthy_sample200.json` | 固定題庫，只含 `corpus.1.txt` 的內容與雜湊 |
| `patches/llama-gpt2-small-japanese-char.patch` | 讓 upstream llama.cpp 認得 zenz tokenizer 名稱 |
| `pin.json` | 版本、模型、允許的 corpus 檔與雜湊 |
| `results/corpus1-quality.json` | `corpus.1.txt` 200 題的品質結果，無延遲欄位 |
| `tests/test_run_spike.py` | percentile、正規化、LCS、prompt 的單元測試 |
| `tests/test_build_sample.py` | corpus 授權白名單與 fail-closed 測試 |

## 安靜時段要補的項目

1. `--isolated` 重跑 CPU 6 threads，取得最終 P50/P95/P99 與載入時間。
2. 量 verify-only：prompt + 候選的單次 forward pass，確認 100 ms 預算。
3. 逐步量 RSS，找出 114 MiB 到 348 MiB 的成長來源。
4. 可選：Windows 原生 CUDA llama.cpp，以及 onnxruntime int8 verify 對照。
5. 實作 Zenzai 限制式候選驗證後重跑品質，取代 free greedy 的 47.5%。
