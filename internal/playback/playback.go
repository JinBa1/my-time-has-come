package playback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/JinBa1/mthc/internal/adapter"
	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/core"
	"github.com/JinBa1/mthc/internal/policy"
	"github.com/JinBa1/mthc/internal/recording"
	"github.com/JinBa1/mthc/internal/state"
)

// Step is a full snapshot of pipeline state at one point in time.
type Step struct {
	TS          time.Time
	EventType   string // "statusline" | "hook"
	EventName   string // for hooks: "PreToolUse", "PostToolBatch"
	SessionID   string
	State       state.State // deep-copied snapshot
	Decision    policy.Decision
	SideEffects []core.SideEffect
	Response    core.HookResponse // raw response for hook events
}

// Replay reads recording files, reconstructs state at each timestamp,
// and returns the sequence of pipeline steps.
// Does NOT read live state.json or write to disk.
func Replay(files []string, cfg *config.Config) ([]Step, error) {
	// Expand directories into their *.jsonl contents
	var expanded []string
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", f, err)
		}
		if info.IsDir() {
			entries, err := os.ReadDir(f)
			if err != nil {
				return nil, fmt.Errorf("readdir %q: %w", f, err)
			}
			for _, e := range entries {
				if filepath.Ext(e.Name()) == ".jsonl" {
					expanded = append(expanded, filepath.Join(f, e.Name()))
				}
			}
		} else {
			expanded = append(expanded, f)
		}
	}
	sort.Strings(expanded)
	files = expanded

	entries, err := recording.LoadFiles(files)
	if err != nil {
		return nil, err
	}

	s := &state.State{
		SchemaVersion:     1,
		Sessions:          make(map[string]*state.Session),
		PolicyState:       state.PolicyState{HandoffPaths: make(map[string]string)},
		TranscriptCursors: make(map[string]*state.CursorEntry),
	}

	var steps []Step
	for _, entry := range entries {
		switch entry.Type {
		case "statusline":
			payload, err := parseStatuslinePayload(entry.Payload)
			if err != nil {
				return nil, fmt.Errorf("parse statusline payload at %v: %w", entry.TS, err)
			}
			result := core.ProcessStatusline(s, cfg, payload, entry.TS)
			step := Step{
				TS:          entry.TS,
				EventType:   "statusline",
				SessionID:   entry.SessionID,
				Decision:    result.Decision,
				SideEffects: result.SideEffects,
			}
			cp, err := deepCopyState(s)
			if err != nil {
				return nil, fmt.Errorf("deep copy state at %v: %w", entry.TS, err)
			}
			step.State = cp
			steps = append(steps, step)

		case "hook":
			result := core.ProcessHook(s, cfg, core.HookEvent{
				HookEventName: entry.Event,
				SessionID:     entry.SessionID,
			}, entry.TS, "")
			step := Step{
				TS:          entry.TS,
				EventType:   "hook",
				EventName:   entry.Event,
				SessionID:   entry.SessionID,
				Decision:    result.Decision,
				SideEffects: result.SideEffects,
				Response:    result.Response,
			}
			cp, err := deepCopyState(s)
			if err != nil {
				return nil, fmt.Errorf("deep copy state at %v: %w", entry.TS, err)
			}
			step.State = cp
			steps = append(steps, step)
		}
	}

	return steps, nil
}

// parseStatuslinePayload converts the raw payload from a recording entry
// into an adapter.StatuslinePayload. The payload was stored via
// json.RawMessage(stdinData) (C1 fix), so it's already valid JSON bytes.
func parseStatuslinePayload(raw any) (adapter.StatuslinePayload, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return adapter.StatuslinePayload{}, err
	}
	return adapter.ParseStatusline(bytes.NewReader(data))
}

func deepCopyState(s *state.State) (state.State, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return state.State{}, fmt.Errorf("marshal state: %w", err)
	}
	var dup state.State
	if err := json.Unmarshal(data, &dup); err != nil {
		return state.State{}, fmt.Errorf("unmarshal state: %w", err)
	}
	return dup, nil
}
