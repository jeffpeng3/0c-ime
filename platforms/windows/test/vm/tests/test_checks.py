from __future__ import annotations

import copy
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from p2vm.checks import (  # noqa: E402
    analyze_p2_suite,
    candidate_language_label_issues,
)


def snapshot(**values):
    return {"revision": 1, "handled": True, "mode": "zhuyin", **values}


def good_result():
    profile = {
        "clsid": "{138C35D0-7F08-4CEF-BA90-C34C16301528}",
        "profiles": [
            {
                "language": "0x0404",
                "guid": "{EF62F743-9855-4C94-A658-1F0F98B7B840}",
                "display": "Mixed",
                "values": {"(default)": "Mixed"},
            }
        ],
        "language_tips": [
            "0404:{138C35D0-7F08-4CEF-BA90-C34C16301528}{EF62F743-9855-4C94-A658-1F0F98B7B840}"
        ],
    }
    trace = [
        {"step": "english.direct", "snapshot": snapshot(commit="hello")},
        {"step": "zhuyin.commit", "snapshot": snapshot(commit="你好")},
        {
            "step": "segment.compose",
            "snapshot": snapshot(preedit=[{"text": "麻婆豆腐"}], cursor={"rune": 4}),
        },
        {
            "step": "segment.cursor0.candidates",
            "snapshot": snapshot(
                preedit=[{"text": "麻婆"}, {"text": "豆腐"}],
                cursor={"rune": 0},
                candidates={
                    "start": 0,
                    "length": 4,
                    "items": [{"id": 1, "text": "麻婆豆腐", "label": "common"}],
                },
            ),
        },
        {
            "step": "segment.cursor2.candidates",
            "snapshot": snapshot(
                preedit=[{"text": "麻婆"}, {"text": "豆腐"}],
                cursor={"rune": 2},
                candidates={
                    "start": 2,
                    "length": 2,
                    "items": [{"id": 1, "text": "豆腐", "label": "common"}],
                },
            ),
        },
    ]
    return {
        "passed": True,
        "trace": trace,
        "profile": profile,
        "host": {"english_text": "hello", "candidate_visible": True, "segment_switch_visible": True},
    }


def statuses(checks):
    return {check.id: check.status for check in checks}


class AnalyzeGoodTraceTest(unittest.TestCase):
    def test_good_trace_passes_every_phase2_check(self):
        checks = analyze_p2_suite(good_result())
        self.assertTrue(checks)
        self.assertTrue(all(check.status == "PASS" for check in checks), [c.as_dict() for c in checks])
        self.assertEqual(
            set(statuses(checks)),
            {
                "input.segment_fixture",
                "candidate.segment_switch",
                "candidate.long_word_one",
                "input.english_direct",
                "input.zhuyin_commit",
                "candidate.no_language_labels",
                "profile.registered_single_mixed",
                "profile.no_legacy_ja",
                "profile.language_list_single_tip",
                "app.wpf_english_direct",
                "app.wpf_candidate_visible",
                "app.wpf_segment_switch_visible",
            },
        )

    def test_missing_result_fails_closed(self):
        checks = analyze_p2_suite(None)
        self.assertTrue(all(check.status == "FAIL" for check in checks))
        self.assertIn("candidate.long_word_one", statuses(checks))


class ProfileChecksTest(unittest.TestCase):
    def test_second_profile_fails(self):
        result = good_result()
        result["profile"] = copy.deepcopy(result["profile"])
        result["profile"]["profiles"].append({
            "language": "0x0411",
            "guid": "{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}",
            "display": "日本語",
            "values": {"(default)": "日本語"},
        })
        checks = statuses(analyze_p2_suite(result))
        self.assertEqual(checks["profile.registered_single_mixed"], "FAIL")
        self.assertEqual(checks["profile.no_legacy_ja"], "FAIL")

    def test_second_language_tip_fails(self):
        result = good_result()
        result["profile"]["language_tips"].append(
            "0411:{138C35D0-7F08-4CEF-BA90-C34C16301528}{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}"
        )
        checks = statuses(analyze_p2_suite(result))
        self.assertEqual(checks["profile.language_list_single_tip"], "FAIL")


class CandidateChecksTest(unittest.TestCase):
    def test_language_label_in_label_fails(self):
        result = good_result()
        result["trace"][3]["snapshot"]["candidates"]["items"][0]["label"] = "ja"
        checks = statuses(analyze_p2_suite(result))
        self.assertEqual(checks["candidate.no_language_labels"], "FAIL")

    def test_language_note_fails(self):
        result = good_result()
        result["trace"][3]["snapshot"]["candidates"]["items"][0]["note"] = "日文"
        issues = candidate_language_label_issues(result["trace"])
        self.assertTrue(any("note" in issue for issue in issues), issues)
        self.assertEqual(statuses(analyze_p2_suite(result))["candidate.no_language_labels"], "FAIL")

    def test_bracketed_language_title_fails(self):
        result = good_result()
        result["trace"][3]["snapshot"]["candidates"]["title"] = "候選〔日文〕"
        self.assertEqual(statuses(analyze_p2_suite(result))["candidate.no_language_labels"], "FAIL")

    def test_candidate_text_that_is_a_word_is_not_a_label(self):
        result = good_result()
        result["trace"][3]["snapshot"]["candidates"]["items"][0]["text"] = "日文"
        checks = statuses(analyze_p2_suite(result))
        self.assertEqual(checks["candidate.no_language_labels"], "PASS")

    def test_long_word_must_be_one_item(self):
        result = good_result()
        items = result["trace"][3]["snapshot"]["candidates"]["items"]
        items.append(dict(items[0], id=2))
        checks = statuses(analyze_p2_suite(result))
        self.assertEqual(checks["candidate.long_word_one"], "FAIL")

    def test_long_word_must_cover_four_slots(self):
        result = good_result()
        result["trace"][3]["snapshot"]["candidates"]["length"] = 2
        checks = statuses(analyze_p2_suite(result))
        self.assertEqual(checks["candidate.long_word_one"], "FAIL")
        self.assertEqual(checks["candidate.segment_switch"], "FAIL")

    def test_cursor_switch_must_change_range(self):
        result = good_result()
        result["trace"][4]["snapshot"]["candidates"]["start"] = 1
        checks = statuses(analyze_p2_suite(result))
        self.assertEqual(checks["candidate.segment_switch"], "FAIL")

    def test_missing_segment_step_fails(self):
        result = good_result()
        result["trace"] = [step for step in result["trace"] if step["step"] != "segment.cursor2.candidates"]
        checks = statuses(analyze_p2_suite(result))
        self.assertEqual(checks["candidate.segment_switch"], "FAIL")
        self.assertEqual(checks["candidate.long_word_one"], "PASS")


class InputChecksTest(unittest.TestCase):
    def test_english_must_commit_directly(self):
        result = good_result()
        result["trace"][0]["snapshot"]["commit"] = "ㄏㄜ"
        self.assertEqual(statuses(analyze_p2_suite(result))["input.english_direct"], "FAIL")

    def test_english_mode_switch_is_not_direct(self):
        result = good_result()
        result["trace"][0]["snapshot"]["mode"] = "english"
        check = [c for c in analyze_p2_suite(result) if c.id == "input.english_direct"][0]
        self.assertEqual(check.status, "FAIL")
        self.assertIn("switched to English mode", check.detail)

    def test_zhuyin_commit_mismatch_fails(self):
        result = good_result()
        result["trace"][1]["snapshot"]["commit"] = "妳好"
        self.assertEqual(statuses(analyze_p2_suite(result))["input.zhuyin_commit"], "FAIL")

    def test_wpf_segment_candidate_capture_must_change(self):
        result = good_result()
        result["host"]["segment_switch_visible"] = False
        result["host"]["segment_candidate_start"] = True
        result["host"]["segment_candidate_end"] = False
        check = [c for c in analyze_p2_suite(result) if c.id == "app.wpf_segment_switch_visible"][0]
        self.assertEqual(check.status, "FAIL")
        self.assertIn("start=True", check.detail)


if __name__ == "__main__":
    unittest.main()
