"""Pure checks over the Phase 2 acceptance trace.

The Windows suite records every protocol request/response to ``p2-trace.json``
and the raw registry/profile view to ``p2-profile.json``.  Keeping the decisions
here means the same logic runs in the harness self-tests on Linux, and a failed
trace can be inspected later without a VM.
"""

from __future__ import annotations

import re
from typing import Any, Iterable

from .model import FAIL, PASS, Check

# Engine labels are reasons ("common", "related", "personal"), never a language.
ALLOWED_CANDIDATE_LABELS = {"", "common", "related", "personal"}
LANGUAGE_WORDS = {
    "中文": "zh",
    "日文": "ja",
    "日本語": "ja",
    "注音": "zh-Zhuyin",
    "英文": "en",
    "英語": "en",
    "zh": "zh",
    "ja": "ja",
    "en": "en",
    "zh-hant": "zh",
    "ja-jp": "ja",
    "zh-tw": "zh",
    "ja_jp": "ja",
}
BRACKETED_LANGUAGE = re.compile(
    r"[〔【\[（(]\s*(中文|日文|日本語|注音|英文|英語|zh(?:-hant|-tw)?|ja(?:-jp)?|en)\s*[〕】\]）)]",
    re.IGNORECASE,
)
MIXED_GUID = "{EF62F743-9855-4C94-A658-1F0F98B7B840}"
LEGACY_JA_GUID = "{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}"
ZUIN_CLSID = "{138C35D0-7F08-4CEF-BA90-C34C16301528}"


def _steps(trace: Iterable[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    return {str(step.get("step")): step for step in trace}


def _snapshot(step: dict[str, Any] | None) -> dict[str, Any]:
    if not step:
        return {}
    snapshot = step.get("snapshot")
    return snapshot if isinstance(snapshot, dict) else {}


def _preedit_text(snapshot: dict[str, Any]) -> str:
    return "".join(
        str(segment.get("text", ""))
        for segment in snapshot.get("preedit") or []
        if isinstance(segment, dict)
    )


def _candidate_items(snapshot: dict[str, Any]) -> list[dict[str, Any]]:
    candidates = snapshot.get("candidates")
    if not isinstance(candidates, dict):
        return []
    items = candidates.get("items") or []
    return [item for item in items if isinstance(item, dict)]


def _candidate_field(snapshot: dict[str, Any], name: str) -> Any:
    candidates = snapshot.get("candidates")
    return candidates.get(name) if isinstance(candidates, dict) else None


def candidate_language_label_issues(trace: Iterable[dict[str, Any]]) -> list[str]:
    """Return every language-looking label in candidate UI output.

    The UI receives candidate text, note, title, suggestion, notice and reading
    (``UiCandidate`` has no label field at all).  Candidate *text* is excluded
    on purpose: a legitimate dictionary word such as 「日文」 is content, not a
    language label.  Labels, notes, titles and bracket markers are metadata.
    """
    issues: list[str] = []
    for step in trace:
        snapshot = _snapshot(step)
        name = step.get("step", "?")
        candidates = snapshot.get("candidates")
        if isinstance(candidates, dict):
            title = str(candidates.get("title") or "")
            if title and BRACKETED_LANGUAGE.search(title):
                issues.append(f"{name}: candidate title carries a language tag: {title!r}")
            if title.strip().lower() in LANGUAGE_WORDS:
                issues.append(f"{name}: candidate title is a language label: {title!r}")
            for index, item in enumerate(_candidate_items(snapshot)):
                label = str(item.get("label") or "")
                if label.lower() not in ALLOWED_CANDIDATE_LABELS:
                    issues.append(
                        f"{name}: candidate #{index} label {label!r} is not a neutral reason"
                    )
                note = str(item.get("note") or "").strip()
                if note.lower() in LANGUAGE_WORDS:
                    issues.append(
                        f"{name}: candidate #{index} note is a language label: {note!r}"
                    )
        for field in ("suggestion", "notice", "reading"):
            value = str(snapshot.get(field) or "")
            if value and BRACKETED_LANGUAGE.search(value):
                issues.append(f"{name}: snapshot {field} carries a language tag: {value!r}")
    return issues


def _commit(snapshot: dict[str, Any]) -> str:
    return str(snapshot.get("commit") or "")


def check_english_direct(trace: Iterable[dict[str, Any]]) -> Check:
    steps = _steps(trace)
    step = steps.get("english.direct")
    if not step:
        return Check("input.english_direct", "vm", FAIL, "english.direct step missing")
    snapshot = _snapshot(step)
    mode = str(snapshot.get("mode") or "")
    if _commit(snapshot) != "hello":
        return Check(
            "input.english_direct",
            "vm",
            FAIL,
            f'expected commit "hello", got {_commit(snapshot)!r} (preedit {_preedit_text(snapshot)!r})',
            {"step": "english.direct"},
        )
    if mode == "english":
        return Check(
            "input.english_direct",
            "vm",
            FAIL,
            "English text committed only after the session switched to English mode; Mixed profile must type it directly",
            {"step": "english.direct", "mode": mode},
        )
    return Check(
        "input.english_direct",
        "vm",
        PASS,
        f'English text committed directly in Mixed profile (mode={mode or "unset"})',
        {"step": "english.direct"},
    )


def check_zhuyin_smoke(trace: Iterable[dict[str, Any]]) -> Check:
    steps = _steps(trace)
    step = steps.get("zhuyin.commit")
    if not step:
        return Check("input.zhuyin_commit", "vm", FAIL, "zhuyin.commit step missing")
    snapshot = _snapshot(step)
    if _commit(snapshot) != "你好":
        return Check(
            "input.zhuyin_commit",
            "vm",
            FAIL,
            f'expected commit "你好", got {_commit(snapshot)!r} (preedit {_preedit_text(snapshot)!r})',
            {"step": "zhuyin.commit"},
        )
    return Check("input.zhuyin_commit", "vm", PASS, 'zhuyin keys committed "你好"', {"step": "zhuyin.commit"})


def check_segment_switch(trace: Iterable[dict[str, Any]]) -> Check:
    steps = _steps(trace)
    first = steps.get("segment.cursor0.candidates")
    second = steps.get("segment.cursor2.candidates")
    if not first or not second:
        missing = [name for name, step in (("segment.cursor0.candidates", first),
                                           ("segment.cursor2.candidates", second)) if not step]
        return Check(
            "candidate.segment_switch",
            "vm",
            FAIL,
            "segment cursor steps missing: " + ", ".join(missing),
        )
    first_snap, second_snap = _snapshot(first), _snapshot(second)
    first_start = _candidate_field(first_snap, "start")
    first_length = _candidate_field(first_snap, "length")
    second_start = _candidate_field(second_snap, "start")
    second_length = _candidate_field(second_snap, "length")
    # 麻婆豆腐 covers all four slots at the start; 豆腐 covers two slots at 2.
    if (first_start, first_length) != (0, 4):
        return Check(
            "candidate.segment_switch",
            "vm",
            FAIL,
            f"cursor at segment 0 expected candidates start=0 length=4, got start={first_start} length={first_length}",
            {"step": "segment.cursor0.candidates"},
        )
    if (second_start, second_length) != (2, 2):
        return Check(
            "candidate.segment_switch",
            "vm",
            FAIL,
            f"cursor at segment 2 expected start=2 length=2, got start={second_start} length={second_length}",
            {"step": "segment.cursor2.candidates"},
        )
    if not _candidate_items(first_snap) or not _candidate_items(second_snap):
        return Check(
            "candidate.segment_switch",
            "vm",
            FAIL,
            "candidate list was empty at one of the cursor positions",
            {"steps": ["segment.cursor0.candidates", "segment.cursor2.candidates"]},
        )
    first_text = [_preedit_text(first_snap), first_snap.get("cursor", {}).get("rune")]
    second_text = [_preedit_text(second_snap), second_snap.get("cursor", {}).get("rune")]
    return Check(
        "candidate.segment_switch",
        "vm",
        PASS,
        f"candidate range followed the segment cursor: {first_start}/{first_length} -> {second_start}/{second_length}",
        {"first": first_text, "second": second_text},
    )


def check_long_word(trace: Iterable[dict[str, Any]]) -> Check:
    steps = _steps(trace)
    step = steps.get("segment.cursor0.candidates")
    if not step:
        return Check("candidate.long_word_one", "vm", FAIL, "segment.cursor0.candidates step missing")
    snapshot = _snapshot(step)
    items = _candidate_items(snapshot)
    matches = [item for item in items if item.get("text") == "麻婆豆腐"]
    if not matches:
        return Check(
            "candidate.long_word_one",
            "vm",
            FAIL,
            "long-word candidate 麻婆豆腐 missing from the candidate page; got "
            + repr([item.get("text") for item in items]),
            {"step": "segment.cursor0.candidates"},
        )
    if len(matches) != 1:
        return Check(
            "candidate.long_word_one",
            "vm",
            FAIL,
            f"long word 麻婆豆腐 appeared {len(matches)} times; a lexicon match must stay one candidate",
            {"step": "segment.cursor0.candidates"},
        )
    length = _candidate_field(snapshot, "length")
    if length != 4:
        return Check(
            "candidate.long_word_one",
            "vm",
            FAIL,
            f"long word 麻婆豆腐 covered {length} composition slots, expected 4",
            {"step": "segment.cursor0.candidates"},
        )
    return Check(
        "candidate.long_word_one",
        "vm",
        PASS,
        "long-word match 麻婆豆腐 is exactly one candidate covering four input slots",
        {"step": "segment.cursor0.candidates"},
    )


def check_segment_text(trace: Iterable[dict[str, Any]]) -> Check:
    steps = _steps(trace)
    step = steps.get("segment.compose")
    if not step:
        return Check("input.segment_fixture", "vm", FAIL, "segment.compose step missing")
    text = _preedit_text(_snapshot(step))
    if text != "麻婆豆腐":
        return Check(
            "input.segment_fixture",
            "vm",
            FAIL,
            f'fixture expected preedit "麻婆豆腐", got {text!r}',
            {"step": "segment.compose"},
        )
    return Check("input.segment_fixture", "vm", PASS, 'preedit is "麻婆豆腐"', {"step": "segment.compose"})


def _normalise(value: str) -> str:
    return value.strip().upper()


def analyze_profile(profile: dict[str, Any] | None) -> list[Check]:
    checks: list[Check] = []
    if not profile:
        return [
            Check("profile.registered_single_mixed", "vm", FAIL, "profile report missing from the suite"),
            Check("profile.no_legacy_ja", "vm", FAIL, "profile report missing from the suite"),
            Check("profile.language_list_single_tip", "vm", FAIL, "profile report missing from the suite"),
        ]

    registered = [p for p in profile.get("profiles") or [] if isinstance(p, dict)]
    mixed = [p for p in registered if _normalise(str(p.get("guid") or "")) == MIXED_GUID]
    legacy = [p for p in registered if _normalise(str(p.get("guid") or "")) == LEGACY_JA_GUID]
    described = [
        p for p in registered
        if "mixed" in " ".join(str(v) for v in (p.get("values") or {}).values()).lower()
        or "mixed" in str(p.get("display") or "").lower()
    ]
    if len(registered) == 1 and len(mixed) == 1 and described:
        checks.append(Check(
            "profile.registered_single_mixed",
            "vm",
            PASS,
            "exactly one TSF language profile is registered, and it is Mixed",
            {"profiles": registered},
        ))
    else:
        checks.append(Check(
            "profile.registered_single_mixed",
            "vm",
            FAIL,
            f"expected exactly one Mixed profile, got {len(registered)} profile(s): "
            + repr([(p.get("language"), p.get("guid"), p.get("display")) for p in registered]),
            {"profiles": registered},
        ))

    if legacy:
        checks.append(Check(
            "profile.no_legacy_ja",
            "vm",
            FAIL,
            "legacy ja-JP profile is still registered",
            {"legacy": legacy},
        ))
    else:
        checks.append(Check("profile.no_legacy_ja", "vm", PASS, "no legacy ja-JP profile remains"))

    tips = [str(tip) for tip in profile.get("language_tips") or []]
    ours = [tip for tip in tips if ZUIN_CLSID in _normalise(tip)]
    legacy_tips = [tip for tip in ours if LEGACY_JA_GUID in _normalise(tip)]
    if len(ours) == 1 and not legacy_tips:
        checks.append(Check(
            "profile.language_list_single_tip",
            "vm",
            PASS,
            "the user's language list contains one Zuin input tip",
            {"tips": ours},
        ))
    else:
        checks.append(Check(
            "profile.language_list_single_tip",
            "vm",
            FAIL,
            f"language list should contain exactly one Zuin tip, found {len(ours)}"
            + (f"; legacy ja-JP tips: {legacy_tips!r}" if legacy_tips else ""),
            {"tips": ours},
        ))
    return checks


def analyze_p2_suite(result: dict[str, Any] | None) -> list[Check]:
    """Turn ``p2-mixed-result.json`` into report checks."""
    if not result:
        return [
            Check("candidate.no_language_labels", "vm", FAIL, "p2-mixed result missing"),
            Check("candidate.segment_switch", "vm", FAIL, "p2-mixed result missing"),
            Check("candidate.long_word_one", "vm", FAIL, "p2-mixed result missing"),
            Check("input.english_direct", "vm", FAIL, "p2-mixed result missing"),
            Check("input.zhuyin_commit", "vm", FAIL, "p2-mixed result missing"),
        ]
    trace = [step for step in result.get("trace") or [] if isinstance(step, dict)]
    checks = [
        check_segment_text(trace),
        check_segment_switch(trace),
        check_long_word(trace),
        check_english_direct(trace),
        check_zhuyin_smoke(trace),
    ]
    issues = candidate_language_label_issues(trace)
    checks.append(Check(
        "candidate.no_language_labels",
        "vm",
        FAIL if issues else PASS,
        "candidate UI metadata contains language labels: " + "; ".join(issues[:5])
        if issues else "candidate output carries neutral labels only",
        {"issues": issues},
    ))
    checks.extend(analyze_profile(result.get("profile")))

    host = result.get("host") or {}
    if host.get("english_text") == "hello":
        checks.append(Check("app.wpf_english_direct", "app", PASS, "WPF text box received hello without toggling"))
    else:
        checks.append(Check(
            "app.wpf_english_direct",
            "app",
            FAIL,
            f"WPF text box expected hello, got {host.get('english_text')!r}",
        ))
    if host.get("candidate_visible"):
        checks.append(Check("app.wpf_candidate_visible", "app", PASS, "WPF candidate window was visible"))
    else:
        checks.append(Check("app.wpf_candidate_visible", "app", FAIL, "WPF candidate window was not visible"))
    if host.get("segment_switch_visible"):
        checks.append(Check(
            "app.wpf_segment_switch_visible",
            "app",
            PASS,
            "candidate window reopened for both cursor segments and the two captures differ",
        ))
    else:
        checks.append(Check(
            "app.wpf_segment_switch_visible",
            "app",
            FAIL,
            "moving the WPF composition cursor to another segment did not produce two different candidate windows"
            f" (start={bool(host.get('segment_candidate_start'))}, end={bool(host.get('segment_candidate_end'))})",
        ))
    return checks
