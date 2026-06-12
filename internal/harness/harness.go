// Package harness identifies which coding-agent CLI invoked the current
// process. Env markers are the primary signal; payload shape is secondary.
// Markers are spoofable by design: the threat model is misattribution
// (the OmO incident), not an adversary.
package harness

import "strings"

const (
	ClaudeCode = "claude-code"
	OpenCode   = "opencode"
	Unknown    = "unknown"
)

// PayloadHints carries weak shape-based signals from a parsed payload.
type PayloadHints struct {
	// ClaudeStatuslineShape is true when the payload carried a Claude Code
	// statusline rate_limits object.
	ClaudeStatuslineShape bool
}

// DetectEnv detects the harness from process environment entries
// ("KEY=value" form, as returned by os.Environ()).
//
// Marker table — verified empirically 2026-06-12 against Claude Code
// v2.1.173 and opencode, including the nested/relay case (see
// teachback-logs/2026-06-12-harness-env-probe.md):
//
//	OPENCODE=1              → opencode
//	OPENCODE_RUN_ID=<any>   → opencode
//	CLAUDECODE=1            → claude-code
//	CLAUDE_CODE_ENTRYPOINT  → claude-code
//
// Exact names only — never prefix-match: user shells leak globals like
// OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=1 into every harness.
// Opencode is checked first: under relay/nesting (the OmO incident
// class) a process carries BOTH marker sets, and the relayer is the
// correct attribution. Known accepted limitation: claude-inside-opencode
// nesting also attributes to opencode.
func DetectEnv(environ []string) string {
	vars := make(map[string]string, len(environ))
	for _, kv := range environ {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		vars[kv[:i]] = kv[i+1:]
	}
	if vars["OPENCODE"] != "" || vars["OPENCODE_RUN_ID"] != "" {
		return OpenCode
	}
	// CLAUDECODE compares against "1" exactly (the only value Claude Code
	// was observed to set); any-non-empty would misattribute a leaked
	// CLAUDECODE=0 from a user shell.
	if vars["CLAUDECODE"] == "1" || vars["CLAUDE_CODE_ENTRYPOINT"] != "" {
		return ClaudeCode
	}
	return Unknown
}

// DetectPayload is the weak secondary signal. Callers must never let it
// overwrite a known harness (stickiness rules live in core).
func DetectPayload(h PayloadHints) string {
	if h.ClaudeStatuslineShape {
		return ClaudeCode
	}
	return Unknown
}
