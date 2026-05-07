package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/adapter"
	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/policy"
	"github.com/JinBa1/my-time-has-come/internal/state"
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

func TestRenderHandoffPathReplacesDefaultWindowIDToken(t *testing.T) {
	s := &state.State{
		Sessions: map[string]*state.Session{
			"sess-1": {
				CWD:     "/work/project",
				ModelID: "claude-sonnet",
			},
		},
	}
	path := renderHandoffPath(config.Defaults(), s, "sess-1", policy.Trigger{
		WindowID:    policy.WindowFiveHour,
		WindowLabel: "5-hour",
		ResetsAt:    1745000000,
	}, "")
	if strings.Contains(path, "{window_id}") {
		t.Fatalf("path should not contain literal window_id token: %q", path)
	}
	if !strings.Contains(path, "five_hour") {
		t.Fatalf("path should include current five-hour window id, got %q", path)
	}
}

func TestProcessStatuslineNoActionBelowThreshold(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourPresent:  true,
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
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourPresent:  true,
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
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourPresent:  true,
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
	if s.PolicyState.HardTriggeredByWindow["five_hour"] != 1745000000 {
		t.Error("expected HardTriggeredByWindow[five_hour] to be set")
	}
	// C4 fix: core must set HandoffPaths so replay state matches live
	if s.PolicyState.HandoffPathsByWindow["five_hour"]["sess-1"] == "" {
		t.Error("expected HandoffPathsByWindow[five_hour][sess-1] to be set by core")
	}
}

func TestProcessStatuslineSevenDayHardStopWithWindowPath(t *testing.T) {
	s := &state.State{
		Sessions:    map[string]*state.Session{},
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourUsedPct:  50,
		FiveHourResetsAt: 1745000000,
		FiveHourPresent:  true,
		SevenDayUsedPct:  91,
		SevenDayResetsAt: 1745432000,
		SevenDayPresent:  true,
	}
	result := ProcessStatusline(s, cfg, p, time.Now())
	if result.Decision != policy.HardStop {
		t.Fatalf("decision = %v, want HardStop", result.Decision)
	}
	if result.Trigger.WindowID != "seven_day" {
		t.Fatalf("trigger window = %q, want seven_day", result.Trigger.WindowID)
	}
	if s.PolicyState.HardTriggeredByWindow["seven_day"] != 1745432000 {
		t.Fatalf("hard trigger map = %+v", s.PolicyState.HardTriggeredByWindow)
	}
	var handoff SideEffect
	for _, se := range result.SideEffects {
		if se.Type == SideEffectHandoffWrite {
			handoff = se
		}
	}
	if handoff.Path == "" {
		t.Fatal("expected handoff side effect")
	}
	if !strings.Contains(handoff.Path, "seven_day") {
		t.Fatalf("handoff path should include window id, got %q", handoff.Path)
	}
	if !strings.Contains(handoff.Content, "7-day window") {
		t.Fatalf("handoff content should name 7-day window:\n%s", handoff.Content)
	}
}

func TestProcessStatuslineSingleMissingSevenDayTickDoesNotFlap(t *testing.T) {
	now := time.Now()
	s := &state.State{
		AccountWindow: state.AccountWindow{
			SevenDay: state.WindowObservation{
				UsedPercentage: 99,
				ResetsAt:       1745432000,
				Source:         "statusline",
				LastObservedAt: now.Add(-5 * time.Second),
			},
		},
		Sessions:    map[string]*state.Session{},
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourUsedPct:  50,
		FiveHourResetsAt: 1745000000,
		FiveHourPresent:  true,
	}
	result := ProcessStatusline(s, cfg, p, now)
	if result.Decision != policy.HardStop {
		t.Fatalf("decision = %v, want HardStop from fresh stored seven_day observation", result.Decision)
	}
	if s.AccountWindow.SevenDay.Absent {
		t.Fatal("seven_day should stay present after a single missing tick")
	}
}

func TestProcessStatuslineMissingRateLimitsRootDoesNotFlapFreshObservation(t *testing.T) {
	now := time.Now()
	s := &state.State{
		AccountWindow: state.AccountWindow{
			SevenDay: state.WindowObservation{
				UsedPercentage: 99,
				ResetsAt:       1745432000,
				Source:         "statusline",
				LastObservedAt: now.Add(-5 * time.Second),
			},
		},
		Sessions:    map[string]*state.Session{},
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID: "sess-1",
	}
	result := ProcessStatusline(s, cfg, p, now)
	if result.Decision != policy.HardStop {
		t.Fatalf("decision = %v, want HardStop from fresh stored seven_day observation", result.Decision)
	}
	if s.AccountWindow.SevenDay.Absent {
		t.Fatal("seven_day should stay present after a missing rate_limits tick")
	}
}

func TestProcessStatuslineStaleMissingSevenDayIsIgnored(t *testing.T) {
	now := time.Now()
	s := &state.State{
		AccountWindow: state.AccountWindow{
			SevenDay: state.WindowObservation{
				UsedPercentage: 99,
				ResetsAt:       1745432000,
				Source:         "statusline",
				LastObservedAt: now.Add(-time.Minute),
			},
		},
		Sessions:    map[string]*state.Session{},
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourUsedPct:  50,
		FiveHourResetsAt: 1745000000,
		FiveHourPresent:  true,
	}
	result := ProcessStatusline(s, cfg, p, now)
	if result.Decision != policy.NoAction {
		t.Fatalf("decision = %v, want NoAction", result.Decision)
	}
	if !s.AccountWindow.SevenDay.Absent {
		t.Fatal("seven_day should be marked absent after staleness cutoff")
	}
}

func TestProcessStatuslineSessionUpsert(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	now := time.Now()
	p := adapter.StatuslinePayload{
		SessionID:        "sess-1",
		FiveHourPresent:  true,
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
	if sess.SoftInjectedByWindow == nil {
		t.Error("expected SoftInjectedByWindow to be initialized")
	}
}

func TestProcessStatuslinePrunesStaleSessions(t *testing.T) {
	s := &state.State{
		Sessions:    make(map[string]*state.Session),
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	now := time.Now()
	s.Sessions["stale-sess"] = &state.Session{
		LastSeenAt: now.Add(-time.Hour),
	}

	p := adapter.StatuslinePayload{
		FiveHourPresent:  true,
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
		PolicyState: newTestPolicyState(),
	}
	cfg := config.Defaults()
	now := time.Date(2026, 4, 28, 14, 30, 0, 0, time.UTC)
	p := adapter.StatuslinePayload{
		FiveHourPresent:  true,
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
		Sessions: map[string]*state.Session{
			"sess-1": {LastSeenAt: time.Now(), SoftInjectedByWindow: map[string]int64{}},
		}, // no CWD set
		PolicyState: newTestPolicyState(),
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
	const hardStopReason = "MTHC: local quota policy active; 5-hour window is 96.0% used and resets at 2025-04-18T18:13:20Z. Tool use blocked."

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
	wantJSON := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"MTHC: local quota policy active; 5-hour window is 96.0% used and resets at 2025-04-18T18:13:20Z. Tool use blocked."}}`
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

func TestProcessHookPreToolUseReasonNamesTriggerWindow(t *testing.T) {
	s := &state.State{
		AccountWindow: state.AccountWindow{
			SevenDay: state.WindowObservation{
				UsedPercentage: 91,
				ResetsAt:       1745432000,
				Source:         "statusline",
			},
		},
		Sessions: map[string]*state.Session{
			"sess-1": {LastSeenAt: time.Now(), SoftInjectedByWindow: map[string]int64{}},
		},
		PolicyState: state.PolicyState{
			HardTriggeredByWindow:    map[string]int64{"seven_day": 1745432000},
			HandoffWrittenAtByWindow: map[string]time.Time{},
			HandoffPathsByWindow:     map[string]map[string]string{},
		},
	}
	result := ProcessHook(s, config.Defaults(), HookEvent{
		HookEventName: "PreToolUse",
		SessionID:     "sess-1",
	}, time.Now(), "")
	hookOutput, ok := result.Response.HookSpecificOutput.(map[string]any)
	if !ok {
		t.Fatalf("expected hookSpecificOutput map, got %T", result.Response.HookSpecificOutput)
	}
	reason, _ := hookOutput["permissionDecisionReason"].(string)
	if !strings.Contains(reason, "7-day window") {
		t.Fatalf("deny reason should name 7-day window, got %q", reason)
	}
	if !strings.Contains(reason, "91.0% used") {
		t.Fatalf("deny reason should include usage, got %q", reason)
	}
}

func TestProcessHookPreToolUsePrefersFiveHourWhenBothWindowsArmed(t *testing.T) {
	resetsAt := int64(1745000000)
	s := &state.State{
		AccountWindow: state.AccountWindow{
			FiveHour: state.WindowObservation{
				UsedPercentage: 95,
				ResetsAt:       resetsAt,
				Source:         "statusline",
			},
			SevenDay: state.WindowObservation{
				UsedPercentage: 90,
				ResetsAt:       1745432000,
				Source:         "statusline",
			},
		},
		Sessions: map[string]*state.Session{
			"sess-1": {LastSeenAt: time.Now(), SoftInjectedByWindow: map[string]int64{}},
		},
		PolicyState: state.PolicyState{
			HardTriggeredByWindow:    map[string]int64{"five_hour": resetsAt, "seven_day": 1745432000},
			HandoffWrittenAtByWindow: map[string]time.Time{},
			HandoffPathsByWindow:     map[string]map[string]string{},
		},
	}
	result := ProcessHook(s, config.Defaults(), HookEvent{
		HookEventName: "PreToolUse",
		SessionID:     "sess-1",
	}, time.Now(), "")
	if result.Trigger.WindowID != policy.WindowFiveHour {
		t.Fatalf("trigger window = %q, want five_hour", result.Trigger.WindowID)
	}
	hookOutput, ok := result.Response.HookSpecificOutput.(map[string]any)
	if !ok {
		t.Fatalf("expected hookSpecificOutput map, got %T", result.Response.HookSpecificOutput)
	}
	reason, _ := hookOutput["permissionDecisionReason"].(string)
	if !strings.Contains(reason, "5-hour window") {
		t.Fatalf("deny reason should name 5-hour window, got %q", reason)
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
	if s.PolicyState.HandoffPathsByWindow["five_hour"]["sess-1"] == "" {
		t.Error("expected HandoffPathsByWindow[five_hour][sess-1] to be set by core")
	}
}

// C4 regression: second PreToolUse on same session must NOT produce
// another handoff (map already set by first call).
func TestProcessHookLateJoinHandoffIdempotent(t *testing.T) {
	s := stateWithHardTriggered(96.0, 1745000000)
	cfg := config.Defaults()
	now := time.Now()

	// First call sets HandoffPathsByWindow["five_hour"]["sess-1"]
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
		Sessions: map[string]*state.Session{
			"sess-1": {LastSeenAt: time.Now(), SoftInjectedByWindow: map[string]int64{}},
		},
		PolicyState: newTestPolicyState(),
	}
}

func stateWithHardTriggered(pct float64, resetsAt int64) *state.State {
	s := stateWithHardNotTriggered(pct, resetsAt)
	s.PolicyState.HardTriggeredByWindow["five_hour"] = resetsAt
	return s
}

func newTestPolicyState() state.PolicyState {
	return state.PolicyState{
		HardTriggeredByWindow:    map[string]int64{},
		HandoffWrittenAtByWindow: map[string]time.Time{},
		HandoffPathsByWindow:     map[string]map[string]string{},
	}
}
