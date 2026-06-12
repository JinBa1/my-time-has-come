package playback

import (
	"path/filepath"
	"testing"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/policy"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

func TestReplayBaselineNoAction(t *testing.T) {
	steps, err := Replay([]string{filepath.Join("testdata", "baseline.jsonl")}, config.Defaults())
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	for _, s := range steps {
		if s.Decision != policy.NoAction {
			t.Errorf("expected NoAction at step %v, got %v", s.TS, s.Decision)
		}
		if s.State.SchemaVersion != 2 {
			t.Errorf("expected schema version 2 at step %v, got %d", s.TS, s.State.SchemaVersion)
		}
	}
}

func TestReplaySoftInject(t *testing.T) {
	steps, err := Replay([]string{filepath.Join("testdata", "soft_inject.jsonl")}, config.Defaults())
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	softInjectCount := 0
	for _, s := range steps {
		if s.Decision == policy.SoftInject {
			softInjectCount++
			if s.Trigger.WindowID != policy.WindowFiveHour {
				t.Errorf("soft trigger window: got %q, want %q", s.Trigger.WindowID, policy.WindowFiveHour)
			}
			if s.Trigger.WindowLabel != "5-hour" {
				t.Errorf("soft trigger label: got %q, want 5-hour", s.Trigger.WindowLabel)
			}
		}
	}
	if softInjectCount == 0 {
		t.Error("expected at least one SoftInject step")
	}
}

func TestReplayHardGate(t *testing.T) {
	steps, err := Replay([]string{filepath.Join("testdata", "hard_gate.jsonl")}, config.Defaults())
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	hasHardStop := false
	for _, s := range steps {
		if s.Decision == policy.HardStop {
			hasHardStop = true
			if s.Trigger.WindowID != policy.WindowFiveHour {
				t.Errorf("hard trigger window: got %q, want %q", s.Trigger.WindowID, policy.WindowFiveHour)
			}
			if s.Trigger.WindowLabel != "5-hour" {
				t.Errorf("hard trigger label: got %q, want 5-hour", s.Trigger.WindowLabel)
			}
		}
	}
	if !hasHardStop {
		t.Error("expected at least one HardStop step")
	}
}

func TestReplayLegacyEntryYieldsUnknownHarness(t *testing.T) {
	steps, err := Replay([]string{filepath.Join("testdata", "baseline.jsonl")}, config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	last := steps[len(steps)-1].State
	for id, sess := range last.Sessions {
		if sess.Harness != state.HarnessUnknown && sess.Harness != "" {
			t.Fatalf("session %s harness = %q, want unknown", id, sess.Harness)
		}
	}
}

func TestReplayStateIsDeepCopied(t *testing.T) {
	steps, err := Replay([]string{filepath.Join("testdata", "baseline.jsonl")}, config.Defaults())
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(steps) < 2 {
		t.Fatal("need at least 2 steps")
	}
	steps[0].State.SchemaVersion = 999
	if steps[1].State.SchemaVersion == 999 {
		t.Error("state not deep-copied between steps")
	}
}
