#!/usr/bin/env python3
"""P2-25P placeholder ONNX 的載入與推論探測（開發／驗證用，不進正式安裝包）。

讀取 0c.model.manifest/1，核對 int8（或 fp32）檔案的大小與 SHA-256，用
ONNX Runtime CPU 載入，送一組固定 token id，回報載入時間、第一次推論時間、
輸出形狀與 provider。placeholder 旗標會原樣輸出，呼叫端不得把它當成已訓練
模型。

輸出是一行 JSON；需要 numpy 與 onnxruntime。GPU execution provider 只在使用者
以 ZUIN_ORT_PROVIDERS 指定且實際可用時加入，否則一律 CPU；建立 session 失敗
也會回退 CPU。
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import statistics
import sys
import time

os.environ['ORT_DISABLE_TELEMETRY'] = '1'
os.environ['ORT_RUNNING_UNIT_TESTS'] = '1'

SCHEMA = '0c.model.manifest/1'


def sha256(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def resolve(path, manifest_dir, root):
    p = Path(path)
    if p.is_absolute():
        return p
    for base in (manifest_dir, manifest_dir.parent, Path(root) if root else None):
        if base is None:
            continue
        candidate = base / p
        if candidate.is_file():
            return candidate
    raise FileNotFoundError(path)


def providers(ort):
    requested = [x.strip() for x in os.environ.get('ZUIN_ORT_PROVIDERS', '').split(',') if x.strip()]
    available = set(ort.get_available_providers())
    chosen = [p for p in requested if p in available]
    if 'CPUExecutionProvider' not in chosen:
        chosen.append('CPUExecutionProvider')
    return chosen


def create_session(ort, path, options):
    chosen = providers(ort)
    try:
        return ort.InferenceSession(str(path), sess_options=options, providers=chosen)
    except Exception:
        if chosen == ['CPUExecutionProvider']:
            raise
        return ort.InferenceSession(str(path), sess_options=options, providers=['CPUExecutionProvider'])


def main():
    process_started = time.perf_counter()
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--manifest', type=Path, required=True)
    p.add_argument('--artifact-root', type=Path, default=None)
    p.add_argument('--format', default='int8')
    p.add_argument('--threads', type=int, default=1)
    p.add_argument('--runs', type=int, default=5)
    p.add_argument('--seq', type=int, default=32)
    a = p.parse_args()
    if not 1 <= a.threads <= 8 or a.runs < 1 or not 2 <= a.seq <= 512:
        p.error('threads 1-8, runs >= 1, seq 2-512')

    manifest = json.loads(a.manifest.read_text())
    if manifest.get('schema') != SCHEMA or manifest.get('kind') not in ('reranker', 'punctuation'):
        raise ValueError('manifest_contract_mismatch')
    fmt = a.format if a.format in manifest.get('files', {}) else ('fp32' if 'fp32' in manifest.get('files', {}) else None)
    if fmt is None:
        raise ValueError('no_model_file')
    entry = manifest['files'][fmt]
    path = resolve(entry['path'], a.manifest.parent, a.artifact_root)
    size = path.stat().st_size
    if entry.get('bytes') and size != entry['bytes']:
        raise ValueError('model_size_mismatch')
    digest = sha256(path)
    if entry.get('sha256') and digest != entry['sha256']:
        raise ValueError('model_hash_mismatch')

    import numpy as np
    import onnxruntime as ort
    if hasattr(ort, 'disable_telemetry_events'):
        ort.disable_telemetry_events()

    options = ort.SessionOptions()
    options.intra_op_num_threads = a.threads
    options.inter_op_num_threads = 1
    options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    options.add_session_config_entry('session.intra_op.allow_spinning', '0')
    started = time.perf_counter()
    session = create_session(ort, path, options)
    load_ms = (time.perf_counter() - started) * 1000.0

    ids = np.arange(1, a.seq + 1, dtype=np.int64).reshape(1, -1)
    ids %= max(2, int(manifest.get('architecture', {}).get('vocab_size', 14080)))
    mask = np.ones_like(ids)
    outputs = [x['name'] for x in manifest.get('io', {}).get('outputs', [])]
    if not outputs:
        outputs = [session.get_outputs()[0].name]

    def infer():
        return session.run(outputs, {'input_ids': ids, 'attention_mask': mask})[0]

    first = time.perf_counter()
    result = infer()
    first_ms = (time.perf_counter() - first) * 1000.0
    ready_ms = (time.perf_counter() - process_started) * 1000.0
    if not np.isfinite(result).all():
        raise ValueError('non_finite_output')
    kind = manifest['kind']
    expected = (1,) if kind == 'reranker' else (1, a.seq, 4)
    if tuple(result.shape) != expected:
        raise ValueError(f'output_shape {tuple(result.shape)} != {expected}')
    timings = []
    for _ in range(a.runs - 1):
        t = time.perf_counter()
        infer()
        timings.append((time.perf_counter() - t) * 1000.0)
    median_ms = statistics.median(timings) if timings else first_ms
    print(json.dumps({
        'ready': True,
        'schema': SCHEMA,
        'kind': kind,
        'artifact_id': manifest.get('artifact_id'),
        'variant': manifest.get('variant'),
        'format': fmt,
        'placeholder': bool(manifest.get('placeholder', False)),
        'quality': manifest.get('quality'),
        'production_eligible': bool(manifest.get('production_eligible', False)),
        'sha256': digest,
        'bytes': size,
        'provider': session.get_providers(),
        'load_ms': round(load_ms, 3),
        'first_inference_ms': round(first_ms, 3),
        'ready_ms': round(ready_ms, 3),
        'median_ms': round(median_ms, 3),
        'output_name': outputs[0],
        'output_shape': list(result.shape),
    }, ensure_ascii=False))


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        print(json.dumps({'ready': False, 'error': str(exc)}, ensure_ascii=False), file=sys.stderr)
        raise SystemExit(2)
