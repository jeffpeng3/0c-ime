#!/usr/bin/env python3
"""P2-30N parity oracle: Python input assembly + int8 placeholder inference.

The script imports prepare() from an archived Python reference implementation,
builds requests from real Japanese lines, records every assembled tensor, runs
the same int8 placeholder model, and also tokenizes the lines with Python
sentencepiece. The Go parity test feeds the recorded requests to the Go port
and the native ONNX backend.

This is a development-only test oracle; it is not part of the product path and
the repository ships no Python worker.
"""
import argparse
import importlib.util
import json
import time

import numpy as np
import onnxruntime as ort
import sentencepiece as spm


def load_worker(path):
    spec = importlib.util.spec_from_file_location('sequence_runtime', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--sequence-runtime', required=True)
    ap.add_argument('--spm', required=True)
    ap.add_argument('--onnx', required=True)
    ap.add_argument('--texts', required=True)
    ap.add_argument('--limit', type=int, default=200)
    ap.add_argument('--seq', type=int, default=32)
    ap.add_argument('--threads', type=int, default=2)
    ap.add_argument('--vocab-cap', type=int, default=14080)
    ap.add_argument('--out', required=True)
    args = ap.parse_args()

    worker = load_worker(args.sequence_runtime)
    proc = spm.SentencePieceProcessor(model_file=args.spm)
    with open(args.texts, encoding='utf-8') as f:
        texts = [line.rstrip('\n').rstrip('\r') for line in f][:args.limit]

    bases = ['a', 'e', 'i', 'o', 'u']
    cases = []
    characters = set()
    for i, line in enumerate(texts):
        runes = list(line)
        if len(runes) < 6:
            continue
        n = 2 + (i % 3)
        if len(runes) < n + 3:
            continue
        picks = []
        for off in range(0, len(runes) - n + 1):
            text = ''.join(runes[off:off + n])
            if text not in picks:
                picks.append(text)
            if len(picks) == 3:
                break
        if len(picks) < 2:
            continue
        characters.update(''.join(picks))
        context = texts[i - 1] if i > 0 else ''.join(runes[:min(64, len(runes))])
        reading = [bases[(i + j) % len(bases)] for j in range(n)]
        tone_known = None
        allow_unknown = False
        if i % 7 == 0:
            tone_known = [True] * n
            tone_known[(i // 7) % n] = False
            allow_unknown = True
        candidates = [
            {'text': text, 'log_score': 0.0,
             'segments': [{'Start': 0, 'End': n, 'Word': text,
                           'Partial': False, 'Correction': False}]}
            for text in picks
        ]
        cases.append({
            'line_index': i,
            'request': {
                'context': context, 'reading': reading, 'candidates': candidates,
                'allow_partial': False, 'text_only': False,
                'neutral_last': i % 4 == 0,
                'tone_known': tone_known,
                'tone_known_set': tone_known is not None,
                'allow_unknown_tones': allow_unknown,
            },
            'spm_ids': proc.encode(line),
        })

    characters = sorted(characters)
    maps = ({x: i + 2 for i, x in enumerate(bases)},
            {x: i + 8 for i, x in enumerate(characters)})

    options = ort.SessionOptions()
    options.intra_op_num_threads = args.threads
    options.inter_op_num_threads = 1
    options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    options.add_session_config_entry('session.intra_op.allow_spinning', '0')
    session = ort.InferenceSession(args.onnx, sess_options=options,
                                   providers=['CPUExecutionProvider'])

    started = time.perf_counter()
    for case in cases:
        req = case['request']
        arr = worker.prepare(req['context'], req['reading'], req['candidates'], maps,
                             allow_partial=req['allow_partial'],
                             text_only=req['text_only'],
                             neutral_last=req['neutral_last'],
                             tone_known=req['tone_known'],
                             allow_unknown_tones=req['allow_unknown_tones'])
        ids = arr['ids'][0].tolist()
        seq = ids[:args.seq]
        if args.vocab_cap > 0:
            seq = [x % args.vocab_cap for x in seq]
        pad = [0] * (args.seq - len(seq))
        input_ids = np.array([seq + pad], dtype=np.int64)
        mask = np.array([[1] * len(seq) + [0] * len(pad)], dtype=np.int64)
        out = session.run(None, {'input_ids': input_ids,
                                 'attention_mask': mask})[0]
        case['arrays'] = {
            'ids': ids,
            'bases': arr['bases'][0].tolist(),
            'tones': arr['tones'][0].tolist(),
            'types': arr['types'][0].tolist(),
            'positions': arr['positions'][0].tolist(),
            'mask': arr['mask'][0][0].tolist(),
            'proj_pos': arr['proj_pos'].tolist(),
            'target_node': arr['target_node'].tolist(),
            'target_ids': arr['target_ids'].tolist(),
        }
        case['flat_ids'] = seq
        case['score'] = float(out[0])
    elapsed = time.perf_counter() - started

    result = {
        'bases': bases,
        'characters': characters,
        'texts': len(texts),
        'cases': len(cases),
        'seq': args.seq,
        'vocab_cap': args.vocab_cap,
        'python_latency_ms_total': elapsed * 1000,
        'python_latency_ms_per_case': elapsed * 1000 / max(1, len(cases)),
        'cases_data': cases,
    }
    with open(args.out, 'w', encoding='utf-8') as f:
        json.dump(result, f, ensure_ascii=False)
    print(json.dumps({'texts': len(texts), 'cases': len(cases),
                      'latency_ms_per_case': result['python_latency_ms_per_case']}))


if __name__ == '__main__':
    main()
