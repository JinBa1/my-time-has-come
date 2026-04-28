package core

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JinBa1/mthc/internal/policy"
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
