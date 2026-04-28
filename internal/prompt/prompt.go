package prompt

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
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

const defaultTemplate = `You are nearing the end of the current 5-hour Claude Code usage window.
Current usage: {{printf "%g" .UsedPercentage}}% (resets at {{.ResetsAtHuman}}).

This is an automated intervention. Please stop the current task after
the immediate subtask reaches a consistent state, then:

1. In one paragraph, summarize what you were working toward and why.
2. Record the current repository state in the handoff:
   - list of modified / new / deleted files
   - whether changes are committed, staged, or uncommitted
   - only commit WIP if that matches the user's or repo's normal workflow;
     otherwise note the uncommitted state explicitly.
3. Write a handoff document to {{.HandoffPath}} containing:
   - Goal: the objective of this session.
   - Progress: done / in-progress / not-started.
   - Next steps: concrete actions for the next session.
   - Open questions, risks, or blockers.
4. After writing the handoff, stop. Do not begin new work.

Reply with only the summary + handoff confirmation. Do not ask for
user input.
`

func Render(p Params, customPath string) (string, error) {
	src := defaultTemplate
	if customPath != "" {
		b, err := os.ReadFile(customPath)
		if err != nil {
			return "", fmt.Errorf("read soft_prompt_path: %w", err)
		}
		src = string(b)
	}
	tmpl, err := template.New("soft").Parse(src)
	if err != nil {
		return "", fmt.Errorf("parse soft prompt: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		return "", fmt.Errorf("render soft prompt: %w", err)
	}
	return buf.String(), nil
}
