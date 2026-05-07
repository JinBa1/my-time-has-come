package policy

import (
	"testing"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

func TestNoActionWhenWindowAbsent(t *testing.T) {
	s := stateWithWindows(99, 0, 99, 0)
	s.AccountWindow.FiveHour.Absent = true
	s.AccountWindow.SevenDay.Absent = true
	addActiveSession(s, "sess-1")
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != NoAction {
		t.Errorf("got %v, want NoAction when no window data", result.Decision)
	}
}

func TestNoActionBelowSoftThreshold(t *testing.T) {
	s := stateWithWindows(50.0, 1745000000, 50.0, 1745432000)
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != NoAction {
		t.Errorf("got %v, want NoAction at 50%%", result.Decision)
	}
}

func TestSoftInjectAtSoftThreshold(t *testing.T) {
	s := stateWithWindows(85.0, 1745000000, 50.0, 1745432000)
	addActiveSession(s, "sess-1")
	session, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != SoftInject {
		t.Fatalf("got %v, want SoftInject at 85%%", result.Decision)
	}
	if result.Trigger.WindowID != WindowFiveHour {
		t.Fatalf("trigger window = %q, want five_hour", result.Trigger.WindowID)
	}
	if len(session) != 1 || session["sess-1"] == nil {
		t.Fatal("expected sess-1 in SoftInject sessions")
	}
}

func TestSevenDaySoftThresholdTriggers(t *testing.T) {
	s := stateWithWindows(50, 1745000000, 81, 1745432000)
	addActiveSession(s, "sess-1")
	sessions, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != SoftInject {
		t.Fatalf("decision = %v, want SoftInject", result.Decision)
	}
	if result.Trigger.WindowID != WindowSevenDay {
		t.Fatalf("trigger window = %q, want seven_day", result.Trigger.WindowID)
	}
	if result.Trigger.WindowLabel != "7-day" {
		t.Fatalf("trigger label = %q, want 7-day", result.Trigger.WindowLabel)
	}
	if result.Trigger.UsedPercentage != 81 {
		t.Fatalf("trigger used percentage = %v, want 81", result.Trigger.UsedPercentage)
	}
	if result.Trigger.ResetsAt != 1745432000 {
		t.Fatalf("trigger resets_at = %v, want 1745432000", result.Trigger.ResetsAt)
	}
	if result.Trigger.Severity != SoftInject {
		t.Fatalf("trigger severity = %v, want SoftInject", result.Trigger.Severity)
	}
	if sessions["sess-1"] == nil {
		t.Fatal("sess-1 should be pending")
	}
}

func TestSoftInjectIdempotentPerWindow(t *testing.T) {
	s := stateWithWindows(88.0, 1745000000, 50.0, 1745432000)
	sess := addActiveSession(s, "sess-1")
	resetsAt := int64(1745000000)
	sess.SoftInjectedByWindow[WindowFiveHour] = resetsAt
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != NoAction {
		t.Errorf("got %v, want NoAction when already injected this window", result.Decision)
	}
}

func TestHardStopAtHardThreshold(t *testing.T) {
	s := stateWithWindows(95.0, 1745000000, 50.0, 1745432000)
	addActiveSession(s, "sess-1")
	session, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != HardStop {
		t.Fatalf("got %v, want HardStop at 95%%", result.Decision)
	}
	if result.Trigger.WindowID != WindowFiveHour {
		t.Fatalf("trigger window = %q, want five_hour", result.Trigger.WindowID)
	}
	if len(session) != 1 || session["sess-1"] == nil {
		t.Fatal("expected sess-1 in HardStop sessions")
	}
}

func TestSevenDayHardThresholdTriggers(t *testing.T) {
	s := stateWithWindows(50, 1745000000, 91, 1745432000)
	addActiveSession(s, "sess-1")
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != HardStop {
		t.Fatalf("decision = %v, want HardStop", result.Decision)
	}
	if result.Trigger.WindowID != WindowSevenDay {
		t.Fatalf("trigger window = %q, want seven_day", result.Trigger.WindowID)
	}
	if result.Trigger.WindowLabel != "7-day" {
		t.Fatalf("trigger label = %q, want 7-day", result.Trigger.WindowLabel)
	}
	if result.Trigger.UsedPercentage != 91 {
		t.Fatalf("trigger used percentage = %v, want 91", result.Trigger.UsedPercentage)
	}
	if result.Trigger.ResetsAt != 1745432000 {
		t.Fatalf("trigger resets_at = %v, want 1745432000", result.Trigger.ResetsAt)
	}
	if result.Trigger.Severity != HardStop {
		t.Fatalf("trigger severity = %v, want HardStop", result.Trigger.Severity)
	}
}

func TestHardStopIdempotentFallsThroughToSoft(t *testing.T) {
	s := stateWithWindows(97.0, 1745000000, 50.0, 1745432000)
	addActiveSession(s, "sess-1")
	resetsAt := int64(1745000000)
	s.PolicyState.HardTriggeredByWindow[WindowFiveHour] = resetsAt
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != SoftInject {
		t.Errorf("got %v, want SoftInject when hard already triggered but session not soft-injected", result.Decision)
	}
}

func TestHardStopSkipsSoftWhenHardFires(t *testing.T) {
	s := stateWithWindows(95.0, 1745000000, 50.0, 1745432000)
	addActiveSession(s, "sess-1")
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != HardStop {
		t.Fatalf("got %v, want HardStop (hard takes precedence)", result.Decision)
	}
}

func TestHardStopWinsAcrossWindows(t *testing.T) {
	s := stateWithWindows(88, 1745000000, 91, 1745432000)
	addActiveSession(s, "sess-1")
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != HardStop {
		t.Fatalf("decision = %v, want HardStop", result.Decision)
	}
	if result.Trigger.WindowID != WindowSevenDay {
		t.Fatalf("trigger window = %q, want seven_day", result.Trigger.WindowID)
	}
}

func TestLargestOvershootSelectsTrigger(t *testing.T) {
	s := stateWithWindows(96, 1745000000, 94, 1745432000)
	addActiveSession(s, "sess-1")
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != HardStop {
		t.Fatalf("decision = %v, want HardStop", result.Decision)
	}
	if result.Trigger.WindowID != WindowSevenDay {
		t.Fatalf("trigger window = %q, want seven_day", result.Trigger.WindowID)
	}
}

func TestEqualOvershootPrefersFiveHour(t *testing.T) {
	s := stateWithWindows(96, 1745000000, 91, 1745432000)
	addActiveSession(s, "sess-1")
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != HardStop {
		t.Fatalf("decision = %v, want HardStop", result.Decision)
	}
	if result.Trigger.WindowID != WindowFiveHour {
		t.Fatalf("trigger window = %q, want five_hour", result.Trigger.WindowID)
	}
}

func TestPolicyDisabledReturnsNoAction(t *testing.T) {
	s := stateWithWindows(99, 1745000000, 99, 1745432000)
	addActiveSession(s, "sess-1")
	cfg := config.Defaults()
	cfg.Policy.Enabled = false
	_, result := Decide(s, cfg, time.Now())
	if result.Decision != NoAction {
		t.Fatalf("decision = %v, want NoAction", result.Decision)
	}
}

func TestDisabledWindowIgnored(t *testing.T) {
	s := stateWithWindows(50, 1745000000, 99, 1745432000)
	addActiveSession(s, "sess-1")
	cfg := config.Defaults()
	cfg.Thresholds.SevenDay.Enabled = false
	_, result := Decide(s, cfg, time.Now())
	if result.Decision != NoAction {
		t.Fatalf("decision = %v, want NoAction", result.Decision)
	}
}

func TestNoActionForStaleSessions(t *testing.T) {
	s := stateWithWindows(88.0, 1745000000, 50.0, 1745432000)
	s.Sessions["sess-stale"] = &state.Session{
		LastSeenAt:           time.Now().Add(-time.Minute),
		SoftInjectedByWindow: map[string]int64{},
	}
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != NoAction {
		t.Errorf("got %v, want NoAction for stale session", result.Decision)
	}
}

func TestWindowRolloverReArms(t *testing.T) {
	s := stateWithWindows(88.0, 1745000001, 50.0, 1745432000)
	sess := addActiveSession(s, "sess-1")
	oldResetsAt := int64(1745000000)
	sess.SoftInjectedByWindow[WindowFiveHour] = oldResetsAt
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != SoftInject {
		t.Errorf("got %v, want SoftInject after window rollover", result.Decision)
	}
}

func TestBoundaryJustBelowHard(t *testing.T) {
	s := stateWithWindows(94.9, 1745000000, 50.0, 1745432000)
	addActiveSession(s, "sess-1")
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != SoftInject {
		t.Errorf("got %v, want SoftInject at 94.9%% (just below hard)", result.Decision)
	}
}

func TestHardTriggeredAndSoftAlreadyInjected(t *testing.T) {
	s := stateWithWindows(97.0, 1745000000, 50.0, 1745432000)
	sess := addActiveSession(s, "sess-1")
	resetsAt := int64(1745000000)
	s.PolicyState.HardTriggeredByWindow[WindowFiveHour] = resetsAt
	sess.SoftInjectedByWindow[WindowFiveHour] = resetsAt
	_, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != NoAction {
		t.Errorf("got %v, want NoAction when hard triggered and soft already injected this window", result.Decision)
	}
}

func TestMultipleSessionsMixedInjectionState(t *testing.T) {
	s := stateWithWindows(88.0, 1745000000, 50.0, 1745432000)
	sess1 := addActiveSession(s, "sess-1")
	resetsAt := int64(1745000000)
	sess1.SoftInjectedByWindow[WindowFiveHour] = resetsAt // already injected
	addActiveSession(s, "sess-2")                         // not injected

	sessions, result := Decide(s, config.Defaults(), time.Now())
	if result.Decision != SoftInject {
		t.Fatalf("got %v, want SoftInject", result.Decision)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 pending session, got %d", len(sessions))
	}
	if sessions["sess-1"] != nil {
		t.Error("sess-1 should not be pending (already injected)")
	}
	if sessions["sess-2"] == nil {
		t.Error("sess-2 should be pending")
	}
}

func stateWithWindows(fivePct float64, fiveReset int64, sevenPct float64, sevenReset int64) *state.State {
	return &state.State{
		AccountWindow: state.AccountWindow{
			FiveHour: state.WindowObservation{
				UsedPercentage: fivePct,
				ResetsAt:       fiveReset,
				Source:         "statusline",
			},
			SevenDay: state.WindowObservation{
				UsedPercentage: sevenPct,
				ResetsAt:       sevenReset,
				Source:         "statusline",
			},
		},
		Sessions: map[string]*state.Session{},
		PolicyState: state.PolicyState{
			HardTriggeredByWindow:    map[string]int64{},
			HandoffWrittenAtByWindow: map[string]time.Time{},
			HandoffPathsByWindow:     map[string]map[string]string{},
		},
	}
}

func addActiveSession(s *state.State, id string) *state.Session {
	sess := &state.Session{LastSeenAt: time.Now(), SoftInjectedByWindow: map[string]int64{}}
	s.Sessions[id] = sess
	return sess
}
