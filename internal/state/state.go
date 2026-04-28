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
	AccountWindow     AccountWindow           `json:"account_window"`
	Sessions          map[string]*Session     `json:"sessions"`
	PolicyState       PolicyState             `json:"policy_state"`
	TranscriptCursors map[string]*CursorEntry `json:"transcript_cursors"`
}

type AccountWindow struct {
	FiveHour WindowObservation `json:"five_hour"`
	SevenDay WindowObservation `json:"seven_day"`
}

type WindowObservation struct {
	UsedPercentage float64   `json:"used_percentage"`
	ResetsAt       int64     `json:"resets_at"`
	Source         string    `json:"source"`
	LastObservedAt time.Time `json:"last_observed_at"`
	Absent         bool      `json:"absent"`
}

type Session struct {
	PID                     int       `json:"pid"`
	CWD                     string    `json:"cwd"`
	TranscriptPath          string    `json:"transcript_path"`
	ModelID                 string    `json:"model_id"`
	LastSeenAt              time.Time `json:"last_seen_at"`
	SoftInjectedForResetsAt *int64    `json:"soft_injected_for_resets_at"`
	TurnTokens              []any     `json:"turn_tokens"`
}

type PolicyState struct {
	HardTriggeredForResetsAt *int64            `json:"hard_triggered_for_resets_at"`
	HandoffWrittenAt         *time.Time        `json:"handoff_written_at"`
	HandoffPaths             map[string]string `json:"handoff_paths"`
	DismissedAt              *time.Time        `json:"dismissed_at"`
}

type CursorEntry struct {
	Offset  int64 `json:"offset"`
	MtimeNS int64 `json:"mtime_ns"`
}

func newState() *State {
	return &State{
		SchemaVersion:     1,
		Sessions:          make(map[string]*Session),
		PolicyState:       PolicyState{HandoffPaths: make(map[string]string)},
		TranscriptCursors: make(map[string]*CursorEntry),
	}
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
	s := newState()
	if err := json.Unmarshal(data, s); err != nil {
		return newState(), nil
	}
	if s.Sessions == nil {
		s.Sessions = make(map[string]*Session)
	}
	if s.PolicyState.HandoffPaths == nil {
		s.PolicyState.HandoffPaths = make(map[string]string)
	}
	if s.TranscriptCursors == nil {
		s.TranscriptCursors = make(map[string]*CursorEntry)
	}
	return s, nil
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

// MonotonicUpdate guards against stale observations overwriting fresher values.
func (w *WindowObservation) MonotonicUpdate(incoming WindowObservation) {
	if incoming.ResetsAt == w.ResetsAt {
		if incoming.UsedPercentage > w.UsedPercentage {
			w.UsedPercentage = incoming.UsedPercentage
		}
	} else if incoming.ResetsAt > w.ResetsAt || w.ResetsAt == 0 {
		*w = incoming
	}
	w.LastObservedAt = incoming.LastObservedAt
	w.Source = incoming.Source
	w.Absent = incoming.Absent
}

// IsActive returns true if the session was seen within 2x refreshInterval.
func (s *Session) IsActive(now time.Time, refreshIntervalSeconds int) bool {
	staleness := time.Duration(refreshIntervalSeconds) * 2 * time.Second
	return now.Sub(s.LastSeenAt) <= staleness
}
