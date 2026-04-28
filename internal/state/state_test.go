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
	if s.SchemaVersion != 1 {
		t.Errorf("schema_version: got %v, want 1", s.SchemaVersion)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	original := &State{
		SchemaVersion: 1,
		UpdatedAt:     time.Now().UTC().Truncate(time.Millisecond),
		AccountWindow: AccountWindow{
			FiveHour: WindowObservation{
				UsedPercentage: 47.2,
				ResetsAt:       1745000000,
				Source:         "statusline",
				LastObservedAt: time.Now().UTC().Truncate(time.Millisecond),
			},
		},
		Sessions: map[string]*Session{
			"sess-1": {
				PID:        85330,
				CWD:        "/home/jin/repos/foo",
				ModelID:    "claude-opus-4-7",
				LastSeenAt: time.Now().UTC().Truncate(time.Millisecond),
			},
		},
		PolicyState: PolicyState{
			HardTriggeredForResetsAt: nil,
			DismissedAt:              nil,
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
