package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/adapter"
	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/handoff"
	"github.com/JinBa1/my-time-has-come/internal/policy"
	"github.com/JinBa1/my-time-has-come/internal/prompt"
	"github.com/JinBa1/my-time-has-come/internal/state"
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
	WindowID  string
	ResetsAt  int64
}

const (
	SideEffectHandoffWrite = "handoff_write"
	SideEffectSoftInject   = "soft_inject"
	SideEffectHardDeny     = "hard_deny"
)

const (
	windowFiveHour = policy.WindowFiveHour
	windowSevenDay = policy.WindowSevenDay
)

// StatuslineResult holds the outcome of processing a statusline tick.
type StatuslineResult struct {
	Decision    policy.Decision
	Trigger     policy.Trigger
	Sessions    map[string]*state.Session
	SideEffects []SideEffect
}

// HookResult holds the outcome of processing a hook event.
type HookResult struct {
	Response    HookResponse
	Decision    policy.Decision
	Trigger     policy.Trigger
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

func windowDurationSeconds(windowID string) int64 {
	switch windowID {
	case windowSevenDay:
		return 7 * 24 * 3600
	default:
		return 5 * 3600
	}
}

func windowParams(w state.WindowObservation) prompt.WindowParams {
	return prompt.WindowParams{
		UsedPercentage: w.UsedPercentage,
		ResetsAtHuman:  time.Unix(w.ResetsAt, 0).UTC().Format(time.RFC3339),
		ResetsAtUnix:   w.ResetsAt,
		Absent:         w.Absent || w.ResetsAt == 0,
	}
}

func updateWindowObservation(w *state.WindowObservation, present bool, pct float64, resetsAt int64, cfg *config.Config, now time.Time) {
	if present {
		w.MonotonicUpdate(state.WindowObservation{
			UsedPercentage: pct,
			ResetsAt:       resetsAt,
			Source:         "statusline",
			LastObservedAt: now,
			Absent:         false,
		})
		return
	}
	staleAfter := time.Duration(cfg.Statusline.RefreshIntervalSeconds) * 2 * time.Second
	if !w.LastObservedAt.IsZero() && now.Sub(w.LastObservedAt) <= staleAfter {
		return
	}
	markWindowAbsent(w, now)
}

func markWindowAbsent(w *state.WindowObservation, now time.Time) {
	w.Absent = true
	w.LastObservedAt = now
}

func ProcessStatusline(s *state.State, cfg *config.Config, p adapter.StatuslinePayload, now time.Time) StatuslineResult {
	ensureStateMaps(s)
	s.UpdatedAt = now

	if p.RateLimitsAbsent {
		markWindowAbsent(&s.AccountWindow.FiveHour, now)
		markWindowAbsent(&s.AccountWindow.SevenDay, now)
	} else {
		updateWindowObservation(&s.AccountWindow.FiveHour, p.FiveHourPresent, p.FiveHourUsedPct, p.FiveHourResetsAt, cfg, now)
		updateWindowObservation(&s.AccountWindow.SevenDay, p.SevenDayPresent, p.SevenDayUsedPct, p.SevenDayResetsAt, cfg, now)
	}

	if p.SessionID != "" {
		sess, exists := s.Sessions[p.SessionID]
		if !exists {
			sess = &state.Session{SoftInjectedByWindow: make(map[string]int64)}
			s.Sessions[p.SessionID] = sess
		}
		if sess.SoftInjectedByWindow == nil {
			sess.SoftInjectedByWindow = make(map[string]int64)
		}
		sess.LastSeenAt = now
		sess.TranscriptPath = p.TranscriptPath
		sess.ModelID = p.ModelID
		if p.CWD != "" {
			sess.CWD = p.CWD
		}
	}

	pruneStaleSessions(s, cfg, now)

	sessions, decisionResult := policy.Decide(s, cfg, now)

	result := StatuslineResult{
		Decision: decisionResult.Decision,
		Trigger:  decisionResult.Trigger,
		Sessions: sessions,
	}

	if decisionResult.Decision == policy.HardStop {
		trigger := decisionResult.Trigger
		resetsAt := trigger.ResetsAt
		windowID := trigger.WindowID
		s.PolicyState.HardTriggeredByWindow[windowID] = resetsAt
		ensureHandoffPathWindow(s, windowID)
		for id := range sessions {
			primaryPath := renderHandoffPath(cfg, s, id, trigger, "") // statusline has no process cwd; getCWD fallback covers it
			handoffContent := handoff.Render(handoff.Params{
				SessionID:      id,
				ModelID:        getModelID(s, id),
				ISO8601:        now.Format(time.RFC3339Nano),
				UsedPercentage: trigger.UsedPercentage,
				ResetsAtHuman:  time.Unix(resetsAt, 0).UTC().Format(time.RFC3339),
				WindowID:       windowID,
				WindowLabel:    trigger.WindowLabel,
				CWD:            getCWD(s, id, ""),
				HandoffPath:    primaryPath,
				TranscriptPath: getTranscriptPath(s, id),
			})
			result.SideEffects = append(result.SideEffects, SideEffect{
				Type:      SideEffectHandoffWrite,
				SessionID: id,
				Path:      primaryPath,
				Content:   handoffContent,
				WindowID:  windowID,
				ResetsAt:  resetsAt,
			})
			// C4 fix: set HandoffPaths inside core so replay state
			// matches live. Live caller may overwrite with
			// collision-resolved path; replay uses primary path.
			s.PolicyState.HandoffPathsByWindow[windowID][id] = primaryPath
		}
	}

	return result
}

func ProcessHook(s *state.State, cfg *config.Config, event HookEvent, now time.Time, processCWD string) HookResult {
	ensureStateMaps(s)
	s.UpdatedAt = now

	if event.SessionID != "" {
		sess, exists := s.Sessions[event.SessionID]
		if !exists {
			sess = &state.Session{SoftInjectedByWindow: make(map[string]int64)}
			s.Sessions[event.SessionID] = sess
		}
		if sess.SoftInjectedByWindow == nil {
			sess.SoftInjectedByWindow = make(map[string]int64)
		}
		sess.LastSeenAt = now
	}

	sessions, decisionResult := policy.Decide(s, cfg, now)

	switch event.HookEventName {
	case "PostToolBatch":
		return handlePostToolBatch(s, cfg, event, sessions, decisionResult, processCWD)
	case "PreToolUse":
		return handlePreToolUse(s, cfg, event, now, processCWD)
	}

	return HookResult{Decision: policy.NoAction}
}

func handlePostToolBatch(s *state.State, cfg *config.Config, event HookEvent, sessions map[string]*state.Session, decisionResult policy.Result, processCWD string) HookResult {
	if decisionResult.Decision == policy.SoftInject && sessions[event.SessionID] != nil {
		trigger := decisionResult.Trigger
		resetsAt := trigger.ResetsAt
		windowID := trigger.WindowID
		p := prompt.Params{
			UsedPercentage: trigger.UsedPercentage,
			ResetsAtHuman:  time.Unix(resetsAt, 0).UTC().Format(time.RFC3339),
			ResetsAtUnix:   resetsAt,
			WindowID:       windowID,
			WindowLabel:    trigger.WindowLabel,
			FiveHour:       windowParams(s.AccountWindow.FiveHour),
			SevenDay:       windowParams(s.AccountWindow.SevenDay),
			SessionID:      event.SessionID,
			HandoffPath:    renderHandoffPath(cfg, s, event.SessionID, trigger, processCWD),
			CWD:            getCWD(s, event.SessionID, processCWD),
			ModelID:        getModelID(s, event.SessionID),
		}
		text, err := prompt.Render(p, cfg.Handoff.SoftPromptPath)
		if err != nil {
			return HookResult{Decision: policy.NoAction} // fail-open
		}
		if sessions[event.SessionID].SoftInjectedByWindow == nil {
			sessions[event.SessionID].SoftInjectedByWindow = make(map[string]int64)
		}
		sessions[event.SessionID].SoftInjectedByWindow[windowID] = resetsAt

		return HookResult{
			Decision: policy.SoftInject,
			Trigger:  trigger,
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
				WindowID:  windowID,
				ResetsAt:  resetsAt,
			}},
		}
	}
	return HookResult{Decision: policy.NoAction}
}

func handlePreToolUse(s *state.State, cfg *config.Config, event HookEvent, now time.Time, processCWD string) HookResult {
	trigger, isArmed := armedHardTrigger(s, cfg)
	if !isArmed || !cfg.HardStop.EnablePreToolDeny {
		return HookResult{Decision: policy.NoAction}
	}

	resetsAt := trigger.ResetsAt
	windowID := trigger.WindowID
	var effects []SideEffect
	if event.SessionID != "" {
		ensureHandoffPathWindow(s, windowID)
		if _, exists := s.PolicyState.HandoffPathsByWindow[windowID][event.SessionID]; !exists {
			primaryPath := renderHandoffPath(cfg, s, event.SessionID, trigger, processCWD)
			handoffContent := handoff.Render(handoff.Params{
				SessionID:      event.SessionID,
				ModelID:        getModelID(s, event.SessionID),
				ISO8601:        now.Format(time.RFC3339Nano),
				UsedPercentage: trigger.UsedPercentage,
				ResetsAtHuman:  time.Unix(resetsAt, 0).UTC().Format(time.RFC3339),
				WindowID:       windowID,
				WindowLabel:    trigger.WindowLabel,
				CWD:            getCWD(s, event.SessionID, processCWD),
				HandoffPath:    primaryPath,
				TranscriptPath: getTranscriptPath(s, event.SessionID),
			})
			effects = append(effects, SideEffect{
				Type:      SideEffectHandoffWrite,
				SessionID: event.SessionID,
				Path:      primaryPath,
				Content:   handoffContent,
				WindowID:  windowID,
				ResetsAt:  resetsAt,
			})
			// C4 fix: set HandoffPaths inside core so replay state
			// matches live. Live caller may overwrite with
			// collision-resolved path later.
			s.PolicyState.HandoffPathsByWindow[windowID][event.SessionID] = primaryPath
		}
	}

	effects = append(effects, SideEffect{
		Type:      SideEffectHardDeny,
		SessionID: event.SessionID,
		WindowID:  windowID,
		ResetsAt:  resetsAt,
	})

	reason := fmt.Sprintf(
		"MTHC: local quota policy active; %s window is %.1f%% used and resets at %s. Tool use blocked.",
		trigger.WindowLabel,
		trigger.UsedPercentage,
		time.Unix(trigger.ResetsAt, 0).UTC().Format(time.RFC3339),
	)

	return HookResult{
		Decision: policy.HardStop,
		Trigger:  trigger,
		Response: HookResponse{
			HookSpecificOutput: map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "deny",
				"permissionDecisionReason": reason,
			},
		},
		SideEffects: effects,
	}
}

func armedHardTrigger(s *state.State, cfg *config.Config) (policy.Trigger, bool) {
	if !cfg.Policy.Enabled {
		return policy.Trigger{}, false
	}
	candidates := []struct {
		id    string
		label string
		w     state.WindowObservation
		c     config.WindowThresholdConfig
	}{
		{windowFiveHour, "5-hour", s.AccountWindow.FiveHour, cfg.Thresholds.FiveHour},
		{windowSevenDay, "7-day", s.AccountWindow.SevenDay, cfg.Thresholds.SevenDay},
	}
	for _, candidate := range candidates {
		if !candidate.c.Enabled || candidate.w.Absent || candidate.w.ResetsAt == 0 {
			continue
		}
		if s.PolicyState.HardTriggeredByWindow[candidate.id] == candidate.w.ResetsAt {
			return policy.Trigger{
				WindowID:       candidate.id,
				WindowLabel:    candidate.label,
				UsedPercentage: candidate.w.UsedPercentage,
				ResetsAt:       candidate.w.ResetsAt,
				Severity:       policy.HardStop,
			}, true
		}
	}
	return policy.Trigger{}, false
}

func ensureStateMaps(s *state.State) {
	if s.Sessions == nil {
		s.Sessions = make(map[string]*state.Session)
	}
	for id, sess := range s.Sessions {
		if sess == nil {
			s.Sessions[id] = &state.Session{SoftInjectedByWindow: make(map[string]int64)}
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
}

func ensureHandoffPathWindow(s *state.State, windowID string) {
	if s.PolicyState.HandoffPathsByWindow == nil {
		s.PolicyState.HandoffPathsByWindow = make(map[string]map[string]string)
	}
	if s.PolicyState.HandoffPathsByWindow[windowID] == nil {
		s.PolicyState.HandoffPathsByWindow[windowID] = make(map[string]string)
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
	// Empty fallback resolves to "." to avoid root-anchored handoff paths.
	if fallback == "" {
		return "."
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

func renderHandoffPath(cfg *config.Config, s *state.State, sessionID string, trigger policy.Trigger, processCWD string) string {
	tmpl := cfg.Handoff.PathTemplate
	windowStart := trigger.ResetsAt - windowDurationSeconds(trigger.WindowID)
	cwd := getCWD(s, sessionID, processCWD)
	p := tmpl
	p = strings.ReplaceAll(p, "{cwd}", cwd)
	p = strings.ReplaceAll(p, "{session_id}", sessionID)
	p = strings.ReplaceAll(p, "{window_id}", trigger.WindowID)
	p = strings.ReplaceAll(p, "{window_label}", trigger.WindowLabel)
	p = strings.ReplaceAll(p, "{resets_at_unix}", fmt.Sprintf("%d", trigger.ResetsAt))
	p = strings.ReplaceAll(p, "{window_start_ts}", fmt.Sprintf("%d", windowStart))
	p = strings.ReplaceAll(p, "{model_id}", getModelID(s, sessionID))
	return p
}
