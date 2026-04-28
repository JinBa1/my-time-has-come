package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/JinBa1/mthc/internal/adapter"
	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/core"
	"github.com/JinBa1/mthc/internal/state"
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

	_ = state.Update(statePath, func(s *state.State) error {
		now := time.Now().UTC()

		result := core.ProcessStatusline(s, cfg, p, now)

		// Apply side effects: handoff writes
		for _, se := range result.SideEffects {
			if se.Type == core.SideEffectHandoffWrite {
				writeHandoffFromSideEffect(s, se, now, home)
			}
		}

		return nil
	})

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
//   - fallback: ~/.config/mthc/handoffs/handoff-{sessionID}-{resetsAt}.md
//
// Does NOT use ResolveHandoffPath — that is for replay's accumulator only.
func writeHandoffFromSideEffect(s *state.State, se core.SideEffect, now time.Time, home string) {
	targetPath := se.Path
	if _, err := os.Stat(se.Path); err == nil {
		// Collision: use deterministic fallback name
		fallbackDir := filepath.Join(home, ".config", "mthc", "handoffs")
		os.MkdirAll(fallbackDir, 0700)
		targetPath = filepath.Join(fallbackDir, fmt.Sprintf("handoff-%s-%d.md", se.SessionID, s.AccountWindow.FiveHour.ResetsAt))
	} else {
		os.MkdirAll(filepath.Dir(targetPath), 0700)
	}
	if err := os.WriteFile(targetPath, []byte(se.Content), 0644); err != nil {
		return
	}
	// Overwrite core's primary path with collision-resolved path
	s.PolicyState.HandoffPaths[se.SessionID] = targetPath
	nowCopy := now
	s.PolicyState.HandoffWrittenAt = &nowCopy
}
