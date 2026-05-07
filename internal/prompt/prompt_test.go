package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderDefaultPrompt(t *testing.T) {
	result, err := Render(Params{
		UsedPercentage: 87.5,
		ResetsAtHuman:  "2026-04-18T15:00:00Z",
		HandoffPath:    "/repo/.mthc/handoff-sess-123-1745000000.md",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
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

func TestRenderBadCustomPath(t *testing.T) {
	_, err := Render(Params{}, "/nonexistent/path/template.txt")
	if err == nil {
		t.Fatal("expected error for missing custom path")
	}
}

func TestRenderBadTemplateSyntax(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.tmpl"
	tmpl := "Hello {{.UsedPercentage" // unclosed action
	if err := os.WriteFile(path, []byte(tmpl), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Render(Params{}, path)
	if err == nil {
		t.Fatal("expected error for bad template syntax")
	}
}

func TestRenderCustomTemplate(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/custom.tmpl"
	tmpl := "Custom: {{printf \"%g\" .UsedPercentage}}% session={{.SessionID}}"
	if err := os.WriteFile(path, []byte(tmpl), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := Render(Params{UsedPercentage: 50, SessionID: "abc"}, path)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Custom: 50% session=abc" {
		t.Fatalf("unexpected output: %q", result)
	}
}

func TestRenderIncludesTriggerWindowFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "soft.tmpl")
	if err := os.WriteFile(path, []byte(`{{.WindowLabel}} {{.WindowID}} {{printf "%g" .SevenDay.UsedPercentage}} {{.SevenDay.Absent}}`), 0600); err != nil {
		t.Fatal(err)
	}
	text, err := Render(Params{
		UsedPercentage: 91,
		ResetsAtHuman:  "2026-04-23T00:00:00Z",
		ResetsAtUnix:   1745432000,
		WindowID:       "seven_day",
		WindowLabel:    "7-day",
		SevenDay: WindowParams{
			UsedPercentage: 91,
			ResetsAtHuman:  "2026-04-23T00:00:00Z",
			ResetsAtUnix:   1745432000,
		},
	}, path)
	if err != nil {
		t.Fatal(err)
	}
	if text != "7-day seven_day 91 false" {
		t.Fatalf("text = %q", text)
	}
}
