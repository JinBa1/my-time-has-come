package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type State struct {
	SchemaVersion     int                     `json:"schema_version"`
	UpdatedAt         time.Time               `json:"updated_at"`
	Sessions          map[string]*Session     `json:"sessions"`
	PolicyState       PolicyState             `json:"policy_state"`
	TranscriptCursors map[string]*CursorEntry `json:"transcript_cursors"`
	// Observations is keyed window ID → source → observation. Invariant:
	// outer key == obs.Window.ID and inner key == obs.Source; normalize
	// drops violating entries.
	Observations map[string]map[string]*Observation `json:"observations,omitempty"`
}

type Session struct {
	PID                  int              `json:"pid"`
	CWD                  string           `json:"cwd"`
	TranscriptPath       string           `json:"transcript_path"`
	ModelID              string           `json:"model_id"`
	LastSeenAt           time.Time        `json:"last_seen_at"`
	Harness              string           `json:"harness,omitempty"`
	SoftInjectedByWindow map[string]int64 `json:"soft_injected_by_window"`
}

type PolicyState struct {
	HardTriggeredByWindow    map[string]int64             `json:"hard_triggered_by_window"`
	HandoffWrittenAtByWindow map[string]time.Time         `json:"handoff_written_at_by_window"`
	HandoffPathsByWindow     map[string]map[string]string `json:"handoff_paths_by_window"`
	DismissedAt              *time.Time                   `json:"dismissed_at"`
}

type CursorEntry struct {
	Offset  int64 `json:"offset"`
	MtimeNS int64 `json:"mtime_ns"`
}

// Unit, source, scope, and harness identifiers for observations.
const (
	UnitPercent = "percent"
	UnitTokens  = "tokens"
	UnitUSD     = "usd"
	// "requests" is reserved, not implemented.

	SourceStatusline = "statusline"

	ScopeAccount = "account"

	HarnessUnknown = "unknown"
)

// Observation is one usage measurement from one source for one window.
type Observation struct {
	Source     string    `json:"source"`
	Harness    string    `json:"harness"`
	Unit       string    `json:"unit"`
	Value      float64   `json:"value"`
	Window     WindowRef `json:"window"`
	Scope      string    `json:"scope"`
	ObservedAt time.Time `json:"observed_at"`
	Absent     bool      `json:"absent"`
}

type WindowRef struct {
	ID       string `json:"id"`
	ResetsAt int64  `json:"resets_at"`
}

// MonotonicUpdate guards a single (window, source) slot against stale data.
// Equal ResetsAt: keeps the max Value; always overwrites ObservedAt, Harness,
// Unit, Scope, and Absent — even when the incoming Value is lower, so
// staleness detection sees the latest source contact. Newer or zero-existing
// ResetsAt: replaces wholesale.
func (w *Observation) MonotonicUpdate(in Observation) {
	if in.Window.ResetsAt == w.Window.ResetsAt {
		if in.Value > w.Value {
			w.Value = in.Value
		}
		w.ObservedAt = in.ObservedAt
		w.Harness = in.Harness
		w.Unit = in.Unit
		w.Scope = in.Scope
		w.Absent = in.Absent
	} else if in.Window.ResetsAt > w.Window.ResetsAt || w.Window.ResetsAt == 0 {
		*w = in
	}
}

func newState() *State {
	return &State{
		SchemaVersion: 3,
		Sessions:      make(map[string]*Session),
		PolicyState: PolicyState{
			HardTriggeredByWindow:    make(map[string]int64),
			HandoffWrittenAtByWindow: make(map[string]time.Time),
			HandoffPathsByWindow:     make(map[string]map[string]string),
		},
		TranscriptCursors: make(map[string]*CursorEntry),
		Observations:      make(map[string]map[string]*Observation),
	}
}

type legacyPolicyState struct {
	HandoffPaths map[string]string `json:"handoff_paths"`
}

type legacyWindowObs struct {
	UsedPercentage float64   `json:"used_percentage"`
	ResetsAt       int64     `json:"resets_at"`
	LastObservedAt time.Time `json:"last_observed_at"`
	Absent         bool      `json:"absent"`
}

type legacyStateFile struct {
	PolicyState   legacyPolicyState `json:"policy_state"`
	AccountWindow *struct {
		FiveHour legacyWindowObs `json:"five_hour"`
		SevenDay legacyWindowObs `json:"seven_day"`
	} `json:"account_window"`
}

// Load reads state from disk without locking. For read-only access.
// For read-modify-write, use Update instead.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newState(), nil
		}
		return nil, err
	}
	var legacy legacyStateFile
	_ = json.Unmarshal(data, &legacy)

	s := newState()
	if err := json.Unmarshal(data, s); err != nil {
		corruptPath := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		_ = os.Rename(path, corruptPath)
		fmt.Fprintf(os.Stderr, "mthc: state.json corrupt, preserved at %s\n", corruptPath)
		return nil, fmt.Errorf("unmarshal state %q: %w", path, err)
	}
	s.normalize(legacy.PolicyState.HandoffPaths, legacy.AccountWindow)
	return s, nil
}

func (s *State) normalize(legacyHandoffPaths map[string]string, legacyAW *struct {
	FiveHour legacyWindowObs `json:"five_hour"`
	SevenDay legacyWindowObs `json:"seven_day"`
}) {
	// Schema v2 migration is intentionally local to known v0/v1/v2 state files.
	// Future schema versions should add explicit migration branches instead of
	// broadening this ratchet.
	originalVersion := s.SchemaVersion
	if s.SchemaVersion == 0 || s.SchemaVersion == 1 || s.SchemaVersion == 2 {
		s.SchemaVersion = 3
	}
	if s.Sessions == nil {
		s.Sessions = make(map[string]*Session)
	}
	for id, sess := range s.Sessions {
		if sess == nil {
			s.Sessions[id] = &Session{SoftInjectedByWindow: make(map[string]int64)}
			continue
		}
		if sess.SoftInjectedByWindow == nil {
			sess.SoftInjectedByWindow = make(map[string]int64)
		}
	}
	if s.PolicyState.HardTriggeredByWindow == nil {
		s.PolicyState.HardTriggeredByWindow = make(map[string]int64)
	}
	if s.PolicyState.HandoffWrittenAtByWindow == nil {
		s.PolicyState.HandoffWrittenAtByWindow = make(map[string]time.Time)
	}
	if s.PolicyState.HandoffPathsByWindow == nil {
		s.PolicyState.HandoffPathsByWindow = make(map[string]map[string]string)
	}
	if len(legacyHandoffPaths) > 0 && len(s.PolicyState.HandoffPathsByWindow) == 0 {
		s.PolicyState.HandoffPathsByWindow["five_hour"] = legacyHandoffPaths
	}
	if s.TranscriptCursors == nil {
		s.TranscriptCursors = make(map[string]*CursorEntry)
	}
	if s.Observations == nil {
		s.Observations = make(map[string]map[string]*Observation)
	}
	for windowID, bySource := range s.Observations {
		for source, o := range bySource {
			if o == nil || o.Window.ID != windowID || o.Source != source {
				delete(bySource, source)
			}
		}
		if len(bySource) == 0 {
			delete(s.Observations, windowID)
		}
	}
	// Never re-migrate a v3 file: a residual account_window key must not inject stale data.
	if originalVersion < 3 && legacyAW != nil && len(s.Observations) == 0 {
		migrate := func(windowID string, w legacyWindowObs) {
			if w.ResetsAt == 0 && w.LastObservedAt.IsZero() {
				return // never observed; skip junk slot
			}
			s.Observations[windowID] = map[string]*Observation{
				SourceStatusline: {
					Source:     SourceStatusline,
					Harness:    HarnessUnknown,
					Unit:       UnitPercent,
					Value:      w.UsedPercentage,
					Window:     WindowRef{ID: windowID, ResetsAt: w.ResetsAt},
					Scope:      ScopeAccount,
					ObservedAt: w.LastObservedAt,
					Absent:     w.Absent,
				},
			}
		}
		migrate("five_hour", legacyAW.FiveHour)
		migrate("seven_day", legacyAW.SevenDay)
	}
}

// Update performs an atomic read-modify-write under an exclusive flock.
func Update(path string, fn func(*State) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	lockPath := path + ".lock"
	lockF, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer lockF.Close()

	if err := syscall.Flock(int(lockF.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(lockF.Fd()), syscall.LOCK_UN)

	s, err := Load(path)
	if err != nil {
		return fmt.Errorf("load under lock: %w", err)
	}
	if err := fn(s); err != nil {
		return err
	}
	return writeAtomic(path, s)
}

// Write is a convenience for writing state without reading first.
// It does NOT hold a lock — use Update for concurrent access.
func (s *State) Write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := uniqueTmp(path)
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeAtomic(path string, s *State) error {
	s.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := uniqueTmp(path)
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func uniqueTmp(path string) string {
	return fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
}

// Observation returns the slot for (windowID, source), or nil.
func (s *State) Observation(windowID, source string) *Observation {
	if s.Observations == nil {
		return nil
	}
	return s.Observations[windowID][source]
}

// UpsertObservation monotonically updates the (window, source) slot,
// creating maps as needed and upholding the key invariant.
func (s *State) UpsertObservation(o Observation) {
	if s.Observations == nil {
		s.Observations = make(map[string]map[string]*Observation)
	}
	if s.Observations[o.Window.ID] == nil {
		s.Observations[o.Window.ID] = make(map[string]*Observation)
	}
	slot := s.Observations[o.Window.ID][o.Source]
	if slot == nil {
		cp := o
		s.Observations[o.Window.ID][o.Source] = &cp
		return
	}
	slot.MonotonicUpdate(o)
}

// IsActive returns true if the session was seen within 2x refreshInterval.
func (s *Session) IsActive(now time.Time, refreshIntervalSeconds int) bool {
	staleness := time.Duration(refreshIntervalSeconds) * 2 * time.Second
	return now.Sub(s.LastSeenAt) <= staleness
}
