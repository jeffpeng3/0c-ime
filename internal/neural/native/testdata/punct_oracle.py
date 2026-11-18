#!/usr/bin/env python3
"""Reference outputs for the native punctuation parity test.

This is a test-only oracle. It uses the exported 0c.model.manifest/1 model,
the public-core SentencePiece model and ONNX Runtime to produce:

- one entry per packed window (token ids and raw per-position argmax),
- one entry per input text (merged decisions, edits and inserted text).

The runtime contract is deliberately identical to internal/neural/native:
SentencePiece ids, 256-token windows with 128 stride, pad_id 0 with
attention_mask 0, argmax over none/comma/period/question, and a mark appended
after the labelled token.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sys

os.environ['ORT_DISABLE_TELEMETRY'] = '1'
os.environ['ORT_RUNNING_UNIT_TESTS'] = '1'

import numpy as np
import onnxruntime as ort


def sha256(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def resolve(path, manifest_dir):
    p = Path(path)
    if p.is_absolute():
        return p
    for base in (manifest_dir, manifest_dir.parent):
        candidate = base / p
        if candidate.is_file():
            return candidate
    raise FileNotFoundError(path)


def window_starts(n, window, stride):
    if n <= 0:
        return []
    if n <= window:
        return [0]
    return list(range(0, n - window, stride)) + [n - window]


def merge(rows, starts, n, window):
    out, best = [None] * n, [-1] * n
    for wi, s in enumerate(starts):
        m = len(rows[wi])
        for j in range(min(m, n - s)):
            margin = min(j, m - 1 - j) if n > window else 0
            if margin > best[s + j]:
                best[s + j] = margin
                out[s + j] = rows[wi][j]
    return np.stack(out)


def inserted(text, ids, offsets, surfaces, decisions, marks):
    seen = set()
    edits = []
    for i, label in enumerate(decisions):
        if label <= 0 or label >= len(marks) or i >= len(offsets):
            continue
        surface = surfaces[i].replace('\u2581', ' ').strip()
        if not surface:
            continue
        offset = offsets[i][1]
        if offset <= 0 or offset in seen:
            continue
        seen.add(offset)
        edits.append({'offset': offset, 'text': marks[label]})
    runes = list(text)
    out = []
    at = 0
    for i in range(len(runes) + 1):
        while at < len(edits) and edits[at]['offset'] == i:
            out.append(edits[at]['text'])
            at += 1
        if i < len(runes):
            out.append(runes[i])
    return edits, ''.join(out)


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--manifest', type=Path, required=True)
    p.add_argument('--tokenizer', type=Path, required=True)
    p.add_argument('--texts', type=Path, required=True)
    p.add_argument('--limit', type=int, default=240)
    p.add_argument('--window', type=int, default=256)
    p.add_argument('--stride', type=int, default=128)
    p.add_argument('--threads', type=int, default=2)
    p.add_argument('--out', type=Path, required=True)
    a = p.parse_args()

    manifest = json.loads(a.manifest.read_text(encoding='utf-8'))
    if manifest.get('schema') != '0c.model.manifest/1' or manifest.get('kind') != 'punctuation':
        raise ValueError('manifest_contract_mismatch')
    labels = manifest['architecture']['labels']
    marks = manifest['architecture']['marks']
    if labels != ['none', 'comma', 'period', 'question'] or len(marks) != 4:
        raise ValueError('label_contract_mismatch')
    entry = manifest['files'].get('int8') or manifest['files']['fp32']
    model = resolve(entry['path'], a.manifest.parent)
    if entry.get('bytes') and model.stat().st_size != entry['bytes']:
        raise ValueError('model_size_mismatch')
    if entry.get('sha256') and sha256(model) != entry['sha256']:
        raise ValueError('model_hash_mismatch')
    tokenizer = a.tokenizer
    if not tokenizer.is_file():
        raise FileNotFoundError(tokenizer)

    import sentencepiece as spm
    sp = spm.SentencePieceProcessor(model_file=str(tokenizer))
    options = ort.SessionOptions()
    options.intra_op_num_threads = a.threads
    options.inter_op_num_threads = 1
    options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    options.add_session_config_entry('session.intra_op.allow_spinning', '0')
    session = ort.InferenceSession(str(model), sess_options=options, providers=['CPUExecutionProvider'])

    lines = a.texts.read_text(encoding='utf-8').splitlines()
    if not lines:
        raise ValueError('empty_texts')
    windows, texts = [], []
    for line_no, text in enumerate(lines[:a.limit]):
        if not text:
            continue
        om = sp.encode_as_offset_mapping(text)
        ids = list(om['ids'])
        offsets = list(om['offsets'])
        surfaces = list(om['pieces'])
        if not ids:
            continue
        starts = window_starts(len(ids), a.window, a.stride)
        rows = []
        for start in starts:
            packed = np.zeros((1, a.window), dtype=np.int64)
            mask = np.zeros((1, a.window), dtype=np.int64)
            chunk = ids[start:start + a.window]
            packed[0, :len(chunk)] = chunk
            mask[0, :len(chunk)] = 1
            logits = session.run(['logits'], {'input_ids': packed, 'attention_mask': mask})[0][0]
            rows.append(logits)
            windows.append({
                'line': line_no,
                'start': start,
                'ids': chunk,
                'decisions': [int(x) for x in np.argmax(logits[:len(chunk)], axis=-1)],
            })
        merged = merge(rows, starts, len(ids), a.window)
        decisions = [int(x) for x in np.argmax(merged, axis=-1)]
        edits, out = inserted(text, ids, offsets, surfaces, decisions, marks)
        texts.append({
            'line': line_no,
            'ids': ids,
            'decisions': decisions,
            'edits': edits,
            'text': out,
        })

    a.out.write_text(json.dumps({
        'schema': '0c.punct-oracle/1',
        'model_sha256': sha256(model),
        'tokenizer_sha256': sha256(tokenizer),
        'labels': labels,
        'window': a.window,
        'stride': a.stride,
        'windows': windows,
        'texts': texts,
    }, ensure_ascii=False), encoding='utf-8')


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        print(json.dumps({'ready': False, 'error': str(exc)}, ensure_ascii=False), file=sys.stderr)
        raise SystemExit(2)
