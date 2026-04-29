package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/core"
	"github.com/JinBa1/mthc/internal/recording"
	"github.com/JinBa1/mthc/internal/state"
)

func runHookShim() error {
	var raw struct {
		HookEventName string `json:"hook_event_name"`
		SessionID     string `json:"session_id"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&raw); err != nil {
		fmt.Print("{}")
		return nil
	}

	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Print("{}")
		return nil
	}

	var resp core.HookResponse
	var recEntry *recording.Entry
	err = state.Update(statePath, func(s *state.State) error {
		now := time.Now().UTC()
		cwd, _ := os.Getwd()

		result := core.ProcessHook(s, cfg, core.HookEvent{
			HookEventName: raw.HookEventName,
			SessionID:     raw.SessionID,
		}, now, cwd)

		resp = result.Response

		// Apply side effects
		for _, se := range result.SideEffects {
			if se.Type == core.SideEffectHandoffWrite {
				writeHandoffFromSideEffect(s, se, now, home)
			}
		}

		// Capture recording entry data inside lock
		recEntry = &recording.Entry{
			V:         1,
			TS:        now,
			Type:      "hook",
			SessionID: raw.SessionID,
			Event:     raw.HookEventName,
		}

		return nil
	})
	if err != nil {
		fmt.Print("{}")
		return nil
	}

	// Record entry outside lock to minimize critical section
	if recEntry != nil && cfg.Recording.Enabled && cfg.Recording.ActiveWindow != "" {
		recording.Record(recording.Config{
			Enabled:      cfg.Recording.Enabled,
			Dir:          cfg.Recording.Dir,
			ActiveWindow: cfg.Recording.ActiveWindow,
		}, *recEntry)
	}

	out, _ := json.Marshal(resp)
	fmt.Print(string(out))
	return nil
}
