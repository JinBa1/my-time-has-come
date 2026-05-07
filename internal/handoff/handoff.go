package handoff

import "fmt"

type Params struct {
	SessionID      string
	ModelID        string
	ISO8601        string
	UsedPercentage float64
	ResetsAtHuman  string
	WindowID       string
	WindowLabel    string
	CWD            string
	HandoffPath    string
	TranscriptPath string
}

func Render(p Params) string {
	return "# my-time-has-come — hard-stop handoff\n\n" +
		fmt.Sprintf("Session: %s\n", p.SessionID) +
		fmt.Sprintf("Model: %s\n", p.ModelID) +
		fmt.Sprintf("Stopped at: %s\n", p.ISO8601) +
		fmt.Sprintf("%s window: %.1f%% used, resets at %s\n", p.WindowLabel, p.UsedPercentage, p.ResetsAtHuman) +
		"Termination: PreToolUse deny gate active;\n" +
		"             SIGINT deferred to v1\n" +
		fmt.Sprintf("Working dir: %s\n", p.CWD) +
		fmt.Sprintf("Handoff path: %s\n", p.HandoffPath) +
		fmt.Sprintf("Raw transcript: %s\n\n", p.TranscriptPath) +
		"## To resume\n\n" +
		fmt.Sprintf("If `%s` still exists, open a new Claude Code session there. If it does not, reopen the project in the appropriate location first.\n\n", p.CWD) +
		"Paste:\n" +
		"    > You were stopped mid-task because the usage window was exhausted.\n" +
		fmt.Sprintf("    > Read the handoff at `%s` for session details.\n", p.HandoffPath) +
		fmt.Sprintf("    > Read the raw transcript at `%s` for full context.\n", p.TranscriptPath) +
		"    > If this is a Git repository, check `git status` and `git diff` for uncommitted changes.\n" +
		"    > Continue the work.\n"
}
