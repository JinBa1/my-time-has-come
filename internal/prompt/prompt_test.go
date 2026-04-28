package prompt

import (
	"strings"
	"testing"
)

func TestRenderDefaultPrompt(t *testing.T) {
	result := Render(Params{
		UsedPercentage: 87.5,
		ResetsAtHuman:  "2026-04-18T15:00:00Z",
		HandoffPath:    "/repo/.mthc/handoff-sess-123-1745000000.md",
	})
	if result == "" {
		t.Fatal("prompt should not be empty")
	}
	if !strings.Contains(result, "87.5") {
		t.Error("prompt should contain used_percentage")
	}
	if !strings.Contains(result, "/repo/.mthc/handoff-sess-123-1745000000.md") {
		t.Error("prompt should contain handoff_path")
	}
}
