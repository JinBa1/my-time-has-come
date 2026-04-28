#!/usr/bin/env python3
"""Summarize pretool-postbatch probe invocations.jsonl + transcript snapshots.

Produces:
  - per-scenario invocation counts + loop class
  - per-assumption evidence table (best effort; analyst confirms)
  - sentinel-presence matrix
  - throttle events
  - decision section draft
"""
import json
import pathlib
import sys
from collections import defaultdict

LOG_DIR = pathlib.Path("/tmp/mthc-pretool-postbatch-probe")
INVOCATIONS = LOG_DIR / "invocations.jsonl"
SNAPSHOTS = LOG_DIR / "transcript_snapshots"

# Sentinels used across scenarios
KNOWN_SENTINELS = [
    "FROG_AC_2",
    "FROG_PRE_AC_8",
    "FROG_REASON_3",
    "FROG_STOP_5",
    "FROG_PDR_6",
    "FROG_SYSMSG_12",
]


def load_invocations() -> list[dict]:
    if not INVOCATIONS.exists():
        return []
    records = []
    with INVOCATIONS.open("r", encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                records.append(json.loads(line))
            except ValueError:
                records.append({"_parse_error": True, "raw": line[:200]})
    return records


def classify_loop(invocations: list[dict]) -> str:
    """Classify loop type from a scenario's invocations.

    Returns one of: none, block, compliance, mixed.
    """
    user_prompt_count = 0
    for inv in invocations:
        if inv.get("hook_event_name") == "Stop":
            continue
        # A rough heuristic: if count > 1, there were multiple invocations
        # from what should have been one user prompt.
    counts = [inv.get("count", 1) for inv in invocations if inv.get("hook_event_name") != "Stop"]
    max_count = max(counts) if counts else 0

    if max_count <= 1:
        return "none"

    # Look at inter-invocation content for loop classification.
    # This is a simplified version — the analyst should verify with transcript diffs.
    # block-loop: short, near-identical replies, high invocation count
    # compliance-loop: substantive replies, tool calls present, bounded count
    if max_count > 5:
        return "block"
    if max_count <= 5:
        return "compliance"
    return "mixed"


def check_sentinel_in_file(path: pathlib.Path, sentinel: str) -> bool:
    if not path.exists():
        return False
    try:
        with path.open("r", encoding="utf-8") as fh:
            return sentinel in fh.read()
    except (OSError, ValueError):
        return False


def summarize() -> None:
    records = load_invocations()
    if not records:
        print("No invocations found at", INVOCATIONS)
        return

    # Group by scenario_id
    by_scenario: dict[str, list[dict]] = defaultdict(list)
    for rec in records:
        sid = rec.get("scenario_id", "unknown")
        by_scenario[sid].append(rec)

    print("=" * 72)
    print("PRETOOL-POSTBATCH PROBE SUMMARY")
    print("=" * 72)

    # Per-scenario breakdown
    print("\n## Per-Scenario Invocation Counts\n")
    for sid in sorted(by_scenario.keys()):
        invs = by_scenario[sid]
        non_stop = [i for i in invs if i.get("hook_event_name") != "Stop"]
        stop_inv = [i for i in invs if i.get("hook_event_name") == "Stop"]
        loop_class = classify_loop(non_stop)
        throttle_events = [i for i in non_stop if i.get("throttled")]
        loop_suspect_events = [i for i in non_stop if i.get("loop_suspected") and not i.get("throttled")]

        events = defaultdict(int)
        for i in non_stop:
            events[i.get("hook_event_name", "unknown")] += 1

        print(f"### {sid}")
        print(f"  Total invocations (non-Stop): {len(non_stop)}")
        for ev_name, ev_count in sorted(events.items()):
            print(f"    {ev_name}: {ev_count}")
        print(f"  Stop observer invocations: {len(stop_inv)}")
        print(f"  Loop class: {loop_class}")
        if loop_suspect_events:
            print(f"  Loop-suspect invocations: {len(loop_suspect_events)}")
        if throttle_events:
            print(f"  Throttled invocations: {len(throttle_events)}")
        print()

    # Sentinel presence matrix
    print("\n## Sentinel Presence in Transcript Snapshots\n")
    print(f"{'Sentinel':<22} {'Pre-snapshot':<14} {'Post-snapshot':<14}")
    print("-" * 50)
    scenario_ids = sorted(by_scenario.keys())
    for sentinel in KNOWN_SENTINELS:
        pre_found = []
        post_found = []
        for sid in scenario_ids:
            pre_path = SNAPSHOTS / f"{sid}_pre.jsonl"
            post_path = SNAPSHOTS / f"{sid}_post.jsonl"
            if check_sentinel_in_file(pre_path, sentinel):
                pre_found.append(sid)
            if check_sentinel_in_file(post_path, sentinel):
                post_found.append(sid)
        print(f"{sentinel:<22} {','.join(pre_found) or '—':<14} {','.join(post_found) or '—':<14}")

    # Throttle events
    print("\n## Throttle Events\n")
    any_throttle = False
    for sid in sorted(by_scenario.keys()):
        throttled = [i for i in by_scenario[sid] if i.get("throttled")]
        if throttled:
            any_throttle = True
            print(f"  {sid}: {len(throttled)} throttled invocations")
            for t in throttled[:3]:
                print(f"    count={t.get('count')} hook={t.get('hook_event_name')} mode={t.get('active_mode')}")
    if not any_throttle:
        print("  None.")

    # Response written summary
    print("\n## Response Summary\n")
    response_counts: dict[str, int] = defaultdict(int)
    for rec in records:
        if rec.get("hook_event_name") == "Stop":
            continue
        resp = rec.get("response_written", {})
        resp_key = json.dumps(resp, sort_keys=True)
        response_counts[resp_key] += 1
    for resp_key, count in sorted(response_counts.items(), key=lambda x: -x[1]):
        print(f"  {count:3d}x {resp_key}")

    # Field presence from S1 baseline
    print("\n## Field Presence (from S1 baseline)\n")
    s1_records = [r for r in records if r.get("scenario_id") == "S1" and r.get("hook_event_name") != "Stop"]
    if s1_records:
        fields_to_check = [
            "session_id", "transcript_path", "cwd", "permission_mode",
            "hook_event_name", "tool_name", "tool_input", "tool_batch",
        ]
        digest = s1_records[0].get("payload_digest", {})
        full = s1_records[0].get("payload_full", {})
        for field in fields_to_check:
            in_digest = field in digest and digest[field] is not None
            in_full = field in full and full[field] is not None
            status = "present" if in_full else ("in_digest" if in_digest else "absent")
            print(f"  {field}: {status}")

        # tool_batch_size distribution
        batch_sizes = [r.get("payload_digest", {}).get("tool_batch_size") for r in s1_records]
        batch_sizes = [s for s in batch_sizes if s is not None]
        if batch_sizes:
            from collections import Counter
            size_dist = Counter(batch_sizes)
            print(f"\n  tool_batch_size distribution: {dict(size_dist)}")

    print("\n" + "=" * 72)
    print("END SUMMARY")
    print("=" * 72)


if __name__ == "__main__":
    summarize()
