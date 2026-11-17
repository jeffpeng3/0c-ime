"""Report writing: machine-readable summary, human summary and bug bundle."""

from __future__ import annotations

import hashlib
import json
import zipfile
from datetime import datetime
from pathlib import Path
from typing import Any, Iterable

from .checks import analyze_p2_suite
from .config import APP_SUITES, RunConfig
from .model import FAIL, NOT_RUN, PASS, SKIP, Check, overall_status

SECRET_PLACEHOLDER = "<ssh-key>"
MAX_TAIL_BYTES = 32_000


def utc_now() -> str:
    return datetime.now().astimezone().isoformat(timespec="seconds")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def bundle_info(path: Path | None) -> dict[str, Any] | None:
    if path is None:
        return None
    info: dict[str, Any] = {
        "path": str(path),
        "bytes": path.stat().st_size,
        "sha256": sha256_file(path),
    }
    try:
        with zipfile.ZipFile(path) as archive:
            for name in archive.namelist():
                if name.endswith("release.json"):
                    release = json.loads(archive.read(name).decode("utf-8-sig"))
                    info["version"] = release.get("version")
                    info["channel"] = release.get("channel")
                    info["os"] = release.get("os")
                    info["arch"] = release.get("arch")
                    break
    except (zipfile.BadZipFile, KeyError, json.JSONDecodeError, UnicodeDecodeError) as error:
        info["release_error"] = str(error)
    return info


def collect_suite_results(validation_dir: Path) -> list[dict[str, Any]]:
    """Read every per-suite result JSON below an acceptance result directory."""
    out: list[dict[str, Any]] = []
    if not validation_dir.is_dir():
        return out
    for path in sorted(validation_dir.rglob("*-result.json")):
        if path.name in {"acceptance-result.json", "vm-run-summary.json"}:
            continue
        try:
            data = json.loads(path.read_text(encoding="utf-8-sig"))
        except (json.JSONDecodeError, UnicodeDecodeError):
            continue
        suite = path.parent.name
        out.append({
            "suite": suite,
            "result_file": str(path),
            "passed": bool(data.get("passed")),
            "skipped": str(data.get("skipped") or ""),
            "error": str(data.get("error") or ""),
            "checks": list(data.get("checks") or []),
            "result": data,
        })
    return out


def app_checks(suites: Iterable[dict[str, Any]], requested: Iterable[str],
               os_build: int | None) -> list[Check]:
    by_suite = {item["suite"]: item for item in suites}
    checks: list[Check] = []
    for suite in requested:
        app = APP_SUITES.get(suite)
        if not app:
            continue
        result = by_suite.get(suite)
        check_id = f"app.{suite}"
        if suite == "search" and os_build is not None and os_build < 22000:
            checks.append(Check(check_id, "app", SKIP, f"{app}: needs Windows 11 (build {os_build})"))
        elif result is None:
            checks.append(Check(check_id, "app", NOT_RUN, f"{app}: suite did not produce a result"))
        elif result["skipped"]:
            checks.append(Check(check_id, "app", SKIP, f"{app}: {result['skipped']}", {"suite": suite}))
        elif result["passed"]:
            checks.append(Check(check_id, "app", PASS, f"{app}: {len(result['checks'])} checks passed", {"suite": suite}))
        elif suite == "search" and os_build is not None and os_build < 22000:
            checks.append(Check(check_id, "app", SKIP, f"{app}: needs Windows 11 (build {os_build})", {"suite": suite}))
        else:
            checks.append(Check(check_id, "app", FAIL, f"{app}: {result['error'] or 'suite failed'}", {"suite": suite}))
    return checks


def suite_findings(suites: Iterable[dict[str, Any]]) -> list[str]:
    findings = []
    for item in suites:
        if item["skipped"] or item["passed"]:
            continue
        findings.append(f"{item['suite']}: {item['error'] or 'suite failed'}")
    return findings


def redact(text: str, config: RunConfig) -> str:
    text = text.replace(str(config.key), SECRET_PLACEHOLDER)
    return text


def _tail(path: Path) -> str:
    try:
        data = path.read_bytes()
    except OSError:
        return ""
    if len(data) > MAX_TAIL_BYTES:
        data = data[-MAX_TAIL_BYTES:]
    return data.decode("utf-8", "replace")


def collect_log_tails(result_dir: Path, validation_dir: Path | None) -> dict[str, str]:
    tails: dict[str, str] = {}
    for path in sorted((result_dir / "logs").glob("*.log")):
        if path.name.endswith("-local-golden.log") or path.stat().st_size < 4_000_000:
            tails[str(path.relative_to(result_dir))] = _tail(path)
    candidates: list[Path] = []
    if validation_dir and validation_dir.is_dir():
        candidates += list(validation_dir.rglob("tsf.log"))
        candidates += list(validation_dir.rglob("server.err"))
        candidates += list(validation_dir.rglob("server.out"))
        candidates += list(validation_dir.rglob("*.log"))
    for path in candidates:
        if path.stat().st_size == 0:
            continue
        key = str(path.relative_to(result_dir)) if result_dir in path.parents else str(path)
        if key not in tails:
            tails[key] = _tail(path)
    return tails


def write_summary(result_dir: Path, *, config: RunConfig, revision: str, created: str,
                  bundle: dict[str, Any] | None, checks: list[Check],
                  environment: dict[str, Any] | None, suites: list[dict[str, Any]],
                  live_reason: str, run_command: str, artifacts: dict[str, Any] | None = None,
                  validation_dir: Path | None = None) -> dict[str, Any]:
    result_dir.mkdir(parents=True, exist_ok=True)
    live = live_reason == ""
    status = overall_status(checks, live=live)
    if not live and status != FAIL:
        status = NOT_RUN
    failures = [check for check in checks if check.status == FAIL]
    summary = {
        "schema": 1,
        "task": "P2-44",
        "revision": revision,
        "created": created,
        "live_run": {
            "status": status,
            "reason": live_reason,
            "command": redact(run_command, config),
        },
        "target": {"host": config.host, "user": config.user, "port": config.port},
        "bundle": bundle,
        "environment": environment,
        "checks": [check.as_dict() for check in checks],
        "suites": [
            {
                "suite": item["suite"],
                "passed": item["passed"],
                "skipped": item["skipped"],
                "error": item["error"],
                "checks": len(item["checks"]),
            }
            for item in suites
        ],
        "failures": [
            {
                "id": check.id,
                "detail": check.detail,
                "evidence": check.evidence,
            }
            for check in failures
        ],
        "suite_findings": suite_findings(suites),
        "artifacts": artifacts or {},
    }
    (result_dir / "summary.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    (result_dir / "summary.md").write_text(
        render_markdown(summary), encoding="utf-8"
    )
    if failures or suite_findings(suites):
        (result_dir / "bug-report.md").write_text(
            render_bug_report(result_dir, summary, config, validation_dir), encoding="utf-8"
        )
    return summary


def render_markdown(summary: dict[str, Any]) -> str:
    lines = [
        "# P2-44 Windows VM acceptance",
        "",
        f"- Revision: `{summary['revision']}`",
        f"- Created: {summary['created']}",
        f"- Live run: **{summary['live_run']['status']}**",
    ]
    reason = summary["live_run"].get("reason")
    if reason:
        lines.append(f"- Reason: {reason}")
    lines.append("")
    bundle = summary.get("bundle")
    if bundle:
        lines += [
            "## Bundle",
            "",
            f"- `{bundle.get('path')}`",
            f"- SHA-256 `{bundle.get('sha256')}`",
            f"- Version `{bundle.get('version', '?')}` / {bundle.get('channel', '?')} / {bundle.get('os', '?')}-{bundle.get('arch', '?')}",
            "",
        ]
    env = summary.get("environment")
    if env:
        lines += [
            "## VM",
            "",
            f"- Computer: `{env.get('computer', '?')}`",
            f"- OS: {env.get('os', '?')} (build {env.get('build', '?')})",
            f"- Architecture: {env.get('arch', '?')}",
            f"- Interactive: {env.get('interactive', '?')}",
            f"- Administrator: {env.get('admin', '?')}",
            "",
        ]
    lines += ["## Checks", "", "| Check | Scope | Result | Detail |", "|---|---|---|---|"]
    for check in summary["checks"]:
        detail = str(check.get("detail", "")).replace("|", "\\|").replace("\n", " ")
        lines.append(f"| `{check['id']}` | {check['scope']} | {check['status']} | {detail} |")
    lines.append("")
    if summary["suites"]:
        lines += ["## Suites", "", "| Suite | Result | Skipped | Error |", "|---|---|---|---|"]
        for suite in summary["suites"]:
            result = "PASS" if suite["passed"] and not suite["skipped"] else ("SKIP" if suite["skipped"] else "FAIL")
            error = str(suite["error"]).replace("|", "\\|").replace("\n", " ")[:300]
            lines.append(f"| {suite['suite']} | {result} | {suite['skipped']} | {error} |")
        lines.append("")
    if summary["failures"] or summary["suite_findings"]:
        lines += ["## Failures", ""]
        for item in summary["failures"]:
            lines.append(f"- `{item['id']}`: {item['detail']}")
        for item in summary["suite_findings"]:
            lines.append(f"- suite: {item}")
        lines.append("")
        lines.append("See `bug-report.md` for logs, screenshots and the rerun command.")
        lines.append("")
    artifacts = summary.get("artifacts") or {}
    if artifacts.get("screenshots"):
        lines += ["## Screenshots", ""]
        for shot in artifacts["screenshots"]:
            lines.append(f"- `{shot}`")
        lines.append("")
    lines += [
        "## Reproduce",
        "",
        "```sh",
        summary["live_run"]["command"],
        "```",
        "",
    ]
    return "\n".join(lines)


def render_bug_report(result_dir: Path, summary: dict[str, Any], config: RunConfig,
                      validation_dir: Path | None) -> str:
    lines = [
        "# P2-44 acceptance failure report",
        "",
        "The harness only reports this failure. It does not change product code or fix the shell.",
        "",
        "## Identity",
        "",
        f"- Revision: `{summary['revision']}`",
        f"- Created: {summary['created']}",
    ]
    bundle = summary.get("bundle") or {}
    if bundle:
        lines += [
            f"- Bundle: `{bundle.get('path')}`",
            f"- Bundle SHA-256: `{bundle.get('sha256')}`",
            f"- Bundle version: `{bundle.get('version', '?')}`",
        ]
    env = summary.get("environment") or {}
    lines += [
        f"- VM: `{env.get('computer', '?')}` / {env.get('os', '?')} build {env.get('build', '?')} / {env.get('arch', '?')}",
        f"- Target: `{summary['target']['user']}@{summary['target']['host']}:{summary['target']['port']}`",
        "",
        "## Failed checks",
        "",
    ]
    for item in summary["failures"]:
        lines.append(f"### `{item['id']}`")
        lines.append("")
        lines.append(str(item["detail"]))
        evidence = item.get("evidence")
        if evidence:
            lines += ["", "```json", json.dumps(evidence, ensure_ascii=False, indent=2)[:8000], "```"]
        lines.append("")
    if summary["suite_findings"]:
        lines += ["## Failed suites", ""]
        for item in summary["suite_findings"]:
            lines.append(f"- {item}")
        lines.append("")
    lines += ["## Environments and apps", "", "```json",
              json.dumps(env.get("apps", {}), ensure_ascii=False, indent=2), "```", ""]
    lines += ["## Logs (tail)", ""]
    tails = collect_log_tails(result_dir, validation_dir)
    for name, text in tails.items():
        lines += [f"### `{name}`", "", "```text", redact(text, config), "```", ""]
    screenshots = (summary.get("artifacts") or {}).get("screenshots") or []
    if screenshots:
        lines += ["## Screenshots", ""]
        for shot in screenshots:
            lines.append(f"- `{shot}`")
        lines.append("")
    else:
        lines += ["## Screenshots", "", "No screenshots were collected. Check the remote validation directory.", ""]
    lines += [
        "## Rerun",
        "",
        "```sh",
        summary["live_run"]["command"],
        "```",
        "",
        "Attach this directory (or at least `summary.json`, `bug-report.md`, the failed suite result JSON,",
        "the matching `tsf.log` tail and screenshots) to the issue.",
        "",
    ]
    return "\n".join(lines)


def add_p2_checks(checks: list[Check], p2_result: dict[str, Any] | None) -> None:
    checks.extend(analyze_p2_suite(p2_result))
