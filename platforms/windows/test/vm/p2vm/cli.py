"""Command line entry point for the P2-44 Windows VM harness."""

from __future__ import annotations

import argparse
import json
import os
import shlex
import shutil
import subprocess
import sys
from datetime import datetime
from pathlib import Path
from typing import Any

from . import __version__
from .config import APP_SUITES, ConfigError, RunConfig, from_args
from .model import FAIL, NOT_RUN, PASS, SKIP, Check
from .report import (
    add_p2_checks,
    app_checks,
    bundle_info,
    collect_suite_results,
    write_summary,
)
from .runner import LocalRunner, RemoteRunner, RunnerError

EXIT_PASS = 0
EXIT_FAIL = 1
EXIT_USAGE = 2
EXIT_NOT_RUN = 3
EXIT_HARNESS = 4


def repo_root() -> Path:
    return Path(__file__).resolve().parents[5]


def revision(root: Path) -> str:
    try:
        return subprocess.check_output(
            ["git", "-C", str(root), "rev-parse", "HEAD"], text=True, stderr=subprocess.DEVNULL
        ).strip()
    except (OSError, subprocess.CalledProcessError):
        return "unknown"


def golden_check(root: Path, log_dir: Path) -> Check:
    if shutil.which("go") is None:
        return Check("local.zhuyin_golden", "local", SKIP, "Go toolchain not on PATH")
    local = LocalRunner(log_dir)
    try:
        result = local.run(
            ["go", "run", "./cmd/0c-golden"],
            cwd=root, timeout=300, name="local-golden", check=True,
        )
    except RunnerError as error:
        return Check(
            "local.zhuyin_golden", "local", FAIL,
            "zhuyin golden no longer matches: " + (error.stderr or str(error))[-1200:],
            {"log": str(error.log) if error.log else ""},
        )
    output = (result.stdout + result.stderr).strip()
    return Check("local.zhuyin_golden", "local", PASS, output[-500:] or "baseline matched")


def selftest(root: Path, with_golden: bool = True) -> int:
    tests = Path(__file__).resolve().parents[1] / "tests"
    local = LocalRunner(Path(os.environ.get("TMPDIR", "/tmp")) / "p2vm-selftest")
    print(f"P2-44 harness {__version__}: self-test")
    argv = [sys.executable, "-m", "unittest", "discover", "-s", str(tests), "-v"]
    result = local.run(argv, cwd=root, timeout=300, name="selftest", check=False)
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    if result.exit_code != 0:
        print(f"self-test log: {result.log}", file=sys.stderr)
        return EXIT_FAIL
    if with_golden:
        check = golden_check(root, local.log_dir)
        print(f"{check.id}: {check.status} {check.detail}")
        if check.status == FAIL:
            return EXIT_FAIL
    return EXIT_PASS


def _new_result_dir(config: RunConfig, sha: str | None, tag: str) -> Path:
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    suffix = f"-{sha[:8]}" if sha else ""
    base = config.result_root / f"{tag}-{stamp}{suffix}"
    candidate = base
    index = 2
    while candidate.exists():
        candidate = Path(str(base) + f"-{index}")
        index += 1
    candidate.mkdir(parents=True, exist_ok=False)
    return candidate


def _run_command_line(argv: list[str]) -> str:
    return "python3 " + " ".join(shlex.quote(part) for part in argv)


def _remote_task_command(config: RunConfig) -> str:
    remote_script = config.remote_test_dir + r"\vm\Invoke-Interactive.ps1"
    remote_acceptance = config.remote_test_dir + r"\vm\Remote-P2Acceptance.ps1"
    args = (
        f'-WorkRoot {config.remote_root} '
        f'-ResultDir {config.remote_validation} '
        f'-Suite {",".join(config.suites)} '
        f'-TimeoutMinutes {config.timeout_minutes}'
    )
    if config.bundle is not None:
        args += f' -BundleZip {config.remote_bundle}'
    if config.preflight_only:
        args += " -PreflightOnly"
    powershell = (
        f'powershell -NoProfile -ExecutionPolicy Bypass -File "{remote_script}" '
        f'-ScriptPath "{remote_acceptance}" '
        f'-ScriptArguments \'{args}\' '
        f'-TaskName 0c-P2VM-Acceptance -TimeoutMinutes {config.timeout_minutes + 10}'
    )
    return "ssh ... " + powershell


def plan(config: RunConfig) -> None:
    print("P2-44 Windows VM acceptance plan")
    print(f"  target:      {config.target}:{config.port}")
    print(f"  remote root: {config.remote_root}")
    print(f"  bundle:      {config.bundle or '(preflight only)'}")
    print(f"  suites:      {', '.join(config.suites)}")
    print(f"  result root: {config.result_root}")
    print("")
    print("1. copy platforms/windows/test to", config.remote_test_dir)
    print("2. copy the bundle to", config.remote_bundle)
    print("3. run in an interactive elevated scheduled task:")
    print("   " + _remote_task_command(config))
    print("4. copy logs and validation back, then analyze p2-trace.json")
    print("")
    print("No host, user or key is stored by the harness; see README.md.")


def _read_json(path: Path) -> dict[str, Any] | None:
    try:
        return json.loads(path.read_text(encoding="utf-8-sig"))
    except (OSError, json.JSONDecodeError, UnicodeDecodeError):
        return None


def _find_p2_result(validation_dir: Path) -> dict[str, Any] | None:
    direct = validation_dir / "p2-mixed" / "p2-mixed-result.json"
    if direct.is_file():
        return _read_json(direct)
    for path in sorted(validation_dir.rglob("p2-mixed-result.json")):
        return _read_json(path)
    return None


def _find_json(root: Path, name: str) -> dict[str, Any] | None:
    if not root.is_dir():
        return None
    for path in sorted(root.rglob(name)):
        data = _read_json(path)
        if data is not None:
            return data
    return None


def _environment(remote_logs: Path) -> dict[str, Any] | None:
    return _find_json(remote_logs, "environment.json") or _find_json(remote_logs, "remote-summary.json")


def _screenshots(*roots: Path) -> list[str]:
    out: list[str] = []
    for root in roots:
        if not root.is_dir():
            continue
        for path in sorted(root.rglob("*.png")):
            out.append(str(path))
    return out


def _cleanup_remote(remote: RemoteRunner, config: RunConfig, log_dir: Path) -> None:
    try:
        remote.run_raw(
            f'powershell -NoProfile -Command "Remove-Item -LiteralPath \'{config.remote_root}\' '
            f'-Recurse -Force -ErrorAction SilentlyContinue"',
            timeout=120, name="remote-cleanup", check=False,
        )
    except Exception:
        pass


def run(config: RunConfig, argv: list[str]) -> int:
    root = config.repo_root
    command = _run_command_line(argv)
    if config.dry_run:
        plan(config)
        print("dry-run command:", command)
        return EXIT_PASS

    config.validate(require_bundle=not config.preflight_only)
    sha = bundle_info(config.bundle)["sha256"] if config.bundle else None
    tag = "preflight" if config.preflight_only else "acceptance"
    result_dir = _new_result_dir(config, sha, tag)
    log_dir = result_dir / "logs"
    log_dir.mkdir(parents=True, exist_ok=True)
    local = LocalRunner(log_dir)
    remote = RemoteRunner(config, local)
    checks: list[Check] = []
    created = datetime.now().astimezone().isoformat(timespec="seconds")
    rev = revision(root)
    info = bundle_info(config.bundle)
    environment: dict[str, Any] | None = None
    suites: list[dict[str, Any]] = []
    live_reason = ""
    validation_dir = result_dir / "validation"
    remote_logs = result_dir / "logs" / "remote"

    if not config.skip_local_golden:
        checks.append(golden_check(root, log_dir))
    else:
        checks.append(Check("local.zhuyin_golden", "local", SKIP, "skipped by --skip-local-golden"))

    print(f"P2-44 harness {__version__}: {config.target}")
    print(f"result directory: {result_dir}")
    rc = EXIT_PASS
    try:
        print("checking SSH and VM reachability")
        probe = remote.probe()
        checks.append(Check(
            "vm.reachable", "infra", PASS,
            probe.stdout.strip().splitlines()[-1] if probe.stdout.strip() else "SSH reachable",
            {"log": str(probe.log)},
        ))
    except RunnerError as error:
        checks.append(Check("vm.reachable", "infra", NOT_RUN, str(error), {"log": str(error.log or "")}))
        live_reason = "no reachable Windows VM over SSH; live checks were not run"
        print("NOT RUN:", live_reason, file=sys.stderr)
        write_summary(
            result_dir, config=config, revision=rev, created=created, bundle=info,
            checks=checks, environment=None, suites=[], live_reason=live_reason,
            run_command=command, validation_dir=None,
            artifacts={"screenshots": [], "logs": str(log_dir)},
        )
        return EXIT_NOT_RUN

    try:
        # Reset the staging tree so a rerun cannot consume stale scripts or a
        # previous bundle.  Only the harness-owned root is touched.
        print("resetting remote staging")
        remote.run_raw(
            f'powershell -NoProfile -Command "Remove-Item -LiteralPath \'{config.remote_root}\' '
            f'-Recurse -Force -ErrorAction SilentlyContinue"',
            timeout=180, name="remote-reset", check=False,
        )
        remote.run_raw(
            f'powershell -NoProfile -Command "New-Item -ItemType Directory -Force -Path '
            f'\'{config.remote_root}\' | Out-Null"',
            timeout=60, name="remote-mkdir", check=True,
        )

        print("uploading acceptance scripts")
        remote.copy_to(
            root / "platforms" / "windows" / "test",
            config.remote_root + r"\repo\platforms\windows\test",
            recurse=True,
        )
        if config.bundle is not None:
            print("uploading bundle")
            remote.copy_to(config.bundle, config.remote_bundle)

        print("running remote harness in an interactive elevated task")
        remote_script = config.remote_test_dir + r"\vm\Remote-P2Acceptance.ps1"
        task = config.remote_test_dir + r"\vm\Invoke-Interactive.ps1"
        args = (
            f'-WorkRoot {config.remote_root} -ResultDir {config.remote_validation} '
            f'-Suite {",".join(config.suites)} -TimeoutMinutes {config.timeout_minutes}'
        )
        if config.bundle is not None:
            args += f' -BundleZip {config.remote_bundle}'
        if config.preflight_only:
            args += " -PreflightOnly"
        params = {
            "ScriptPath": remote_script,
            "ScriptArguments": args,
            "TaskName": "0c-P2VM-Acceptance",
            "TimeoutMinutes": str(config.timeout_minutes + 10),
            "LogPath": config.remote_logs + r"\interactive-task.log",
        }
        task_result = remote.run_powershell(
            task, params=params, timeout=config.timeout_minutes * 60 + 900,
            name="interactive-acceptance",
        )
        checks.append(Check(
            "vm.interactive_task", "infra", PASS,
            f"interactive task exited {task_result.exit_code}",
            {"log": str(task_result.log)},
        ))
    except RunnerError as error:
        checks.append(Check("vm.interactive_task", "infra", NOT_RUN, str(error), {"log": str(error.log or "")}))
        live_reason = "the interactive acceptance task could not be started or completed"
        print("NOT RUN:", live_reason, file=sys.stderr)
    else:
        print("copying logs and validation back")
        remote_logs.mkdir(parents=True, exist_ok=True)
        logs_ok = True
        validation_ok = True
        try:
            remote.copy_from(config.remote_logs, remote_logs, recurse=True, check=False)
        except RunnerError as error:
            logs_ok = False
            checks.append(Check("vm.logs_collected", "infra", FAIL, str(error), {"log": str(error.log or "")}))
        try:
            remote.copy_from(config.remote_validation, validation_dir, recurse=True, check=True)
        except RunnerError as error:
            validation_ok = False
            checks.append(Check(
                "vm.validation_collected", "infra", FAIL, str(error),
                {"log": str(error.log or "")},
            ))
        environment = _environment(remote_logs)
        if environment is None:
            # scp may place the directory one level down when the destination
            # already exists.
            environment = _environment(remote_logs / "logs")
        remote_summary = _find_json(remote_logs, "remote-summary.json")
        if remote_summary and remote_summary.get("fatal"):
            checks.append(Check(
                "vm.remote", "infra", FAIL, str(remote_summary["fatal"]),
                {"summary": str(remote_summary.get("when", ""))},
            ))
        if not logs_ok and not validation_ok:
            live_reason = "results were produced remotely but could not be copied back"
        suites = collect_suite_results(validation_dir)
        if not config.preflight_only:
            p2_result = _find_p2_result(validation_dir)
            add_p2_checks(checks, p2_result)
            checks.extend(app_checks(suites, config.suites, _build_number(environment)))
        if not config.keep_remote and validation_ok:
            _cleanup_remote(remote, config, log_dir)

    if config.preflight_only and not suites:
        environment = environment or _environment(remote_logs) or {}
        ok = bool(environment.get("preflight_ok"))
        blockers = environment.get("blockers") or environment.get("preflight_error") or []
        if isinstance(blockers, list):
            blockers = "; ".join(str(item) for item in blockers)
        checks.append(Check(
            "vm.preflight", "infra", PASS if ok else FAIL,
            "VM meets the Phase 2 preflight requirements" if ok
            else str(blockers or "preflight failed; see environment.json"),
        ))

    screenshots = _screenshots(validation_dir, remote_logs)
    summary = write_summary(
        result_dir, config=config, revision=rev, created=created, bundle=info,
        checks=checks, environment=environment, suites=suites,
        live_reason=live_reason, run_command=command, validation_dir=validation_dir,
        artifacts={
            "result_dir": str(result_dir),
            "logs": str(log_dir),
            "validation": str(validation_dir) if validation_dir.exists() else "",
            "screenshots": screenshots,
        },
    )
    status = summary["live_run"]["status"]
    print(f"live run: {status}")
    for check in checks:
        if check.status in (FAIL, NOT_RUN, SKIP):
            print(f"  {check.status:7} {check.id}: {check.detail}")
    print(f"summary: {result_dir / 'summary.md'}")
    if status == FAIL:
        return EXIT_FAIL
    if status == NOT_RUN:
        return EXIT_NOT_RUN
    return EXIT_PASS


def _build_number(environment: dict[str, Any] | None) -> int | None:
    if not environment:
        return None
    try:
        return int(environment.get("build"))
    except (TypeError, ValueError):
        return None


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="p2vm.py",
        description="P2-44 Windows 10/11 x64 VM acceptance harness (run on Linux or any host with ssh/scp).",
    )
    parser.add_argument("--version", action="version", version=f"P2-44 harness {__version__}")
    sub = parser.add_subparsers(dest="command", required=True)

    def remote_args(p: argparse.ArgumentParser) -> None:
        p.add_argument("--host", help="VM host/address (or P2VM_HOST)")
        p.add_argument("--user", help="VM user with administrator rights (or P2VM_USER)")
        p.add_argument("--key", help="SSH private key (or P2VM_KEY); never stored")
        p.add_argument("--port", type=int, default=22)
        p.add_argument("--bundle", help="Windows x64 preview/public-beta .zip (or P2VM_BUNDLE)")
        p.add_argument("--result-root", help="local result root (default build/windows/validation)")
        p.add_argument("--remote-root", default=r"C:\0c-p2vm")
        p.add_argument("--suite", help="comma-separated suites (default: the Phase 2 target apps)")
        p.add_argument("--timeout-minutes", type=int, default=150)
        p.add_argument("--skip-local-golden", action="store_true")
        p.add_argument("--keep-remote", action="store_true", help="leave the remote work root in place")
        p.add_argument("--dry-run", action="store_true", help="print the plan and commands without contacting the VM")
        p.add_argument("--ssh", help="ssh executable path")
        p.add_argument("--scp", help="scp executable path")
        p.add_argument("--ssh-option", action="append", metavar="KEY=VALUE")

    run_p = sub.add_parser("run", help="preflight, install, run interactive acceptance, collect results")
    remote_args(run_p)
    pre_p = sub.add_parser("preflight", help="check SSH, interactive desktop, OS and target apps without installing")
    remote_args(pre_p)
    pre_p.set_defaults(preflight_only=True)

    plan_p = sub.add_parser("plan", help="print what the harness would do")
    remote_args(plan_p)
    plan_p.set_defaults(dry_run=True)

    sub.add_parser("selftest", help="run the harness tests that work on Linux")
    sub.add_parser("suites", help="list suite names")
    return parser


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv if argv is None else argv)
    parser = build_parser()
    args = parser.parse_args(argv[1:])
    root = repo_root()
    try:
        if args.command == "selftest":
            return selftest(root)
        if args.command == "suites":
            print("p2-mixed")
            for name, label in APP_SUITES.items():
                print(f"{name:10} {label}")
            return EXIT_PASS
        config = from_args(args)
        if config.dry_run:
            config.validate(require_bundle=False)
            plan(config)
            return EXIT_PASS
        return run(config, argv)
    except ConfigError as error:
        print(f"p2vm: {error}", file=sys.stderr)
        return EXIT_USAGE
    except KeyboardInterrupt:
        return 130
    except Exception as error:  # keep infra failures distinct from product failures
        print(f"p2vm: harness error: {error}", file=sys.stderr)
        return EXIT_HARNESS


if __name__ == "__main__":
    sys.exit(main())
