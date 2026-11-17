#!/usr/bin/env python3
"""Entry point for the P2-44 Windows VM acceptance harness.

Usage:
    python3 platforms/windows/test/vm/p2vm.py selftest
    python3 platforms/windows/test/vm/p2vm.py plan --host ... --user ... --key ...
    python3 platforms/windows/test/vm/p2vm.py preflight --host ... --user ... --key ...
    python3 platforms/windows/test/vm/p2vm.py run --bundle ... --host ... --user ... --key ...
"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from p2vm.cli import main  # noqa: E402

if __name__ == "__main__":
    sys.exit(main())
