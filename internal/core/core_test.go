package core

import (
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
