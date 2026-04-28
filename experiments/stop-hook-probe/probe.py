#!/usr/bin/env python3

import argparse
import datetime as dt
import json
import os
import pathlib
import sys
import time
import uuid

BASIC_ENV_KEYS = {"HOME", "LOGNAME", "PATH", "PWD", "SHELL", "TERM", "USER"}
ALLOWED_MODES = {
    "normal",
    "block",
    "continue_false",
    "both_override",
    "sleep",
    "exit1",
    "invalid_json",
    "empty",
}


def utc_now():
    return (
        dt.datetime.now(dt.timezone.utc)
        .isoformat(timespec="milliseconds")
        .replace("+00:00", "Z")
    )


def append_jsonl(path, record):
    path = pathlib.Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(record, separators=(",", ":"), sort_keys=True))
        handle.write("\n")
        handle.flush()


def load_control(log_dir):
    control = {
        "mode": "normal",
        "sleep_ms": 0,
        "block_reason": "MTHC_PROBE_BLOCK_SENTINEL",
        "stop_reason": "MTHC_PROBE_STOP_SENTINEL",
        "stdout_text": "",
    }
    control_path = pathlib.Path(log_dir) / "control.json"
    if not control_path.exists():
        return control

    try:
        raw = json.loads(control_path.read_text(encoding="utf-8"))
    except Exception:
        return control

    if not isinstance(raw, dict):
        return control

    mode = raw.get("mode")
    if isinstance(mode, str) and mode in ALLOWED_MODES:
        control["mode"] = mode

    sleep_ms = raw.get("sleep_ms")
    if isinstance(sleep_ms, int) and not isinstance(sleep_ms, bool) and sleep_ms >= 0:
        control["sleep_ms"] = sleep_ms

    block_reason = raw.get("block_reason")
    if isinstance(block_reason, str):
        control["block_reason"] = block_reason

    stop_reason = raw.get("stop_reason")
    if isinstance(stop_reason, str):
        control["stop_reason"] = stop_reason

    stdout_text = raw.get("stdout_text")
    if isinstance(stdout_text, str):
        control["stdout_text"] = stdout_text

    return control


def select_env():
    return {
        key: os.environ[key]
        for key in sorted(os.environ)
        if key.startswith("CLAUDE_") or key in BASIC_ENV_KEYS
    }


def safe_get(value, *keys):
    current = value
    for key in keys:
        if not isinstance(current, dict):
            return None
        current = current.get(key)
    return current


def extract_fields(payload):
    if not isinstance(payload, dict):
        return {
            "payload_is_json_object": False,
        }

    rate_limits = payload.get("rate_limits")
    known_fields = {
        "session_id": payload.get("session_id"),
        "transcript_path": payload.get("transcript_path"),
        "model_id": safe_get(payload, "model", "id"),
        "rate_limits_present": isinstance(rate_limits, dict),
        "five_hour_used_pct": safe_get(rate_limits, "five_hour", "used_percentage"),
        "five_hour_resets_at": safe_get(rate_limits, "five_hour", "resets_at"),
    }

    extra_keys = sorted(
        k for k in payload if k not in {"session_id", "transcript_path", "model", "rate_limits"}
    )
    extra = {k: payload[k] for k in extra_keys}

    result = {}
    for k, v in known_fields.items():
        result[k] = v
    if extra:
        result["extra_payload_keys"] = extra_keys
        result["extra_payload"] = extra
    return result


def build_response(control):
    mode = control["mode"]
    if mode == "normal":
        return {}
    elif mode == "block":
        return {"decision": "block", "reason": control["block_reason"]}
    elif mode == "continue_false":
        return {"continue": False, "stopReason": control["stop_reason"]}
    elif mode == "both_override":
        return {
            "decision": "block",
            "reason": control["block_reason"],
            "continue": False,
            "stopReason": control["stop_reason"],
        }
    elif mode == "sleep":
        return {}
    elif mode == "exit1":
        return {}
    elif mode == "empty":
        return None
    elif mode == "invalid_json":
        return None
    return {}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--log-dir", required=True)
    args = parser.parse_args()

    log_dir = pathlib.Path(args.log_dir)
    records_path = log_dir / "stop_hook_invocations.jsonl"
    control = load_control(log_dir)
    invocation_id = uuid.uuid4().hex
    started_at = time.perf_counter()
    raw_stdin = sys.stdin.read()

    payload = None
    parse_error = None
    if raw_stdin.strip():
        try:
            payload = json.loads(raw_stdin)
        except Exception as exc:
            parse_error = str(exc)

    append_jsonl(
        records_path,
        {
            "argv": sys.argv,
            "control": control,
            "cwd": str(pathlib.Path.cwd()),
            "env": select_env(),
            "fields": extract_fields(payload),
            "invocation_id": invocation_id,
            "parse_error": parse_error,
            "payload": payload,
            "phase": "start",
            "pid": os.getpid(),
            "raw_stdin": raw_stdin,
            "ts": utc_now(),
        },
    )

    exit_code = 0
    mode = control["mode"]
    response_written = None

    if mode == "sleep":
        time.sleep(control["sleep_ms"] / 1000.0)
    elif mode == "exit1":
        exit_code = 1

    if mode == "empty":
        pass
    elif mode == "invalid_json":
        response_written = "(invalid_json)"
        sys.stdout.write('{"decision":"block","reason":INVALID')
    else:
        response = build_response(control)
        if response is not None:
            response_written = response
            sys.stdout.write(json.dumps(response, separators=(",", ":")))
            if control.get("stdout_text"):
                sys.stdout.write(control["stdout_text"])
    sys.stdout.flush()

    append_jsonl(
        records_path,
        {
            "duration_ms": round((time.perf_counter() - started_at) * 1000.0, 3),
            "exit_code": exit_code,
            "invocation_id": invocation_id,
            "mode": mode,
            "phase": "finish",
            "response_written": response_written,
            "ts": utc_now(),
        },
    )

    return exit_code


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SystemExit:
        raise
    except Exception:
        raise SystemExit(0)
