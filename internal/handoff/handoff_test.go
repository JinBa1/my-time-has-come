package handoff

import (
	"strings"
	"testing"
)

func TestRenderDeterministicHandoff(t *testing.T) {
	result := Render(Params{
		SessionID:      "sess-123",
		ModelID:        "claude-opus-4-7",
		ISO8601:        "2026-04-18T12:34:56.789Z",
		UsedPercentage: 95.1,
		ResetsAtHuman:  "2026-04-18T15:00:00Z",
		CWD:            "/home/jin/repos/foo",
		HandoffPath:    "/home/jin/repos/foo/.mthc/handoff-sess-123.md",
		TranscriptPath: "/home/jin/.claude/projects/.../X.jsonl",
	})
	if result == "" {
		t.Fatal("handoff should not be empty")
	}
	if !strings.Contains(result, "sess-123") {
		t.Error("should contain session_id")
	}
	if !strings.Contains(result, "PreToolUse deny gate active") {
		t.Error("should reference deny gate")
	}
	if !strings.Contains(result, "git status") {
		t.Error("should mention git status")
	}
}
