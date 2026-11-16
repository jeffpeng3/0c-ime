#!/usr/bin/env python3
"""日文神經驗證 worker（llama.cpp CPU 後端）。

T03 的結論：zenz-v3.1-small 的 Q5_K_M GGUF 可在 CPU 載入，但每次按鍵跑完整
free generation 會超過 100 ms。這個 worker 只做 draft + verify：

  * draft 由統計轉換器提供，worker 不生成整句。
  * 候選以 GBNF 固定成 candidate 表記，逐 token 取得原始 logprob 與 top-1。
  * 若候選 token 不是該位置的 top-1，回報到分歧點為止的高機率前綴。

對外是 stdin/stdout 的單行 JSON 協定，由 internal/neural 的 JaRuntime 啟動。
llama-server 只綁 127.0.0.1，模型不進 Git；manifest.json 綁定模型雜湊與
llama.cpp commit，兩個都對上才啟動。
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unicodedata
import urllib.error
import urllib.request

MAX_FRAME = 65536
SCHEMA = 'ja-zenz-verify-v1'
BACKEND = 'llama.cpp'
CONTEXT_POLICY = 'confirmed_left_128'
CONTEXT_TAG = '\uee02'
INPUT_TAG = '\uee00'
OUTPUT_TAG = '\uee01'
CONTEXT_LIMIT = 128
READING_LIMIT = 64
CANDIDATE_LIMIT = 8
SURFACE_LIMIT = 64
DEFAULT_CTX = 512
# worker 自己留 10 ms 給 JSON 與排程；宿主另有硬逾時，兩者都不排隊。
DEADLINE_MARGIN_S = 0.010


class Error(Exception):
    """固定代碼的協定錯誤；detail 不帶使用者文字。"""

    def __init__(self, code: str, detail: str = ''):
        super().__init__(code)
        self.code = code
        self.detail = detail


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1 << 20), b''):
            h.update(block)
    return h.hexdigest()


def _plain_file_name(name: object) -> bool:
    return isinstance(name, str) and name not in ('', '.', '..') and Path(name).name == name


def load_manifest(package: Path, expected_model: str, expected_commit: str) -> dict:
    """核對 manifest、模型大小與雜湊；任何缺漏都 fail closed。"""
    path = package / 'manifest.json'
    try:
        raw = path.read_text(encoding='utf-8')
    except FileNotFoundError as e:
        raise Error('model_manifest_missing') from e
    except OSError as e:
        raise Error('model_manifest_missing', type(e).__name__) from e
    try:
        m = json.loads(raw)
    except (ValueError, TypeError) as e:
        raise Error('model_manifest_invalid') from e
    if not isinstance(m, dict):
        raise Error('model_manifest_invalid')
    if m.get('schema') != SCHEMA or m.get('context_policy') != CONTEXT_POLICY:
        raise Error('manifest_contract_mismatch')
    if m.get('backend') != BACKEND:
        raise Error('backend_mismatch')
    commit = m.get('backend_commit')
    if not isinstance(commit, str) or not commit:
        raise Error('manifest_contract_mismatch')
    if expected_commit and commit != expected_commit:
        raise Error('backend_mismatch')
    model = m.get('model')
    if not isinstance(model, dict) or not _plain_file_name(model.get('file')):
        raise Error('model_manifest_invalid')
    if not isinstance(model.get('sha256'), str) or len(model['sha256']) != 64:
        raise Error('model_manifest_invalid')
    if expected_model and model['sha256'] != expected_model:
        raise Error('model_hash_mismatch')
    if type(model.get('bytes')) is not int or model['bytes'] <= 0:
        raise Error('model_manifest_invalid')
    limits = m.get('limits')
    if not isinstance(limits, dict):
        raise Error('manifest_contract_mismatch')
    for key, maximum in (('max_reading_units', READING_LIMIT),
                         ('max_candidates', CANDIDATE_LIMIT),
                         ('max_left_context_units', CONTEXT_LIMIT)):
        value = limits.get(key)
        if type(value) is not int or not 1 <= value <= maximum:
            raise Error('manifest_contract_mismatch')
    ctx = m.get('context_size', DEFAULT_CTX)
    if type(ctx) is not int or not 256 <= ctx <= 2048:
        raise Error('manifest_contract_mismatch')
    model_path = package / model['file']
    try:
        size = model_path.stat().st_size
    except OSError as e:
        raise Error('model_missing', type(e).__name__) from e
    if size != model['bytes']:
        raise Error('model_hash_mismatch')
    if sha256_file(model_path) != model['sha256']:
        raise Error('model_hash_mismatch')
    return m


def build_prompt(left_context: str, reading: str) -> str:
    """zenz-v3 格式：context 前置，再放輸入與輸出標記。

    zenz 的輸入是片假名；T30 傳來的讀音是かな單位，這裡只轉輸入，
    候選表記與讀音對應不變。
    """
    reading = to_katakana(reading)
    if left_context:
        return CONTEXT_TAG + left_context + INPUT_TAG + reading + OUTPUT_TAG
    return INPUT_TAG + reading + OUTPUT_TAG


def to_katakana(text: str) -> str:
    out = []
    for ch in text:
        code = ord(ch)
        if 0x3041 <= code <= 0x3096 or 0x309D <= code <= 0x309E:
            out.append(chr(code + 0x60))
        else:
            out.append(ch)
    return ''.join(out)


def grammar_literal(text: str) -> str:
    """把候選表記變成只允許該字串的 GBNF。控制字元一律拒絕。"""
    if not text:
        raise Error('unsupported_candidate')
    out = []
    for ch in text:
        if ch in ('\\', '"'):
            out.append('\\' + ch)
        elif unicodedata.category(ch) in ('Cc', 'Cs', 'Cf') or ord(ch) < 0x20:
            raise Error('unsupported_candidate')
        else:
            out.append(ch)
    return 'root ::= "' + ''.join(out) + '"'


def valid_surface(text: object) -> bool:
    if not isinstance(text, str) or not text or len(text) > SURFACE_LIMIT:
        return False
    try:
        text.encode('utf-8')
    except UnicodeEncodeError:
        return False
    return all(ch.isprintable() and unicodedata.category(ch) not in ('Cc', 'Cs', 'Cf') for ch in text)


def valid_reading(reading: object, allowed: int) -> bool:
    if not isinstance(reading, list) or not 1 <= len(reading) <= allowed:
        return False
    for unit in reading:
        if not isinstance(unit, str) or not unit:
            return False
        if any(ch.isspace() for ch in unit):
            return False
    return True


def score_completion(surface: str, reply: object) -> dict:
    """從 llama-server 的 completion_probabilities 算分與分歧前綴。

    候選已被 GBNF 固定，所以每個 token 的 logprob 就是候選在該位置的分數。
    top_logprobs[0] 是未受 grammar 影響的原始 top-1；與候選 token 不同即為
    Zenzai 的 fixRequired 位置。
    """
    if not isinstance(reply, dict):
        raise Error('invalid_server_reply')
    content = reply.get('content')
    if content != surface:
        raise Error('candidate_not_reproduced')
    probs = reply.get('completion_probabilities')
    if not isinstance(probs, list) or not probs:
        raise Error('invalid_server_reply')
    total = 0.0
    progress = bytearray()
    for item in probs:
        if not isinstance(item, dict):
            raise Error('invalid_server_reply')
        piece = item.get('bytes')
        if not isinstance(piece, list) or any(type(b) is not int or not 0 <= b <= 255 for b in piece):
            raise Error('invalid_server_reply')
        # 結尾的 EOS 不是候選的一部分；Zenzai 只評候選 token。
        if not piece and item.get('token') == '':
            continue
        logprob = item.get('logprob')
        if not isinstance(logprob, (int, float)) or not math.isfinite(logprob) or logprob < -1e30:
            raise Error('invalid_server_reply')
        total += float(logprob)
        top = item.get('top_logprobs')
        if not isinstance(top, list) or not top:
            raise Error('invalid_server_reply')
        best = top[0]
        if not isinstance(best, dict) or type(best.get('id')) is not int:
            raise Error('invalid_server_reply')
        progress.extend(piece)
        if best.get('id') != item.get('id'):
            best_bytes = best.get('bytes')
            if isinstance(best_bytes, list) and all(type(b) is int and 0 <= b <= 255 for b in best_bytes):
                combined = bytes(progress) + bytes(best_bytes)
                try:
                    prefix = combined.decode('utf-8')
                except UnicodeDecodeError:
                    prefix = bytes(progress).decode('utf-8', 'replace')
            else:
                prefix = bytes(progress).decode('utf-8', 'replace')
            return {'logprob': total, 'score': total / max(1, len(surface)),
                    'prefix': prefix, 'passed': False}
    return {'logprob': total, 'score': total / max(1, len(surface)),
            'prefix': bytes(progress).decode('utf-8', 'replace'), 'passed': True}


def fixed_prefix(draft: dict, prefix: str) -> tuple[int, int]:
    """回報高機率前綴涵蓋的完整文節數與讀音單位數，供 T30 固定重搜。"""
    if not isinstance(draft, dict) or not isinstance(prefix, str) or not prefix:
        return 0, 0
    clauses = draft.get('clauses')
    if not isinstance(clauses, list):
        return 0, 0
    covered = 0
    clauses_done = 0
    units = 0
    for clause in clauses:
        if not isinstance(clause, dict):
            break
        text = clause.get('surface')
        reading = clause.get('reading')
        if not isinstance(text, str) or not valid_reading(reading, READING_LIMIT):
            break
        if covered + len(text) > len(prefix):
            break
        covered += len(text)
        clauses_done += 1
        units += len(reading)
    return clauses_done, units


class LlamaServer:
    """只綁 loopback 的 llama-server；模型與執行檔路徑由 manifest/宿主提供。"""

    def __init__(self, binary: str, model: Path, threads: int, ctx: int):
        self.binary = binary
        self.model = model
        self.threads = threads
        self.ctx = ctx
        self.proc: subprocess.Popen | None = None
        self.port = 0
        self.log_path: Path | None = None
        self._token_cache: dict[str, list[int]] = {}

    def _free_port(self) -> int:
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            return sock.getsockname()[1]

    def start(self, timeout_s: float = 30.0) -> None:
        binary = Path(self.binary)
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise Error('runtime_missing')
        self.port = self._free_port()
        fd, name = tempfile.mkstemp(prefix='0c-ja-llama-', suffix='.log')
        os.close(fd)
        self.log_path = Path(name)
        log = self.log_path.open('wb')
        # GNU/Linux：worker 被宿主殺掉時，llama-server 一起結束，不留孤兒。
        def die_with_parent():
            if sys.platform.startswith('linux'):
                import ctypes
                try:
                    ctypes.CDLL('libc.so.6', use_errno=True).prctl(1, signal.SIGTERM)
                except Exception:
                    pass
        try:
            self.proc = subprocess.Popen(
                [str(binary), '-m', str(self.model), '--host', '127.0.0.1', '--port', str(self.port),
                 '-c', str(self.ctx), '-t', str(self.threads), '-np', '1', '--no-webui',
                 '--no-slots', '--log-disable'],
                stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=log,
                preexec_fn=die_with_parent if sys.platform.startswith('linux') else None,
            )
        finally:
            log.close()
        deadline = time.monotonic() + timeout_s
        while time.monotonic() < deadline:
            if self.proc.poll() is not None:
                raise Error('server_start_failed', self._log_tail())
            try:
                self._get('/health', 1.0)
                return
            except (urllib.error.URLError, TimeoutError, OSError):
                time.sleep(0.05)
        raise Error('server_timeout', self._log_tail())

    def _log_tail(self) -> str:
        if not self.log_path:
            return ''
        try:
            data = self.log_path.read_bytes()[-2048:]
        except OSError:
            return ''
        text = data.decode('utf-8', 'replace')
        return text.replace('\n', ' ')[-200:]

    def _url(self, path: str) -> str:
        return f'http://127.0.0.1:{self.port}{path}'

    def _get(self, path: str, timeout: float) -> object:
        with urllib.request.urlopen(self._url(path), timeout=timeout) as r:
            return json.loads(r.read())

    def _post(self, path: str, body: dict, timeout: float) -> object:
        data = json.dumps(body, ensure_ascii=False).encode('utf-8')
        req = urllib.request.Request(self._url(path), data=data,
                                     headers={'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(req, timeout=max(0.001, timeout)) as r:
                return json.loads(r.read())
        except (socket.timeout, TimeoutError) as e:
            raise Error('timeout') from e
        except urllib.error.URLError as e:
            if isinstance(getattr(e, 'reason', None), (socket.timeout, TimeoutError)):
                raise Error('timeout') from e
            raise Error('server_error') from e

    def tokenize(self, prompt: str, timeout: float) -> list[int]:
        cached = self._token_cache.get(prompt)
        if cached is not None:
            return cached
        reply = self._post('/tokenize', {'content': prompt, 'add_special': False,
                                        'parse_special': True, 'with_pieces': True}, timeout)
        tokens = reply.get('tokens') if isinstance(reply, dict) else None
        if not isinstance(tokens, list) or not tokens:
            raise Error('invalid_server_reply')
        ids = []
        for token in tokens:
            if type(token) is int:
                ids.append(token)
                continue
            if not isinstance(token, dict) or type(token.get('id')) is not int:
                raise Error('invalid_server_reply')
            ids.append(token['id'])
        if len(self._token_cache) > 32:
            self._token_cache.clear()
        self._token_cache[prompt] = ids
        return ids

    def verify(self, prompt_ids: list[int], surface: str, timeout: float) -> dict:
        grammar = grammar_literal(surface)
        n_predict = min(128, len(surface) + 8)
        reply = self._post('/completion', {
            'prompt': prompt_ids,
            'n_predict': n_predict,
            'temperature': 0,
            'top_k': 1,
            'n_probs': 1,
            'post_sampling_probs': False,
            'cache_prompt': True,
            'grammar': grammar,
            'stream': False,
        }, timeout)
        return score_completion(surface, reply)

    def stop(self) -> None:
        proc = self.proc
        self.proc = None
        if proc is not None and proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                proc.kill()
                try:
                    proc.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    pass
        if self.log_path is not None:
            try:
                self.log_path.unlink()
            except OSError:
                pass
            self.log_path = None


class Runtime:
    def __init__(self, package: Path, manifest: dict, server: LlamaServer, timeout_ms: int):
        self.package = package
        self.manifest = manifest
        self.server = server
        self.timeout_ms = timeout_ms
        self.limits = manifest['limits']
        self.model_sha = manifest['model']['sha256']
        self.reading = build_prompt('', 'あ')
        ids = server.tokenize(self.reading, timeout_ms / 1000)
        # 暖機只用標記與單一かな，不含使用者文字。
        server.verify(ids, 'あ', max(0.05, timeout_ms / 1000))

    def _validate(self, request: object) -> tuple[str, str, list[dict], dict | None]:
        if not isinstance(request, dict):
            raise Error('unsupported_request')
        if request.get('schema') != SCHEMA:
            raise Error('unsupported_request')
        if request.get('model_sha256') != self.model_sha:
            raise Error('unsupported_request')
        reading = request.get('reading')
        if not valid_reading(reading, self.limits['max_reading_units']):
            raise Error('unsupported_request')
        left = request.get('left_context', '')
        if not isinstance(left, str):
            raise Error('unsupported_request')
        if len(left) > self.limits['max_left_context_units']:
            left = left[-self.limits['max_left_context_units']:]
        candidates = request.get('candidates')
        if not isinstance(candidates, list) or not 1 <= len(candidates) <= self.limits['max_candidates']:
            raise Error('unsupported_request')
        for candidate in candidates:
            if not isinstance(candidate, dict) or not valid_surface(candidate.get('surface')):
                raise Error('unsupported_request')
            if candidate.get('reading') != reading:
                raise Error('unsupported_request')
        draft = request.get('draft')
        if draft is not None:
            if not isinstance(draft, dict) or not valid_surface(draft.get('surface')):
                raise Error('unsupported_request')
            if draft.get('reading') != reading:
                raise Error('unsupported_request')
            if not isinstance(draft.get('clauses', []), list):
                raise Error('unsupported_request')
        return build_prompt(left, ''.join(reading)), ''.join(reading), candidates, draft

    def handle(self, request: object) -> dict:
        prompt, reading_text, candidates, draft = self._validate(request)
        deadline = time.monotonic() + max(0.02, self.timeout_ms / 1000 - DEADLINE_MARGIN_S)
        prompt_ids = self.server.tokenize(prompt, max(0.001, deadline - time.monotonic()))
        cache: dict[str, dict] = {}

        def evaluate(surface: str) -> dict:
            if surface in cache:
                return cache[surface]
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise Error('timeout')
            verdict = self.server.verify(prompt_ids, surface, remaining)
            cache[surface] = verdict
            return verdict

        draft_verdict = None
        if draft is not None:
            draft_verdict = dict(evaluate(draft['surface']))
        global_prefix = draft_verdict['prefix'] if draft_verdict else ''
        if draft_verdict is not None:
            draft_verdict['prefix_ok'] = draft['surface'].startswith(global_prefix)
        verdicts = []
        for candidate in candidates:
            verdict = dict(evaluate(candidate['surface']))
            verdict['surface'] = candidate['surface']
            verdict['prefix_ok'] = candidate['surface'].startswith(global_prefix) if global_prefix else True
            verdicts.append(verdict)
        if draft_verdict is None:
            global_prefix = verdicts[0]['prefix']
            for verdict in verdicts:
                verdict['prefix_ok'] = verdict['surface'].startswith(global_prefix) if global_prefix else True
        clauses_done, units_done = fixed_prefix(draft, global_prefix) if draft_verdict else (0, 0)
        return {
            'prefix': global_prefix,
            'fixed_clauses': clauses_done,
            'fixed_reading_units': units_done,
            'draft': draft_verdict,
            'candidates': verdicts,
            'reading': reading_text,
        }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', type=Path, required=True)
    parser.add_argument('--llama-server', required=True)
    parser.add_argument('--threads', type=int, default=4)
    parser.add_argument('--timeout-ms', type=int, default=100)
    parser.add_argument('--model-sha256', default='')
    parser.add_argument('--backend-commit', default='')
    args = parser.parse_args()
    if not 1 <= args.threads <= 8 or not 1 <= args.timeout_ms <= 100:
        parser.error('threads must be 1..8 and timeout-ms 1..100')
    server = LlamaServer(args.llama_server, args.package / 'model.gguf', args.threads, DEFAULT_CTX)
    try:
        manifest = load_manifest(args.package, args.model_sha256, args.backend_commit)
        server.model = args.package / manifest['model']['file']
        server.ctx = manifest.get('context_size', DEFAULT_CTX)
        server.start()
        runtime = Runtime(args.package, manifest, server, args.timeout_ms)
    except Error as e:
        print(json.dumps({'ready': False, 'error': e.code, 'detail': e.detail}, ensure_ascii=False),
              flush=True)
        server.stop()
        return 1
    except Exception as e:  # 啟動失敗的原因只回固定代碼，不把 traceback 送給宿主
        print(json.dumps({'ready': False, 'error': 'internal_error', 'detail': type(e).__name__}),
              flush=True)
        server.stop()
        return 1
    print(json.dumps({
        'ready': True,
        'schema': SCHEMA,
        'backend': BACKEND,
        'backend_commit': manifest['backend_commit'],
        'context_policy': CONTEXT_POLICY,
        'model_sha256': manifest['model']['sha256'],
        'limits': manifest['limits'],
    }, ensure_ascii=False), flush=True)
    try:
        for line in sys.stdin.buffer:
            if len(line) > MAX_FRAME or not line.endswith(b'\n'):
                raise Error('request_frame_limit')
            try:
                request = json.loads(line)
            except (ValueError, TypeError):
                raise Error('invalid_json')
            reply = {
                'id': request.get('id'),
                'revision': request.get('revision'),
                'schema': SCHEMA,
                'model_sha256': manifest['model']['sha256'],
            }
            try:
                reply.update(runtime.handle(request))
            except Error as e:
                if e.code == 'timeout':
                    reply['fallback'] = 'timeout'
                elif e.code.startswith('unsupported') or e.code.endswith('mismatch') \
                        or e.code in ('invalid_json', 'invalid_server_reply', 'candidate_not_reproduced'):
                    reply['fallback'] = 'unsupported_request'
                else:
                    reply['fallback'] = 'internal_error'
                reply['detail'] = e.code
            print(json.dumps(reply, ensure_ascii=False, allow_nan=False), flush=True)
    except Error:
        return 1
    finally:
        server.stop()
    return 0


if __name__ == '__main__':
    sys.exit(main())
