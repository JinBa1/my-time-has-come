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
	"github.com/JinBa1/mthc/internal/policy"
	"github.com/JinBa1/mthc/internal/state"
)

func runStatuslineShim() error {
	// Read stdin once (reuse for chained command)
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil || len(stdinData) == 0 {
		fmt.Print("{}")
		return nil
	}

	// Parse the statusline payload. On parse error, skip state update but
	// still exec the chained command (fail-open invariant).
	p, parseErr := adapter.ParseStatusline(bytes.NewReader(stdinData))

	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	cfg, cfgErr := config.Resolve()

	// Use defer to guarantee the chained statusline command is always exec'd,
	// regardless of any errors in parsing, config, or state update.
	// This preserves the "statusline-shim always execs the chained command"
	// invariant from ARCHITECTURE.md.
	defer execChainedStatusline(cfg, stdinData)

	if parseErr != nil || cfgErr != nil {
		return nil
	}

	_ = state.Update(statePath, func(s *state.State) error {
		now := time.Now().UTC()

		// Update state with observation
		s.AccountWindow.FiveHour.MonotonicUpdate(state.WindowObservation{
			UsedPercentage: p.FiveHourUsedPct,
			ResetsAt:       p.FiveHourResetsAt,
			Source:         "statusline",
			LastObservedAt: now,
			Absent:         p.RateLimitsAbsent,
		})
		s.AccountWindow.SevenDay.MonotonicUpdate(state.WindowObservation{
			UsedPercentage: p.SevenDayUsedPct,
			ResetsAt:       p.SevenDayResetsAt,
			Source:         "statusline",
			LastObservedAt: now,
		})

		// Upsert session
		if p.SessionID != "" {
			sess, exists := s.Sessions[p.SessionID]
			if !exists {
				sess = &state.Session{}
				s.Sessions[p.SessionID] = sess
			}
			sess.LastSeenAt = now
			sess.TranscriptPath = p.TranscriptPath
			sess.ModelID = p.ModelID
			if p.CWD != "" {
				sess.CWD = p.CWD
			}
		}

		// Prune stale sessions
		pruneStaleSessions(s, cfg, now)

		// Run policy
		sessions, decision := policy.Decide(s, cfg, now)

		// HardStop: eagerly write handoff for all active sessions
		if decision == policy.HardStop {
			resetsAt := s.AccountWindow.FiveHour.ResetsAt
			s.PolicyState.HardTriggeredForResetsAt = &resetsAt
			for id := range sessions {
				if _, exists := s.PolicyState.HandoffPaths[id]; !exists {
					writeHandoffForSession(s, cfg, id, resetsAt, now, home)
				}
			}
		}

		return nil
	})

	return nil
}

// execChainedStatusline execs the user's prior statusline command captured
// at install time. This must always be called, even on mthc errors, so that
// a broken mthc never breaks the user's prior statusline (fail-open).
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

func pruneStaleSessions(s *state.State, cfg *config.Config, now time.Time) {
	for id, sess := range s.Sessions {
		if !sess.IsActive(now, cfg.Statusline.RefreshIntervalSeconds) {
			delete(s.Sessions, id)
		}
	}
}
