# 日文 reranker 效能 harness（P2-63）

量 `reranker-public-core.int8.onnx` 在 ONNX Runtime CPU 上的冷啟動、每候選／每組延遲、thread 設定、RSS 與 soak。模型與 `.onnx` 不進 Git；路徑用參數傳入。完整結果見 [P2-63-reranker-perf.md](P2-63-reranker-perf.md)。

## 環境

venv 放在 repo 外，避免把 `onnxruntime`、`numpy` 帶進產品邊界：

```sh
uv venv --python 3.13 /home/fedora/.venvs/p2-63
uv pip install --python /home/fedora/.venvs/p2-63/bin/python onnxruntime==1.32.0 numpy
```

`onnxruntime==1.32.0` 與 DGX 量測相同。Python 只用來跑 harness；產品的 Windows 路徑走 native ONNX Runtime，不經過這個 venv。

## 輸入 fixture

`fixtures/p2-63-groups.npz` 是 200 組 × 5 候選、`int64`、seq 244：

```text
<bos> left(120) <eos> candidate <eos> right(120)
```

固定 seed `20261118`，所以每次重跑都是同一批 token id。這台機器沒有 homophone-v1 的 `validation.jsonl`（`/mnt/work/0c/data/...` 不存在），因此用同 shape 的合成輸入；fixture 的 token id 範圍與產生方式記錄在 `fixtures/p2-63-groups.json`。seq 較短的量測由同一支產生器用同 seed 重建，不讀 npz。最短是 seq 124（60+60），因為 pooling 固定讀 index 122，seq 84 會讓 Gather 超出範圍。

重新產生 fixture：

```sh
/home/fedora/.venvs/p2-63/bin/python scripts/perf/ja_reranker_perf.py fixture \
  --fixture scripts/perf/fixtures/p2-63-groups.npz
```

## 量測

```sh
MODEL=/home/fedora/.opencode/plan/0c-ja/phase2/reference/models/trained/reranker-public-core/reranker-public-core.int8.onnx

# 冷啟動 5 次、thread 掃描、seq 244/164/124
/home/fedora/.venvs/p2-63/bin/python scripts/perf/ja_reranker_perf.py orchestrate \
  --model "$MODEL" \
  --fixture scripts/perf/fixtures/p2-63-groups.npz \
  --out-dir scripts/perf/results \
  --passes 3 --thread-passes 2 --shape-passes 2 --cold-repeats 5

# 30 分鐘 soak，每 200 ms 一組，每組 5 個候選逐一推論
/home/fedora/.venvs/p2-63/bin/python scripts/perf/ja_reranker_perf.py soak \
  --model "$MODEL" \
  --fixture scripts/perf/fixtures/p2-63-groups.npz \
  --threads 4 --minutes 30 --period-ms 200 \
  --json scripts/perf/results/soak.json \
  --samples scripts/perf/results/soak.jsonl
```

`orchestrate` 把 `cold.json`、`bench-<config>.json`、`shape-<seq>.json`、`summary.json` 寫到 `--out-dir`。`bench` 的 session options 對齊產品：`inter_op_num_threads=1`、`ORT_SEQUENTIAL`、`session.intra_op.allow_spinning=0`；`t8-spin` 例外，用來量 spinning 的差異。

延遲的百分位用 numpy 的線性插值（`numpy.percentile` 預設）。每次 `bench` 先 warmup，再跑多個 pass；報告取「pass median 最接近所有 pass median 中位數」的那個 pass，跟 DGX manifest 的方法一致。`--individual-only` 只量逐候選推論，給 thread 掃描用；`t4` 跑完整 batch 1/2/3/4/5/8。

RSS 從 `/proc/self/status` 的 `VmRSS`／`VmHWM` 讀取；`bench` 期間另有 25 ms 取樣執行緒記錄峰值。

## 檔案

| 路徑 | 內容 |
|---|---|
| `ja_reranker_perf.py` | harness：fixture、cold、bench、soak、orchestrate |
| `fixtures/p2-63-groups.npz` | 200×5×244 的 `int64` ids 與 mask |
| `fixtures/p2-63-groups.json` | fixture metadata 與 sha256 |
| `results/` | 這台機器的原始 JSON 與 soak JSONL |
| `P2-63-reranker-perf.md` | 量測報告與建議 |

## 已知限制

- 量測機是 WSL2 上的 AMD Ryzen 5 5600X（6 核 12 執行緒），不是 Windows 桌面原生環境，也沒有其他應用程式的真實負載。
- 模型檔已在 OS page cache；沒有 root 無法 `drop_caches`，冷啟動是「新 process、暖 page cache」。
- 合成輸入只固定 shape 與 token 分布，不代表 homophone-v1 的實際候選內容；延遲主要取決於 shape 與 batch，不取決於 token 值。
- soak 的 200 ms 週期包含 5 次推論與 sleep；實際 IME 的使用者節奏會更低或更突發。
