#!/usr/bin/env python3
"""Python numpy+onnxruntime latency reference for the P2-30S spike.

Mirrors the Go probe's session configuration (sequential execution, one inter-op
thread, intra-op threads from --threads, spinning disabled) so the Go numbers
can be compared against the Python path on the same machine.

Reference only; not part of the product.
"""
import argparse
import json
import statistics
import time

import numpy as np
import onnxruntime as ort


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--onnx', required=True)
    ap.add_argument('--seq', type=int, default=32)
    ap.add_argument('--runs', type=int, default=60)
    ap.add_argument('--warmup', type=int, default=10)
    ap.add_argument('--threads', type=int, default=2)
    ap.add_argument('--out', required=True)
    args = ap.parse_args()

    options = ort.SessionOptions()
    options.intra_op_num_threads = args.threads
    options.inter_op_num_threads = 1
    options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    options.add_session_config_entry('session.intra_op.allow_spinning', '0')

    t_load = time.perf_counter()
    session = ort.InferenceSession(args.onnx, sess_options=options,
                                   providers=['CPUExecutionProvider'])
    load_ms = (time.perf_counter() - t_load) * 1000

    ids = np.arange(args.seq, dtype=np.int64)[None, :] % 14080
    mask = np.ones((1, args.seq), dtype=np.int64)
    feed = {'input_ids': ids, 'attention_mask': mask}

    t_first = time.perf_counter()
    out = session.run(None, feed)[0]
    first_ms = (time.perf_counter() - t_first) * 1000
    for _ in range(args.warmup):
        session.run(None, feed)

    lat = []
    for _ in range(args.runs):
        t = time.perf_counter()
        session.run(None, feed)
        lat.append((time.perf_counter() - t) * 1000)

    lat.sort()
    result = {
        'onnx': args.onnx,
        'onnxruntime_version': ort.__version__,
        'numpy_version': np.__version__,
        'seq': args.seq,
        'threads': args.threads,
        'runs': args.runs,
        'load_ms': load_ms,
        'first_inference_ms': first_ms,
        'min_ms': lat[0],
        'median_ms': statistics.median(lat),
        'p95_ms': lat[int(0.95 * len(lat))],
        'max_ms': lat[-1],
        'score': [float(x) for x in out],
        'latencies_ms': lat,
    }
    with open(args.out, 'w', encoding='utf-8') as f:
        json.dump(result, f, indent=2)
    print(json.dumps({k: (round(v, 3) if isinstance(v, float) else v)
                      for k, v in result.items() if k != 'latencies_ms'}))


if __name__ == '__main__':
    main()
