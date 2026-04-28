package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/JinBa1/mthc/internal/adapter"
	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/handoff"
	"github.com/JinBa1/mthc/internal/policy"
	"github.com/JinBa1/mthc/internal/prompt"
	"github.com/JinBa1/mthc/internal/state"
)

// HookEvent is the shim-agnostic representation of a hook invocation.
type HookEvent struct {
	HookEventName string
	SessionID     string
}

// HookResponse is the JSON shape emitted to Claude Code stdout.
type HookResponse struct {
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	HookSpecificOutput       any    `json:"hookSpecificOutput,omitempty"`
}

// SideEffect captures an intended but not yet applied action.
type SideEffect struct {
	Type      string // SideEffectHandoffWrite, SideEffectSoftInject, etc.
	SessionID string
	Path      string // intended file path
	Content   string // rendered content (for assertion)
}

const (
	SideEffectHandoffWrite = "handoff_write"
	SideEffectSoftInject   = "soft_inject"
	SideEffectHardDeny     = "hard_deny"
)

// StatuslineResult holds the outcome of processing a statusline tick.
type StatuslineResult struct {
	Decision    policy.Decision
	Sessions    map[string]*state.Session
	SideEffects []SideEffect
}

// HookResult holds the outcome of processing a hook event.
type HookResult struct {
	Response    HookResponse
	Decision    policy.Decision
	SideEffects []SideEffect
}

// ResolveHandoffPath picks the first non-colliding path given already-used paths.
// Pure function, no filesystem access. Increments a numeric suffix on collision.
// Reserved for future multi-session replay collision tracking. Not used by live
// shims (which use os.Stat + deterministic fallback) or v0 single-session replay.
func ResolveHandoffPath(intended string, existing []string) string {
	existingSet := make(map[string]bool, len(existing))
	for _, p := range existing {
		existingSet[p] = true
	}
	if !existingSet[intended] {
		return intended
	}
	ext := filepath.Ext(intended)
	base := strings.TrimSuffix(intended, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if !existingSet[candidate] {
			return candidate
		}
	}
}

func ProcessStatusline(s *state.State, cfg *config.Config, p adapter.StatuslinePayload, now time.Time) StatuslineResult {
	s.UpdatedAt = now

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

	pruneStaleSessions(s, cfg, now)

	sessions, decision := policy.Decide(s, cfg, now)

	result := StatuslineResult{
		Decision: decision,
		Sessions: sessions,
	}

	if decision == policy.HardStop {
		resetsAt := s.AccountWindow.FiveHour.ResetsAt
		s.PolicyState.HardTriggeredForResetsAt = &resetsAt
		for id := range sessions {
			primaryPath := renderHandoffPath(cfg, s, id, resetsAt)
			handoffContent := handoff.Render(handoff.Params{
				SessionID:      id,
				ModelID:        getModelID(s, id),
				ISO8601:        now.Format(time.RFC3339Nano),
				UsedPercentage: s.AccountWindow.FiveHour.UsedPercentage,
				ResetsAtHuman:  time.Unix(resetsAt, 0).UTC().Format(time.RFC3339),
				CWD:            getCWD(s, id, ""),
				HandoffPath:    primaryPath,
				TranscriptPath: getTranscriptPath(s, id),
			})
			result.SideEffects = append(result.SideEffects, SideEffect{
				Type:      SideEffectHandoffWrite,
				SessionID: id,
				Path:      primaryPath,
				Content:   handoffContent,
			})
			// C4 fix: set HandoffPaths inside core so replay state
			// matches live. Live caller may overwrite with
			// collision-resolved path; replay uses primary path.
			s.PolicyState.HandoffPaths[id] = primaryPath
		}
	}

	return result
}

func ProcessHook(s *state.State, cfg *config.Config, event HookEvent, now time.Time, processCWD string) HookResult {
	s.UpdatedAt = now

	if event.SessionID != "" {
		sess, exists := s.Sessions[event.SessionID]
		if !exists {
			sess = &state.Session{}
			s.Sessions[event.SessionID] = sess
		}
		sess.LastSeenAt = now
	}

	sessions, decision := policy.Decide(s, cfg, now)

	var result HookResult
	result.Decision = decision

	switch event.HookEventName {
	case "PostToolBatch":
		result = handlePostToolBatch(s, cfg, event, sessions, decision)
	case "PreToolUse":
		result = handlePreToolUse(s, cfg, event, now, processCWD)
	}

	return result
}

func handlePostToolBatch(s *state.State, cfg *config.Config, event HookEvent, sessions map[string]*state.Session, decision policy.Decision) HookResult {
	if decision == policy.SoftInject && sessions[event.SessionID] != nil {
		resetsAt := s.AccountWindow.FiveHour.ResetsAt
		p := prompt.Params{
			UsedPercentage: s.AccountWindow.FiveHour.UsedPercentage,
			ResetsAtHuman:  time.Unix(resetsAt, 0).UTC().Format(time.RFC3339),
			ResetsAtUnix:   resetsAt,
			SessionID:      event.SessionID,
			HandoffPath:    renderHandoffPath(cfg, s, event.SessionID, resetsAt),
			CWD:            getCWD(s, event.SessionID, ""),
			ModelID:        getModelID(s, event.SessionID),
		}
		text, err := prompt.Render(p, cfg.Handoff.SoftPromptPath)
		if err != nil {
			return HookResult{Decision: policy.NoAction} // fail-open
		}
		sessions[event.SessionID].SoftInjectedForResetsAt = &resetsAt

		return HookResult{
			Decision: policy.SoftInject,
			Response: HookResponse{
				HookSpecificOutput: map[string]any{
					"hookEventName":     "PostToolBatch",
					"additionalContext": text,
				},
			},
			SideEffects: []SideEffect{{
				Type:      SideEffectSoftInject,
				SessionID: event.SessionID,
				Content:   text,
			}},
		}
	}
	return HookResult{Decision: policy.NoAction}
}

func handlePreToolUse(s *state.State, cfg *config.Config, event HookEvent, now time.Time, processCWD string) HookResult {
	resetsAt := s.AccountWindow.FiveHour.ResetsAt
	isArmed := s.PolicyState.HardTriggeredForResetsAt != nil &&
		*s.PolicyState.HardTriggeredForResetsAt == resetsAt

	if !isArmed || !cfg.HardStop.EnablePreToolDeny {
		return HookResult{Decision: policy.NoAction}
	}

	var effects []SideEffect
	if event.SessionID != "" {
		if _, exists := s.PolicyState.HandoffPaths[event.SessionID]; !exists {
			primaryPath := renderHandoffPath(cfg, s, event.SessionID, resetsAt)
			handoffContent := handoff.Render(handoff.Params{
				SessionID:      event.SessionID,
				ModelID:        getModelID(s, event.SessionID),
				ISO8601:        now.Format(time.RFC3339Nano),
				UsedPercentage: s.AccountWindow.FiveHour.UsedPercentage,
				ResetsAtHuman:  time.Unix(resetsAt, 0).UTC().Format(time.RFC3339),
				CWD:            getCWD(s, event.SessionID, processCWD),
				HandoffPath:    primaryPath,
				TranscriptPath: getTranscriptPath(s, event.SessionID),
			})
			effects = append(effects, SideEffect{
				Type:      SideEffectHandoffWrite,
				SessionID: event.SessionID,
				Path:      primaryPath,
				Content:   handoffContent,
			})
			// C4 fix: set HandoffPaths inside core so replay state
			// matches live. Live caller may overwrite with
			// collision-resolved path later.
			s.PolicyState.HandoffPaths[event.SessionID] = primaryPath
		}
	}

	effects = append(effects, SideEffect{
		Type:      SideEffectHardDeny,
		SessionID: event.SessionID,
	})

	return HookResult{
		Decision: policy.HardStop,
		Response: HookResponse{
			PermissionDecision:       "deny",
			PermissionDecisionReason: "MTHC: local quota policy active, usage window near exhaustion. Tool use blocked.",
		},
		SideEffects: effects,
	}
}

func pruneStaleSessions(s *state.State, cfg *config.Config, now time.Time) {
	for id, sess := range s.Sessions {
		if !sess.IsActive(now, cfg.Statusline.RefreshIntervalSeconds) {
			delete(s.Sessions, id)
		}
	}
}

func getCWD(s *state.State, sessionID string, fallback string) string {
	if sess := s.Sessions[sessionID]; sess != nil && sess.CWD != "" {
		return sess.CWD
	}
	return fallback
}

func getModelID(s *state.State, sessionID string) string {
	if sess := s.Sessions[sessionID]; sess != nil {
		return sess.ModelID
	}
	return ""
}

func getTranscriptPath(s *state.State, sessionID string) string {
	if sess := s.Sessions[sessionID]; sess != nil {
		return sess.TranscriptPath
	}
	return ""
}

func renderHandoffPath(cfg *config.Config, s *state.State, sessionID string, resetsAt int64) string {
	tmpl := cfg.Handoff.PathTemplate
	windowStart := resetsAt - 5*3600
	cwd := getCWD(s, sessionID, "")
	p := tmpl
	p = strings.ReplaceAll(p, "{cwd}", cwd)
	p = strings.ReplaceAll(p, "{session_id}", sessionID)
	p = strings.ReplaceAll(p, "{resets_at_unix}", fmt.Sprintf("%d", resetsAt))
	p = strings.ReplaceAll(p, "{window_start_ts}", fmt.Sprintf("%d", windowStart))
	p = strings.ReplaceAll(p, "{model_id}", getModelID(s, sessionID))
	return p
}
