package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JinBa1/mthc/internal/adapter"
	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/policy"
	"github.com/JinBa1/mthc/internal/state"
)

func TestResolveHandoffPathNoCollision(t *testing.T) {
	result := ResolveHandoffPath("/tmp/handoff.md", nil)
	if result != "/tmp/handoff.md" {
		t.Errorf("expected /tmp/handoff.md, got %q", result)
	}
}

func TestResolveHandoffPathSingleCollision(t *testing.T) {
	existing := []string{"/tmp/handoff.md"}
	result := ResolveHandoffPath("/tmp/handoff.md", existing)
	if result != "/tmp/handoff-2.md" {
		t.Errorf("expected /tmp/handoff-2.md, got %q", result)
	}
}

func TestResolveHandoffPathMultipleCollisions(t *testing.T) {
	existing := []string{"/tmp/handoff.md", "/tmp/handoff-2.md"}
	result := ResolveHandoffPath("/tmp/handoff.md", existing)
	if result != "/tmp/handoff-3.md" {
		t.Errorf("expected /tmp/handoff-3.md, got %q", result)
	}
}

func TestProcessStatuslineNoActionBelowThreshold(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourUsedPct:  50.0,
		FiveHourResetsAt: 1745000000,
	}
	now := time.Now()

	result := ProcessStatusline(s, cfg, p, now)
	if result.Decision != policy.NoAction {
		t.Errorf("expected NoAction, got %v", result.Decision)
	}
}

func TestProcessStatuslineSoftInject(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourUsedPct:  87.0,
		FiveHourResetsAt: 1745000000,
	}
	now := time.Now()

	result := ProcessStatusline(s, cfg, p, now)
	if result.Decision != policy.SoftInject {
		t.Fatalf("expected SoftInject, got %v", result.Decision)
	}
	if len(result.Sessions) != 1 || result.Sessions["sess-1"] == nil {
		t.Fatal("expected sess-1 in result sessions")
	}
}

func TestProcessStatuslineHardStopWithHandoff(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourUsedPct:  96.0,
		FiveHourResetsAt: 1745000000,
	}
	now := time.Now()

	result := ProcessStatusline(s, cfg, p, now)
	if result.Decision != policy.HardStop {
		t.Fatalf("expected HardStop, got %v", result.Decision)
	}
	hasHandoff := false
	for _, se := range result.SideEffects {
		if se.Type == SideEffectHandoffWrite && se.SessionID == "sess-1" {
			hasHandoff = true
		}
	}
	if !hasHandoff {
		t.Error("expected SideEffectHandoffWrite for sess-1")
	}
	if s.PolicyState.HardTriggeredForResetsAt == nil || *s.PolicyState.HardTriggeredForResetsAt != 1745000000 {
		t.Error("expected HardTriggeredForResetsAt to be set")
	}
	// C4 fix: core must set HandoffPaths so replay state matches live
	if s.PolicyState.HandoffPaths["sess-1"] == "" {
		t.Error("expected HandoffPaths[sess-1] to be set by core")
	}
}

func TestProcessStatuslineSessionUpsert(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
	cfg := config.Defaults()
	now := time.Now()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourUsedPct:  50.0,
		FiveHourResetsAt: 1745000000,
		TranscriptPath:   "/tmp/transcript.jsonl",
		ModelID:          "claude-sonnet",
		CWD:              "/home/user/project",
	}

	ProcessStatusline(s, cfg, p, now)
	sess := s.Sessions["sess-1"]
	if sess == nil {
		t.Fatal("expected session to be upserted")
	}
	if sess.TranscriptPath != "/tmp/transcript.jsonl" {
		t.Errorf("expected TranscriptPath, got %q", sess.TranscriptPath)
	}
	if sess.ModelID != "claude-sonnet" {
		t.Errorf("expected ModelID, got %q", sess.ModelID)
	}
	if sess.CWD != "/home/user/project" {
		t.Errorf("expected CWD, got %q", sess.CWD)
	}
}

func TestProcessStatuslinePrunesStaleSessions(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
	cfg := config.Defaults()
	now := time.Now()
	s.Sessions["stale-sess"] = &state.Session{
		LastSeenAt: now.Add(-time.Hour),
	}

	p := adapter.StatuslinePayload{
		FiveHourUsedPct:  50.0,
		FiveHourResetsAt: 1745000000,
	}
	ProcessStatusline(s, cfg, p, now)
	if _, exists := s.Sessions["stale-sess"]; exists {
		t.Error("expected stale session to be pruned")
	}
}

func TestProcessStatuslineSetsUpdatedAt(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
	cfg := config.Defaults()
	now := time.Date(2026, 4, 28, 14, 30, 0, 0, time.UTC)
	p := adapter.StatuslinePayload{
		FiveHourUsedPct:  50.0,
		FiveHourResetsAt: 1745000000,
	}

	ProcessStatusline(s, cfg, p, now)
	if !s.UpdatedAt.Equal(now) {
		t.Errorf("expected UpdatedAt=%v, got %v", now, s.UpdatedAt)
	}
}

func TestProcessHookPostToolBatchSoftInjectProcessCWD(t *testing.T) {
	// Regression: processCWD must flow through handlePostToolBatch when
	// session CWD is empty, so the handoff path is anchored correctly.
	s := &state.State{
		AccountWindow: state.AccountWindow{
			FiveHour: state.WindowObservation{
				UsedPercentage: 87.0,
				ResetsAt:       1745000000,
				Source:         "statusline",
			},
		},
		Sessions:    map[string]*state.Session{"sess-1": {LastSeenAt: time.Now()}}, // no CWD set
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
	cfg := config.Defaults()
	now := time.Now()

	result := ProcessHook(s, cfg, HookEvent{
		HookEventName: "PostToolBatch",
		SessionID:     "sess-1",
	}, now, "/some/test/cwd")

	if result.Decision != policy.SoftInject {
		t.Fatalf("expected SoftInject, got %v", result.Decision)
	}
	hasSoft := false
	for _, se := range result.SideEffects {
		if se.Type == SideEffectSoftInject && se.SessionID == "sess-1" {
			hasSoft = true
			if !strings.Contains(se.Content, "/some/test/cwd") {
				t.Errorf("expected soft inject content to contain processCWD %q, got:\n%s", "/some/test/cwd", se.Content)
			}
		}
	}
	if !hasSoft {
		t.Error("expected SideEffectSoftInject for sess-1")
	}
}

func TestProcessHookPostToolBatchSoftInject(t *testing.T) {
	s := stateWithHardNotTriggered(87.0, 1745000000)
	cfg := config.Defaults()
	now := time.Now()

	result := ProcessHook(s, cfg, HookEvent{
		HookEventName: "PostToolBatch",
		SessionID:     "sess-1",
	}, now, "")

	if result.Decision != policy.SoftInject {
		t.Fatalf("expected SoftInject, got %v", result.Decision)
	}
	hasSoft := false
	for _, se := range result.SideEffects {
		if se.Type == SideEffectSoftInject && se.SessionID == "sess-1" {
			hasSoft = true
			if se.Content == "" {
				t.Error("expected non-empty soft inject content")
			}
		}
	}
	if !hasSoft {
		t.Error("expected SideEffectSoftInject for sess-1")
	}
}

func TestProcessHookPostToolBatchBelowThreshold(t *testing.T) {
	s := stateWithHardNotTriggered(50.0, 1745000000)
	cfg := config.Defaults()
	now := time.Now()

	result := ProcessHook(s, cfg, HookEvent{
		HookEventName: "PostToolBatch",
		SessionID:     "sess-1",
	}, now, "")

	if result.Decision != policy.NoAction {
		t.Errorf("expected NoAction, got %v", result.Decision)
	}
	if len(result.SideEffects) != 0 {
		t.Errorf("expected no side effects, got %d", len(result.SideEffects))
	}
}

func TestProcessHookPreToolUseArmedGate(t *testing.T) {
	s := stateWithHardTriggered(96.0, 1745000000)
	cfg := config.Defaults()
	now := time.Now()
	const hardStopReason = "MTHC: local quota policy active, usage window near exhaustion. Tool use blocked."

	result := ProcessHook(s, cfg, HookEvent{
		HookEventName: "PreToolUse",
		SessionID:     "sess-1",
	}, now, "")

	if result.Response.PermissionDecision != "" {
		t.Errorf("expected no top-level permissionDecision, got %q", result.Response.PermissionDecision)
	}
	if result.Response.PermissionDecisionReason != "" {
		t.Errorf("expected no top-level permissionDecisionReason, got %q", result.Response.PermissionDecisionReason)
	}

	hookOutput, ok := result.Response.HookSpecificOutput.(map[string]any)
	if !ok {
		t.Fatalf("expected hookSpecificOutput map, got %T", result.Response.HookSpecificOutput)
	}
	if hookOutput["hookEventName"] != "PreToolUse" {
		t.Errorf("expected hookEventName PreToolUse, got %q", hookOutput["hookEventName"])
	}
	if hookOutput["permissionDecision"] != "deny" {
		t.Errorf("expected nested deny, got %q", hookOutput["permissionDecision"])
	}
	if hookOutput["permissionDecisionReason"] != hardStopReason {
		t.Errorf("expected nested permissionDecisionReason %q, got %q", hardStopReason, hookOutput["permissionDecisionReason"])
	}

	out, err := json.Marshal(result.Response)
	if err != nil {
		t.Fatalf("marshal HookResponse: %v", err)
	}
	wantJSON := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"MTHC: local quota policy active, usage window near exhaustion. Tool use blocked."}}`
	if string(out) != wantJSON {
		t.Errorf("unexpected hardstop JSON\nwant: %s\n got: %s", wantJSON, out)
	}

	hasDeny := false
	for _, se := range result.SideEffects {
		if se.Type == SideEffectHardDeny {
			hasDeny = true
		}
	}
	if !hasDeny {
		t.Error("expected SideEffectHardDeny")
	}
}

func TestProcessHookPreToolUseDisarmedGate(t *testing.T) {
	s := stateWithHardNotTriggered(50.0, 1745000000)
	cfg := config.Defaults()
	now := time.Now()

	result := ProcessHook(s, cfg, HookEvent{
		HookEventName: "PreToolUse",
		SessionID:     "sess-1",
	}, now, "")

	if result.Response.PermissionDecision != "" {
		t.Errorf("expected empty response, got %q", result.Response.PermissionDecision)
	}
	out, err := json.Marshal(result.Response)
	if err != nil {
		t.Fatalf("marshal HookResponse: %v", err)
	}
	if string(out) != "{}" {
		t.Errorf("expected empty JSON response, got %s", out)
	}
}

func TestProcessHookLateJoinHandoff(t *testing.T) {
	s := stateWithHardTriggered(96.0, 1745000000)
	cfg := config.Defaults()
	now := time.Now()

	result := ProcessHook(s, cfg, HookEvent{
		HookEventName: "PreToolUse",
		SessionID:     "sess-1",
	}, now, "")

	hasHandoff := false
	for _, se := range result.SideEffects {
		if se.Type == SideEffectHandoffWrite && se.SessionID == "sess-1" {
			hasHandoff = true
		}
	}
	if !hasHandoff {
		t.Error("expected SideEffectHandoffWrite for late-join session")
	}
	// C4 fix: core must set HandoffPaths so replay state matches live
	if s.PolicyState.HandoffPaths["sess-1"] == "" {
		t.Error("expected HandoffPaths[sess-1] to be set by core")
	}
}

// C4 regression: second PreToolUse on same session must NOT produce
// another handoff (map already set by first call).
func TestProcessHookLateJoinHandoffIdempotent(t *testing.T) {
	s := stateWithHardTriggered(96.0, 1745000000)
	cfg := config.Defaults()
	now := time.Now()

	// First call sets HandoffPaths["sess-1"]
	ProcessHook(s, cfg, HookEvent{
		HookEventName: "PreToolUse",
		SessionID:     "sess-1",
	}, now, "")

	// Second call should not produce another handoff write
	result := ProcessHook(s, cfg, HookEvent{
		HookEventName: "PreToolUse",
		SessionID:     "sess-1",
	}, now, "")

	for _, se := range result.SideEffects {
		if se.Type == SideEffectHandoffWrite {
			t.Error("expected no duplicate SideEffectHandoffWrite on second call")
		}
	}
}

// Helpers for core tests

func stateWithHardNotTriggered(pct float64, resetsAt int64) *state.State {
	return &state.State{
		AccountWindow: state.AccountWindow{
			FiveHour: state.WindowObservation{
				UsedPercentage: pct,
				ResetsAt:       resetsAt,
				Source:         "statusline",
			},
		},
		Sessions:    map[string]*state.Session{"sess-1": {LastSeenAt: time.Now()}},
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
}

func stateWithHardTriggered(pct float64, resetsAt int64) *state.State {
	s := stateWithHardNotTriggered(pct, resetsAt)
	s.PolicyState.HardTriggeredForResetsAt = &resetsAt
	return s
}
