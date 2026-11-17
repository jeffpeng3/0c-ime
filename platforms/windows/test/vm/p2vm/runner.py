"""Thin subprocess wrappers for ssh, scp and local commands.

Every invocation is written under ``logs/`` in the result directory so a failed
run has the exact command, exit code, stdout and stderr needed for a bug report.
"""

from __future__ import annotations

import shlex
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

from .config import RunConfig


class RunnerError(RuntimeError):
    def __init__(self, message: str, *, command: str = "", log: Path | None = None,
                 stdout: str = "", stderr: str = "", exit_code: int | None = None):
        super().__init__(message)
        self.command = command
        self.log = log
        self.stdout = stdout
        self.stderr = stderr
        self.exit_code = exit_code


@dataclass
class CommandResult:
    argv: list[str]
    command: str
    exit_code: int
    stdout: str
    stderr: str
    log: Path
    seconds: float


def _quote_arg(value: str) -> str:
    # Windows PowerShell -File arguments: quote values containing anything but
    # a simple path token.  Remote paths are validated to have no spaces, but
    # bundle names and suite lists may still contain commas or quotes.
    if value == "":
        return '""'
    if any(ch in value for ch in ' \t"&|<>^'):
        return '"' + value.replace('"', '\\"') + '"'
    return value


class LocalRunner:
    def __init__(self, log_dir: Path):
        self.log_dir = log_dir
        self.log_dir.mkdir(parents=True, exist_ok=True)
        self.counter = 0

    def run(self, argv: Iterable[str], *, timeout: float, name: str,
            cwd: Path | None = None, check: bool = False) -> CommandResult:
        argv = [str(part) for part in argv]
        self.counter += 1
        log = self.log_dir / f"{self.counter:02d}-{_safe(name)}.log"
        suffix = 2
        while log.exists():
            log = self.log_dir / f"{self.counter:02d}-{_safe(name)}-{suffix}.log"
            suffix += 1
        started = time.monotonic()
        try:
            proc = subprocess.run(
                argv,
                cwd=str(cwd) if cwd else None,
                text=True,
                errors="replace",
                capture_output=True,
                timeout=timeout,
            )
        except subprocess.TimeoutExpired as error:
            elapsed = time.monotonic() - started
            stdout = error.stdout or ""
            stderr = (error.stderr or "") + f"\nTIMEOUT after {timeout:.0f}s\n"
            if isinstance(stdout, bytes):
                stdout = stdout.decode("utf-8", "replace")
            if isinstance(stderr, bytes):
                stderr = stderr.decode("utf-8", "replace")
            _write_log(log, argv, None, stdout, stderr, elapsed)
            raise RunnerError(
                f"{name} timed out after {timeout:.0f}s",
                command=_display(argv), log=log, stdout=stdout, stderr=stderr,
            ) from error
        elapsed = time.monotonic() - started
        _write_log(log, argv, proc.returncode, proc.stdout, proc.stderr, elapsed)
        result = CommandResult(argv, _display(argv), proc.returncode, proc.stdout, proc.stderr, log, elapsed)
        if check and proc.returncode != 0:
            raise RunnerError(
                f"{name} failed with exit code {proc.returncode}",
                command=result.command, log=log, stdout=proc.stdout, stderr=proc.stderr,
                exit_code=proc.returncode,
            )
        return result


def _safe(name: str) -> str:
    return "".join(ch if ch.isalnum() or ch in "-_." else "-" for ch in name)[:80]


def _display(argv: Iterable[str]) -> str:
    return " ".join(shlex.quote(str(part)) for part in argv)


def _write_log(path: Path, argv: list[str], code: int | None, stdout: str, stderr: str, seconds: float) -> None:
    parts = [
        "$ " + _display(argv),
        f"exit: {code}",
        f"seconds: {seconds:.1f}",
        "",
        "--- stdout ---",
        stdout or "",
        "--- stderr ---",
        stderr or "",
    ]
    path.write_text("\n".join(parts) + "\n", encoding="utf-8")


class RemoteRunner:
    def __init__(self, config: RunConfig, local: LocalRunner):
        self.config = config
        self.local = local

    def _ssh_argv(self) -> list[str]:
        argv = [
            self.config.ssh_path,
            "-o", "BatchMode=yes",
            "-o", "StrictHostKeyChecking=accept-new",
            "-o", "ConnectTimeout=10",
            "-o", "ServerAliveInterval=30",
            "-p", str(self.config.port),
            "-i", str(self.config.key),
        ]
        for option in self.config.ssh_options:
            argv.extend(["-o", option])
        argv.append(self.config.target)
        return argv

    def _scp_argv(self, *, source: str, destination: str, recurse: bool) -> list[str]:
        argv = [
            self.config.scp_path,
            "-o", "BatchMode=yes",
            "-o", "StrictHostKeyChecking=accept-new",
            "-P", str(self.config.port),
            "-i", str(self.config.key),
        ]
        if recurse:
            argv.append("-r")
        argv.extend([source, destination])
        return argv

    def _remote_powershell(self, script: str, params: dict[str, str], switches: Iterable[str] = ()) -> str:
        parts = [
            "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
            "-File", f'"{script}"',
        ]
        for key, value in params.items():
            parts.extend(["-" + key, _quote_arg(value)])
        for switch in switches:
            parts.append("-" + switch)
        return " ".join(parts)

    def run_powershell(self, script: str, *, params: dict[str, str] | None = None,
                       switches: Iterable[str] = (), timeout: float = 600,
                       name: str = "remote-powershell") -> CommandResult:
        command = self._remote_powershell(script, params or {}, switches)
        argv = self._ssh_argv() + [command]
        return self.local.run(argv, timeout=timeout, name=name, check=True)

    def run_raw(self, command: str, *, timeout: float = 600,
                name: str = "remote-command", check: bool = True) -> CommandResult:
        argv = self._ssh_argv() + [command]
        return self.local.run(argv, timeout=timeout, name=name, check=check)

    def probe(self, timeout: float = 60) -> CommandResult:
        command = "powershell -NoProfile -Command \"$env:COMPUTERNAME; whoami; [Environment]::OSVersion.VersionString\""
        argv = self._ssh_argv() + [command]
        return self.local.run(argv, timeout=timeout, name="ssh-probe", check=True)

    def copy_to(self, local_path: Path | str, remote_path: str, *, recurse: bool = False,
                timeout: float = 900) -> CommandResult:
        argv = self._scp_argv(source=str(local_path), destination=self.config.scp_remote(remote_path),
                              recurse=recurse)
        return self.local.run(argv, timeout=timeout, name="scp-to", check=True)

    def copy_from(self, remote_path: str, local_path: Path | str, *, recurse: bool = True,
                  timeout: float = 900, check: bool = True) -> CommandResult:
        argv = self._scp_argv(source=self.config.scp_remote(remote_path), destination=str(local_path),
                              recurse=recurse)
        return self.local.run(argv, timeout=timeout, name="scp-from", check=check)
