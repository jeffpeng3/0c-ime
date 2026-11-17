"""Run configuration and SSH/SCP command construction.

Nothing here contains a host, user or key.  Callers supply them explicitly or
through the P2VM_* environment variables documented in ../README.md.
"""

from __future__ import annotations

import argparse
import os
from dataclasses import dataclass, field
from pathlib import Path

DEFAULT_SUITES = [
    "p2-mixed",
    "wpf",
    "notepad",
    "terminal",
    "browser",
    "webview2",
    "search",
    "wpf32",
    "native32",
]

APP_SUITES = {
    "wpf": "WPF",
    "notepad": "Notepad",
    "terminal": "Windows Terminal",
    "browser": "Edge",
    "webview2": "WebView2",
    "search": "Windows search box",
    "wpf32": "32-bit WPF",
    "native32": "32-bit native EDIT",
}


class ConfigError(ValueError):
    pass


@dataclass
class RunConfig:
    host: str
    user: str
    key: Path
    bundle: Path | None = None
    port: int = 22
    repo_root: Path = field(default_factory=lambda: Path(__file__).resolve().parents[5])
    result_root: Path | None = None
    remote_root: str = r"C:\0c-p2vm"
    suites: list[str] = field(default_factory=lambda: list(DEFAULT_SUITES))
    timeout_minutes: int = 150
    preflight_only: bool = False
    keep_remote: bool = False
    skip_local_golden: bool = False
    dry_run: bool = False
    ssh_path: str = "ssh"
    scp_path: str = "scp"
    ssh_options: list[str] = field(default_factory=list)

    def __post_init__(self) -> None:
        self.repo_root = Path(self.repo_root).resolve()
        self.key = Path(self.key)
        if self.bundle is not None:
            self.bundle = Path(self.bundle)
        if self.result_root is None:
            self.result_root = self.repo_root / "build" / "windows" / "validation"
        else:
            self.result_root = Path(self.result_root)
        self.suites = _unique(self.suites)

    def validate(self, require_bundle: bool = True) -> None:
        if not self.host:
            raise ConfigError("missing VM host (--host or P2VM_HOST)")
        if not self.user:
            raise ConfigError("missing VM user (--user or P2VM_USER)")
        if not self.key.is_file():
            raise ConfigError(f"SSH key not found: {self.key}")
        if require_bundle and self.bundle is None:
            raise ConfigError("missing bundle (--bundle or P2VM_BUNDLE)")
        if self.bundle is not None and not self.bundle.is_file():
            raise ConfigError(f"bundle not found: {self.bundle}")
        if self.bundle is not None and self.bundle.suffix.lower() != ".zip":
            raise ConfigError("the Windows acceptance bundle must be a .zip")
        if not self.repo_root.is_dir():
            raise ConfigError(f"repo root not found: {self.repo_root}")
        if " " in self.remote_root:
            raise ConfigError("--remote-root must not contain spaces")
        if not self.remote_root.startswith("C:") and not self.remote_root.startswith("c:"):
            raise ConfigError("--remote-root must be an absolute C: path")
        if self.port < 1 or self.port > 65535:
            raise ConfigError("--port must be 1..65535")
        if self.timeout_minutes < 1:
            raise ConfigError("--timeout-minutes must be positive")
        unknown = [name for name in self.suites if name not in APP_SUITES and name != "p2-mixed"]
        if unknown:
            raise ConfigError("unknown suite(s): " + ", ".join(unknown))

    @property
    def target(self) -> str:
        return f"{self.user}@{self.host}"

    @property
    def remote_test_dir(self) -> str:
        return self.remote_root + r"\repo\platforms\windows\test"

    @property
    def remote_bundle(self) -> str:
        return self.remote_root + r"\bundle.zip"

    @property
    def remote_logs(self) -> str:
        return self.remote_root + r"\logs"

    @property
    def remote_validation(self) -> str:
        return self.remote_root + r"\validation"

    def scp_remote(self, remote_path: str) -> str:
        # OpenSSH scp on Windows accepts forward slashes and a drive letter;
        # backslashes are also accepted but are easy to mangle in a URI-ish
        # user@host:path argument.
        return self.target + ":" + remote_path.replace("\\", "/")


def _unique(values: list[str]) -> list[str]:
    out: list[str] = []
    for value in values:
        if value and value not in out:
            out.append(value)
    return out


def from_args(args: argparse.Namespace) -> RunConfig:
    key = args.key or os.environ.get("P2VM_KEY")
    if not key:
        raise ConfigError("missing SSH key: --key or P2VM_KEY")
    return RunConfig(
        host=args.host or os.environ.get("P2VM_HOST", ""),
        user=args.user or os.environ.get("P2VM_USER", ""),
        key=Path(key),
        bundle=Path(args.bundle or os.environ["P2VM_BUNDLE"]) if (args.bundle or os.environ.get("P2VM_BUNDLE")) else None,
        port=int(args.port),
        result_root=Path(args.result_root) if args.result_root else None,
        remote_root=args.remote_root,
        suites=_split_suites(args.suite) if args.suite else list(DEFAULT_SUITES),
        timeout_minutes=args.timeout_minutes,
        preflight_only=bool(getattr(args, "preflight_only", False)),
        keep_remote=bool(getattr(args, "keep_remote", False)),
        skip_local_golden=bool(getattr(args, "skip_local_golden", False)),
        dry_run=bool(getattr(args, "dry_run", False)),
        ssh_path=args.ssh or "ssh",
        scp_path=args.scp or "scp",
        ssh_options=list(args.ssh_option or []),
    )


def _split_suites(value: str) -> list[str]:
    return _unique([part.strip() for part in value.split(",") if part.strip()])
