#!/usr/bin/env python3
import importlib.util
import pathlib
import unittest


SPIKE_PATH = pathlib.Path(__file__).with_name("deny_schema_spike.py")
SPEC = importlib.util.spec_from_file_location("deny_schema_spike", SPIKE_PATH)
spike = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(spike)


TARGET = "/tmp/mthc-pretool-postbatch-probe-workspace/target.txt"
CONTENT = "MTHC_SCHEMA_CONTENT_SENTINEL"


def read_tool_use(tool_use_id: str = "toolu_read") -> dict:
    return {
        "type": "assistant",
        "message": {
            "content": [
                {
                    "type": "tool_use",
                    "id": tool_use_id,
                    "name": "Read",
                    "input": {"file_path": TARGET},
                }
            ]
        },
    }


def pretool_hook(tool_use_id: str = "toolu_read", attachment_type: str = "hook_success") -> dict:
    return {
        "type": "attachment",
        "attachment": {
            "type": attachment_type,
            "hookName": "PreToolUse:Read",
            "hookEvent": "PreToolUse",
            "toolUseID": tool_use_id,
            "stdout": "{}",
            "stderr": "",
        },
    }


def tool_result(result_type: str, tool_use_id: str = "toolu_read", content: str = CONTENT) -> dict:
    return {
        "type": "user",
        "message": {
            "content": [
                {
                    "type": "tool_result",
                    "tool_use_id": tool_use_id,
                    "content": content,
                }
            ]
        },
        "toolUseResult": {
            "type": result_type,
            "file": {"filePath": TARGET, "content": content},
        },
    }


def assistant_text(text: str) -> dict:
    return {
        "type": "assistant",
        "message": {"content": [{"type": "text", "text": text}]},
    }


class TranscriptClassificationTest(unittest.TestCase):
    def test_fresh_tool_execution_is_classified_from_tool_result(self) -> None:
        result = spike.classify_transcript_records(
            [read_tool_use(), pretool_hook(), tool_result("text"), assistant_text(CONTENT)],
            TARGET,
            CONTENT,
        )

        self.assertEqual(result["outcome"], "fresh_tool_executed")
        self.assertTrue(result["content_marker_in_tool_result"])
        self.assertTrue(result["content_marker_in_assistant_text"])

    def test_assistant_text_marker_without_tool_result_is_not_fresh_execution(self) -> None:
        result = spike.classify_transcript_records(
            [read_tool_use(), pretool_hook(), assistant_text(CONTENT)],
            TARGET,
            CONTENT,
        )

        self.assertEqual(result["outcome"], "no_tool_result_after_hook")
        self.assertFalse(result["content_marker_in_tool_result"])
        self.assertTrue(result["content_marker_in_assistant_text"])

    def test_cached_file_result_is_not_fresh_execution(self) -> None:
        result = spike.classify_transcript_records(
            [read_tool_use(), pretool_hook(), tool_result("file_unchanged", content="File unchanged"), assistant_text(CONTENT)],
            TARGET,
            CONTENT,
        )

        self.assertEqual(result["outcome"], "cached_result")
        self.assertFalse(result["content_marker_in_tool_result"])

    def test_hook_error_is_reported_separately(self) -> None:
        result = spike.classify_transcript_records(
            [read_tool_use(), pretool_hook(attachment_type="hook_error")],
            TARGET,
            CONTENT,
        )

        self.assertEqual(result["outcome"], "hook_runtime_error")
        self.assertEqual(result["hook_errors"], 1)

    def test_blocking_hook_error_means_tool_was_blocked(self) -> None:
        result = spike.classify_transcript_records(
            [read_tool_use(), pretool_hook(attachment_type="hook_blocking_error"), assistant_text("blocked")],
            TARGET,
            CONTENT,
        )

        self.assertEqual(result["outcome"], "hook_blocked_tool")
        self.assertEqual(result["blocking_errors"], 1)

    def test_s16_is_exploratory(self) -> None:
        self.assertEqual(spike.SCENARIOS["S16"]["expected_outcome"], "exploratory")


if __name__ == "__main__":
    unittest.main()
