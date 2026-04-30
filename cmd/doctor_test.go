package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestCheckBinaryFound(t *testing.T) {
	bin := "/usr/local/bin/mthc"
	ctx := checkContext{mthcOnPath: bin}
	r := checkBinary(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass", r.Severity)
	}
	if r.Check != "mthc.binary" {
		t.Errorf("check = %q, want %q", r.Check, "mthc.binary")
	}
}

func TestCheckBinaryMissing(t *testing.T) {
	ctx := checkContext{mthcOnPath: ""}
	r := checkBinary(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error", r.Severity)
	}
	if r.Check != "mthc.binary" {
		t.Errorf("check = %q, want %q", r.Check, "mthc.binary")
	}
	if r.Remediation == "" {
		t.Error("error should have remediation")
	}
}

func TestCheckInstallShimMatch(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:      bin,
		hasStatusline: true,
		hasHooks:      true,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{
				"command": bin + " statusline-shim",
			},
			"PostToolBatch": []any{
				map[string]any{"type": "command", "command": bin + " hook-shim"},
			},
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": bin + " hook-shim", "matcher": "*"},
			},
		},
	}
	r := checkInstall(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass: %s", r.Severity, r.Message)
	}
}

func TestCheckInstallMissingStatuslineEntry(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:       bin,
		hasStatusline:  true,
		hasHooks:       false,
		mergedSettings: map[string]any{},
	}
	r := checkInstall(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for missing statusline entry", r.Severity)
	}
}

func TestCheckInstallShimPathNotFound(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	fakePath := t.TempDir() + "/nonexistent/mthc" // never created

	ctx := checkContext{
		selfPath:      bin,
		hasStatusline: true,
		hasHooks:      false,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{
				"command": fakePath + " statusline-shim",
			},
		},
	}
	r := checkInstall(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for shim path that does not exist", r.Severity)
	}
}

func TestCheckInstallSkippedWhenNoSettings(t *testing.T) {
	ctx := checkContext{
		selfPath:      "/usr/local/bin/mthc",
		hasStatusline: true,
	}
	r := checkInstall(ctx)
	if r.Severity != sevSkipped {
		t.Errorf("got %v, want skipped when mergedSettings is nil", r.Severity)
	}
}

func TestCheckInstallDriftDetected(t *testing.T) {
	bin1 := t.TempDir() + "/mthc-old"
	writeFile(t, bin1, "#!/bin/sh\n")
	os.Chmod(bin1, 0755)

	bin2 := t.TempDir() + "/mthc-new"
	writeFile(t, bin2, "#!/bin/sh\n")
	os.Chmod(bin2, 0755)

	ctx := checkContext{
		selfPath:      bin2, // running binary is bin2
		hasStatusline: true,
		hasHooks:      false,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{
				"command": bin1 + " statusline-shim", // settings point to bin1
			},
		},
	}
	r := checkInstallDrift(ctx)
	if r.Severity != sevWarn {
		t.Errorf("got %v, want warn for drift", r.Severity)
	}
	if r.Details["settings_path"] == "" || r.Details["running_path"] == "" {
		t.Error("drift warn should have both paths in details")
	}
}

func TestCheckInstallDriftNoDrift(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:      bin,
		hasStatusline: true,
		hasHooks:      false,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{
				"command": bin + " statusline-shim",
			},
		},
	}
	r := checkInstallDrift(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass (no drift)", r.Severity)
	}
}

func TestCheckInstallDriftSkippedWhenInstallFails(t *testing.T) {
	ctx := checkContext{
		selfPath:      "/home/jin/.local/bin/mthc",
		hasStatusline: true,
		hasHooks:      false,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{
				"command": "/usr/local/bin/mthc statusline-shim",
			},
		},
	}
	// settings_path doesn't exist as a real file, so install check fails → drift skipped
	r := checkInstallDrift(ctx)
	if r.Severity != sevSkipped {
		t.Errorf("got %v, want skipped when install check would fail", r.Severity)
	}
}

func TestCheckInstallDriftSymlinkResolvesEqual(t *testing.T) {
	real := t.TempDir() + "/mthc-real"
	writeFile(t, real, "#!/bin/sh\n")
	os.Chmod(real, 0755)

	sym := t.TempDir() + "/mthc-link"
	os.Symlink(real, sym)

	ctx := checkContext{
		selfPath:      sym, // running via symlink
		hasStatusline: true,
		hasHooks:      false,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{
				"command": real + " statusline-shim",
			},
		},
	}
	r := checkInstallDrift(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass (symlink should resolve equal)", r.Severity)
	}
}

func TestCheckInstallPartialHooksOnly(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:      bin,
		hasStatusline: false,
		hasHooks:      true,
		mergedSettings: map[string]any{
			"PostToolBatch": []any{
				map[string]any{"type": "command", "command": bin + " hook-shim"},
			},
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": bin + " hook-shim", "matcher": "*"},
			},
		},
	}
	r := checkInstall(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass for hooks-only install: %s", r.Severity, r.Message)
	}
}

func TestParseShimPath(t *testing.T) {
	tests := []struct {
		cmd        string
		subcommand string
		want       string
	}{
		{"/usr/local/bin/mthc statusline-shim", "statusline-shim", "/usr/local/bin/mthc"},
		{"/usr/local/bin/mthc hook-shim", "hook-shim", "/usr/local/bin/mthc"},
		{"", "statusline-shim", ""},
		{"/usr/local/bin/mthc", "statusline-shim", ""},
		{"/usr/local/bin/mthc other-shim", "statusline-shim", ""},
		{"  /usr/local/bin/mthc  statusline-shim  ", "statusline-shim", "/usr/local/bin/mthc"},
	}
	for _, tc := range tests {
		got := parseShimPath(tc.cmd, tc.subcommand)
		if got != tc.want {
			t.Errorf("parseShimPath(%q, %q) = %q, want %q", tc.cmd, tc.subcommand, got, tc.want)
		}
	}
}

func TestCheckConfigValid(t *testing.T) {
	cfg := defaultsConfig()
	s := newState()
	ctx := checkContext{home: "/tmp", cfg: cfg, state: s}

	r := checkConfig(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass: %s", r.Severity, r.Message)
	}
}

func TestCheckConfigAbsent(t *testing.T) {
	ctx := checkContext{home: "/tmp", configAbsent: true}

	r := checkConfig(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for absent config", r.Severity)
	}
	if !strings.Contains(r.Message, "config.toml") {
		t.Errorf("message should mention config.toml: %s", r.Message)
	}
}

func TestCheckConfigCorrupt(t *testing.T) {
	ctx := checkContext{
		home:      "/tmp",
		configErr: fmt.Errorf("toml: invalid character"),
	}

	r := checkConfig(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for corrupt config", r.Severity)
	}
}

func TestCheckConfigStateMissing(t *testing.T) {
	cfg := defaultsConfig()
	ctx := checkContext{home: "/tmp", cfg: cfg, stateAbsent: true}

	r := checkConfig(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass (state.json absent is OK)", r.Severity)
	}
	if !strings.Contains(r.Message, "not yet created") {
		t.Errorf("message should mention not yet created: %s", r.Message)
	}
}

func TestCheckConfigStateCorrupt(t *testing.T) {
	cfg := defaultsConfig()
	ctx := checkContext{
		home:     "/tmp",
		cfg:      cfg,
		stateErr: fmt.Errorf("json: invalid character"),
	}

	r := checkConfig(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for corrupt state", r.Severity)
	}
}
