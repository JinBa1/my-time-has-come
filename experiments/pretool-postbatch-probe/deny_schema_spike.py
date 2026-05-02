#!/usr/bin/env python3
"""Operator helper for the focused PreToolUse deny schema spike.

This helper prepares a throwaway Claude workspace under /tmp. It never writes
to the user's real ~/.claude or ~/.config/mthc paths.
"""
import argparse
import datetime as dt
import json
import pathlib
import shutil
import sys


LOG_DIR = pathlib.Path("/tmp/mthc-pretool-postbatch-probe")
WORKSPACE = pathlib.Path("/tmp/mthc-pretool-postbatch-probe-workspace")
CLAUDE_CONFIG_DIR = LOG_DIR / "claude-config"
SNAPSHOTS = LOG_DIR / "transcript_snapshots"
MANIFEST = LOG_DIR / "deny_schema_manifest.json"
MODE_FILE = LOG_DIR / "mode.json"
INVOCATIONS = LOG_DIR / "invocations.jsonl"

BLOCKING_OUTCOMES = {"hook_blocked_tool", "no_tool_result_after_hook", "tool_result_without_content"}

SCENARIOS = {
    "S13": {
        "label": "passive-control",
        "pretool_mode": "passive",
        "content_prefix": "MTHC_SCHEMA_PASSIVE_CONTENT",
        "expected_outcome": "fresh_tool_executed",
        "reason": "",
    },
    "S14": {
        "label": "top-level-deny-control",
        "pretool_mode": "deny_with_reason",
        "content_prefix": "MTHC_SCHEMA_TOP_LEVEL_CONTENT",
        "expected_outcome": "fresh_tool_executed",
        "reason": "MTHC_DENY_TOP_LEVEL",
    },
    "S15": {
        "label": "nested-deny-with-event",
        "pretool_mode": "deny_nested_with_event",
        "content_prefix": "MTHC_SCHEMA_NESTED_WITH_EVENT_CONTENT",
        "expected_outcome": "blocked",
        "reason": "MTHC_DENY_NESTED_WITH_EVENT",
    },
    "S16": {
        "label": "nested-deny-without-event",
        "pretool_mode": "deny_nested_without_event",
        "content_prefix": "MTHC_SCHEMA_NESTED_WITHOUT_EVENT_CONTENT",
        "expected_outcome": "exploratory",
        "reason": "MTHC_DENY_NESTED_WITHOUT_EVENT",
    },
}


def repo_root() -> pathlib.Path:
    return pathlib.Path(__file__).resolve().parents[2]


def utc_run_id() -> str:
    return dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def read_json(path: pathlib.Path) -> dict:
    with path.open("r", encoding="utf-8") as fh:
        return json.load(fh)


def write_json(path: pathlib.Path, data: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as fh:
        json.dump(data, fh, indent=2, sort_keys=True)
        fh.write("\n")


def load_manifest() -> dict:
    if not MANIFEST.exists():
        raise SystemExit(f"missing manifest: run setup first ({MANIFEST})")
    return read_json(MANIFEST)


def setup(reset: bool) -> None:
    if reset:
        shutil.rmtree(WORKSPACE, ignore_errors=True)
        shutil.rmtree(LOG_DIR, ignore_errors=True)
    elif WORKSPACE.exists() or LOG_DIR.exists():
        raise SystemExit("workspace/log dir already exists; rerun with --reset or clean /tmp manually")

    run_id = utc_run_id()
    probe_path = repo_root() / "experiments" / "pretool-postbatch-probe" / "probe.py"

    (WORKSPACE / ".claude").mkdir(parents=True, exist_ok=True)
    CLAUDE_CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    SNAPSHOTS.mkdir(parents=True, exist_ok=True)

    settings = {
        "hooks": {
            "PreToolUse": [
                {
                    "matcher": "*",
                    "hooks": [{"type": "command", "command": str(probe_path)}],
                }
            ],
            "Stop": [
                {
                    "matcher": "",
                    "hooks": [{"type": "command", "command": str(probe_path)}],
                }
            ],
        }
    }
    write_json(WORKSPACE / ".claude" / "settings.json", settings)

    manifest = {
        "run_id": run_id,
        "workspace": str(WORKSPACE),
        "log_dir": str(LOG_DIR),
        "claude_config_dir": str(CLAUDE_CONFIG_DIR),
        "probe_path": str(probe_path),
        "scenarios": {},
    }

    for scenario_id, spec in SCENARIOS.items():
        file_name = f"{scenario_id}_{spec['label']}_{run_id}.txt"
        file_path = WORKSPACE / file_name
        content = f"{spec['content_prefix']}_{run_id}"
        file_path.write_text(content + "\n", encoding="utf-8")
        manifest["scenarios"][scenario_id] = {
            **spec,
            "file": str(file_path),
            "content": content,
            "prompt": f"Read {file_path} and reply with only the file content.",
        }

    write_json(MANIFEST, manifest)
    print(f"workspace: {WORKSPACE}")
    print(f"manifest:  {MANIFEST}")
    print(f"settings:  {WORKSPACE / '.claude' / 'settings.json'}")
    print(f"user config isolation: {CLAUDE_CONFIG_DIR}")
    print()
    print("Open Claude Code from the workspace with isolated user config:")
    print(f"  cd {WORKSPACE}")
    print(f"  CLAUDE_CONFIG_DIR={CLAUDE_CONFIG_DIR} claude")
    print()
    print("Then run scenarios with:")
    print(f"  python3 {pathlib.Path(__file__).resolve()} mode S13")


def write_mode(scenario_id: str) -> None:
    manifest = load_manifest()
    scenarios = manifest["scenarios"]
    if scenario_id not in scenarios:
        raise SystemExit(f"unknown scenario {scenario_id}; choose one of: {', '.join(sorted(scenarios))}")

    spec = scenarios[scenario_id]
    sentinels = {}
    if spec["reason"]:
        sentinels["permissionDecisionReason"] = spec["reason"]

    mode = {
        "scenario_id": scenario_id,
        "active_modes": {
            "PostToolBatch": "passive",
            "PreToolUse": spec["pretool_mode"],
        },
        "sentinels": sentinels,
        "compliance_phrase": None,
        "updated_input": None,
        "throttle_threshold": 4,
        "loop_suspect_threshold": 2,
        "sleep_ms": 0,
    }
    write_json(MODE_FILE, mode)
    print(f"mode: {scenario_id} ({spec['label']})")
    print(f"file: {spec['file']}")
    print("prompt to paste into Claude Code:")
    print(spec["prompt"])


def load_invocations() -> list[dict]:
    if not INVOCATIONS.exists():
        return []
    records = []
    with INVOCATIONS.open("r", encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if line:
                records.append(json.loads(line))
    return records


def transcript_paths_for(records: list[dict], scenario_id: str) -> list[pathlib.Path]:
    paths = []
    for record in records:
        if record.get("scenario_id") != scenario_id:
            continue
        payload = record.get("payload_full", {})
        path = payload.get("transcript_path")
        if path:
            paths.append(pathlib.Path(path))
    return paths


def snapshot(scenario_id: str, tag: str) -> None:
    records = load_invocations()
    paths = [path for path in transcript_paths_for(records, scenario_id) if path.exists()]
    if not paths:
        raise SystemExit(f"no transcript_path found for {scenario_id}; run the scenario first")
    target = SNAPSHOTS / f"{scenario_id}_{tag}.jsonl"
    shutil.copyfile(paths[-1], target)
    print(f"copied {paths[-1]} -> {target}")


def file_contains(path: pathlib.Path, needle: str) -> bool:
    try:
        return needle in path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return False


def load_jsonl(path: pathlib.Path) -> list[dict]:
    records = []
    with path.open("r", encoding="utf-8", errors="replace") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                records.append(json.loads(line))
            except ValueError:
                records.append({"_parse_error": True, "raw": line[:200]})
    return records


def transcript_paths_for_analysis(invocation_records: list[dict], scenario_id: str) -> list[pathlib.Path]:
    snapshot_path = SNAPSHOTS / f"{scenario_id}_post.jsonl"
    if snapshot_path.exists():
        return [snapshot_path]

    unique = []
    seen = set()
    for path in transcript_paths_for(invocation_records, scenario_id):
        if not path.exists():
            continue
        key = str(path)
        if key in seen:
            continue
        seen.add(key)
        unique.append(path)
    return unique


def load_transcript_records(paths: list[pathlib.Path]) -> list[dict]:
    records = []
    for path in paths:
        records.extend(load_jsonl(path))
    return records


def message_content_items(record: dict) -> list[dict]:
    message = record.get("message")
    if not isinstance(message, dict):
        return []
    content = message.get("content")
    if not isinstance(content, list):
        return []
    return [item for item in content if isinstance(item, dict)]


def classify_transcript_records(records: list[dict], file_path: str, content_marker: str) -> dict:
    read_tool_ids = []
    hooks = []
    hook_errors = []
    hook_blocking_errors = []
    results = []
    content_marker_in_assistant_text = False

    for record in records:
        for item in message_content_items(record):
            if (
                item.get("type") == "tool_use"
                and item.get("name") == "Read"
                and item.get("input", {}).get("file_path") == file_path
            ):
                tool_id = item.get("id")
                if tool_id:
                    read_tool_ids.append(tool_id)

            if item.get("type") == "text" and content_marker in item.get("text", ""):
                content_marker_in_assistant_text = True

        attachment = record.get("attachment")
        if isinstance(attachment, dict) and attachment.get("hookEvent") == "PreToolUse":
            hook = {
                "type": attachment.get("type"),
                "tool_use_id": attachment.get("toolUseID"),
                "hook_name": attachment.get("hookName"),
                "stdout": attachment.get("stdout", ""),
                "stderr": attachment.get("stderr", ""),
            }
            hooks.append(hook)
            if attachment.get("type") == "hook_blocking_error":
                hook_blocking_errors.append(hook)
            if attachment.get("type") != "hook_success" or attachment.get("stderr"):
                hook_errors.append(hook)

        tool_result = record.get("toolUseResult")
        if isinstance(tool_result, dict):
            result_file = tool_result.get("file") if isinstance(tool_result.get("file"), dict) else {}
            item_ids = [
                item.get("tool_use_id")
                for item in message_content_items(record)
                if item.get("type") == "tool_result" and item.get("tool_use_id")
            ]
            result_content = json.dumps(tool_result, sort_keys=True)
            for item in message_content_items(record):
                if item.get("type") == "tool_result":
                    result_content += "\n" + str(item.get("content", ""))
            results.append({
                "type": tool_result.get("type"),
                "file_path": result_file.get("filePath"),
                "tool_use_ids": item_ids,
                "contains_marker": content_marker in result_content,
                "is_error": any(bool(item.get("is_error")) for item in message_content_items(record)),
            })

    read_tool_id_set = set(read_tool_ids)
    matching_hooks = [hook for hook in hooks if hook.get("tool_use_id") in read_tool_id_set]
    matching_hook_errors = [hook for hook in hook_errors if hook.get("tool_use_id") in read_tool_id_set]
    matching_hook_blocking_errors = [
        hook for hook in hook_blocking_errors if hook.get("tool_use_id") in read_tool_id_set
    ]
    matching_results = [
        result
        for result in results
        if result.get("file_path") == file_path
        or any(tool_id in read_tool_id_set for tool_id in result.get("tool_use_ids", []))
    ]

    result_types = [result.get("type") or "<unknown>" for result in matching_results]
    content_marker_in_tool_result = any(result["contains_marker"] for result in matching_results)

    if not records:
        outcome = "not_run"
    elif not read_tool_ids:
        outcome = "no_read_tool_use"
    elif not matching_hooks:
        outcome = "hook_missing"
    elif content_marker_in_tool_result and "text" in result_types:
        outcome = "fresh_tool_executed"
    elif "file_unchanged" in result_types:
        outcome = "cached_result"
    elif matching_hook_blocking_errors:
        outcome = "hook_blocked_tool"
    elif matching_hook_errors:
        outcome = "hook_runtime_error"
    elif matching_results:
        outcome = "tool_result_without_content"
    else:
        outcome = "no_tool_result_after_hook"

    return {
        "outcome": outcome,
        "read_tool_uses": len(read_tool_ids),
        "pretool_hooks": len(matching_hooks),
        "hook_errors": len(matching_hook_errors),
        "blocking_errors": len(matching_hook_blocking_errors),
        "tool_result_types": result_types,
        "content_marker_in_tool_result": content_marker_in_tool_result,
        "content_marker_in_assistant_text": content_marker_in_assistant_text,
    }


def expected_verdict(expected: str, outcome: str) -> str:
    if expected == "exploratory":
        return "exploratory"
    if expected == "blocked":
        return "matches expectation" if outcome in BLOCKING_OUTCOMES else "unexpected"
    return "matches expectation" if outcome == expected else "unexpected"


def analyze() -> None:
    manifest = load_manifest()
    records = load_invocations()
    print("PreToolUse deny schema spike analysis")
    print(f"run_id: {manifest['run_id']}")
    print(f"claude_config_dir: {manifest.get('claude_config_dir', '<not recorded>')}")
    print()

    for scenario_id in sorted(manifest["scenarios"]):
        spec = manifest["scenarios"][scenario_id]
        scenario_records = [record for record in records if record.get("scenario_id") == scenario_id]
        pretool_records = [record for record in scenario_records if record.get("hook_event_name") == "PreToolUse"]
        responses = [record.get("response_written", {}) for record in pretool_records]

        transcript_candidates = transcript_paths_for_analysis(records, scenario_id)
        transcript_records = load_transcript_records(transcript_candidates)
        transcript_result = classify_transcript_records(transcript_records, spec["file"], spec["content"])
        reason_present = bool(spec["reason"]) and any(file_contains(path, spec["reason"]) for path in transcript_candidates)
        expected = spec.get("expected_outcome", "exploratory")
        if not pretool_records:
            verdict = "not run"
        else:
            verdict = expected_verdict(expected, transcript_result["outcome"])

        print(f"{scenario_id} {spec['label']}")
        print(f"  Expected outcome: {expected}")
        print(f"  PreToolUse invocations: {len(pretool_records)}")
        if responses:
            print(f"  Last response: {json.dumps(responses[-1], sort_keys=True)}")
        else:
            print("  Last response: <none>")
        print(f"  Read tool uses: {transcript_result['read_tool_uses']}")
        print(f"  PreToolUse hooks in transcript: {transcript_result['pretool_hooks']}")
        print(f"  Hook errors in transcript: {transcript_result['hook_errors']}")
        print(f"  Blocking hook errors in transcript: {transcript_result['blocking_errors']}")
        print(f"  Tool result types: {', '.join(transcript_result['tool_result_types']) or '<none>'}")
        print(f"  Content marker in tool result: {str(transcript_result['content_marker_in_tool_result']).lower()}")
        print(f"  Content marker in assistant text: {str(transcript_result['content_marker_in_assistant_text']).lower()}")
        print(f"  Outcome: {transcript_result['outcome']} ({verdict})")
        if spec["reason"]:
            print(f"  Reason marker present:  {str(reason_present).lower()}")
        print()


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    setup_parser = subparsers.add_parser("setup", help="create /tmp workspace and scenario manifest")
    setup_parser.add_argument("--reset", action="store_true", help="remove prior /tmp spike workspace/logs first")

    mode_parser = subparsers.add_parser("mode", help="write mode.json for a scenario")
    mode_parser.add_argument("scenario_id", choices=sorted(SCENARIOS))

    snapshot_parser = subparsers.add_parser("snapshot", help="copy latest transcript for a scenario")
    snapshot_parser.add_argument("scenario_id", choices=sorted(SCENARIOS))
    snapshot_parser.add_argument("tag", nargs="?", default="post")

    subparsers.add_parser("analyze", help="summarize invocations/transcripts")

    args = parser.parse_args(argv)
    if args.command == "setup":
        setup(args.reset)
    elif args.command == "mode":
        write_mode(args.scenario_id)
    elif args.command == "snapshot":
        snapshot(args.scenario_id, args.tag)
    elif args.command == "analyze":
        analyze()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
