package recording

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordNoopWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Enabled: false, Dir: dir, ActiveWindow: "test-window"}
	entry := Entry{V: 1, TS: time.Now(), Type: "statusline", SessionID: "sess-1"}

	Record(cfg, entry)

	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Errorf("expected no files when disabled, got %d", len(files))
	}
}

func TestRecordCreatesJSONL(t *testing.T) {
	dir := t.TempDir()
	windowDir := filepath.Join(dir, "test-window")
	os.MkdirAll(windowDir, 0755)
	cfg := Config{Enabled: true, Dir: dir, ActiveWindow: "test-window"}
	entry := Entry{
		V:         1,
		TS:        time.Date(2026, 4, 28, 14, 30, 0, 0, time.UTC),
		Type:      "statusline",
		SessionID: "sess-1",
		Payload:   map[string]any{"usage": 50.0},
	}

	Record(cfg, entry)

	path := filepath.Join(windowDir, "sess-1.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected at least one line")
	}
	var got map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["v"].(float64) != 1 {
		t.Errorf("expected v=1, got %v", got["v"])
	}
	if got["type"].(string) != "statusline" {
		t.Errorf("expected type=statusline, got %v", got["type"])
	}
	if got["session_id"].(string) != "sess-1" {
		t.Errorf("expected session_id=sess-1, got %v", got["session_id"])
	}
}

func TestRecordAppendsMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	windowDir := filepath.Join(dir, "test-window")
	os.MkdirAll(windowDir, 0755)
	cfg := Config{Enabled: true, Dir: dir, ActiveWindow: "test-window"}

	Record(cfg, Entry{V: 1, TS: time.Now(), Type: "statusline", SessionID: "sess-1"})
	Record(cfg, Entry{V: 1, TS: time.Now(), Type: "hook", SessionID: "sess-1", Event: "PreToolUse"})

	path := filepath.Join(windowDir, "sess-1.jsonl")
	data, _ := os.ReadFile(path)
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Errorf("expected 2 lines, got %d", lines)
	}
}

func TestRecordCreatesWindowDir(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Enabled: true, Dir: dir, ActiveWindow: "new-window"}

	Record(cfg, Entry{V: 1, TS: time.Now(), Type: "statusline", SessionID: "sess-1"})

	windowDir := filepath.Join(dir, "new-window")
	if _, err := os.Stat(windowDir); os.IsNotExist(err) {
		t.Error("expected window directory to be created")
	}
}

func TestLoadFiles(t *testing.T) {
	dir := t.TempDir()
	windowDir := filepath.Join(dir, "test-window")
	os.MkdirAll(windowDir, 0755)

	ts1 := time.Date(2026, 4, 28, 14, 30, 0, 0, time.UTC)
	ts2 := time.Date(2026, 4, 28, 14, 31, 0, 0, time.UTC)

	cfg := Config{Enabled: true, Dir: dir, ActiveWindow: "test-window"}
	Record(cfg, Entry{V: 1, TS: ts2, Type: "hook", SessionID: "sess-1", Event: "PreToolUse"})
	Record(cfg, Entry{V: 1, TS: ts1, Type: "statusline", SessionID: "sess-1"})

	entries, err := LoadFiles([]string{filepath.Join(windowDir, "sess-1.jsonl")})
	if err != nil {
		t.Fatalf("LoadFiles failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Type != "statusline" {
		t.Errorf("expected first entry to be statusline (earlier ts), got %s", entries[0].Type)
	}
	if entries[1].Type != "hook" {
		t.Errorf("expected second entry to be hook (later ts), got %s", entries[1].Type)
	}
}
