#!/usr/bin/env python3
"""P2-30C parity oracle for the real Chinese select model.

This runs the shipping Python worker implementation (platforms/neural/runtime/
sequence_runtime.py) on the cases produced by select_cases_test.go. It records
the exact tensors the worker's prepare() assembles, the worker's scores, the
full candidate ranking, the chosen candidate and per-case latency.

It is a test oracle, not part of the product path. ORT telemetry is disabled
before importing onnxruntime, matching the worker host.
"""
import time

PROCESS_START = time.perf_counter()

import argparse
import importlib.util
import json
import os
import statistics
import sys

os.environ['ORT_DISABLE_TELEMETRY'] = '1'
os.environ['ORT_RUNNING_UNIT_TESTS'] = '1'

import numpy as np
import onnxruntime as ort

IMPORT_MS = (time.perf_counter() - PROCESS_START) * 1000.0


def load_worker(path):
    spec = importlib.util.spec_from_file_location('sequence_runtime', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def arrays_of(arr):
    return {
        'ids': arr['ids'][0].tolist(),
        'bases': arr['bases'][0].tolist(),
        'tones': arr['tones'][0].tolist(),
        'types': arr['types'][0].tolist(),
        'positions': arr['positions'][0].tolist(),
        'mask': arr['mask'][0][0].tolist(),
        'proj_pos': arr['proj_pos'].tolist(),
        'target_node': arr['target_node'].tolist(),
        'target_ids': arr['target_ids'].tolist(),
    }


def rank(candidates, scores, weight, margin):
    """Mirror core/rerank.Choose plus a full stable ordering."""
    values = [float(c['log_score']) + weight * float(s) for c, s in zip(candidates, scores)]
    order = sorted(range(len(values)), key=lambda i: (-values[i], i))
    best = 0
    for i in range(1, len(values)):
        if values[i] > values[best]:
            best = i
    chosen = best if values[best] - values[0] > margin else 0
    return order, chosen, values


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--sequence-runtime', required=True)
    ap.add_argument('--package', required=True)
    ap.add_argument('--cases', required=True)
    ap.add_argument('--threads', type=int, default=2)
    ap.add_argument('--out', required=True)
    args = ap.parse_args()

    cases = json.load(open(args.cases, encoding='utf-8'))
    weight = float(cases['weight'])
    margin = float(cases['margin'])
    dictionary = cases['dictionary_sha256']

    worker = load_worker(args.sequence_runtime)

    # Wrap prepare() so the oracle records the exact tensors the worker uses.
    recorded = {}
    original_prepare = worker.prepare

    def recording_prepare(*a, **kw):
        out = original_prepare(*a, **kw)
        recorded['arrays'] = arrays_of(out)
        return out

    worker.prepare = recording_prepare

    runtime = worker.Runtime(args.package, dictionary, args.threads)
    init_ms = (time.perf_counter() - PROCESS_START) * 1000.0

    results = []
    warm_ms = []
    for i, case in enumerate(cases['cases']):
        req = case['request']
        body = {
            'id': i + 1,
            'revision': 1,
            'dictionary_sha256': dictionary,
            'model_sha256': runtime.model_sha,
            'context': req.get('context', ''),
            'reading': req['reading'],
            'candidates': req['candidates'],
            'allow_partial': req.get('allow_partial', False),
        }
        if req.get('mode'):
            body['mode'] = req['mode']
        recorded.clear()
        started = time.perf_counter()
        try:
            scores = runtime.score(body)
            elapsed = (time.perf_counter() - started) * 1000.0
            order, chosen, values = rank(req['candidates'], scores, weight, margin)
            entry = {
                'name': case['name'],
                'status': 'ok',
                'scores': scores,
                'combined': values,
                'ranking': order,
                'chosen': chosen,
                'chosen_text': req['candidates'][chosen]['text'],
                'ms': elapsed,
                'arrays': recorded.get('arrays'),
            }
            if i > 0:
                warm_ms.append(elapsed)
        except (ValueError, KeyError, TypeError) as e:
            elapsed = (time.perf_counter() - started) * 1000.0
            entry = {'name': case['name'], 'status': 'error', 'error': str(e), 'ms': elapsed}
            if i > 0:
                warm_ms.append(elapsed)
        results.append(entry)

    out = {
        'python': sys.version.split()[0],
        'onnxruntime': ort.__version__,
        'numpy': np.__version__,
        'threads': args.threads,
        'import_ms': IMPORT_MS,
        'init_ms': init_ms,
        'first_case_ms': results[0]['ms'] if results else None,
        'first_case_ok': bool(results and results[0]['status'] == 'ok'),
        'warm_median_ms': statistics.median(warm_ms) if warm_ms else None,
        'warm_mean_ms': statistics.fmean(warm_ms) if warm_ms else None,
        'cases': results,
    }
    with open(args.out, 'w', encoding='utf-8') as f:
        json.dump(out, f)
    print(json.dumps({
        'python': out['python'], 'onnxruntime': out['onnxruntime'],
        'import_ms': round(out['import_ms'], 3), 'init_ms': round(out['init_ms'], 3),
        'first_case_ms': out['first_case_ms'], 'warm_median_ms': out['warm_median_ms'],
        'cases': len(results),
    }))


if __name__ == '__main__':
    main()
