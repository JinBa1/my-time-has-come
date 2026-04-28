package recording

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Config struct {
	Enabled      bool
	Dir          string // base recordings dir, e.g. ~/.config/mthc/recordings
	ActiveWindow string // subdirectory name, e.g. "2026-04-28T143000Z"
}

type Entry struct {
	V         int       `json:"v"`
	TS        time.Time `json:"ts"`
	Type      string    `json:"type"` // "statusline" | "hook"
	SessionID string    `json:"session_id"`
	Payload   any       `json:"payload,omitempty"`
	Event     string    `json:"event,omitempty"`
}

// Record appends an entry to the session's JSONL file if recording is enabled.
// Fail-open: logs errors to stderr, never blocks caller.
func Record(cfg Config, entry Entry) {
	if !cfg.Enabled || cfg.Dir == "" || cfg.ActiveWindow == "" {
		return
	}

	line, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mthc: recording marshal error: %v\n", err)
		return
	}
	line = append(line, '\n')

	windowDir := filepath.Join(cfg.Dir, cfg.ActiveWindow)
	os.MkdirAll(windowDir, 0755)

	path := filepath.Join(windowDir, entry.SessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mthc: recording open error: %v\n", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		fmt.Fprintf(os.Stderr, "mthc: recording write error: %v\n", err)
	}
}

// LoadFiles reads entries from one or more JSONL files and returns them
// sorted by timestamp.
func LoadFiles(paths []string) ([]Entry, error) {
	var entries []Entry
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read recording %q: %w", p, err)
		}
		for lineNum, line := range splitLines(data) {
			if len(line) == 0 {
				continue
			}
			var e Entry
			if err := json.Unmarshal(line, &e); err != nil {
				return nil, fmt.Errorf("parse recording %q line %d: %w", p, lineNum+1, err)
			}
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TS.Before(entries[j].TS)
	})
	return entries, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
