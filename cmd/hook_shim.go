package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/policy"
	"github.com/JinBa1/mthc/internal/prompt"
	"github.com/JinBa1/mthc/internal/state"
)

type hookInput struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`
	ToolName      string `json:"tool_name"`
	ToolInput     any    `json:"tool_input"`
}

type hookResponse struct {
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	HookSpecificOutput       any    `json:"hookSpecificOutput,omitempty"`
}

func runHookShim() error {
	var input hookInput
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fmt.Print("{}")
		return nil
	}

	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	cfg, _ := config.Resolve()

	var resp hookResponse
	err := state.Update(statePath, func(s *state.State) error {
		now := time.Now().UTC()

		// Update session liveness
		if input.SessionID != "" {
			sess, exists := s.Sessions[input.SessionID]
			if !exists {
				sess = &state.Session{}
				s.Sessions[input.SessionID] = sess
			}
			sess.LastSeenAt = now
		}

		sessions, decision := policy.Decide(s, cfg, now)

		switch input.HookEventName {
		case "PostToolBatch":
			resp = handlePostToolBatch(s, cfg, input, sessions, decision, now)
		case "PreToolUse":
			resp = handlePreToolUse(s, cfg, input, sessions, decision, now, home)
		}
		return nil
	})
	if err != nil {
		fmt.Print("{}")
		return nil
	}

	out, _ := json.Marshal(resp)
	fmt.Print(string(out))
	return nil
}

func handlePostToolBatch(s *state.State, cfg *config.Config, input hookInput, sessions map[string]*state.Session, decision policy.Decision, now time.Time) hookResponse {
	if decision == policy.SoftInject && sessions[input.SessionID] != nil {
		resetsAt := s.AccountWindow.FiveHour.ResetsAt
		p := prompt.Params{
			UsedPercentage: s.AccountWindow.FiveHour.UsedPercentage,
			ResetsAtHuman:  time.Unix(resetsAt, 0).UTC().Format(time.RFC3339),
			ResetsAtUnix:   resetsAt,
			SessionID:      input.SessionID,
			HandoffPath:    renderHandoffPath(cfg, s, input.SessionID, resetsAt),
			CWD:            getCWD(s, input.SessionID),
			ModelID:        getModelID(s, input.SessionID),
		}
		text, err := prompt.Render(p, cfg.Handoff.SoftPromptPath)
		if err != nil {
			return hookResponse{} // fail-open
		}
		sessions[input.SessionID].SoftInjectedForResetsAt = &resetsAt

		return hookResponse{
			HookSpecificOutput: map[string]any{
				"hookEventName":     "PostToolBatch",
				"additionalContext": text,
			},
		}
	}
	return hookResponse{}
}

func handlePreToolUse(s *state.State, cfg *config.Config, input hookInput, sessions map[string]*state.Session, decision policy.Decision, now time.Time, home string) hookResponse {
	// Check if hard gate is armed
	resetsAt := s.AccountWindow.FiveHour.ResetsAt
	isArmed := s.PolicyState.HardTriggeredForResetsAt != nil &&
		*s.PolicyState.HardTriggeredForResetsAt == resetsAt

	if !isArmed || !cfg.HardStop.EnablePreToolDeny {
		return hookResponse{}
	}

	// Late-join handoff guarantee
	if input.SessionID != "" {
		if _, exists := s.PolicyState.HandoffPaths[input.SessionID]; !exists {
			writeHandoffForSession(s, cfg, input.SessionID, resetsAt, now, home)
		}
	}

	return hookResponse{
		PermissionDecision:       "deny",
		PermissionDecisionReason: "MTHC: local quota policy active, usage window near exhaustion. Tool use blocked.",
	}
}
