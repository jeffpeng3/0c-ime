"""中文語言標籤 adapter 的單元測試；不需要 numpy、onnxruntime 或模型檔。"""
from __future__ import annotations

import copy
import math
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))
import zh_adapter as zh  # noqa: E402


class FakeRuntime:
    dictionary = 'dict-sha'
    model_sha = 'model-sha'
    allow_partial = False

    def __init__(self, scores=None):
        self.calls = []
        self.scores = scores

    def score(self, request):
        self.calls.append(request)
        if self.scores is not None:
            return self.scores
        return [float(len(candidate['text'])) for candidate in request['candidates']]


def candidate(surface, reading, language='zh', variant='zh-Hant'):
    row = {'surface': surface, 'reading': reading}
    if language is not None:
        row['language'] = {'language': language}
    if variant is not None:
        row['variant'] = variant
    return row


def request(*candidates, context='前文', revision=3):
    return {
        'id': 7,
        'revision': revision,
        'schema': zh.SCHEMA,
        'context': context,
        'candidates': list(candidates),
    }


class SyllableTest(unittest.TestCase):
    def test_valid(self):
        for value in ('ㄐㄧㄣ', 'ㄊㄧㄢ', 'ㄇㄜ˙', 'ㄕ'):
            self.assertTrue(zh.valid_syllable(value), value)

    def test_invalid(self):
        for value in ('', 'ㄐㄧㄣˉ', 'ㄧㄐ', 'き', 'ㄅㄅ', 'ㄐ ㄧㄣ', None, 3):
            self.assertFalse(zh.valid_syllable(value), value)

    def test_language_and_variant(self):
        self.assertEqual(zh.language_id({}), 'zh')
        self.assertEqual(zh.language_id({'language': {'language': ''}}), 'zh')
        self.assertEqual(zh.language_id({'language': {'language': 'ja'}}), 'ja')
        self.assertEqual(zh.language_id({'language': 'en'}), 'en')
        self.assertEqual(zh.language_id({'language': 3}), '')
        self.assertEqual(zh.variant_language('zh-Hant'), 'zh')
        self.assertEqual(zh.variant_language('ja-Kyujitai'), 'ja')
        self.assertEqual(zh.variant_language(None), '')


class ScoreTest(unittest.TestCase):
    def test_converts_and_scores(self):
        runtime = FakeRuntime()
        req = request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('巾田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
        )
        self.assertEqual(zh.score_request(runtime, req), [2.0, 2.0, 2.0])
        self.assertEqual(len(runtime.calls), 1)
        legacy = runtime.calls[0]
        self.assertEqual(legacy['reading'], ['ㄐㄧㄣ', 'ㄊㄧㄢ'])
        self.assertEqual(legacy['context'], '前文')
        self.assertEqual(legacy['revision'], 3)
        self.assertEqual(legacy['dictionary_sha256'], 'dict-sha')
        self.assertEqual(legacy['model_sha256'], 'model-sha')
        self.assertFalse(legacy['allow_partial'])
        self.assertEqual([row['text'] for row in legacy['candidates']], ['今天', '金田', '巾田'])
        for row in legacy['candidates']:
            self.assertEqual(row['log_score'], 0.0)
            self.assertEqual(row['segments'], [{
                'Word': row['text'], 'Start': 0, 'End': 2, 'Partial': False, 'Correction': False,
            }])

    def test_mixed_languages_keep_order(self):
        runtime = FakeRuntime(scores=[1.0, 2.0])
        req = request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('きょう', ['き', 'ょ', 'う'], language='ja', variant='ja'),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ'], language=None),
            candidate('kubectl', [], language='en', variant='en'),
        )
        self.assertEqual(zh.score_request(runtime, req), [1.0, 0.0, 2.0, 0.0])
        self.assertEqual(len(runtime.calls), 1)
        self.assertEqual([row['text'] for row in runtime.calls[0]['candidates']], ['今天', '金田'])

    def test_separate_readings(self):
        runtime = FakeRuntime(scores=[1.0, 2.0])
        req = request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('明天', ['ㄇㄧㄥˊ', 'ㄊㄧㄢ']),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('名天', ['ㄇㄧㄥˊ', 'ㄊㄧㄢ']),
        )
        self.assertEqual(zh.score_request(runtime, req), [1.0, 1.0, 2.0, 2.0])
        self.assertEqual(len(runtime.calls), 2)

    def test_duplicate_surface_scored_once(self):
        runtime = FakeRuntime(scores=[7.0, 8.0])
        req = request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
        )
        self.assertEqual(zh.score_request(runtime, req), [7.0, 8.0, 7.0])
        self.assertEqual(len(runtime.calls), 1)
        self.assertEqual(len(runtime.calls[0]['candidates']), 2)

    def test_context_truncated(self):
        runtime = FakeRuntime()
        req = request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            context='文' * 70,
        )
        zh.score_request(runtime, req)
        self.assertEqual(runtime.calls[0]['context'], '文' * zh.MAX_CONTEXT)

    def test_does_not_mutate_request(self):
        req = request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
        )
        before = copy.deepcopy(req)
        zh.score_request(FakeRuntime(), req)
        self.assertEqual(req, before)

    def test_unsupported(self):
        cases = {
            'no chinese': request(candidate('きょう', ['き', 'ょ', 'う'], language='ja', variant='ja')),
            'single candidate': request(candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ'])),
            'kana reading': request(
                candidate('きょう', ['き', 'ょ', 'う'], language=None, variant='zh-Hant'),
                candidate('きよう', ['き', 'よ', 'う'], language=None, variant='zh-Hant'),
            ),
            'wrong length': request(
                candidate('今天', ['ㄐㄧㄣ']),
                candidate('金田', ['ㄐㄧㄣ']),
            ),
            'variant language': request(
                candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ'], language='zh', variant='ja'),
                candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ'], language='zh', variant='ja'),
            ),
        }
        for name, req in cases.items():
            with self.subTest(name=name):
                with self.assertRaises(zh.Unsupported):
                    zh.score_request(FakeRuntime(), req)

    def test_schema_required(self):
        req = request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
        )
        req['schema'] = 'other'
        with self.assertRaises(zh.Unsupported) as caught:
            zh.score_request(FakeRuntime(), req)
        self.assertEqual(caught.exception.code, 'unsupported_schema')

    def test_model_reply_validation(self):
        req = request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
        )
        for scores in ([1.0], [1.0, math.nan], [1.0, math.inf], ['1', 2]):
            with self.subTest(scores=scores):
                with self.assertRaises(zh.Unsupported):
                    zh.score_request(FakeRuntime(scores=scores), req)

    def test_too_many_candidates(self):
        rows = []
        for index in range(zh.MAX_CANDIDATES + 1):
            rows.append(candidate(chr(0x4e00 + index) + '天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']))
        with self.assertRaises(zh.Unsupported) as caught:
            zh.score_request(FakeRuntime(), request(*rows))
        self.assertEqual(caught.exception.code, 'too_many_candidates')

    def test_adapter_response(self):
        adapter = zh.Adapter.__new__(zh.Adapter)
        adapter.runtime = FakeRuntime()
        adapter.dictionary = 'dict-sha'
        adapter.model_sha = 'model-sha'
        reply = adapter.respond(request(
            candidate('今天', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
            candidate('金田', ['ㄐㄧㄣ', 'ㄊㄧㄢ']),
        ))
        self.assertEqual(reply['id'], 7)
        self.assertEqual(reply['revision'], 3)
        self.assertEqual(reply['schema'], zh.SCHEMA)
        self.assertEqual(reply['scores'], [2.0, 2.0])


if __name__ == '__main__':
    unittest.main()
