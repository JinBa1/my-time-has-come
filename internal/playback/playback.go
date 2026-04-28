package playback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Auto-detect: if a single arg is a directory, load all *.jsonl from it
	if len(files) == 1 {
		info, err := os.Stat(files[0])
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", files[0], err)
		}
		if info.IsDir() {
			entries, err := os.ReadDir(files[0])
			if err != nil {
				return nil, fmt.Errorf("readdir %q: %w", files[0], err)
			}
			var jsonlFiles []string
			for _, e := range entries {
				if filepath.Ext(e.Name()) == ".jsonl" {
					jsonlFiles = append(jsonlFiles, filepath.Join(files[0], e.Name()))
				}
			}
			files = jsonlFiles
		}
	}

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
			step.State = deepCopyState(s)
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
			step.State = deepCopyState(s)
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

func deepCopyState(s *state.State) state.State {
	data, _ := json.Marshal(s)
	var dup state.State
	json.Unmarshal(data, &dup)
	return dup
}
