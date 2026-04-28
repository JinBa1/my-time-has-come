#!/usr/bin/env python3

import datetime as dt
import json
import pathlib
import statistics
import sys


def parse_ts(text):
    return dt.datetime.fromisoformat(text.replace("Z", "+00:00"))


def load_records(path):
    records = []
    lines = pathlib.Path(path).read_text(encoding="utf-8").splitlines(keepends=True)
    for index, line in enumerate(lines):
        stripped = line.strip()
        if not stripped:
            continue
        try:
            records.append(json.loads(stripped))
        except json.JSONDecodeError:
            is_last_line = index == len(lines) - 1
            if is_last_line and not line.endswith(("\n", "\r")):
                break
            raise
    return records


def format_stats(values):
    if not values:
        return "n/a"
    return (
        f"mean={statistics.mean(values):.3f} "
        f"min={min(values):.3f} "
        f"max={max(values):.3f}"
    )


def main():
    if len(sys.argv) != 2:
        print(f"usage: {pathlib.Path(sys.argv[0]).name} /path/to/stop_hook_invocations.jsonl")
        return 2

    records = load_records(sys.argv[1])
    starts = {}
    finishes = {}
    start_times = []

    for record in records:
        invocation_id = record.get("invocation_id")
        phase = record.get("phase")
        if invocation_id is None or phase is None:
            continue
        if phase == "start":
            starts[invocation_id] = record
            start_times.append(parse_ts(record["ts"]))
        elif phase == "finish":
            finishes[invocation_id] = record

    complete_ids = sorted(set(starts) & set(finishes))
    incomplete_ids = sorted(set(starts) - set(finishes))

    intervals_ms = [
        (later - earlier).total_seconds() * 1000.0
        for earlier, later in zip(start_times, start_times[1:])
    ]
    durations_ms = [
        finishes[invocation_id]["duration_ms"]
        for invocation_id in complete_ids
        if finishes[invocation_id].get("duration_ms") is not None
    ]

    mode_counts = {}
    for record in starts.values():
        mode = record.get("control", {}).get("mode", "?")
        mode_counts[mode] = mode_counts.get(mode, 0) + 1

    field_names = [
        "session_id",
        "transcript_path",
        "model_id",
        "rate_limits_present",
        "five_hour_used_pct",
        "five_hour_resets_at",
    ]
    presence = {
        field_name: sum(
            1
            for record in starts.values()
            if record.get("fields", {}).get(field_name) is not None
        )
        for field_name in field_names
    }
    unique_sessions = sorted(
        {
            record.get("fields", {}).get("session_id")
            for record in starts.values()
            if record.get("fields", {}).get("session_id") is not None
        }
    )
    unique_modes = sorted(
        {
            record.get("mode")
            for record in finishes.values()
            if record.get("mode") is not None
        }
    )
    extra_key_sets = set()
    for record in starts.values():
        extra_keys = record.get("fields", {}).get("extra_payload_keys")
        if extra_keys:
            extra_key_sets.add(tuple(sorted(extra_keys)))

    response_summary = {}
    for iid in complete_ids:
        resp = finishes[iid].get("response_written")
        resp_key = json.dumps(resp, separators=(",", ":"), sort_keys=True) if resp is not None else "(none)"
        response_summary[resp_key] = response_summary.get(resp_key, 0) + 1

    print(f"starts: {len(starts)}")
    print(f"finishes: {len(finishes)}")
    print(f"complete: {len(complete_ids)}")
    print(f"incomplete: {len(incomplete_ids)}")
    print(f"cadence_ms: {format_stats(intervals_ms)}")
    print(f"duration_ms: {format_stats(durations_ms)}")
    print("modes_observed:")
    for mode in sorted(mode_counts.keys()):
        print(f"  {mode}: {mode_counts[mode]}/{len(starts)}")
    print("responses_written:")
    for resp_key, count in sorted(response_summary.items()):
        print(f"  {resp_key}: {count}/{len(complete_ids)}")
    print("field_presence:")
    total_starts = len(starts)
    for field_name in field_names:
        print(f"  {field_name}: {presence[field_name]}/{total_starts}")
    print("unique_sessions:")
    for session_id in unique_sessions:
        print(f"  {session_id}")
    if extra_key_sets:
        print("extra_payload_keys_seen:")
        for key_tuple in sorted(extra_key_sets):
            print(f"  {list(key_tuple)}")
    if incomplete_ids:
        print("incomplete_invocations:")
        for invocation_id in incomplete_ids:
            print(f"  {invocation_id}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
