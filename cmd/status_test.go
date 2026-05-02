package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/state"
)

func TestStatusShowsStaleHardGateAsDisarmed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	currentResetsAt := int64(200)
	staleResetsAt := int64(100)
	writeStatusState(t, home, &state.State{
		SchemaVersion: 1,
		AccountWindow: state.AccountWindow{
			FiveHour: state.WindowObservation{
				UsedPercentage: 1,
				ResetsAt:       currentResetsAt,
				Source:         "statusline",
				LastObservedAt: time.Now().UTC(),
			},
		},
		Sessions: map[string]*state.Session{},
		PolicyState: state.PolicyState{
			HardTriggeredForResetsAt: &staleResetsAt,
			HandoffPaths:             map[string]string{},
		},
		TranscriptCursors: map[string]*state.CursorEntry{},
	})

	output := captureStatusOutput(t)

	if strings.Contains(output, "Hard gate:     ARMED") {
		t.Fatalf("output = %q, stale hard gate should not be shown as armed", output)
	}
	assertContains(t, output, "Hard gate:     disarmed (stale trigger resets_at=100)")
}

func writeStatusState(t *testing.T, home string, s *state.State) {
	t.Helper()

	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	if err := s.Write(statePath); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

func captureStatusOutput(t *testing.T) string {
	t.Helper()

	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = writer

	if err := runStatus(); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing stdout writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing stdout reader: %v", err)
	}
	return string(output)
}
