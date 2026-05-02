#!/usr/bin/env python3
import importlib.util
import pathlib
import unittest


PROBE_PATH = pathlib.Path(__file__).with_name("probe.py")
SPEC = importlib.util.spec_from_file_location("probe", PROBE_PATH)
probe = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(probe)


def pretool_mode(active_mode: str) -> dict:
    return {
        "active_modes": {"PostToolBatch": "passive", "PreToolUse": active_mode},
        "sentinels": {"permissionDecisionReason": "DENY_SENTINEL"},
        "compliance_phrase": None,
    }


class PreToolUseDenySchemaTest(unittest.TestCase):
    def test_existing_deny_with_reason_remains_top_level_control(self) -> None:
        response = probe.build_response({}, pretool_mode("deny_with_reason"), "PreToolUse")

        self.assertEqual(response, {
            "permissionDecision": "deny",
            "permissionDecisionReason": "DENY_SENTINEL",
        })

    def test_nested_deny_includes_hook_event_name(self) -> None:
        response = probe.build_response({}, pretool_mode("deny_nested_with_event"), "PreToolUse")

        self.assertEqual(response, {
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": "deny",
                "permissionDecisionReason": "DENY_SENTINEL",
            },
        })

    def test_nested_deny_without_event_omits_hook_event_name(self) -> None:
        response = probe.build_response({}, pretool_mode("deny_nested_without_event"), "PreToolUse")

        self.assertEqual(response, {
            "hookSpecificOutput": {
                "permissionDecision": "deny",
                "permissionDecisionReason": "DENY_SENTINEL",
            },
        })


if __name__ == "__main__":
    unittest.main()
