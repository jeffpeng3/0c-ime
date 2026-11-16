"""T14 日文驗證 worker 的單元測試；不啟動 llama.cpp。"""
from __future__ import annotations

import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))
import ja_runtime as ja  # noqa: E402


def reply(surface: str, tokens: list[tuple[str, float, str | None]]) -> dict:
    """tokens: (piece, logprob, top_piece)；top_piece=None 表示候選就是 top-1。"""
    probs = []
    for i, (piece, logprob, top) in enumerate(tokens):
        top_piece = piece if top is None else top
        probs.append({
            'id': i,
            'token': piece,
            'bytes': list(piece.encode()),
            'logprob': logprob,
            'top_logprobs': [{'id': i if top is None else 999, 'token': top_piece,
                              'bytes': list(top_piece.encode()), 'logprob': logprob}],
        })
    return {'content': surface, 'completion_probabilities': probs}


class ManifestTest(unittest.TestCase):
    def package(self, *, backend='llama.cpp', commit='c0ffee', schema=ja.SCHEMA,
                policy=ja.CONTEXT_POLICY, size=None, digest=None, name='model.gguf'):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        path = Path(tmp.name)
        safe = 'model.gguf' if '/' in name or '\\' in name else name
        model = path / safe
        model.write_bytes(b'mock model')
        data = model.read_bytes()
        manifest = {
            'schema': schema,
            'backend': backend,
            'backend_commit': commit,
            'context_policy': policy,
            'model': {
                'file': name,
                'sha256': digest or hashlib.sha256(data).hexdigest(),
                'bytes': len(data) if size is None else size,
            },
            'limits': {'max_reading_units': 64, 'max_candidates': 8, 'max_left_context_units': 128},
            'context_size': 512,
        }
        (path / 'manifest.json').write_text(json.dumps(manifest), encoding='utf-8')
        return path, manifest

    def test_valid(self):
        path, manifest = self.package()
        got = ja.load_manifest(path, manifest['model']['sha256'], 'c0ffee')
        self.assertEqual(got['schema'], ja.SCHEMA)

    def test_missing_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ja.Error) as caught:
                ja.load_manifest(Path(tmp), '', '')
        self.assertEqual(caught.exception.code, 'model_manifest_missing')

    def test_missing_model(self):
        path, manifest = self.package()
        (path / 'model.gguf').unlink()
        with self.assertRaises(ja.Error) as caught:
            ja.load_manifest(path, manifest['model']['sha256'], '')
        self.assertEqual(caught.exception.code, 'model_missing')

    def test_hash_mismatch(self):
        path, manifest = self.package(digest='0' * 64)
        with self.assertRaises(ja.Error) as caught:
            ja.load_manifest(path, manifest['model']['sha256'], '')
        self.assertEqual(caught.exception.code, 'model_hash_mismatch')

    def test_size_mismatch(self):
        path, manifest = self.package(size=1)
        with self.assertRaises(ja.Error) as caught:
            ja.load_manifest(path, manifest['model']['sha256'], '')
        self.assertEqual(caught.exception.code, 'model_hash_mismatch')

    def test_backend_and_contract(self):
        for kwargs, code in [
            ({'backend': 'onnx'}, 'backend_mismatch'),
            ({'schema': 'other'}, 'manifest_contract_mismatch'),
            ({'policy': 'other'}, 'manifest_contract_mismatch'),
        ]:
            path, manifest = self.package(**kwargs)
            with self.assertRaises(ja.Error) as caught:
                ja.load_manifest(path, manifest['model']['sha256'], '')
            self.assertEqual(caught.exception.code, code)

    def test_expected_commit(self):
        path, manifest = self.package(commit='c0ffee')
        with self.assertRaises(ja.Error) as caught:
            ja.load_manifest(path, manifest['model']['sha256'], 'deadbeef')
        self.assertEqual(caught.exception.code, 'backend_mismatch')

    def test_unsafe_file_name(self):
        path, manifest = self.package(name='../model.gguf')
        with self.assertRaises(ja.Error) as caught:
            ja.load_manifest(path, manifest['model']['sha256'], '')
        self.assertEqual(caught.exception.code, 'model_manifest_invalid')


class PromptTest(unittest.TestCase):
    def test_prompt(self):
        self.assertEqual(ja.build_prompt('', 'きょう'), '\uee00キョウ\uee01')
        self.assertEqual(ja.build_prompt('私の', 'きょう'), '\uee02私の\uee00キョウ\uee01')
        self.assertEqual(ja.to_katakana('きょうはいい'), 'キョウハイイ')

    def test_grammar(self):
        self.assertEqual(ja.grammar_literal('今日は'), 'root ::= "今日は"')
        self.assertEqual(ja.grammar_literal('a"b\\c'), 'root ::= "a\\"b\\\\c"')
        for bad in ('', 'a\nb', 'a\x00b'):
            with self.assertRaises(ja.Error):
                ja.grammar_literal(bad)

    def test_validation(self):
        self.assertTrue(ja.valid_surface('今日は'))
        self.assertFalse(ja.valid_surface(''))
        self.assertFalse(ja.valid_surface('a\nb'))
        self.assertTrue(ja.valid_reading(['き', 'ょ', 'う'], 64))
        self.assertFalse(ja.valid_reading([], 64))
        self.assertFalse(ja.valid_reading(['き', ''], 64))
        self.assertFalse(ja.valid_reading(['き', ' '], 64))
        self.assertFalse(ja.valid_reading(['き'] * 65, 64))


class ScoreTest(unittest.TestCase):
    def test_all_top(self):
        got = ja.score_completion('今日', reply('今日', [('今', -0.5, None), ('日', -0.25, None)]))
        self.assertAlmostEqual(got['logprob'], -0.75)
        self.assertAlmostEqual(got['score'], -0.375)
        self.assertEqual(got['prefix'], '今日')
        self.assertTrue(got['passed'])

    def test_divergence(self):
        got = ja.score_completion('今日', reply('今日', [('今', -0.5, None), ('日', -9.0, '他')]))
        self.assertEqual(got['prefix'], '今日他')
        self.assertFalse(got['passed'])

    def test_eos_excluded(self):
        got = ja.score_completion('今日', reply('今日', [('今', -0.5, None), ('日', -0.25, None), ('', -0.01, None)]))
        self.assertAlmostEqual(got['logprob'], -0.75)
        self.assertEqual(got['prefix'], '今日')
        self.assertTrue(got['passed'])

    def test_content_mismatch(self):
        with self.assertRaises(ja.Error) as caught:
            ja.score_completion('今日', reply('今日は', [('今', -0.5, None)]))
        self.assertEqual(caught.exception.code, 'candidate_not_reproduced')

    def test_nonfinite(self):
        bad = reply('今', [('今', -0.5, None)])
        bad['completion_probabilities'][0]['logprob'] = -1e300
        with self.assertRaises(ja.Error):
            ja.score_completion('今', bad)

    def test_fixed_prefix(self):
        draft = {'surface': '今日は', 'clauses': [
            {'surface': '今日', 'reading': ['き', 'ょ', 'う']},
            {'surface': 'は', 'reading': ['は']},
        ]}
        self.assertEqual(ja.fixed_prefix(draft, '今日は'), (2, 4))
        self.assertEqual(ja.fixed_prefix(draft, '今日'), (1, 3))
        self.assertEqual(ja.fixed_prefix({'surface': '今', 'clauses': [
            {'surface': '今日', 'reading': ['き', 'ょ', 'う']}]}, '今'), (0, 0))


class FakeServer:
    def __init__(self, sleep=0.0):
        self.calls = []
        self.sleep = sleep

    def tokenize(self, prompt, timeout):
        return [1, 2, 3]

    def verify(self, prompt_ids, surface, timeout):
        self.calls.append(surface)
        if self.sleep:
            time.sleep(self.sleep)
        return {'logprob': -float(len(surface)), 'score': -1.0,
                'prefix': surface, 'passed': True}


class RuntimeTest(unittest.TestCase):
    def manifest(self, candidates=8):
        return {
            'schema': ja.SCHEMA,
            'backend': ja.BACKEND,
            'backend_commit': 'c',
            'context_policy': ja.CONTEXT_POLICY,
            'model': {'file': 'model.gguf', 'sha256': 'a' * 64, 'bytes': 1},
            'limits': {'max_reading_units': 64, 'max_candidates': candidates,
                       'max_left_context_units': 128},
        }

    def runtime(self, server, timeout_ms=60):
        runtime = ja.Runtime(Path('.'), self.manifest(), server, timeout_ms)
        server.calls.clear()
        return runtime

    def request(self, surfaces):
        return {
            'schema': ja.SCHEMA, 'model_sha256': 'a' * 64, 'left_context': '私の',
            'reading': ['き', 'ょ', 'う'],
            'draft': {'surface': surfaces[0], 'reading': ['き', 'ょ', 'う'],
                      'clauses': [{'surface': surfaces[0], 'reading': ['き', 'ょ', 'う']}]},
            'candidates': [{'surface': s, 'reading': ['き', 'ょ', 'う']} for s in surfaces],
        }

    def test_handle_and_cache(self):
        server = FakeServer()
        runtime = self.runtime(server)
        got = runtime.handle(self.request(['今日', '今日']))
        self.assertEqual(len(got['candidates']), 2)
        self.assertEqual(got['prefix'], '今日')
        self.assertEqual(server.calls.count('今日'), 1)  # 暖機 1 次＋驗證 1 次
        self.assertTrue(got['candidates'][0]['passed'])

    def test_timeout_is_reported(self):
        runtime = self.runtime(FakeServer(sleep=0.08), timeout_ms=40)
        with self.assertRaises(ja.Error) as caught:
            runtime.handle(self.request(['今日', '明日']))
        self.assertEqual(caught.exception.code, 'timeout')

    def test_reading_mismatch(self):
        runtime = self.runtime(FakeServer())
        request = self.request(['今日'])
        request['candidates'][0]['reading'] = ['き']
        with self.assertRaises(ja.Error) as caught:
            runtime.handle(request)
        self.assertEqual(caught.exception.code, 'unsupported_request')

    def test_too_many_candidates(self):
        runtime = self.runtime(FakeServer())
        with self.assertRaises(ja.Error):
            runtime.handle(self.request(['あ'] * 9))

    def test_context_trimmed(self):
        server = FakeServer()
        runtime = ja.Runtime(Path('.'), self.manifest(), server, 100)
        server.calls.clear()
        request = self.request(['今日'])
        request['left_context'] = 'あ' * 140
        got = runtime.handle(request)
        self.assertEqual(got['reading'], 'きょう')
        self.assertEqual(server.calls[0], '今日')


if __name__ == '__main__':
    unittest.main()
