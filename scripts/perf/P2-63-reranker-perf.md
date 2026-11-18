# P2-63：日文 reranker 的效能、記憶體與 soak

狀態：量測完成（2026-11-18）。分支 `p2/T63-perf`，基準 `a623355`。產品程式未修改；這份文件只包含量測、比較與建議。

## 結論

- **DGX 的 63.4 ms 不是 DGX 專屬現象。** 同一顆 int8 artifact、相同 seq 244、相同 ONNX Runtime 1.32.0，在這台 x86 桌機的 5 候選組中位數是 109.2 ms（intra_op 8，逐一推論），比 DGX 慢 1.72 倍。對 IME 來說這是真實的產品問題，而且在沒有 VNNI 的 x86 上更嚴重。
- **預設逾時會讓這條路徑完全失效。** `core/ja/rerank` 的預設 `PerRequestTimeout` 是 80 ms、總預算 100 ms。109.2 ms 的一組已經超過兩者，每次 verify 都會逾時並回退統計順序；DGX 的 63 ms 也只剩 17 ms 餘裕，第二輪 2×63 ms 一定超過 100 ms。
- **batching 沒有幫助。** batch 5 的每候選成本是 26.1 ms，batch 8 是 29.0 ms，逐一推論是 21.9 ms。五個候選合成一次呼叫反而慢 19.4%；八個候選更慢 32.5%。DGX 的 batch 5 也是 0.79× 的加速比，兩個平台方向一致。
- **thread 數不是缺少的 2 倍。** 這台 6 核 12 執行緒的機器上，intra_op 8 最好（109.2 ms），4 是 124.5 ms，1 是 282.8 ms；產品預設的 8 已經是最佳。`allow_spinning=1` 反而讓中位數變成 158.9 ms，維持關閉。
- **真正有效的是縮短 context 與減少候選數。** seq 124（60+60）把每組降到 48.4 ms；只評統計前 3 名約 65.6 ms；兩者合併約 28.4 ms，回到 30 ms 等級。再配上 async 顯示字典序，按鍵執行緒不需要等待模型。
- **30 分鐘 soak 沒有向上漂移。** 中位數 108.9 ms、p95 181.4 ms；前 10% 是 112.5 ms，後 10% 是 106.2 ms（-6.3 ms），曲線在系統穩定後持平。tail 的 p99 264 ms、max 900 ms 來自 WSL 排程尖峰，比短跑 bench 差。
- **記憶體緩慢成長，但不是 OOM 等級。** 載入後 RSS 約 122.3 MiB，逐一推論峰值約 196 MiB；soak 從 208.4 MiB 線性增加到 213.1 MiB（+4.7 MiB，約 10.4 MiB/hour）。30 分鐘不足以判定是否收斂，建議另跑 2 小時 soak。

## 1. 量測對象

| 項目 | 值 |
|---|---|
| artifact | `reranker-public-core.int8.onnx` |
| 大小 | 34,242,785 bytes（32.66 MiB） |
| sha256 | `0614a6b3412a2faf7c69d14f4b450bcb44c36a25755883c4f18abf146ccaf0e9` |
| 量化 | dynamic qint8 |
| 架構 | transformer encoder，8 layers、d_model 512、8 heads、FFN 1024、vocab 32768、max positions 512 |
| 參數 | 33,864,193 |
| pooling | candidate-slot，讀 index 122 |
| 驗證集 | 8000 組，top-1 0.86975；5 候選子集 0.87785 |
| placeholder | false（`trained: true`、`production_eligible: true`） |

manifest 也列出 fp32 檔（135,595,838 bytes），但這台機器只有 int8 artifact，因此所有數字都是 int8。

## 2. 環境與方法

| 項目 | 值 |
|---|---|
| CPU | AMD Ryzen 5 5600X，6 核 12 執行緒（SMT） |
| ISA | AVX2、FMA；沒有 AVX-512、沒有 VNNI |
| 記憶體 | 31.3 GiB |
| OS | WSL2，Linux 6.18.58.1-microsoft-standard-WSL2，x86_64 |
| Python | 3.13.7（venv 在 `/home/fedora/.venvs/p2-63`） |
| numpy | 2.5.5 |
| onnxruntime | 1.32.0，`CPUExecutionProvider` |
| 模型路徑 | `/home/fedora/.opencode/plan/0c-ja/phase2/reference/models/trained/reranker-public-core/reranker-public-core.int8.onnx` |

量測在安靜窗口進行；thread 掃描開始前的系統負載約 1.0（12 執行緒），bench 進行時其餘工作只有 harness 自己。WSL2 不是原生 Windows，但 CPU 指令集與 ORT CPU kernel 相同，可以代表 x86 桌機；Windows 原生數字仍應在 P2-65 驗收時重跑。

輸入用合成 fixture，因為這台機器沒有 homophone-v1 的 `validation.jsonl`（manifest 指到的 `/mnt/work/0c/data/...` 不存在）。fixture 是 200 組 × 5 候選、`int64`、seq 244，layout 與 manifest 一致：

```text
<bos> left(120) <eos> candidate <eos> right(120)
```

seed 固定為 `20261118`，token id 範圍：context 1000–7999、candidate 8000–23999。fixture sha256 是 `7b76c69d6c8266394fd993034305594abb3434d03d781047da497765fbc7708c`。延遲取決於 shape 與 batch，不取決於 token 值，因此合成輸入足以回答效能問題；品質仍只能用 8000 組 benchmark 回答。

量測協定：

- session options 對齊產品：`inter_op_num_threads=1`、`ORT_SEQUENTIAL`、`session.intra_op.allow_spinning=0`。
- 每個設定先 warmup 30 次單候選、15 次 5 候選、7 次 8 候選，再跑多個 pass。
- 逐候選延遲是 1000 次 batch-1 呼叫；每組延遲是同一個 pass 內 5 次呼叫的總和。
- batch 5 是 200 次單一 5-row 呼叫；batch 8 是 125 次單一 8-row 呼叫（1000 個候選重新切成 125 組）。
- 百分位用 numpy 線性插值。t8 取 3 個 pass，「pass median 最接近所有 pass median 的中位數」的那個 pass，與 DGX manifest 的方法一致。
- 冷啟動開 5 個全新的 Python process，從 parent 計時 process wall，child 分開記 import、session load、第一次推論。
- RSS 讀 `/proc/self/status`；bench 期間另有 25 ms 取樣執行緒量峰值。

## 3. 冷啟動

| stage | median ms |
|---|---:|
| import numpy + onnxruntime | 111.3 |
| session_load | 104.8 |
| first_inference | 35.6 |
| ready（process 內計時） | 278.9 |
| process_wall（parent 計時，含 interpreter 啟動與離開） | 344.0 |

冷啟動中位數 278.9 ms：import numpy/onnxruntime 111.3 ms、建立 session 104.8 ms、第一次推論 35.6 ms。RSS 在 import 後是 52.8 MiB，載入模型後是 110.5 MiB。

模型檔案已在 OS page cache；沒有 root 無法 `drop_caches`，所以這是「新 process、暖 page cache」的數字。首次從磁碟讀 32.66 MiB 會再多幾十毫秒。產品應在啟動時 preload 並 warmup，否則日文第一次按鍵會遇到約 279 ms 的停頓。

## 4. 延遲：逐一推論與 batching

headline 用 intra_op 8，seq 244：

| mode | samples | candidates/call | median ms | p95 ms | mean ms | ms/candidate | per-candidate cost vs batch 1 |
|---|---:|---:|---:|---:|---:|---:|---:|
| batch 1, per candidate | 1000 | 1 | 21.9 | 32.4 | 23.0 | - | 1.00 |
| batch 1, group of 5 (5 calls) | 200 | 5 | 109.2 | 143.5 | 114.9 | 21.9 | 1.00 |
| batch 5 (one call) | 200 | 5 | 130.5 | 160.2 | 135.1 | 26.1 | 1.19 |
| batch 8 (one call) | 125 | 8 | 231.6 | 279.6 | 239.5 | 29.0 | 1.32 |

逐一推論 5 個候選的組中位數是 109.2 ms、p95 143.5 ms。同一批候選用一次 batch 5 呼叫是 130.5 ms、p95 160.2 ms；換算每候選 26.1 ms，比逐一推論慢 19.4%。batch 8 是 231.6 ms、p95 279.6 ms，每候選 29.0 ms，慢 32.5%。換成每候選等效 p95，batch 5 是 32.0 ms、batch 8 是 35.0 ms，逐一推論是 32.4 ms。

batch sweep 顯示相同趨勢：

| batch | groups | median ms | p95 ms | ms/candidate | speedup vs batch 1 |
|---|---:|---:|---:|---:|---:|
| 1 | 200 | 21.6 | 30.4 | 21.6 | 1.00 |
| 2 | 200 | 46.1 | 68.8 | 23.1 | 0.47 |
| 3 | 200 | 71.0 | 92.7 | 23.7 | 0.30 |
| 4 | 200 | 102.1 | 133.1 | 25.5 | 0.21 |
| 5 | 200 | 130.5 | 160.2 | 26.1 | 0.17 |
| 8 | 125 | 231.6 | 279.6 | 29.0 | 0.09 |

ORT 的 intra-op 執行緒在 batch 1 時可以把一個 row 的矩陣運算切給 8 個執行緒；batch 變大時，每個 row 的平行度下降，總時間接近線性相加，還會多出 padding 與 activation 量化的成本。因此 batching 在這個模型上是反效果，跨組合併也沒有價值。

## 5. Thread 設定

| config | intra_op | spinning | candidate median | candidate p95 | group median | group p95 |
|---|---|---:|---:|---:|---:|---:|
| t1 | 1 | no | 56.3 | 63.5 | 282.8 | 301.9 |
| t2 | 2 | no | 33.5 | 42.1 | 171.1 | 195.4 |
| t4 | 4 | no | 25.2 | 32.0 | 124.5 | 148.4 |
| t6 | 6 | no | 24.1 | 32.2 | 120.0 | 147.8 |
| t8 | 8 | no | 21.9 | 32.4 | 109.2 | 143.5 |
| default | default | no | 24.4 | 38.4 | 122.6 | 172.7 |
| t8-spin | 8 | yes | 31.6 | 38.4 | 158.9 | 177.4 |

- intra_op 8 最快，4 是 124.5 ms（慢 14.0%），6 是 120.0 ms，1 是 282.8 ms（慢 159.0%）。
- `default`（不指定，讓 ORT 自己決定）是 122.6 ms，比固定 8 慢。這台機器的 12 個 logical CPU 中有 6 個是 SMT，超過 8 條執行緒沒有幫助。
- `allow_spinning=1` 讓中位數從 109.2 ms 變成 158.9 ms（慢 45.5%），p95 也變差。產品關掉 spinning 是正確的；在按鍵之間的空檔，spinning 只會燒 CPU。
- 產品預設 `DefaultThreads()` 是 `min(8, NumCPU)`，在這台機器就是 8，已經是量測到的最佳值。**thread tuning 沒有隱藏的 2 倍。**

## 6. Context 長度

seq 244 是 120+120。模型支援動態 seq，但 pooling 固定讀 index 122，所以最短可用 seq 是 123；60+60（seq 124）是能縮到的最小值。用同一支產生器把左右 context 對稱縮短：

| seq | context each side | candidate median | group median | group p95 | relative to seq 244 |
|---:|---:|---:|---:|---:|---:|
| 244 | 120 | 20.6 | 103.0 | 133.2 | 1.00 |
| 164 | 80 | 13.2 | 66.3 | 86.1 | 0.64 |
| 124 | 60 | 9.5 | 48.4 | 67.8 | 0.47 |

延遲大致隨 seq 下降：seq 164 是 seq 244 的 0.64，seq 124 是 0.47。seq 124 的每候選中位數是 9.5 ms，5 候選組是 48.4 ms（p95 67.8 ms），中位數低於 80 ms 的單次上限，p95 仍有壓力。seq 124 + top 3 的 p95 推估約 43 ms（3 × 候選 p95 14.3 ms），在 80 ms 內。

context 的量測是 individual-only、2 個 pass，所以 seq 244 的 103.0 ms 與 headline 的 109.2 ms 有約 5% 的 run-to-run 差異；比值不受影響。

這裡只量延遲，沒有量品質。模型是在 120+120 上訓練的，截到 60+60 是 distribution shift；上線前要用 8000 組 benchmark 比較 top-1，不能只看速度。

## 7. 記憶體

| 階段 | RSS |
|---|---:|
| import numpy + onnxruntime | 52.8 MiB |
| 載入 session 後（冷啟動 process） | 110.5 MiB |
| bench process 載入 session + fixture 後 | 122.3 MiB |
| 逐一推論期間峰值（intra_op 6/8） | 196.0 MiB |
| batch 8 期間峰值 | 237.5 MiB |
| soak 暖機 30 組後 | 205.7 MiB |
| 30 分鐘 soak 後（後 10% 中位數） | 213.1 MiB |

模型 weights 是 32.66 MiB，但 ORT session、activation arena 與 Python/numpy 讓載入後 RSS 到 122.3 MiB，逐一推論峰值再多約 73.7 MiB。soak 的 205.7 MiB 是暖機 30 組後的基線，30 分鐘後到 213.1 MiB；這是長時間推論下 arena 與 Python allocator 的實際常駐，比單次 bench 高。這是 Python harness 的數字；產品的 native 後端不帶 CPython 與 numpy，實際常駐會低不少，應在 Windows 套件上另量。batch 8 的 arena 比逐一推論大 41.5 MiB，這也是另外一個不要 batching 的理由。

## 8. 30 分鐘 soak

| metric | value |
|---|---|
| groups scored | 8908 |
| target period / duration | 200 ms / 30.0 min |
| group median / p95 / p99 | 108.9 / 181.4 / 264.1 ms |
| group max | 900.3 ms |
| candidate median / p95 | 21.4 / 38.9 ms |
| first 10% / last 10% group median | 112.5 / 106.2 ms |
| drift / slope | -6.3 ms / -64.2 ms per hour |
| RSS after warmup | 205.7 MiB |
| RSS first 10% / last 10% | 208.4 / 213.1 MiB |
| RSS growth / max / HWM | 4.7 / 213.4 / 217.0 MiB |
| RSS slope | 10.43 MiB per hour |
| dropped ticks | 459 |

soak 用 intra_op 8、seq 244、每 200 ms 一組、每組 5 個候選逐一推論，共 8908 組（4.95 組/秒；459 個 tick 因為前一組超過 200 ms 或排程延遲而被丟棄）。每組中位數 108.9 ms，約佔週期的 54%，這是「每次轉換都觸發 rerank」的上限情境。

- 延遲：前 10% 中位數 112.5 ms，後 10% 106.2 ms，漂移 -6.3 ms；線性斜率 -64.2 ms/hour。沒有向上漂移；前 5 分鐘較慢（120.8 ms），10 分鐘後穩定在 104–109 ms，與重啟後系統仍在暖機、CPU 頻率尚未穩定一致。p95 181.4 ms、p99 264.1 ms、max 900.3 ms 明顯高於短跑 bench，尖峰來自 WSL 排程而不是模型本身；這也說明同步等待模型會讓 UI 遇到將近 1 秒的凍結。
- 記憶體：前 10% 208.4 MiB，後 10% 213.1 MiB，成長 4.7 MiB；峰值 213.4 MiB、HWM 217.0 MiB。5 分鐘區間的中位數從 208.6 穩定增加到 213.0 MiB，沒有回收，線性斜率 10.4 MiB/hour。30 分鐘的資料不足以判定這是 arena 碎片還是真正的 leak，但以 8 小時工作日推估約 +83 MiB，還不到 OOM；建議另跑 2 小時 soak 確認是否收斂。

## 9. 與 DGX 比較

DGX 的數字來自同一個 manifest 的 `latency.int8`：aarch64、20 CPU、intra_op 8、inter_op 1、seq 244、ORT 1.32.0，量測時 host load 8.6–10.1。manifest 沒有記錄 `allow_spinning`；這台機器兩種都量，關閉較快。20 CPU 與 6 核不是同級硬體，這裡比較的是同一顆 artifact 的實際可達延遲，不是 CPU 基準測試。

| 指標 | DGX（aarch64, 20 CPU, intra 8） | 這台 x86（6C/12T, intra 8） | 倍率 |
|---|---:|---:|---:|
| 每候選 median | 15.704 ms | 21.9 ms | 1.39× |
| 每候選 p95 | 16.466 ms | 32.4 ms | 1.97× |
| 5 候選逐一，median | 63.367 ms | 109.2 ms | 1.72× |
| 5 候選逐一，p95 | 93.862 ms | 143.5 ms | 1.53× |
| batch 5，median | 99.159 ms | 130.5 ms | 1.32× |
| batch 5，每候選 | 19.832 ms | 26.1 ms | 1.32× |
| batch 8，median | 158.403 ms | 231.6 ms | 1.46× |
| batch 8，每候選 | 19.800 ms | 29.0 ms | 1.46× |

**判定：63 ms 不是 DGX-CPU artefact，是真實的產品問題。** 同一顆 artifact 在這台 x86 上更慢，逐候選中位數從 15.7 ms 變成 21.9 ms，5 候選組從 63.4 ms 變成 109.2 ms。兩個平台的 batch 行為也一致：batch 5/8 都比逐一推論差。

最可能的原因是 int8 kernel 的指令集差異。Zen 3 只有 AVX2/FMA，沒有 VNNI；ORT 的 x86 `MatMulInteger` 在沒有 VNNI 時走較慢的 path。aarch64 主機若有 dot-product／i8mm 類指令，int8 矩陣乘會快很多。這是推論，沒有在 DGX 上做 profiler 驗證；能確定的是同一份模型、同一版 ORT、同一個 shape，x86 就是比較慢。

另一個差異是 DGX 的 63 ms 是在 host load 8.6–10.1 下量的；這台機器在安靜窗口量，仍然慢約 1.72 倍，因此不能把差距歸給負載。

## 10. 建議與預期節省

以下都是量測後的推估，沒有修改產品程式。每組 5 候選、seq 244、intra_op 8 的基準是 109.2 ms。

| 措施 | 依據 | 預期每組延遲 | 節省 | 風險 |
|---|---|---:|---:|---|
| 啟動時 preload + warmup | 冷啟動 278.9 ms | 第一次按鍵 0 ms | 279 ms（一次性） | 啟動多佔約 110 MiB；residency 已有 preload 機制，確認日文模型有被載入 |
| intra_op 8（維持產品預設） | thread 表 | 109.2 ms | 已是最佳；改 4 會慢 14.0%，改 1 慢 159.0% | 無 |
| 關閉 intra-op spinning（維持） | t8-spin | 109.2 ms | 比 spinning 快 45.5% | 無 |
| context 120+120 → 60+60 | seq 124 | 48.4 ms | 55.7% | 品質未量；需在 8000 組 benchmark 比較 top-1 |
| 只評統計前 3 名 | 3 × 21.9 ms | 65.6 ms | 40.0% | 正確候選掉出前 3 的比率未知；先量 recall@3 |
| seq 124 + top 3 | 3 × 9.5 ms | 28.4 ms | 74.0% | 兩個風險疊加 |
| async：先顯示字典／統計序，分數回來再重排 | seq124 + top3 | 首次顯示 0 ms；重排 28.4 ms | 按鍵執行緒等待 0 ms | 候選會跳動；需要取消過期請求與穩定的顯示策略 |
| 逐候選 progressive（seq124 + top3）：先評第 1 名 | 9.5 ms | 第一個結果 9.5 ms；完整 28.4 ms | 首個候選提前 91.3% | 候選順序會分兩段更新 |

建議順序：

1. **先把 timeout 與 async 一起設計。** 以目前的 shape，80 ms 的同步上限不可能達成；若留在同步路徑，seq 124 + top 3 才有機會。若改成 async，timeout 應設在量到的 p99 加餘裕，而不是 80 ms。
2. **量 seq 124 與 top 3 的品質。** 這是最大的兩個速度槓桿，但都會改變 ranking。用 manifest 指到的 8000 組 validation 比較 top-1、MRR 與「正確候選是否留在前 3」。
3. **不要 batching。** 一次呼叫 5 或 8 個候選在兩個平台都更慢；逐一推論反而簡單且可 progressive。
4. **thread 維持 8、spinning 維持關閉。** 這不是瓶頸，也不值得為它改產品設定。
5. **preload。** 不讓日文第一次按鍵付 279 ms 的冷啟動。

還有一個未量測的選項：這台 x86 沒有 VNNI，int8 動態量化不一定比 fp32 快。manifest 有 fp32 檔但不在這台機器；建議在 x86 上補量 fp32，再決定 Windows 要出哪一個格式。這是格式選擇，不是 ranking 改變。

## 11. 重現

```sh
MODEL=/home/fedora/.opencode/plan/0c-ja/phase2/reference/models/trained/reranker-public-core/reranker-public-core.int8.onnx
PY=/home/fedora/.venvs/p2-63/bin/python

$PY scripts/perf/ja_reranker_perf.py orchestrate \
  --model "$MODEL" \
  --fixture scripts/perf/fixtures/p2-63-groups.npz \
  --out-dir scripts/perf/results \
  --passes 3 --thread-passes 2 --shape-passes 2 --cold-repeats 5 \
  --final-config t8 --shape-threads 8

$PY scripts/perf/ja_reranker_perf.py soak \
  --model "$MODEL" \
  --fixture scripts/perf/fixtures/p2-63-groups.npz \
  --threads 8 --minutes 30 --period-ms 200 \
  --json scripts/perf/results/soak.json \
  --samples scripts/perf/results/soak.jsonl

$PY scripts/perf/assemble_results.py
$PY scripts/perf/report_tables.py
```

venv 在 repo 外：`uv venv --python 3.13 /home/fedora/.venvs/p2-63`，再安裝 `onnxruntime==1.32.0 numpy`。模型與 `.onnx` 不進 Git。

## 12. 限制

- WSL2 上的 x86，不是 Windows 原生；P2-65 的 Windows 實機驗收要重跑同一支 harness。
- 沒有人工輸入的突發節奏；soak 是固定 200 ms 的壓力情境。
- 合成 fixture 固定 shape 與 token 分布，沒有涵蓋 homophone-v1 的候選長度變化；seq 244 的 candidate 是單 token，真實候選可能更長。
- 冷啟動是暖 page cache；沒有 root 無法清 page cache。
- RSS 包含 CPython、numpy 與 fixture；產品的 native 常駐會更低。
- 沒有在 DGX 上跑 profiler，int8 kernel 差異是根據指令集與量測結果的推論。
- 量測期間 WSL 重啟過一次；headline 與 soak 在重啟前後分別完成，同設定重跑差異約 5%。
- 品質（seq 縮短、top 3）未量，這是 P2-67 的工作。
