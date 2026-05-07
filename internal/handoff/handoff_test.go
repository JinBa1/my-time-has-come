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
		WindowID:       "seven_day",
		WindowLabel:    "7-day",
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
	if !strings.Contains(result, "7-day window") {
		t.Error("should mention trigger window")
	}
	if !strings.Contains(result, "git status") {
		t.Error("should mention git status")
	}
}

func TestRenderFormatsUsageWithOneDecimal(t *testing.T) {
	result := Render(Params{
		SessionID:      "sess-123",
		ISO8601:        "2026-04-18T12:34:56.789Z",
		UsedPercentage: 95,
		ResetsAtHuman:  "2026-04-18T15:00:00Z",
		WindowID:       "five_hour",
		WindowLabel:    "5-hour",
		CWD:            "/home/jin/repos/foo",
		HandoffPath:    "/home/jin/repos/foo/.mthc/handoff-sess-123.md",
	})
	if !strings.Contains(result, "5-hour window: 95.0% used") {
		t.Fatalf("usage should use one decimal place, got:\n%s", result)
	}
}
