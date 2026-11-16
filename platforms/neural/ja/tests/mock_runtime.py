#!/usr/bin/env python3
"""T14 協定的 mock worker：不需要 llama.cpp 或模型，給 T30 開發用。

它接受與 ja_runtime.py 完全相同的 stdin/stdout 協定，回傳可預測的分數與
前綴約束：

  * prefix 是 draft 的表記（沒有 draft 時用第一個候選）。
  * candidate 以 prefix 開頭才算 prefix_ok / passed。
  * score 是 -表記長度（越短越高），只用來確認整合順序，不代表品質。

`--delay-ms` 可模擬慢模型，驗證宿主逾時不會卡住 session。這個檔案不進
正式安裝；T30 測試把它接到 JaConfig.Script 即可。
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys
import time

MAX_FRAME = 65536
SCHEMA = 'ja-zenz-verify-v1'
CONTEXT_POLICY = 'confirmed_left_128'


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--llama-server', default='')
    parser.add_argument('--threads', type=int, default=4)
    parser.add_argument('--timeout-ms', type=int, default=100)
    parser.add_argument('--model-sha256', default='')
    parser.add_argument('--backend-commit', default='')
    parser.add_argument('--delay-ms', type=int, default=0)
    args = parser.parse_args()
    manifest = json.loads((args.package / 'manifest.json').read_text(encoding='utf-8'))
    print(json.dumps({
        'ready': True,
        'schema': SCHEMA,
        'backend': 'llama.cpp',
        'backend_commit': manifest.get('backend_commit', 'mock'),
        'context_policy': CONTEXT_POLICY,
        'model_sha256': manifest['model']['sha256'],
        'limits': manifest['limits'],
    }), flush=True)
    for line in sys.stdin.buffer:
        if len(line) > MAX_FRAME or not line.endswith(b'\n'):
            return 1
        request = json.loads(line)
        reply = {
            'id': request.get('id'),
            'revision': request.get('revision'),
            'schema': SCHEMA,
            'model_sha256': manifest['model']['sha256'],
        }
        if args.delay_ms:
            time.sleep(args.delay_ms / 1000)
        draft = request.get('draft')
        prefix = draft['surface'] if isinstance(draft, dict) else \
            (request['candidates'][0]['surface'] if request.get('candidates') else '')
        clauses = draft.get('clauses', []) if isinstance(draft, dict) else []
        fixed_clauses = 0
        fixed_units = 0
        for clause in clauses:
            if prefix.startswith(clause.get('surface', '')):
                fixed_clauses += 1
                fixed_units += len(clause.get('reading', []))
        verdicts = []
        for candidate in request.get('candidates', []):
            surface = candidate['surface']
            ok = surface.startswith(prefix)
            verdicts.append({
                'surface': surface,
                'logprob': -float(len(surface)),
                'score': -float(len(surface)),
                'prefix': prefix if not ok else surface,
                'passed': ok and surface == prefix,
                'prefix_ok': ok,
            })
        reply.update({
            'prefix': prefix,
            'fixed_clauses': fixed_clauses,
            'fixed_reading_units': fixed_units,
            'draft': {'logprob': -float(len(prefix)), 'score': -float(len(prefix)),
                      'prefix': prefix, 'passed': True, 'prefix_ok': True} if draft else None,
            'candidates': verdicts,
        })
        print(json.dumps(reply, ensure_ascii=False), flush=True)
    return 0


if __name__ == '__main__':
    sys.exit(main())
