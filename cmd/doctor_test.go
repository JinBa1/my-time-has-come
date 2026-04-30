package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/state"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		sev  severity
		want string
	}{
		{sevPass, "pass"},
		{sevInfo, "info"},
		{sevWarn, "warn"},
		{sevError, "error"},
		{sevSkipped, "skipped"},
	}
	for _, tc := range tests {
		if got := tc.sev.String(); got != tc.want {
			t.Errorf("severity(%d).String() = %q, want %q", int(tc.sev), got, tc.want)
		}
	}
}

func TestSeverityRank(t *testing.T) {
	if sevWarn.rank() <= sevPass.rank() {
		t.Error("warn should rank higher than pass")
	}
	if sevError.rank() <= sevWarn.rank() {
		t.Error("error should rank higher than warn")
	}
	if sevSkipped.rank() != 0 {
		t.Errorf("skipped rank = %d, want 0", sevSkipped.rank())
	}
}

func TestSeverityMarshalJSON(t *testing.T) {
	b, err := json.Marshal(sevError)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"error"` {
		t.Errorf("MarshalJSON(sevError) = %s, want %q", b, `"error"`)
	}
}

func defaultsConfig() *config.Config {
	return config.Defaults()
}

func newState() *state.State {
	s, _ := state.Load("/nonexistent")
	if s == nil {
		s = &state.State{
			Sessions:          make(map[string]*state.Session),
			PolicyState:       state.PolicyState{HandoffPaths: make(map[string]string)},
			TranscriptCursors: make(map[string]*state.CursorEntry),
		}
	}
	return s
}

func osMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0700)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCheckContextSetsInstallManifest(t *testing.T) {
	home := t.TempDir()
	cfgDir := home + "/.config/mthc"
	osMkdirAll(t, cfgDir)

	cfg := defaultsConfig()
	cfg.Internal.ChainedStatusline = map[string]any{"command": "echo"}
	cfg.Internal.InstalledHookCommand = "/usr/local/bin/mthc hook-shim"

	ctx := buildCheckContext(home, cfg, newState())
	if !ctx.hasStatusline {
		t.Error("hasStatusline should be true when ChainedStatusline is set")
	}
	if !ctx.hasHooks {
		t.Error("hasHooks should be true when InstalledHookCommand is set")
	}
}

func TestBuildCheckContextEmptyInstall(t *testing.T) {
	ctx := buildCheckContext("/tmp", defaultsConfig(), newState())
	if ctx.hasStatusline {
		t.Error("hasStatusline should be false with nil ChainedStatusline")
	}
	if ctx.hasHooks {
		t.Error("hasHooks should be false with empty InstalledHookCommand")
	}
}
