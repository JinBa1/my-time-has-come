package prompt

import (
	"fmt"
	"strings"
)

type Params struct {
	UsedPercentage float64
	ResetsAtHuman  string
	ResetsAtUnix   int64
	SessionID      string
	HandoffPath    string
	CWD            string
	ModelID        string
}

var defaultTemplate = strings.Join([]string{
	"You are nearing the end of the current 5-hour Claude Code usage window.",
	"Current usage: {{.UsedPercentage}}% (resets at {{.ResetsAtHuman}}).",
	"",
	"This is an automated intervention. Please stop the current task after",
	"the immediate subtask reaches a consistent state, then:",
	"",
	"1. In one paragraph, summarize what you were working toward and why.",
	"2. Record the current repository state in the handoff:",
	"   - list of modified / new / deleted files",
	"   - whether changes are committed, staged, or uncommitted",
	"   - only commit WIP if that matches the user's or repo's normal workflow;",
	"     otherwise note the uncommitted state explicitly.",
	"3. Write a handoff document to {{.HandoffPath}} containing:",
	"   - Goal: the objective of this session.",
	"   - Progress: done / in-progress / not-started.",
	"   - Next steps: concrete actions for the next session.",
	"   - Open questions, risks, or blockers.",
	"4. After writing the handoff, stop. Do not begin new work.",
	"",
	"Reply with only the summary + handoff confirmation. Do not ask for",
	"user input.",
}, "\n")

func Render(p Params) string {
	s := defaultTemplate
	s = strings.ReplaceAll(s, "{{.UsedPercentage}}", fmt.Sprintf("%g", p.UsedPercentage))
	s = strings.ReplaceAll(s, "{{.ResetsAtHuman}}", p.ResetsAtHuman)
	s = strings.ReplaceAll(s, "{{.ResetsAtUnix}}", fmt.Sprintf("%d", p.ResetsAtUnix))
	s = strings.ReplaceAll(s, "{{.SessionID}}", p.SessionID)
	s = strings.ReplaceAll(s, "{{.HandoffPath}}", p.HandoffPath)
	s = strings.ReplaceAll(s, "{{.CWD}}", p.CWD)
	s = strings.ReplaceAll(s, "{{.ModelID}}", p.ModelID)
	return s
}
