#!/usr/bin/env python3
"""中文模型的語言標籤 adapter（不改權重）。

這個 worker 把多語言候選請求轉成既有 `0c-sequence-d512` 的 sequence_runtime
請求，再把分數放回原本的候選順序。模型檔、vocab、manifest 與配對都不變；
adapter 不訓練、不重新匯出，也不改寫權重。

非中文候選保留中性分數 0。完全沒有可評分的中文候選、候選形狀不支援或模型
回報錯誤時，回覆 `fallback`，呼叫端保留統計轉換結果。

請求（stdin，一行一個 JSON，最多 64 KiB）：

    {"id": 1, "revision": 3, "schema": "zh-language-adapter-v1",
     "context": "前文",
     "candidates": [
       {"surface": "今天", "reading": ["ㄐㄧㄣ", "ㄊㄧㄢ"],
        "language": {"language": "zh", "confidence": 0.9}, "variant": "zh-Hant"}
     ]}

回覆：

    {"id": 1, "revision": 3, "schema": "zh-language-adapter-v1", "scores": [..]}

或

    {"id": 1, "revision": 3, "schema": "zh-language-adapter-v1",
     "fallback": "unsupported_request"}
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import math
from pathlib import Path
import sys
import unicodedata

SCHEMA = 'zh-language-adapter-v1'
MAX_FRAME = 65536
MAX_CONTEXT = 64
MAX_CANDIDATES = 16
NEUTRAL_SCORE = 0.0

_INITIALS = set('ㄅㄆㄇㄈㄉㄊㄋㄌㄍㄎㄏㄐㄑㄒㄓㄔㄕㄖㄗㄘㄙ')
_MEDIALS = set('ㄧㄨㄩ')
_FINALS = set('ㄚㄛㄜㄝㄞㄟㄠㄡㄢㄣㄤㄥㄦ')
_TONES = set('ˉˊˇˋ˙')


class Unsupported(ValueError):
    """請求無法轉成既有中文模型可接受的形式。"""

    def __init__(self, code='unsupported_request'):
        super().__init__(code)
        self.code = code


def valid_syllable(value) -> bool:
    """回報字串是否為結構合法的注音音節（同 core/zhuyin.Syllable）。"""
    if not isinstance(value, str) or not value:
        return False
    last = 0
    phonetic = False
    for ch in value:
        if ch in _INITIALS:
            cls = 1
        elif ch in _MEDIALS:
            cls = 2
        elif ch in _FINALS:
            cls = 3
        elif ch in _TONES:
            cls = 4
        else:
            return False
        if cls <= last or ch == 'ˉ':
            return False
        if cls != 4:
            phonetic = True
        last = cls
    return phonetic


def language_id(candidate) -> str:
    """取出候選語言 ID；省略或空值沿用舊呼叫端，視為中文。"""
    value = candidate.get('language') if isinstance(candidate, dict) else None
    if isinstance(value, dict):
        value = value.get('language')
    if value is None:
        return 'zh'
    if not isinstance(value, str):
        return ''
    return value or 'zh'


def variant_language(variant) -> str:
    if not isinstance(variant, str) or not variant:
        return ''
    return variant.split('-', 1)[0]


def candidate_reading(candidate):
    """回傳候選的注音 tuple；不是可送中文模型的候選時回傳 None。"""
    if not isinstance(candidate, dict):
        return None
    if language_id(candidate) != 'zh':
        return None
    variant = candidate.get('variant')
    if variant and variant_language(variant) != 'zh':
        return None
    surface = candidate.get('surface')
    reading = candidate.get('reading')
    if not isinstance(surface, str) or not surface:
        return None
    if not isinstance(reading, list) or not reading:
        return None
    if len(reading) != len(surface):
        return None
    if any(unicodedata.category(ch) == 'Cc' for ch in surface):
        return None
    if not all(valid_syllable(unit) for unit in reading):
        return None
    return tuple(reading)


def truncate_context(context) -> str:
    if not isinstance(context, str):
        raise Unsupported('invalid_context')
    if len(context) <= MAX_CONTEXT:
        return context
    return context[-MAX_CONTEXT:]


def groups(candidates):
    """依讀音分組；同詞面只留一次，indexes 記回所有原始位置。"""
    if not isinstance(candidates, list):
        raise Unsupported('invalid_candidates')
    ordered = []
    by_reading = {}
    for index, candidate in enumerate(candidates):
        reading = candidate_reading(candidate)
        if reading is None:
            continue
        key = ' '.join(reading)
        group = by_reading.get(key)
        if group is None:
            group = {'reading': reading, 'unique': [], 'by_surface': {}}
            by_reading[key] = group
            ordered.append(group)
        surface = candidate['surface']
        entry = group['by_surface'].get(surface)
        if entry is None:
            entry = {'score_index': len(group['unique']), 'all': []}
            group['by_surface'][surface] = entry
            group['unique'].append(candidate)
        entry['all'].append(index)
    return ordered


def legacy_request(group, context, dictionary, model, allow_partial, revision=0):
    """把一組中文候選轉成 sequence_runtime 既有請求。"""
    rows = []
    for candidate in group['unique']:
        surface = candidate['surface']
        rows.append({
            'text': surface,
            'log_score': 0.0,
            'segments': [{
                'Word': surface,
                'Start': 0,
                'End': len(surface),
                'Partial': False,
                'Correction': False,
            }],
        })
    return {
        'revision': revision,
        'context': context,
        'reading': list(group['reading']),
        'candidates': rows,
        'allow_partial': bool(allow_partial),
        'dictionary_sha256': dictionary,
        'model_sha256': model,
    }


def score_request(runtime, request):
    """回傳與 request['candidates'] 等長的分數；無法評分時丟 Unsupported。"""
    if not isinstance(request, dict) or request.get('schema') != SCHEMA:
        raise Unsupported('unsupported_schema')
    context = truncate_context(request.get('context', ''))
    candidates = request.get('candidates')
    scores = [NEUTRAL_SCORE] * len(candidates) if isinstance(candidates, list) else []
    scored = False
    for group in groups(candidates):
        if len(group['unique']) < 2:
            # 既有中文模型一次至少評兩個候選；單一候選留給統計轉換。
            continue
        if len(group['unique']) > MAX_CANDIDATES:
            raise Unsupported('too_many_candidates')
        legacy = legacy_request(
            group, context,
            getattr(runtime, 'dictionary', ''),
            getattr(runtime, 'model_sha', ''),
            getattr(runtime, 'allow_partial', False),
            request.get('revision', 0),
        )
        raw = runtime.score(legacy)
        if not isinstance(raw, list) or len(raw) != len(group['unique']):
            raise Unsupported('invalid_model_reply')
        if any(not isinstance(value, (int, float)) or not math.isfinite(value) for value in raw):
            raise Unsupported('nonfinite_model_score')
        for i, value in enumerate(raw):
            surface = group['unique'][i]['surface']
            for index in group['by_surface'][surface]['all']:
                scores[index] = float(value)
        scored = True
    if not scored:
        raise Unsupported('no_chinese_candidates')
    return scores


def load_runtime(path):
    """載入既有 sequence_runtime；只在啟動 worker 時呼叫，測試不需要 numpy。"""
    spec = importlib.util.spec_from_file_location('oc_zh_sequence_runtime', path)
    if spec is None or spec.loader is None:
        raise Unsupported('runtime_missing')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class Adapter:
    def __init__(self, package, dictionary, threads=2, runtime_path=None):
        path = Path(runtime_path) if runtime_path else Path(__file__).resolve().parent.parent / 'runtime' / 'sequence_runtime.py'
        self.runtime = load_runtime(path).Runtime(Path(package), dictionary, threads)
        self.dictionary = dictionary
        self.model_sha = self.runtime.model_sha

    def respond(self, request):
        if not isinstance(request, dict) or request.get('schema') != SCHEMA:
            raise Unsupported('unsupported_schema')
        return {
            'id': request.get('id'),
            'revision': request.get('revision', 0),
            'schema': SCHEMA,
            'scores': score_request(self.runtime, request),
        }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--dictionary', required=True)
    parser.add_argument('--threads', type=int, default=2)
    parser.add_argument('--runtime', type=Path, default=None, help='既有 sequence_runtime.py')
    args = parser.parse_args()
    if not 1 <= args.threads <= 8:
        parser.error('threads must be between 1 and 8')
    adapter = Adapter(args.package, args.dictionary, args.threads, args.runtime)
    print(json.dumps({
        'ready': True,
        'schema': SCHEMA,
        'model_sha256': adapter.model_sha,
        'dictionary_sha256': adapter.dictionary,
    }), flush=True)
    while True:
        line = sys.stdin.buffer.readline(MAX_FRAME + 1)
        if not line:
            return
        if len(line) > MAX_FRAME or not line.endswith(b'\n'):
            raise ValueError('request_frame_limit')
        request = json.loads(line)
        reply = {
            'id': request.get('id'),
            'revision': request.get('revision', 0),
            'schema': SCHEMA,
        }
        try:
            reply['scores'] = score_request(adapter.runtime, request)
        except (Unsupported, ValueError, KeyError, TypeError) as error:
            code = error.code if isinstance(error, Unsupported) else 'unsupported_request'
            reply['fallback'] = code
        print(json.dumps(reply, allow_nan=False), flush=True)


if __name__ == '__main__':
    main()
