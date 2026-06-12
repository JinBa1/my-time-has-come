package harness

import "testing"

func TestDetectEnv(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		want string
	}{
		{"empty env", nil, Unknown},
		{"claude code", []string{"CLAUDECODE=1", "TERM=xterm"}, ClaudeCode},
		{"claude entrypoint only", []string{"CLAUDE_CODE_ENTRYPOINT=cli"}, ClaudeCode},
		{"opencode", []string{"OPENCODE=1"}, OpenCode},
		{"opencode run id only", []string{"OPENCODE_RUN_ID=f268fa78"}, OpenCode},
		{"opencode wins over relayed claude markers", []string{"OPENCODE=1", "CLAUDECODE=1"}, OpenCode},
		{"global OPENCODE_EXPERIMENTAL_* leak is not opencode", []string{"OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=1"}, Unknown},
		{"leak plus claude markers is claude", []string{"OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=1", "CLAUDECODE=1"}, ClaudeCode},
		{"unrelated vars", []string{"HOME=/x", "PATH=/bin"}, Unknown},
		{"malformed entries ignored", []string{"NOEQUALS", "CLAUDECODE=1"}, ClaudeCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectEnv(tc.env); got != tc.want {
				t.Fatalf("DetectEnv(%v) = %q, want %q", tc.env, got, tc.want)
			}
		})
	}
}

func TestDetectPayload(t *testing.T) {
	if got := DetectPayload(PayloadHints{ClaudeStatuslineShape: true}); got != ClaudeCode {
		t.Fatalf("claude-shaped payload: %q", got)
	}
	if got := DetectPayload(PayloadHints{}); got != Unknown {
		t.Fatalf("empty hints: %q", got)
	}
}
