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
	if s.SchemaVersion != 3 {
		t.Errorf("schema_version: got %v, want 3", s.SchemaVersion)
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
	now := time.Now().UTC().Truncate(time.Millisecond)
	original := &State{
		SchemaVersion: 3,
		UpdatedAt:     now,
		Sessions: map[string]*Session{
			"sess-1": {
				PID:                  85330,
				CWD:                  "/home/jin/repos/foo",
				ModelID:              "claude-opus-4-7",
				LastSeenAt:           now,
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
	original.UpsertObservation(Observation{
		Source: SourceStatusline, Unit: UnitPercent, Value: 47.2,
		Window: WindowRef{ID: "five_hour", ResetsAt: 1745000000},
		Scope:  ScopeAccount, ObservedAt: now,
	})
	original.UpsertObservation(Observation{
		Source: SourceStatusline, Unit: UnitPercent, Value: 18.4,
		Window: WindowRef{ID: "seven_day", ResetsAt: 1745432000},
		Scope:  ScopeAccount, ObservedAt: now,
	})
	if err := original.Write(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if fh := loaded.Observation("five_hour", SourceStatusline); fh == nil || fh.Value != 47.2 {
		t.Errorf("five_hour used_percentage: got %+v, want 47.2", fh)
	}
	if loaded.Sessions["sess-1"].PID != 85330 {
		t.Errorf("pid: got %v, want 85330", loaded.Sessions["sess-1"].PID)
	}
	if sd := loaded.Observation("seven_day", SourceStatusline); sd == nil || sd.Value != 18.4 {
		t.Errorf("seven_day used_percentage: got %+v, want 18.4", sd)
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
	if s.SchemaVersion != 3 {
		t.Fatalf("schema_version = %d, want 3", s.SchemaVersion)
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
	// Seed with an initial session counter at 0.
	if err := Update(path, func(s *State) error {
		s.Sessions["counter"] = &Session{PID: 0, SoftInjectedByWindow: map[string]int64{}}
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
				s.Sessions["counter"].PID++
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
	if final.Sessions["counter"].PID != 20 {
		t.Errorf("concurrent updates lost: got %v, want 20", final.Sessions["counter"].PID)
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

func TestUpsertObservationCreatesSlot(t *testing.T) {
	s := newState()
	o := Observation{
		Source: SourceStatusline, Harness: "claude-code", Unit: UnitPercent,
		Value: 42, Window: WindowRef{ID: "five_hour", ResetsAt: 100},
		Scope: ScopeAccount, ObservedAt: time.Unix(50, 0).UTC(),
	}
	s.UpsertObservation(o)
	got := s.Observation("five_hour", SourceStatusline)
	if got == nil || got.Value != 42 || got.Unit != UnitPercent {
		t.Fatalf("slot not created correctly: %+v", got)
	}
}

func TestUpsertObservationMonotonicSameReset(t *testing.T) {
	s := newState()
	s.UpsertObservation(Observation{Source: SourceStatusline, Unit: UnitPercent, Value: 96, Window: WindowRef{ID: "five_hour", ResetsAt: 100}})
	s.UpsertObservation(Observation{Source: SourceStatusline, Unit: UnitPercent, Value: 90, Window: WindowRef{ID: "five_hour", ResetsAt: 100}})
	if got := s.Observation("five_hour", SourceStatusline).Value; got != 96 {
		t.Fatalf("dip overwrote max: got %v want 96", got)
	}
	// Upsert with same ResetsAt, different metadata: max Value kept, but metadata overwritten.
	s.UpsertObservation(Observation{
		Source: SourceStatusline, Unit: UnitPercent, Value: 80, Window: WindowRef{ID: "five_hour", ResetsAt: 100},
		Absent: true, Harness: "opencode", ObservedAt: time.Unix(999, 0).UTC(),
	})
	slot := s.Observation("five_hour", SourceStatusline)
	if slot.Value != 96 {
		t.Fatalf("value overwritten despite lower incoming: got %v want 96", slot.Value)
	}
	if slot.Absent != true {
		t.Fatalf("Absent not overwritten: got %v want true", slot.Absent)
	}
	if slot.Harness != "opencode" {
		t.Fatalf("Harness not overwritten: got %q want opencode", slot.Harness)
	}
	if slot.ObservedAt != time.Unix(999, 0).UTC() {
		t.Fatalf("ObservedAt not overwritten: got %v want %v", slot.ObservedAt, time.Unix(999, 0).UTC())
	}
}

func TestUpsertObservationRolloverReplaces(t *testing.T) {
	s := newState()
	s.UpsertObservation(Observation{Source: SourceStatusline, Unit: UnitPercent, Value: 96, Window: WindowRef{ID: "five_hour", ResetsAt: 100}})
	s.UpsertObservation(Observation{Source: SourceStatusline, Unit: UnitPercent, Value: 10, Window: WindowRef{ID: "five_hour", ResetsAt: 200}})
	got := s.Observation("five_hour", SourceStatusline)
	if got.Value != 10 || got.Window.ResetsAt != 200 {
		t.Fatalf("rollover did not replace: %+v", got)
	}
}

func TestUpsertObservationSlotsIndependentPerSource(t *testing.T) {
	s := newState()
	s.UpsertObservation(Observation{Source: SourceStatusline, Unit: UnitPercent, Value: 50, Window: WindowRef{ID: "five_hour", ResetsAt: 100}})
	s.UpsertObservation(Observation{Source: "ccusage", Unit: UnitTokens, Value: 1e6, Window: WindowRef{ID: "five_hour", ResetsAt: 90}})
	if s.Observation("five_hour", SourceStatusline).Value != 50 {
		t.Fatal("statusline slot disturbed by other source")
	}
	if s.Observation("five_hour", "ccusage").Unit != UnitTokens {
		t.Fatal("second source slot missing")
	}
}

func TestNormalizeDropsKeyMismatchedObservations(t *testing.T) {
	s := newState()
	s.Observations["five_hour"] = map[string]*Observation{
		"statusline": {Source: "statusline", Window: WindowRef{ID: "seven_day", ResetsAt: 1}},
	}
	s.normalize(nil, nil)
	if s.Observation("five_hour", "statusline") != nil {
		t.Fatal("invariant-violating entry survived normalize")
	}
}

func TestNormalizeDropsSourceMismatchedObservations(t *testing.T) {
	s := newState()
	s.Observations["five_hour"] = map[string]*Observation{
		"statusline": {Source: "ccusage", Window: WindowRef{ID: "five_hour", ResetsAt: 1}},
	}
	s.normalize(nil, nil)
	if s.Observation("five_hour", "statusline") != nil {
		t.Fatal("source-mismatched entry survived normalize")
	}
}

func TestObservationsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := newState()
	s.UpsertObservation(Observation{Source: SourceStatusline, Unit: UnitPercent, Value: 77, Window: WindowRef{ID: "seven_day", ResetsAt: 555}})
	if err := s.Write(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Observation("seven_day", SourceStatusline); got == nil || got.Value != 77 {
		t.Fatalf("round trip lost observation: %+v", got)
	}
}

func TestLoadMigratesV2AccountWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	v2 := `{
  "schema_version": 2,
  "updated_at": "2026-06-01T00:00:00Z",
  "account_window": {
    "five_hour": {"used_percentage": 96, "resets_at": 100, "source": "statusline", "last_observed_at": "2026-06-01T00:00:00Z", "absent": false},
    "seven_day": {"used_percentage": 50, "resets_at": 200, "source": "statusline", "last_observed_at": "2026-06-01T00:00:00Z", "absent": true}
  },
  "sessions": {},
  "policy_state": {"hard_triggered_by_window": {"five_hour": 100}, "handoff_written_at_by_window": {}, "handoff_paths_by_window": {}},
  "transcript_cursors": {}
}`
	if err := os.WriteFile(path, []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.SchemaVersion != 3 {
		t.Fatalf("schema version: %d", s.SchemaVersion)
	}
	fh := s.Observation("five_hour", SourceStatusline)
	if fh == nil || fh.Value != 96 || fh.Window.ResetsAt != 100 || fh.Unit != UnitPercent || fh.Harness != HarnessUnknown || fh.Absent {
		t.Fatalf("five_hour migration: %+v", fh)
	}
	sd := s.Observation("seven_day", SourceStatusline)
	if sd == nil || !sd.Absent || sd.Window.ResetsAt != 200 {
		t.Fatalf("seven_day migration (Absent must carry over): %+v", sd)
	}
	if s.PolicyState.HardTriggeredByWindow["five_hour"] != 100 {
		t.Fatal("armed gate lost in migration")
	}
}

func TestNewStateIsSchemaV3(t *testing.T) {
	if newState().SchemaVersion != 3 {
		t.Fatal("newState must be schema 3")
	}
}

func TestLoadSkipsNeverObservedLegacyWindows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	v2 := `{"schema_version": 2, "account_window": {"five_hour": {"used_percentage": 0, "resets_at": 0}, "seven_day": {"used_percentage": 0, "resets_at": 0}}, "sessions": {}, "policy_state": {}, "transcript_cursors": {}}`
	if err := os.WriteFile(path, []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Observations) != 0 {
		t.Fatalf("never-observed windows must not migrate: %+v", s.Observations)
	}
}

func TestLoadDoesNotRemigrateV3WithResidualAccountWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	v3 := `{"schema_version": 3, "account_window": {"five_hour": {"used_percentage": 96, "resets_at": 100, "last_observed_at": "2026-06-01T00:00:00Z"}}, "observations": {}, "sessions": {}, "policy_state": {}, "transcript_cursors": {}}`
	if err := os.WriteFile(path, []byte(v3), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Observations) != 0 {
		t.Fatalf("v3 file re-migrated stale account_window: %+v", s.Observations)
	}
}
