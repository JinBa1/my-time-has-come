package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/adapter"
	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/core"
	"github.com/JinBa1/my-time-has-come/internal/harness"
	"github.com/JinBa1/my-time-has-come/internal/recording"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

func runStatuslineShim() error {
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil || len(stdinData) == 0 {
		fmt.Print("{}")
		return nil
	}

	p, parseErr := adapter.ParseStatusline(bytes.NewReader(stdinData))

	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	cfg, cfgErr := config.Resolve()

	defer execChainedStatusline(cfg, stdinData)

	if parseErr != nil || cfgErr != nil {
		return nil
	}

	var recEntry *recording.Entry
	_ = state.Update(statePath, func(s *state.State) error {
		now := time.Now().UTC()

		envH := harness.DetectEnv(os.Environ())
		payloadH := harness.DetectPayload(harness.PayloadHints{ClaudeStatuslineShape: p.HasRateLimits()})
		obsHarness := envH
		if obsHarness == harness.Unknown {
			obsHarness = payloadH
		}
		result := core.ProcessStatusline(s, cfg, p.Observations(obsHarness, now), core.SessionMeta{
			SessionID:      p.SessionID,
			TranscriptPath: p.TranscriptPath,
			ModelID:        p.ModelID,
			CWD:            p.CWD,
			EnvHarness:     envH,
			PayloadHarness: payloadH,
		}, now)

		// Apply side effects: handoff writes
		for _, se := range result.SideEffects {
			if se.Type == core.SideEffectHandoffWrite {
				writeHandoffFromSideEffect(s, se, now, home)
			}
		}

		// Capture recording entry data inside lock only when recording is active
		if cfg.Recording.Enabled && cfg.Recording.ActiveWindow != "" {
			recEntry = &recording.Entry{
				V:              1,
				TS:             now,
				Type:           "statusline",
				SessionID:      p.SessionID,
				Payload:        json.RawMessage(stdinData), // C1 fix: raw bytes, not string
				Harness:        envH,                       // env-derived only; empty/unknown allowed
				HarnessPayload: payloadH,                   // payload-shape-derived
			}
		}

		return nil
	})

	// Record entry outside lock to minimize critical section
	if recEntry != nil {
		recording.Record(recording.Config{
			Enabled:      cfg.Recording.Enabled,
			Dir:          cfg.Recording.Dir,
			ActiveWindow: cfg.Recording.ActiveWindow,
		}, *recEntry)
	}

	return nil
}

// execChainedStatusline execs the user's prior statusline command captured
// at install time. Always called, even on mthc errors (fail-open).
func execChainedStatusline(cfg *config.Config, stdinData []byte) {
	if cfg != nil && cfg.Internal.ChainedStatusline != nil {
		cmdStr, _ := cfg.Internal.ChainedStatusline["command"].(string)
		if cmdStr != "" {
			cmd := exec.Command("sh", "-c", cmdStr)
			cmd.Stdin = bytes.NewReader(stdinData)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
			return
		}
	}
	fmt.Print("{}")
}

// writeHandoffFromSideEffect writes a handoff file from a core SideEffect.
// Uses the same collision-detection pattern as the original
// writeHandoffForSession in handoff_writer.go:
//   - os.Stat primary path → if exists, use deterministic fallback
//   - fallback: ~/.config/mthc/handoffs/handoff-{sessionID}-{windowID}-{resetsAt}.md
//
// Does NOT use ResolveHandoffPath — that is for replay's accumulator only.
func writeHandoffFromSideEffect(s *state.State, se core.SideEffect, now time.Time, home string) {
	targetPath := se.Path
	if _, err := os.Stat(se.Path); err == nil {
		// Collision: use deterministic fallback name
		fallbackDir := filepath.Join(home, ".config", "mthc", "handoffs")
		os.MkdirAll(fallbackDir, 0700)
		targetPath = filepath.Join(fallbackDir, fmt.Sprintf("handoff-%s-%s-%d.md", se.SessionID, se.WindowID, se.ResetsAt))
	} else {
		os.MkdirAll(filepath.Dir(targetPath), 0700)
	}
	if err := os.WriteFile(targetPath, []byte(se.Content), 0644); err != nil {
		return
	}
	// Overwrite core's primary path with collision-resolved path
	if s.PolicyState.HandoffPathsByWindow[se.WindowID] == nil {
		s.PolicyState.HandoffPathsByWindow[se.WindowID] = make(map[string]string)
	}
	s.PolicyState.HandoffPathsByWindow[se.WindowID][se.SessionID] = targetPath
	s.PolicyState.HandoffWrittenAtByWindow[se.WindowID] = now
}
