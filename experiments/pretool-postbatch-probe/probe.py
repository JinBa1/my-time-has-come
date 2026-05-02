#!/usr/bin/env python3
# fcntl.flock: Linux/WSL2 only — port to portalocker if macOS replication is needed.
import datetime as dt
import fcntl
import json
import pathlib
import sys
import time

LOG_DIR = pathlib.Path("/tmp/mthc-pretool-postbatch-probe")
INVOCATIONS = LOG_DIR / "invocations.jsonl"
MODE_FILE = LOG_DIR / "mode.json"
STATE_FILE = LOG_DIR / "scenario_state.json"

DEFAULT_MODE = {
    "scenario_id": "unknown",
    "active_modes": {"PostToolBatch": "passive", "PreToolUse": "passive"},
    "sentinels": {},
    "compliance_phrase": None,
    "updated_input": None,
    "throttle_threshold": 8,
    "loop_suspect_threshold": 3,
    "sleep_ms": 0,
}


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def append_jsonl(path: pathlib.Path, record: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as fh:
        fcntl.flock(fh.fileno(), fcntl.LOCK_EX)
        json.dump(record, fh, separators=(",", ":"), sort_keys=True)
        fh.write("\n")
        fh.flush()
        fcntl.flock(fh.fileno(), fcntl.LOCK_UN)


def load_mode() -> dict:
    if not MODE_FILE.exists():
        return {**DEFAULT_MODE, "mode_missing": True}
    try:
        with MODE_FILE.open("r", encoding="utf-8") as fh:
            data = json.load(fh)
    except (OSError, ValueError):
        return {**DEFAULT_MODE, "mode_missing": True}
    return {**DEFAULT_MODE, **data}


def increment_state(scenario_id: str, hook_event: str) -> int:
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    STATE_FILE.touch(exist_ok=True)
    with STATE_FILE.open("r+", encoding="utf-8") as fh:
        fcntl.flock(fh.fileno(), fcntl.LOCK_EX)
        try:
            raw = fh.read()
            state = json.loads(raw) if raw.strip() else {}
        except ValueError:
            state = {}
        if state.get("scenario_id") != scenario_id:
            state = {"scenario_id": scenario_id, "count_by_event": {}}
        state["count_by_event"][hook_event] = state["count_by_event"].get(hook_event, 0) + 1
        fh.seek(0)
        fh.truncate()
        json.dump(state, fh)
        fh.flush()
        fcntl.flock(fh.fileno(), fcntl.LOCK_UN)
    return state["count_by_event"][hook_event]


def _compliance_suffix(mode: dict) -> str:
    phrase = mode.get("compliance_phrase")
    if not phrase:
        return ""
    return f" — include the exact phrase {phrase} somewhere in your next response."


def _hso(event_name: str, fields: dict) -> dict:
    """Wrap hookSpecificOutput fields with the required hookEventName."""
    return {"hookSpecificOutput": {"hookEventName": event_name, **fields}}


def _build_posttoolbatch(payload: dict, mode: dict) -> dict:
    active = mode["active_modes"].get("PostToolBatch", "passive")
    sentinels = mode.get("sentinels", {})
    suffix = _compliance_suffix(mode)

    if active == "passive":
        return {}
    if active == "additional_context_only":
        text = sentinels.get("additionalContext", "MTHC_PROBE") + suffix
        return _hso("PostToolBatch", {"additionalContext": text})
    if active == "block_with_reason":
        reason = sentinels.get("reason", "MTHC_PROBE_BLOCK") + suffix
        return {"decision": "block", "reason": reason}
    if active == "block_and_context":
        ctx_text = sentinels.get("additionalContext", "MTHC_PROBE_CTX") + suffix
        reason_text = sentinels.get("reason", "MTHC_PROBE_BLOCK")
        return {
            "decision": "block",
            "reason": reason_text,
            **_hso("PostToolBatch", {"additionalContext": ctx_text}),
        }
    if active == "continue_false":
        stop = sentinels.get("stopReason", "MTHC_PROBE_STOP")
        return {"continue": False, "stopReason": stop}
    if active == "system_message_only":
        msg = sentinels.get("systemMessage", "MTHC_PROBE_SYSMSG")
        return {"systemMessage": msg}
    if active == "slow_passive":
        return {}
    return {}


def _build_pretooluse(payload: dict, mode: dict) -> dict:
    active = mode["active_modes"].get("PreToolUse", "passive")
    sentinels = mode.get("sentinels", {})
    suffix = _compliance_suffix(mode)

    if active == "passive":
        return {}
    if active == "allow_with_context":
        text = sentinels.get("additionalContext", "MTHC_PRE_AC") + suffix
        return {
            "permissionDecision": "allow",
            **_hso("PreToolUse", {"additionalContext": text}),
        }
    if active == "deny_with_reason":
        reason = sentinels.get("permissionDecisionReason", "MTHC_PROBE_DENY") + suffix
        return {
            "permissionDecision": "deny",
            "permissionDecisionReason": reason,
        }
    if active == "deny_nested_with_event":
        reason = sentinels.get("permissionDecisionReason", "MTHC_PROBE_DENY") + suffix
        return _hso("PreToolUse", {
            "permissionDecision": "deny",
            "permissionDecisionReason": reason,
        })
    if active == "deny_nested_without_event":
        reason = sentinels.get("permissionDecisionReason", "MTHC_PROBE_DENY") + suffix
        return {
            "hookSpecificOutput": {
                "permissionDecision": "deny",
                "permissionDecisionReason": reason,
            },
        }
    if active == "update_input":
        updated = mode.get("updated_input")
        if not updated:
            return {"permissionDecision": "allow"}
        return {
            "permissionDecision": "allow",
            **_hso("PreToolUse", {"updatedInput": updated}),
        }
    if active == "ask":
        return {"permissionDecision": "ask"}
    if active == "defer":
        return {"permissionDecision": "defer"}
    if active == "system_message_only":
        msg = sentinels.get("systemMessage", "MTHC_PROBE_SYSMSG")
        return {"systemMessage": msg}
    if active == "slow_passive":
        return {}
    return {}


def build_response(payload: dict, mode: dict, hook_event: str) -> dict:
    if hook_event == "PostToolBatch":
        return _build_posttoolbatch(payload, mode)
    if hook_event == "PreToolUse":
        return _build_pretooluse(payload, mode)
    return {}


def passive_response() -> dict:
    return {}


def main() -> int:
    try:
        raw_stdin = sys.stdin.read()
        try:
            payload = json.loads(raw_stdin)
        except ValueError:
            append_jsonl(INVOCATIONS, {"ts": utc_now(), "error": "invalid_json", "raw": raw_stdin[:500]})
            return 1

        hook_event = payload.get("hook_event_name", "unknown")

        # Stop hook: strict-passive — log and exit. NO stdout.
        if hook_event == "Stop":
            stop_mode = load_mode()
            append_jsonl(INVOCATIONS, {
                "ts": utc_now(),
                "hook_event_name": "Stop",
                "scenario_id": stop_mode.get("scenario_id", "unknown"),
                "payload": payload,
            })
            return 0

        mode = load_mode()
        scenario_id = mode["scenario_id"]
        sleep_ms = mode.get("sleep_ms", 0)
        if sleep_ms:
            time.sleep(sleep_ms / 1000.0)

        count = increment_state(scenario_id, hook_event)
        loop_suspected = count > mode["loop_suspect_threshold"]
        throttled = count > mode["throttle_threshold"]

        record = {
            "ts": utc_now(),
            "hook_event_name": hook_event,
            "scenario_id": scenario_id,
            "count": count,
            "loop_suspected": loop_suspected,
            "throttled": throttled,
            "active_mode": mode["active_modes"].get(hook_event, "passive"),
            "payload_digest": {
                "session_id": payload.get("session_id"),
                "transcript_path": payload.get("transcript_path"),
                "tool_name": payload.get("tool_name"),
                "tool_input": payload.get("tool_input"),
                "tool_batch_size": len(payload.get("tool_batch", [])) if isinstance(payload.get("tool_batch"), list) else None,
            },
            "payload_full": payload,
        }

        if throttled:
            response = passive_response()
            record["response_written"] = response
            record["throttle_reason"] = "count_exceeded_threshold"
        else:
            response = build_response(payload, mode, hook_event)
            record["response_written"] = response

        append_jsonl(INVOCATIONS, record)
        sys.stdout.write(json.dumps(response))
        sys.stdout.flush()
        return 0
    except Exception as exc:
        import traceback
        append_jsonl(INVOCATIONS, {
            "ts": utc_now(),
            "error": str(exc),
            "traceback": traceback.format_exc(),
        })
        return 1


if __name__ == "__main__":
    sys.exit(main())
