from __future__ import annotations

import json
import os
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from p2vm.checks import analyze_p2_suite  # noqa: E402

ROOT = Path(__file__).resolve().parents[5]
DATA = ROOT / "testdata" / "golden" / "zh" / "data"


def read_exact(sock, count):
    data = b""
    while len(data) < count:
        chunk = sock.recv(count - len(data))
        if not chunk:
            raise RuntimeError("engine disconnected")
        data += chunk
    return data


def exchange(sock, envelope):
    body = json.dumps(envelope, ensure_ascii=False).encode()
    sock.sendall(struct.pack("<I", len(body)) + body)
    length = struct.unpack("<I", read_exact(sock, 4))[0]
    reply = json.loads(read_exact(sock, length))
    if not reply.get("ok"):
        raise RuntimeError(reply)
    return reply


@unittest.skipIf(sys.platform == "win32", "Linux-side protocol self-test")
@unittest.skipUnless(shutil.which("go") and DATA.is_dir(), "Go toolchain or golden data missing")
class ProtocolTraceTest(unittest.TestCase):
    """Drive a real 0c-server and run the same checks the VM trace uses.

    This is the bridge between the PowerShell recorder and the Python checks:
    if the key sequence, snapshot fields or check thresholds drift, this test
    fails on Linux before a VM is ever involved.
    """

    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.dir = Path(cls.temp.name)
        (cls.dir / "settings.json").write_text('{"mixed_input":true}', encoding="utf-8")
        cls.binary = cls.dir / "0c-server"
        subprocess.run(
            ["go", "build", "-o", str(cls.binary), "./cmd/0c-server"],
            cwd=ROOT, check=True, capture_output=True, text=True, timeout=180,
        )
        cls.socket_path = cls.dir / "engine.sock"
        cls.proc = subprocess.Popen(
            [
                str(cls.binary),
                "-data", str(DATA),
                "-user", str(cls.dir / "user.json"),
                "-socket", str(cls.socket_path),
                "-legacy-socket", "",
                "-idle-exit", "30s",
            ],
            cwd=ROOT,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

    @classmethod
    def tearDownClass(cls):
        cls.proc.terminate()
        try:
            cls.proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            cls.proc.kill()
        cls.temp.cleanup()

    def connect(self):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            sock = socket.socket(socket.AF_UNIX)
            try:
                sock.connect(str(self.socket_path))
                return sock
            except OSError:
                sock.close()
                time.sleep(0.2)
        self.fail("0c-server socket did not become ready")

    def test_recorded_trace_passes_every_check(self):
        sock = self.connect()
        session = exchange(sock, {"op": "open", "version": 0})["session"]
        trace = []

        def handle(step, request):
            reply = exchange(sock, {"op": "handle", "session": session, "req": request})
            trace.append({"step": step, "request": request, "snapshot": reply.get("snapshot")})

        def keys(prefix, text):
            for char in text:
                handle(f"{prefix}.{char}", {"type": "key", "key": char})

        try:
            keys("english", "hello")
            handle("english.direct", {"type": "key", "key": "Enter"})
            handle("english.cancel", {"type": "cancel"})

            keys("zhuyin", "su3cl3")
            handle("zhuyin.commit", {"type": "key", "key": "Enter"})
            handle("zhuyin.cancel", {"type": "cancel"})

            keys("segment", "a86qi62.4zj3")
            handle("segment.compose", {"type": "cursor", "to": 4})
            handle("segment.cursor0", {"type": "cursor", "to": 0})
            handle("segment.cursor0.candidates", {"type": "candidates"})
            handle("segment.cursor2", {"type": "cursor", "to": 2})
            handle("segment.cursor2.candidates", {"type": "candidates"})
            handle("segment.cancel", {"type": "cancel"})
        finally:
            sock.close()

        result = {
            "trace": trace,
            "profile": {
                "profiles": [{
                    "language": "0x0404",
                    "guid": "{EF62F743-9855-4C94-A658-1F0F98B7B840}",
                    "display": "Mixed",
                    "values": {"(default)": "Mixed"},
                }],
                "language_tips": [
                    "0404:{138C35D0-7F08-4CEF-BA90-C34C16301528}"
                    "{EF62F743-9855-4C94-A658-1F0F98B7B840}"
                ],
            },
            "host": {"english_text": "hello", "candidate_visible": True, "segment_switch_visible": True},
        }
        checks = analyze_p2_suite(result)
        self.assertTrue(all(check.status == "PASS" for check in checks), [c.as_dict() for c in checks])


if __name__ == "__main__":
    unittest.main()
