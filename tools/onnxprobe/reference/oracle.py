#!/usr/bin/env python3
"""Reference oracle for the P2-30S native ONNX spike.

This script is a measurement reference only. It uses Python sentencepiece and
numpy+onnxruntime to produce the token ids and scores that the Go probe is
compared against. Nothing here ships with the product.

Usage:
  python oracle.py --spm public_core_ja.model \
      --onnx reranker-public-core.placeholder.int8.onnx \
      --texts sample-ja.txt --limit 200 --seq 32 --threads 2 --out oracle.json
"""
import argparse
import json

import numpy as np
import onnxruntime as ort
import sentencepiece as spm


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--spm', required=True)
    ap.add_argument('--onnx', required=True)
    ap.add_argument('--texts', required=True)
    ap.add_argument('--limit', type=int, default=200)
    ap.add_argument('--seq', type=int, default=32)
    ap.add_argument('--threads', type=int, default=2)
    ap.add_argument('--vocab-cap', type=int, default=0,
                    help='map ids with id%%cap; needed when the model embedding '
                         'table is smaller than the tokenizer vocab')
    ap.add_argument('--out', required=True)
    args = ap.parse_args()

    proc = spm.SentencePieceProcessor(model_file=args.spm)
    with open(args.texts, encoding='utf-8') as f:
        texts = [line.rstrip('\n').rstrip('\r') for line in f][:args.limit]

    options = ort.SessionOptions()
    options.intra_op_num_threads = args.threads
    options.inter_op_num_threads = 1
    options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    options.add_session_config_entry('session.intra_op.allow_spinning', '0')
    session = ort.InferenceSession(args.onnx, sess_options=options,
                                   providers=['CPUExecutionProvider'])

    tokens = []
    scores = []
    for text in texts:
        ids = proc.encode(text, out_type=int)
        tokens.append(ids)
        seq = ids[:args.seq]
        if args.vocab_cap > 0:
            seq = [i % args.vocab_cap for i in seq]
        pad = [0] * (args.seq - len(seq))
        input_ids = np.array([seq + pad], dtype=np.int64)
        mask = np.array([[1] * len(seq) + [0] * len(pad)], dtype=np.int64)
        out = session.run(None, {'input_ids': input_ids,
                                 'attention_mask': mask})[0]
        scores.append(float(out[0]))

    result = {
        'spm': args.spm,
        'onnx': args.onnx,
        'texts': len(texts),
        'limit': args.limit,
        'seq': args.seq,
        'vocab_cap': args.vocab_cap,
        'sentencepiece_version': spm.__version__,
        'onnxruntime_version': ort.__version__,
        'tokens': tokens,
        'scores': scores,
    }
    with open(args.out, 'w', encoding='utf-8') as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
    print(json.dumps({'texts': len(texts), 'token_count': sum(map(len, tokens)),
                      'score_min': min(scores), 'score_max': max(scores)}))


if __name__ == '__main__':
    main()
