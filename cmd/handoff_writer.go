package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/handoff"
	"github.com/JinBa1/mthc/internal/state"
)

// writeHandoffForSession writes the deterministic handoff file for a session.
// Uses collision guard: if a soft handoff exists at primary path, writes to fallback.
func writeHandoffForSession(s *state.State, cfg *config.Config, sessionID string, resetsAt int64, now time.Time, home string) {
	sess := s.Sessions[sessionID]
	if sess == nil {
		return
	}
	primaryPath := renderHandoffPath(cfg, s, sessionID, resetsAt)
	handoffContent := handoff.Render(handoff.Params{
		SessionID:      sessionID,
		ModelID:        sess.ModelID,
		ISO8601:        now.Format(time.RFC3339Nano),
		UsedPercentage: s.AccountWindow.FiveHour.UsedPercentage,
		ResetsAtHuman:  time.Unix(resetsAt, 0).UTC().Format(time.RFC3339),
		CWD:            sess.CWD,
		HandoffPath:    primaryPath,
		TranscriptPath: sess.TranscriptPath,
	})

	// Collision guard: if soft handoff exists at primary, write to fallback
	targetPath := primaryPath
	if _, err := os.Stat(primaryPath); err == nil {
		fallbackDir := filepath.Join(home, ".config", "mthc", "handoffs")
		os.MkdirAll(fallbackDir, 0700)
		targetPath = filepath.Join(fallbackDir, fmt.Sprintf("handoff-%s-%d.md", sessionID, resetsAt))
	} else {
		os.MkdirAll(filepath.Dir(primaryPath), 0700)
	}
	os.WriteFile(targetPath, []byte(handoffContent), 0644)
	s.PolicyState.HandoffPaths[sessionID] = targetPath
	nowCopy := now
	s.PolicyState.HandoffWrittenAt = &nowCopy
}

func renderHandoffPath(cfg *config.Config, s *state.State, sessionID string, resetsAt int64) string {
	tmpl := cfg.Handoff.PathTemplate
	windowStart := resetsAt - 5*3600
	cwd := getCWD(s, sessionID)
	p := tmpl
	p = strings.ReplaceAll(p, "{cwd}", cwd)
	p = strings.ReplaceAll(p, "{session_id}", sessionID)
	p = strings.ReplaceAll(p, "{resets_at_unix}", fmt.Sprintf("%d", resetsAt))
	p = strings.ReplaceAll(p, "{window_start_ts}", fmt.Sprintf("%d", windowStart))
	p = strings.ReplaceAll(p, "{model_id}", getModelID(s, sessionID))
	return p
}

func getCWD(s *state.State, sessionID string) string {
	if sess := s.Sessions[sessionID]; sess != nil && sess.CWD != "" {
		return sess.CWD
	}
	cwd, _ := os.Getwd()
	return cwd
}

func getModelID(s *state.State, sessionID string) string {
	if sess := s.Sessions[sessionID]; sess != nil {
		return sess.ModelID
	}
	return ""
}
