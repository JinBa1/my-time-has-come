package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/policy"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

func TestStatusShowsTwoWindowsPolicyAndWindowState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	currentResetsAt := int64(200)
	staleResetsAt := int64(100)
	sevenDayResetsAt := int64(300)
	now := time.Now().UTC()
	st := &state.State{
		SchemaVersion: 2,
		AccountWindow: state.AccountWindow{
			FiveHour: state.WindowObservation{
				UsedPercentage: 1,
				ResetsAt:       currentResetsAt,
				Source:         "statusline",
				LastObservedAt: now,
			},
			SevenDay: state.WindowObservation{
				UsedPercentage: 91,
				ResetsAt:       sevenDayResetsAt,
				Source:         "statusline",
				LastObservedAt: now,
			},
		},
		Sessions: map[string]*state.Session{
			"sess-1": {
				ModelID:              "claude-sonnet",
				LastSeenAt:           now,
				SoftInjectedByWindow: map[string]int64{"five_hour": currentResetsAt, "seven_day": sevenDayResetsAt},
			},
		},
		PolicyState: state.PolicyState{
			HardTriggeredByWindow:    map[string]int64{"five_hour": staleResetsAt, "seven_day": sevenDayResetsAt},
			HandoffWrittenAtByWindow: map[string]time.Time{},
			HandoffPathsByWindow:     map[string]map[string]string{},
		},
		TranscriptCursors: map[string]*state.CursorEntry{},
	}
	// Dual-write: also populate keyed Observations map.
	st.UpsertObservation(state.Observation{
		Source: state.SourceStatusline, Unit: state.UnitPercent, Value: 1,
		Window: state.WindowRef{ID: policy.WindowFiveHour, ResetsAt: currentResetsAt},
		Scope:  state.ScopeAccount, ObservedAt: now,
	})
	st.UpsertObservation(state.Observation{
		Source: state.SourceStatusline, Unit: state.UnitPercent, Value: 91,
		Window: state.WindowRef{ID: policy.WindowSevenDay, ResetsAt: sevenDayResetsAt},
		Scope:  state.ScopeAccount, ObservedAt: now,
	})
	writeStatusState(t, home, st)

	output := captureStatusOutput(t)

	assertStatusContains(t, output, "5-hour window:")
	assertStatusContains(t, output, "7-day window:")
	assertStatusContains(t, output, "Policy:         enabled=true")
	assertStatusContains(t, output, "Thresholds:")
	assertStatusContains(t, output, "five_hour: enabled=true unit=percent soft=85 hard=95")
	assertStatusContains(t, output, "seven_day: enabled=true unit=percent soft=90 hard=98")
	assertStatusContains(t, output, "five_hour: disarmed (stale trigger resets_at=100)")
	assertStatusContains(t, output, "seven_day: ARMED (resets_at=300)")
	assertStatusContains(t, output, "soft-injected=five_hour:200,seven_day:300")
}

func TestStatusDoesNotShowAbsentWindowHardGateAsArmed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	resetsAt := int64(300)
	now2 := time.Now().UTC()
	st2 := &state.State{
		SchemaVersion: 2,
		AccountWindow: state.AccountWindow{
			SevenDay: state.WindowObservation{
				UsedPercentage: 91,
				ResetsAt:       resetsAt,
				Source:         "statusline",
				LastObservedAt: now2,
				Absent:         true,
			},
		},
		Sessions: map[string]*state.Session{},
		PolicyState: state.PolicyState{
			HardTriggeredByWindow:    map[string]int64{"seven_day": resetsAt},
			HandoffWrittenAtByWindow: map[string]time.Time{},
			HandoffPathsByWindow:     map[string]map[string]string{},
		},
		TranscriptCursors: map[string]*state.CursorEntry{},
	}
	// Dual-write: also populate keyed Observations map (absent).
	st2.UpsertObservation(state.Observation{
		Source: state.SourceStatusline, Unit: state.UnitPercent, Value: 91,
		Window: state.WindowRef{ID: policy.WindowSevenDay, ResetsAt: resetsAt},
		Scope:  state.ScopeAccount, ObservedAt: now2, Absent: true,
	})
	writeStatusState(t, home, st2)

	output := captureStatusOutput(t)
	assertStatusContains(t, output, "7-day window:")
	assertStatusContains(t, output, "No data yet")
	assertStatusContains(t, output, "seven_day: disarmed (stale trigger resets_at=300)")
	if strings.Contains(output, "seven_day: ARMED") {
		t.Fatalf("absent window must not be shown armed:\n%s", output)
	}
}

func TestStatusPrintsSessionsInStableOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now().UTC()
	writeStatusState(t, home, &state.State{
		SchemaVersion: 2,
		Sessions: map[string]*state.Session{
			"z-session": {ModelID: "z", LastSeenAt: now, SoftInjectedByWindow: map[string]int64{}},
			"a-session": {ModelID: "a", LastSeenAt: now, SoftInjectedByWindow: map[string]int64{}},
		},
		PolicyState: state.PolicyState{
			HardTriggeredByWindow:    map[string]int64{},
			HandoffWrittenAtByWindow: map[string]time.Time{},
			HandoffPathsByWindow:     map[string]map[string]string{},
		},
		TranscriptCursors: map[string]*state.CursorEntry{},
	})

	output := captureStatusOutput(t)
	first := strings.Index(output, "a-session:")
	second := strings.Index(output, "z-session:")
	if first < 0 || second < 0 {
		t.Fatalf("expected both sessions in output:\n%s", output)
	}
	if first > second {
		t.Fatalf("sessions should be sorted by ID:\n%s", output)
	}
}

func assertStatusContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q:\n%s", want, got)
	}
}

func writeStatusState(t *testing.T, home string, s *state.State) {
	t.Helper()

	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	if err := s.Write(statePath); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

func captureStatusOutput(t *testing.T) string {
	t.Helper()

	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = writer

	if err := runStatus(); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing stdout writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing stdout reader: %v", err)
	}
	return string(output)
}
