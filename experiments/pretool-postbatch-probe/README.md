# PreToolUse + PostToolBatch Probe — Operator Runbook

## Prerequisites

- Python 3 available as `python3`
- Claude Code CLI installed
- Plan doc read: `docs/superpowers/plans/2026-04-27-pretool-postbatch-probe-spike.md`

## Setup

### 1. Clean stale workspace (if any)

```bash
rm -rf /tmp/mthc-pretool-postbatch-probe-workspace
rm -rf /tmp/mthc-pretool-postbatch-probe
```

### 2. Create workspace + seed files

```bash
mkdir -p /tmp/mthc-pretool-postbatch-probe-workspace/.claude
mkdir -p /tmp/mthc-pretool-postbatch-probe/transcript_snapshots
echo "FOO_FILE_CONTENT_42" > /tmp/mthc-pretool-postbatch-probe-workspace/foo.txt
echo "BAR_FILE_CONTENT_42" > /tmp/mthc-pretool-postbatch-probe-workspace/bar.txt
echo "BAZ_FILE_CONTENT_42" > /tmp/mthc-pretool-postbatch-probe-workspace/baz.txt
echo "MTHC probe workspace" > /tmp/mthc-pretool-postbatch-probe-workspace/README.txt
```

### 3. Write .claude/settings.json

Replace `/abs/path/to/` with the actual path to `experiments/pretool-postbatch-probe/probe.py`.

```bash
PROBE_PATH="$(cd "$(dirname "$0")" && pwd)/probe.py"

cat > /tmp/mthc-pretool-postbatch-probe-workspace/.claude/settings.json << EOF
{
  "hooks": {
    "PostToolBatch": [
      {
        "hooks": [
          {"type": "command", "command": "${PROBE_PATH}"}
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {"type": "command", "command": "${PROBE_PATH}"}
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "${PROBE_PATH}"}
        ]
      }
    ]
  }
}
EOF
```

### 4. Self-check (before live session)

```bash
# Clear any stale state
rm -f /tmp/mthc-pretool-postbatch-probe/invocations.jsonl /tmp/mthc-pretool-postbatch-probe/scenario_state.json

PROBE_PATH="$(cd "$(dirname "$0")" && pwd)/probe.py"

# Test PostToolBatch
echo '{"hook_event_name":"PostToolBatch","session_id":"test","tool_batch":[]}' | python3 "$PROBE_PATH"
echo "Exit code: $?"

# Test PreToolUse
echo '{"hook_event_name":"PreToolUse","session_id":"test","tool_name":"Read","tool_input":{"file_path":"foo.txt"}}' | python3 "$PROBE_PATH"
echo "Exit code: $?"

# Test Stop (should produce NO stdout)
output=$(echo '{"hook_event_name":"Stop","session_id":"test"}' | python3 "$PROBE_PATH")
echo "Stop stdout: '${output}' (should be empty)"
echo "Exit code: $?"

# Verify log
python3 -c "import json; [print(json.dumps(json.loads(l), indent=2)) for l in open('/tmp/mthc-pretool-postbatch-probe/invocations.jsonl') if l.strip()]"
```

### 5. Pre-trust the workspace

Open a throwaway Claude session in the workspace and accept the trust dialog:

```bash
cd /tmp/mthc-pretool-postbatch-probe-workspace
claude
# Accept trust dialog (note: requires \r not \n if automated)
# Send one message, then exit
```

**Trust-dialog gotcha:** If driving via expect/script, the trust dialog requires `\r` (carriage return), not `\n`. Plain `\n` does not advance the TUI.

---

## Running Scenarios

### General procedure

For each scenario:

1. Write `mode.json` for the scenario (templates below)
2. Snapshot the transcript: `cp <transcript_path> /tmp/mthc-pretool-postbatch-probe/transcript_snapshots/S{n}_pre.jsonl`
3. Send the operator prompt(s)
4. Wait for Claude's reply to settle
5. Snapshot the transcript again: `cp <transcript_path> /tmp/mthc-pretool-postbatch-probe/transcript_snapshots/S{n}_post.jsonl`
6. If the scenario produced > 3 invocations from one prompt, `claude --resume` or start a fresh session before the next scenario

**Hook-silence timeout:** If no `invocations.jsonl` entry appears within 30 seconds of sending a hook-triggering prompt, the hook is unregistered or non-existent. Halt the scenario.

### Scenario order

S1 → S2 → S7 → S8 → S3 → S4 → S5 → S6 → **[review point]** → S9a → S9b → S10 → S11 → S12

---

## Focused Spike: PreToolUse Deny Schema

Use this focused spike when validating whether Claude Code enforces
`PreToolUse` deny from the documented nested `hookSpecificOutput` shape. This
is separate from the older S1-S12 matrix because S6 used a cached file and did
not prove fresh tool blocking.

### What this spike tests

- `S13` passive control: a fresh file can be read from the throwaway workspace.
- `S14` top-level deny control: the old probe shape used by current mthc.
- `S15` nested deny with `hookEventName`: the candidate current Claude Code
  schema.
- `S16` nested deny without `hookEventName`: isolates whether runtime requires
  the event name field.

Every scenario uses a unique file path generated during setup. Do not reuse
files between runs; that reintroduces the cache ambiguity from S6.

### Setup

From the repo root:

```bash
python3 experiments/pretool-postbatch-probe/deny_schema_spike.py setup --reset
cd /tmp/mthc-pretool-postbatch-probe-workspace
CLAUDE_CONFIG_DIR=/tmp/mthc-pretool-postbatch-probe/claude-config claude
```

The helper writes only under:

- `/tmp/mthc-pretool-postbatch-probe-workspace`
- `/tmp/mthc-pretool-postbatch-probe`

It does not modify real `~/.claude` or `~/.config/mthc` state.

Use the `CLAUDE_CONFIG_DIR=...` command printed by setup. This isolates
user-level Claude settings so global `PreToolUse` hooks or `disableAllHooks`
do not contaminate the run. Project settings still come from the throwaway
workspace's `.claude/settings.json`. If managed/org-level Claude settings are
in force on the machine, record that before interpreting the spike; this helper
does not override managed policy.

### Run scenarios

For each scenario, from the repo root in a normal shell outside the Claude TUI:

```bash
python3 experiments/pretool-postbatch-probe/deny_schema_spike.py mode S13
```

Paste the printed prompt into Claude Code. After Claude settles, snapshot the
transcript:

```bash
python3 experiments/pretool-postbatch-probe/deny_schema_spike.py snapshot S13
```

Repeat for `S14`, `S15`, and `S16`.

### Analyze

After all four scenarios, from the repo root:

```bash
python3 experiments/pretool-postbatch-probe/deny_schema_spike.py analyze
python3 experiments/pretool-postbatch-probe/summarize.py
```

Interpretation:

- The analyzer classifies actual `Read` tool_use, `PreToolUse` hook, and
  `toolUseResult` records. The decision signal is **not** assistant text alone.
- If `S13` is not `fresh_tool_executed`, the run is invalid.
- If `S14` is `fresh_tool_executed`, the top-level deny control reproduces the
  current mthc failure.
- If `S15` is `hook_blocked_tool`, `no_tool_result_after_hook`, or
  `tool_result_without_content`, nested deny with `hookEventName` is a viable
  hard-gate mechanism for v0.
- If `S15` is `fresh_tool_executed`, `PreToolUse` deny is not a viable v0 hard
  gate in current Claude Code.
- If `S15` is `hook_runtime_error`, the candidate schema was rejected and must
  be corrected before drawing a hard-stop conclusion.
- `S16` is exploratory. It distinguishes whether omitting `hookEventName` is
  accepted, rejected, or still enforced; it is not expected to pass/fail.

---

## Mode.json Templates

### S1 — Baseline (both passive)

```json
{
  "scenario_id": "S1",
  "active_modes": {"PostToolBatch": "passive", "PreToolUse": "passive"},
  "sentinels": {},
  "compliance_phrase": null,
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompts: (a) "Read foo.txt." (b) "Read foo.txt and bar.txt and baz.txt in parallel."

---

### S2 — PostToolBatch additionalContext

```json
{
  "scenario_id": "S2",
  "active_modes": {"PostToolBatch": "additional_context_only", "PreToolUse": "passive"},
  "sentinels": {"additionalContext": "FROG_AC_2"},
  "compliance_phrase": "FROG_AC_2",
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt and bar.txt and baz.txt in parallel."

---

### S7 — PreToolUse updatedInput

```json
{
  "scenario_id": "S7",
  "active_modes": {"PostToolBatch": "passive", "PreToolUse": "update_input"},
  "sentinels": {},
  "compliance_phrase": null,
  "updated_input": {"file_path": "/tmp/mthc-pretool-postbatch-probe-workspace/bar.txt"},
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompts: (a) "Read foo.txt." (b) "What file did you just read?"

---

### S8 — PreToolUse additionalContext on allow

```json
{
  "scenario_id": "S8",
  "active_modes": {"PostToolBatch": "passive", "PreToolUse": "allow_with_context"},
  "sentinels": {"additionalContext": "FROG_PRE_AC_8"},
  "compliance_phrase": "FROG_PRE_AC_8",
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt."

---

### S3 — PostToolBatch block + reason

```json
{
  "scenario_id": "S3",
  "active_modes": {"PostToolBatch": "block_with_reason", "PreToolUse": "passive"},
  "sentinels": {"reason": "FROG_REASON_3"},
  "compliance_phrase": "FROG_REASON_3",
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt and bar.txt and baz.txt in parallel."
**Loop-risk.** Resume after if invocations > 3.

---

### S4 — PostToolBatch block + additionalContext precedence

```json
{
  "scenario_id": "S4",
  "active_modes": {"PostToolBatch": "block_and_context", "PreToolUse": "passive"},
  "sentinels": {"additionalContext": "MTHC_CTX_4", "reason": "MTHC_REASON_4"},
  "compliance_phrase": "MTHC_CTX_4",
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt and bar.txt and baz.txt in parallel."
**Loop-risk.** Resume after if invocations > 3.

---

### S5 — PostToolBatch continue:false

```json
{
  "scenario_id": "S5",
  "active_modes": {"PostToolBatch": "continue_false", "PreToolUse": "passive"},
  "sentinels": {"stopReason": "FROG_STOP_5"},
  "compliance_phrase": null,
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt and bar.txt and baz.txt in parallel."

---

### S6 — PreToolUse deny + reason

```json
{
  "scenario_id": "S6",
  "active_modes": {"PostToolBatch": "passive", "PreToolUse": "deny_with_reason"},
  "sentinels": {"permissionDecisionReason": "FROG_PDR_6"},
  "compliance_phrase": "FROG_PDR_6",
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt."
**Loop-risk.** Resume after if invocations > 3.

**S1 halt rule:** If S1 shows PreToolUse passive-mode tool execution fails (A0b rejects), halt before S6.

---

### S9a — PreToolUse ask (approve)

```json
{
  "scenario_id": "S9a",
  "active_modes": {"PostToolBatch": "passive", "PreToolUse": "ask"},
  "sentinels": {},
  "compliance_phrase": null,
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt."
**When permission dialog appears:** Click **approve**. Record `S9a_dialog_choice=approve` + timestamp.

### S9b — PreToolUse ask (deny)

```json
{
  "scenario_id": "S9b",
  "active_modes": {"PostToolBatch": "passive", "PreToolUse": "ask"},
  "sentinels": {},
  "compliance_phrase": null,
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt."
**When permission dialog appears:** Click **deny**. Record `S9b_dialog_choice=deny` + timestamp.

---

### S10 — PreToolUse defer

```json
{
  "scenario_id": "S10",
  "active_modes": {"PostToolBatch": "passive", "PreToolUse": "defer"},
  "sentinels": {},
  "compliance_phrase": null,
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt."

---

### S11 — Slow hook (5s)

```json
{
  "scenario_id": "S11",
  "active_modes": {"PostToolBatch": "slow_passive", "PreToolUse": "slow_passive"},
  "sentinels": {},
  "compliance_phrase": null,
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 5000
}
```

Prompt: "Read foo.txt and bar.txt and baz.txt in parallel."

---

### S12 — systemMessage visibility

```json
{
  "scenario_id": "S12",
  "active_modes": {"PostToolBatch": "system_message_only", "PreToolUse": "system_message_only"},
  "sentinels": {"systemMessage": "FROG_SYSMSG_12"},
  "compliance_phrase": null,
  "updated_input": null,
  "throttle_threshold": 8,
  "loop_suspect_threshold": 3,
  "sleep_ms": 0
}
```

Prompt: "Read foo.txt and bar.txt and baz.txt in parallel."

---

## Cleanup

```bash
rm -rf /tmp/mthc-pretool-postbatch-probe-workspace
rm -rf /tmp/mthc-pretool-postbatch-probe
```

Add `--keep` equivalent: just skip cleanup if you want to preserve logs for further analysis.

## Post-run

Run summarizer:
```bash
python3 experiments/pretool-postbatch-probe/summarize.py
```

Write results to: `docs/superpowers/analyses/2026-04-27-pretool-postbatch-probe-results.md`
