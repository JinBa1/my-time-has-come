package policy

import (
	"testing"
	"time"

	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/state"
)

func TestNoActionWhenWindowAbsent(t *testing.T) {
	_, decision := Decide(&state.State{}, config.Defaults(), time.Now())
	if decision != NoAction {
		t.Errorf("got %v, want NoAction when no window data", decision)
	}
}

func TestNoActionBelowSoftThreshold(t *testing.T) {
	s := stateWithUsage(50.0, 1745000000)
	_, decision := Decide(s, config.Defaults(), time.Now())
	if decision != NoAction {
		t.Errorf("got %v, want NoAction at 50%%", decision)
	}
}

func TestSoftInjectAtSoftThreshold(t *testing.T) {
	s := stateWithUsage(85.0, 1745000000)
	addActiveSession(s, "sess-1")
	session, decision := Decide(s, config.Defaults(), time.Now())
	if decision != SoftInject {
		t.Fatalf("got %v, want SoftInject at 85%%", decision)
	}
	if len(session) != 1 || session["sess-1"] == nil {
		t.Fatal("expected sess-1 in SoftInject sessions")
	}
}

func TestSoftInjectIdempotentPerWindow(t *testing.T) {
	s := stateWithUsage(88.0, 1745000000)
	sess := addActiveSession(s, "sess-1")
	resetsAt := int64(1745000000)
	sess.SoftInjectedForResetsAt = &resetsAt
	_, decision := Decide(s, config.Defaults(), time.Now())
	if decision != NoAction {
		t.Errorf("got %v, want NoAction when already injected this window", decision)
	}
}

func TestHardStopAtHardThreshold(t *testing.T) {
	s := stateWithUsage(95.0, 1745000000)
	addActiveSession(s, "sess-1")
	session, decision := Decide(s, config.Defaults(), time.Now())
	if decision != HardStop {
		t.Fatalf("got %v, want HardStop at 95%%", decision)
	}
	if len(session) != 1 || session["sess-1"] == nil {
		t.Fatal("expected sess-1 in HardStop sessions")
	}
}

func TestHardStopIdempotentPerWindow(t *testing.T) {
	s := stateWithUsage(97.0, 1745000000)
	addActiveSession(s, "sess-1")
	resetsAt := int64(1745000000)
	s.PolicyState.HardTriggeredForResetsAt = &resetsAt
	_, decision := Decide(s, config.Defaults(), time.Now())
	if decision != NoAction {
		t.Errorf("got %v, want NoAction when hard already triggered", decision)
	}
}

func TestHardStopSkipsSoftWhenHardFires(t *testing.T) {
	s := stateWithUsage(95.0, 1745000000)
	addActiveSession(s, "sess-1")
	_, decision := Decide(s, config.Defaults(), time.Now())
	if decision != HardStop {
		t.Fatalf("got %v, want HardStop (hard takes precedence)", decision)
	}
}

func TestNoActionForStaleSessions(t *testing.T) {
	s := stateWithUsage(88.0, 1745000000)
	sess := &state.Session{
		LastSeenAt: time.Now().Add(-time.Minute),
	}
	s.Sessions["sess-stale"] = sess
	_, decision := Decide(s, config.Defaults(), time.Now())
	if decision != NoAction {
		t.Errorf("got %v, want NoAction for stale session", decision)
	}
}

func TestWindowRolloverReArms(t *testing.T) {
	s := stateWithUsage(88.0, 1745000001)
	sess := addActiveSession(s, "sess-1")
	oldResetsAt := int64(1745000000)
	sess.SoftInjectedForResetsAt = &oldResetsAt
	_, decision := Decide(s, config.Defaults(), time.Now())
	if decision != SoftInject {
		t.Errorf("got %v, want SoftInject after window rollover", decision)
	}
}

func stateWithUsage(pct float64, resetsAt int64) *state.State {
	s := state.State{
		AccountWindow: state.AccountWindow{
			FiveHour: state.WindowObservation{
				UsedPercentage: pct,
				ResetsAt:       resetsAt,
				Source:         "statusline",
			},
		},
		Sessions:    make(map[string]*state.Session),
		PolicyState: state.PolicyState{HandoffPaths: make(map[string]string)},
	}
	return &s
}

func addActiveSession(s *state.State, id string) *state.Session {
	sess := &state.Session{LastSeenAt: time.Now()}
	s.Sessions[id] = sess
	return sess
}
