package state

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadCreatesEmptyStateIfAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.SchemaVersion != 2 {
		t.Errorf("schema_version: got %v, want 2", s.SchemaVersion)
	}
	if s.PolicyState.HardTriggeredByWindow == nil {
		t.Fatal("HardTriggeredByWindow should be initialized")
	}
	if s.PolicyState.HandoffPathsByWindow == nil {
		t.Fatal("HandoffPathsByWindow should be initialized")
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	original := &State{
		SchemaVersion: 2,
		UpdatedAt:     time.Now().UTC().Truncate(time.Millisecond),
		AccountWindow: AccountWindow{
			FiveHour: WindowObservation{
				UsedPercentage: 47.2,
				ResetsAt:       1745000000,
				Source:         "statusline",
				LastObservedAt: time.Now().UTC().Truncate(time.Millisecond),
			},
			SevenDay: WindowObservation{
				UsedPercentage: 18.4,
				ResetsAt:       1745432000,
				Source:         "statusline",
				LastObservedAt: time.Now().UTC().Truncate(time.Millisecond),
			},
		},
		Sessions: map[string]*Session{
			"sess-1": {
				PID:                  85330,
				CWD:                  "/home/jin/repos/foo",
				ModelID:              "claude-opus-4-7",
				LastSeenAt:           time.Now().UTC().Truncate(time.Millisecond),
				SoftInjectedByWindow: map[string]int64{"five_hour": 1745000000},
			},
		},
		PolicyState: PolicyState{
			HardTriggeredByWindow: map[string]int64{"five_hour": 1745000000},
			HandoffPathsByWindow: map[string]map[string]string{
				"five_hour": {"sess-1": "/tmp/handoff.md"},
			},
		},
	}
	if err := original.Write(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccountWindow.FiveHour.UsedPercentage != 47.2 {
		t.Errorf("used_percentage: got %v, want 47.2", loaded.AccountWindow.FiveHour.UsedPercentage)
	}
	if loaded.Sessions["sess-1"].PID != 85330 {
		t.Errorf("pid: got %v, want 85330", loaded.Sessions["sess-1"].PID)
	}
	if loaded.AccountWindow.SevenDay.UsedPercentage != 18.4 {
		t.Errorf("seven_day used_percentage: got %v, want 18.4", loaded.AccountWindow.SevenDay.UsedPercentage)
	}
	if got := loaded.Sessions["sess-1"].SoftInjectedByWindow["five_hour"]; got != 1745000000 {
		t.Errorf("soft injection: got %v, want 1745000000", got)
	}
	if got := loaded.PolicyState.HardTriggeredByWindow["five_hour"]; got != 1745000000 {
		t.Errorf("hard trigger: got %v, want 1745000000", got)
	}
	if got := loaded.PolicyState.HandoffPathsByWindow["five_hour"]["sess-1"]; got != "/tmp/handoff.md" {
		t.Errorf("handoff path: got %q, want /tmp/handoff.md", got)
	}
}

func TestLoadMigratesV1IdempotenceStateToV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	v1 := []byte(`{
  "schema_version": 1,
  "account_window": {
    "five_hour": {"used_percentage": 96, "resets_at": 1745000000, "source": "statusline"}
  },
  "sessions": {
    "sess-1": {
      "last_seen_at": "2026-04-28T14:30:00Z",
      "soft_injected_for_resets_at": 1745000000
    }
  },
  "policy_state": {
    "hard_triggered_for_resets_at": 1745000000,
    "handoff_paths": {"sess-1": "/tmp/old.md"}
  }
}`)
	if err := os.WriteFile(path, v1, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", s.SchemaVersion)
	}
	if len(s.PolicyState.HardTriggeredByWindow) != 0 {
		t.Fatalf("old hard gate should be dropped on migration, got %+v", s.PolicyState.HardTriggeredByWindow)
	}
	if len(s.Sessions["sess-1"].SoftInjectedByWindow) != 0 {
		t.Fatalf("old soft injection should be dropped on migration, got %+v", s.Sessions["sess-1"].SoftInjectedByWindow)
	}
	if got := s.PolicyState.HandoffPathsByWindow["five_hour"]["sess-1"]; got != "/tmp/old.md" {
		t.Fatalf("old handoff path should migrate to five_hour, got %q", got)
	}
}

func TestWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := &State{SchemaVersion: 1}
	if err := s.Write(path); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(path + ".tmp*")
	if len(matches) > 0 {
		t.Errorf("tmpfile should not remain after atomic write, found: %v", matches)
	}
}

func TestUpdateHoldsLockAcrossRMW(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := Update(path, func(s *State) error {
		s.AccountWindow.FiveHour.UsedPercentage = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var errors atomic.Int64
	done := make(chan struct{})
	for range 20 {
		go func() {
			defer func() { done <- struct{}{} }()
			if err := Update(path, func(s *State) error {
				s.AccountWindow.FiveHour.UsedPercentage++
				return nil
			}); err != nil {
				errors.Add(1)
			}
		}()
	}
	for range 20 {
		<-done
	}

	final, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if final.AccountWindow.FiveHour.UsedPercentage != 20 {
		t.Errorf("concurrent updates lost: got %v, want 20", final.AccountWindow.FiveHour.UsedPercentage)
	}
	if errors.Load() > 0 {
		t.Errorf("%d goroutines reported errors", errors.Load())
	}
}

func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{{{{not json}}"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err == nil {
		t.Fatal("expected error for corrupt file, got nil")
	}
	if s != nil {
		t.Fatal("expected nil state for corrupt file")
	}
	if !strings.Contains(err.Error(), "unmarshal state") {
		t.Errorf("error should mention unmarshal: %v", err)
	}
	// Verify the corrupt backup file was created.
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) == 0 {
		t.Error("no .corrupt-* backup file found")
	}
}
