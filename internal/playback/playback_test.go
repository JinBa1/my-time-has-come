package playback

import (
	"path/filepath"
	"testing"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/policy"
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
		}
	}
	if !hasHardStop {
		t.Error("expected at least one HardStop step")
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
