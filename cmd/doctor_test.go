package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

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

func isolateManagedSettings(t *testing.T) {
	t.Helper()
	orig := managedSettingsPath
	managedSettingsPath = filepath.Join(t.TempDir(), "missing-managed-settings.json")
	t.Cleanup(func() { managedSettingsPath = orig })
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

func TestCheckInstallNonExecutableShim(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0644) // not executable

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
	r := checkInstall(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for non-executable shim", r.Severity)
	}
}

func TestCheckInstallHookMissingType(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:      bin,
		hasStatusline: false,
		hasHooks:      true,
		mergedSettings: map[string]any{
			"PostToolBatch": []any{
				map[string]any{"command": bin + " hook-shim"}, // missing "type"
			},
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": bin + " hook-shim", "matcher": "*"},
			},
		},
	}
	r := checkInstall(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for hook with missing type", r.Severity)
	}
}

func TestCheckInstallHookWrongType(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:      bin,
		hasStatusline: false,
		hasHooks:      true,
		mergedSettings: map[string]any{
			"PostToolBatch": []any{
				map[string]any{"type": "prompt", "command": bin + " hook-shim"},
			},
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": bin + " hook-shim", "matcher": "*"},
			},
		},
	}
	r := checkInstall(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for hook with wrong type", r.Severity)
	}
}

func TestCheckInstallPreToolUseMissingMatcher(t *testing.T) {
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
				map[string]any{"type": "command", "command": bin + " hook-shim"}, // missing matcher
			},
		},
	}
	r := checkInstall(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for PreToolUse without matcher", r.Severity)
	}
}

func TestCheckInstallPreToolUseNarrowedMatcher(t *testing.T) {
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
				map[string]any{"type": "command", "command": bin + " hook-shim", "matcher": "Read"}, // narrowed
			},
		},
	}
	r := checkInstall(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for narrowed PreToolUse matcher", r.Severity)
	}
}

func TestIsExecutableFile(t *testing.T) {
	dir := t.TempDir()

	regular := dir + "/regular.txt"
	writeFile(t, regular, "hello")
	os.Chmod(regular, 0644)
	if isExecutableFile(regular) {
		t.Error("regular file without execute bits should not be executable")
	}

	exec := dir + "/exec.sh"
	writeFile(t, exec, "#!/bin/sh\n")
	os.Chmod(exec, 0755)
	if !isExecutableFile(exec) {
		t.Error("file with execute bits should be executable")
	}

	if isExecutableFile(dir + "/nonexistent") {
		t.Error("nonexistent path should not be executable")
	}

	if isExecutableFile(dir) {
		t.Error("directory should not be executable")
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

func TestMergeClaudeSettingsUserOnly(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()
	claudeDir := home + "/.claude"
	osMkdirAll(t, claudeDir)
	writeFile(t, claudeDir+"/settings.json", `{"disableAllHooks": true}`)

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	merged, scope, _ := mergeClaudeSettings(home)
	if merged["disableAllHooks"] != true {
		t.Error("should have disableAllHooks from user scope")
	}
	if scope["disableAllHooks"] != "user" {
		t.Errorf("scope = %q, want %q", scope["disableAllHooks"], "user")
	}
}

func TestMergeClaudeSettingsProjectOverridesUser(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()
	claudeDir := home + "/.claude"
	osMkdirAll(t, claudeDir)
	writeFile(t, home+"/.claude/settings.json", `{"disableAllHooks": false}`)

	proj := home + "/work"
	osMkdirAll(t, proj+"/.claude")
	writeFile(t, proj+"/.claude/settings.json", `{"disableAllHooks": true}`)

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}

	merged, scope, _ := mergeClaudeSettings(home)
	if merged["disableAllHooks"] != true {
		t.Error("project scope should override user")
	}
	if scope["disableAllHooks"] != "project" {
		t.Errorf("scope = %q, want project", scope["disableAllHooks"])
	}
}

func TestMergeClaudeSettingsProjectOnly(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()
	// No ~/.claude/settings.json — only a project-level one

	proj := home + "/work"
	osMkdirAll(t, proj+"/.claude")
	writeFile(t, proj+"/.claude/settings.json", `{"disableAllHooks": true}`)

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}

	merged, scope, _ := mergeClaudeSettings(home)
	if merged == nil {
		t.Fatal("expected non-nil merged settings for project-only case")
	}
	if merged["disableAllHooks"] != true {
		t.Error("should have disableAllHooks from project scope")
	}
	if scope["disableAllHooks"] != "project" {
		t.Errorf("scope = %q, want project", scope["disableAllHooks"])
	}
}

func TestMergeClaudeSettingsNoFiles(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	merged, _, _ := mergeClaudeSettings(home)
	if merged != nil {
		t.Error("expected nil when no settings files found")
	}
}

func TestMergeClaudeSettingsEmptyObject(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()
	claudeDir := home + "/.claude"
	osMkdirAll(t, claudeDir)
	writeFile(t, claudeDir+"/settings.json", "{}")

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	merged, _, _ := mergeClaudeSettings(home)
	if merged == nil {
		t.Error("empty {} settings should still count as present")
	}
}

func TestSettingsPresentMalformedJSON(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()
	claudeDir := home + "/.claude"
	osMkdirAll(t, claudeDir)
	writeFile(t, claudeDir+"/settings.json", "{invalid json")

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	_, _, errs := mergeClaudeSettings(home)
	if len(errs) == 0 {
		t.Fatal("expected settings error for malformed JSON")
	}

	ctx := checkContext{home: home, settingsErrors: errs}
	r := checkSettingsPresent(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for malformed settings", r.Severity)
	}
}

func TestSettingsPresentUnreadableFile(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()
	claudeDir := home + "/.claude"
	osMkdirAll(t, claudeDir)
	path := claudeDir + "/settings.json"
	writeFile(t, path, `{"foo": "bar"}`)
	os.Chmod(path, 0000)
	t.Cleanup(func() { os.Chmod(path, 0600) })

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	_, _, errs := mergeClaudeSettings(home)
	if len(errs) == 0 {
		t.Fatal("expected settings error for unreadable file")
	}

	ctx := checkContext{home: home, settingsErrors: errs}
	r := checkSettingsPresent(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for unreadable settings", r.Severity)
	}
}

func TestMergeClaudeSettingsManagedOverridesAll(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	claudeDir := home + "/.claude"
	osMkdirAll(t, claudeDir)
	writeFile(t, claudeDir+"/settings.json", `{"disableAllHooks": false}`)

	managed := t.TempDir() + "/managed-settings.json"
	writeFile(t, managed, `{"disableAllHooks": true}`)

	orig := managedSettingsPath
	managedSettingsPath = managed
	t.Cleanup(func() { managedSettingsPath = orig })

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	merged, scope, _ := mergeClaudeSettings(home)
	if merged["disableAllHooks"] != true {
		t.Error("managed settings should override user settings")
	}
	if scope["disableAllHooks"] != "managed" {
		t.Errorf("scope = %q, want managed", scope["disableAllHooks"])
	}
}

func TestSettingsPresentCascadeSkipsDependents(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}
	ctx := checkContext{home: home, selfPath: "/x", hasStatusline: true}
	ctx.mergedSettings, ctx.settingsScope, ctx.settingsErrors = mergeClaudeSettings(home)

	if checkSettingsPresent(ctx).Severity != sevError {
		t.Error("expected error")
	}
	if checkDisableAllHooks(ctx).Severity != sevSkipped {
		t.Error("expected skipped for disable_all_hooks")
	}
	if checkStatuslineShadow(ctx).Severity != sevSkipped {
		t.Error("expected skipped for statusline_shadow")
	}
}

func TestCheckSettingsPresentFound(t *testing.T) {
	ctx := checkContext{
		mergedSettings: map[string]any{"foo": "bar"},
	}
	r := checkSettingsPresent(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass", r.Severity)
	}
}

func TestCheckSettingsPresentMissing(t *testing.T) {
	ctx := checkContext{mergedSettings: nil}
	r := checkSettingsPresent(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error", r.Severity)
	}
}

func TestCheckDisableAllHooksTrue(t *testing.T) {
	ctx := checkContext{
		mergedSettings: map[string]any{"disableAllHooks": true},
		settingsScope:  map[string]string{"disableAllHooks": "user"},
	}
	r := checkDisableAllHooks(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error", r.Severity)
	}
	if !strings.Contains(r.Remediation, "disableAllHooks") {
		t.Error("remediation should mention disableAllHooks")
	}
}

func TestCheckDisableAllHooksFalse(t *testing.T) {
	ctx := checkContext{
		mergedSettings: map[string]any{"disableAllHooks": false},
	}
	r := checkDisableAllHooks(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass", r.Severity)
	}
}

func TestCheckDisableAllHooksSkipped(t *testing.T) {
	ctx := checkContext{mergedSettings: nil}
	r := checkDisableAllHooks(ctx)
	if r.Severity != sevSkipped {
		t.Errorf("got %v, want skipped", r.Severity)
	}
}

func TestCheckStatuslineShadowOurShim(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:      bin,
		hasStatusline: true,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{"command": bin + " statusline-shim"},
		},
	}
	r := checkStatuslineShadow(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass for our own shim", r.Severity)
	}
}

func TestCheckStatuslineShadowOtherCommand(t *testing.T) {
	ctx := checkContext{
		selfPath:      "/usr/local/bin/mthc",
		hasStatusline: true,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{"command": "echo prior"},
		},
		settingsScope: map[string]string{"statusLine": "project"},
	}
	r := checkStatuslineShadow(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error for shadow", r.Severity)
	}
	if !strings.Contains(r.Remediation, "project") {
		t.Error("remediation should mention which scope set it")
	}
}

func TestCheckStatuslineShadowSkippedWhenNotInstalled(t *testing.T) {
	ctx := checkContext{
		hasStatusline: false,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{"command": "something else"},
		},
	}
	r := checkStatuslineShadow(ctx)
	if r.Severity != sevSkipped {
		t.Errorf("got %v, want skipped when statusline not installed", r.Severity)
	}
}

func TestFormatTextAllPass(t *testing.T) {
	cfg := defaultsConfig()
	ctx := checkContext{cfg: cfg, home: "/tmp", claudeVersion: "2.1.90"}
	results := []result{
		{Severity: sevPass, Check: "mthc.binary", Message: "/usr/local/bin/mthc"},
	}
	out := formatText(ctx, results)
	if !strings.Contains(out, "mthc doctor") {
		t.Error("should contain header")
	}
	if !strings.Contains(out, "[PASS]") {
		t.Error("should contain PASS tag")
	}
	if !strings.Contains(out, "2.1.90") {
		t.Error("should contain Claude version")
	}
}

func TestFormatTextNoClaudeVersion(t *testing.T) {
	cfg := defaultsConfig()
	ctx := checkContext{cfg: cfg, home: "/tmp", claudeVersion: ""}
	results := []result{}
	out := formatText(ctx, results)
	if strings.Contains(out, "Claude Code version") {
		t.Error("should not contain Claude version line when empty")
	}
}

func TestFormatTextNilCfg(t *testing.T) {
	ctx := checkContext{cfg: nil, home: "/tmp"}
	results := []result{}
	out := formatText(ctx, results)
	if !strings.Contains(out, "mthc doctor") {
		t.Error("should contain header even with nil cfg")
	}
}

func TestFormatTextDetailsSorted(t *testing.T) {
	cfg := defaultsConfig()
	ctx := checkContext{cfg: cfg, home: "/tmp"}
	results := []result{
		{
			Severity: sevError,
			Check:    "test.sort",
			Message:  "test",
			Details:  map[string]string{"z_key": "z", "a_key": "a", "m_key": "m"},
		},
	}
	out := formatText(ctx, results)
	idxA := strings.Index(out, "a_key")
	idxM := strings.Index(out, "m_key")
	idxZ := strings.Index(out, "z_key")
	if idxA == -1 || idxM == -1 || idxZ == -1 {
		t.Fatal("expected all detail keys to appear in output")
	}
	if !(idxA < idxM && idxM < idxZ) {
		t.Errorf("detail keys not sorted: a=%d, m=%d, z=%d", idxA, idxM, idxZ)
	}
}

func TestFormatJSONAllPass(t *testing.T) {
	cfg := defaultsConfig()
	ctx := checkContext{
		cfg:           cfg,
		home:          "/tmp",
		selfPath:      "/usr/local/bin/mthc",
		claudeVersion: "2.1.90",
		hasStatusline: true,
		hasHooks:      true,
	}
	results := []result{
		{Severity: sevPass, Check: "mthc.binary", Message: "/usr/local/bin/mthc"},
	}
	out := formatJSON(ctx, results)

	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if report.Version != 1 {
		t.Errorf("version = %d, want 1", report.Version)
	}
	if report.Environment["claude_code_version"] != "2.1.90" {
		t.Error("environment should contain claude_code_version")
	}
	for _, key := range []string{"pass", "info", "warn", "error", "skipped"} {
		if _, ok := report.Summary[key]; !ok {
			t.Errorf("summary missing key %q", key)
		}
	}
	if report.Summary["pass"] != 1 {
		t.Errorf("summary[pass] = %d, want 1", report.Summary["pass"])
	}
}

func TestFormatJSONOrderedFields(t *testing.T) {
	cfg := defaultsConfig()
	ctx := checkContext{cfg: cfg, home: "/tmp"}
	out := formatJSON(ctx, []result{})

	idxVersion := strings.Index(out, `"version"`)
	idxEnv := strings.Index(out, `"environment"`)
	idxResults := strings.Index(out, `"results"`)
	idxSummary := strings.Index(out, `"summary"`)
	if !(idxVersion < idxEnv && idxEnv < idxResults && idxResults < idxSummary) {
		t.Errorf("JSON field order wrong: version=%d, env=%d, results=%d, summary=%d",
			idxVersion, idxEnv, idxResults, idxSummary)
	}
}

func TestSeverityUnmarshalJSON(t *testing.T) {
	var s severity
	if err := json.Unmarshal([]byte(`"warn"`), &s); err != nil {
		t.Fatal(err)
	}
	if s != sevWarn {
		t.Errorf("got %d, want %d", s, sevWarn)
	}
}

func TestSeverityUnmarshalJSONUnknown(t *testing.T) {
	var s severity
	err := json.Unmarshal([]byte(`"bogus"`), &s)
	if err == nil {
		t.Error("expected error for unknown severity")
	}
}

func TestSeverityJSONRoundTrip(t *testing.T) {
	for _, orig := range []severity{sevPass, sevInfo, sevWarn, sevError, sevSkipped} {
		data, err := json.Marshal(orig)
		if err != nil {
			t.Fatal(err)
		}
		var got severity
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got != orig {
			t.Errorf("round-trip: got %d, want %d", got, orig)
		}
	}
}

func TestMaxSeverityRank(t *testing.T) {
	results := []result{
		{Severity: sevPass},
		{Severity: sevWarn},
	}
	rank := maxSeverityRank(results)
	if rank != sevWarn.rank() {
		t.Errorf("max rank = %d, want %d", rank, sevWarn.rank())
	}
}

func TestMaxSeverityRankEmpty(t *testing.T) {
	rank := maxSeverityRank(nil)
	if rank != 0 {
		t.Errorf("max rank of empty = %d, want 0", rank)
	}
}

func TestColorizeNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	got := colorize(sevError, "[ERROR]", false)
	if got != "[ERROR]" {
		t.Errorf("colorize with colorOn=false = %q, want [ERROR]", got)
	}
}

func TestColorizeWithColor(t *testing.T) {
	tests := []struct {
		sev  severity
		want string
	}{
		{sevError, "\033[31m[X]\033[0m"},
		{sevWarn, "\033[33m[X]\033[0m"},
		{sevPass, "\033[32m[X]\033[0m"},
		{sevInfo, "\033[2m[X]\033[0m"},
		{sevSkipped, "\033[2m[X]\033[0m"},
	}
	for _, tc := range tests {
		got := colorize(tc.sev, "[X]", true)
		if got != tc.want {
			t.Errorf("colorize(%v, [X], true) = %q, want %q", tc.sev, got, tc.want)
		}
	}
}

func TestUseColorNoColorSet(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if useColor() {
		t.Error("useColor should return false when NO_COLOR is set")
	}
}

func TestDetectClaudeVersionMissing(t *testing.T) {
	got := detectClaudeVersion()
	// In test environments, claude is typically not on PATH
	// Just verify it returns a string without panicking
	_ = got
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data))
}

func TestDoctorIntegrationHealthyInstall(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	cfgDir := home + "/.config/mthc"
	claudeDir := home + "/.claude"
	osMkdirAll(t, cfgDir)
	osMkdirAll(t, claudeDir)

	// Create a fake mthc binary
	bin := home + "/bin/mthc"
	osMkdirAll(t, home+"/bin")
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	// Write config via TOML
	cfg := config.Defaults()
	cfg.Internal.ChainedStatusline = map[string]any{"command": "echo prior"}
	cfg.Internal.InstalledHookCommand = bin + " hook-shim"
	cfg.Internal.MthcVersion = "v0-dev-test"
	if err := writeConfigToml(cfgDir+"/config.toml", cfg); err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfgDir+"/state.json", "{}")

	// Write Claude settings pointing to our binary
	settings := map[string]any{
		"statusLine": map[string]any{
			"type":    "command",
			"command": bin + " statusline-shim",
		},
		"PostToolBatch": []any{
			map[string]any{"type": "command", "command": bin + " hook-shim"},
		},
		"PreToolUse": []any{
			map[string]any{"type": "command", "command": bin + " hook-shim", "matcher": "*"},
		},
	}
	writeJSON(t, claudeDir+"/settings.json", settings)

	// chdir so walkSettingsPath can find project settings
	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	// Build context via tolerant load (matching runDoctor's approach)
	var ctx checkContext
	ctx.home = home
	data, _ := os.ReadFile(cfgDir + "/config.toml")
	loadedCfg := config.Defaults()
	toml.Decode(string(data), loadedCfg)
	ctx.cfg = loadedCfg
	sData, _ := os.ReadFile(cfgDir + "/state.json")
	json.Unmarshal(sData, &ctx.state)
	ctx.selfPath = bin
	ctx.mthcOnPath = bin
	ctx.hasStatusline = loadedCfg.Internal.ChainedStatusline != nil
	ctx.hasHooks = loadedCfg.Internal.InstalledHookCommand != ""
	ctx.mergedSettings, ctx.settingsScope, ctx.settingsErrors = mergeClaudeSettings(home)

	checks := []checkFunc{
		checkBinary,
		checkInstall,
		checkInstallDrift,
		checkConfig,
		checkSettingsPresent,
		checkDisableAllHooks,
		checkStatuslineShadow,
	}

	var results []result
	for _, cf := range checks {
		results = append(results, cf(ctx))
	}

	// All checks should pass
	for _, r := range results {
		if r.Severity != sevPass {
			t.Errorf("check %s: got %v, want pass: %s", r.Check, r.Severity, r.Message)
		}
	}

	if maxSeverityRank(results) > 0 {
		t.Error("healthy install should have max severity rank 0")
	}
}

func TestFormatJSONGoldenOutput(t *testing.T) {
	cfg := config.Defaults()
	cfg.Internal.MthcVersion = "v0.1.0-test"
	ctx := checkContext{
		home:          "/home/testuser",
		cfg:           cfg,
		claudeVersion: "2.1.90",
		selfPath:      "/usr/local/bin/mthc",
		hasStatusline: true,
		hasHooks:      true,
		stateAbsent:   false,
	}
	results := []result{
		{Severity: sevPass, Check: "mthc.binary", Message: "/usr/local/bin/mthc"},
		{Severity: sevPass, Check: "mthc.install", Message: "shim entries match running binary"},
		{Severity: sevPass, Check: "mthc.install_drift", Message: "shim paths current"},
		{Severity: sevPass, Check: "mthc.config", Message: "config and state parse OK"},
		{Severity: sevPass, Check: "claude.settings_present", Message: "settings.json parsed OK"},
		{Severity: sevPass, Check: "claude.disable_all_hooks", Message: "not set"},
		{Severity: sevPass, Check: "claude.statusline_shadow", Message: "no shadow detected"},
	}

	out := formatJSON(ctx, results)

	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if report.Version != 1 {
		t.Errorf("version = %d, want 1", report.Version)
	}

	env := report.Environment
	if env["mthc_version"] != "v0.1.0-test" {
		t.Errorf("environment.mthc_version = %v, want v0.1.0-test", env["mthc_version"])
	}
	if env["config_path"] != "/home/testuser/.config/mthc/config.toml" {
		t.Errorf("environment.config_path = %v, want /home/testuser/.config/mthc/config.toml", env["config_path"])
	}
	if env["state_path"] != "/home/testuser/.config/mthc/state.json" {
		t.Errorf("environment.state_path = %v, want /home/testuser/.config/mthc/state.json", env["state_path"])
	}
	if env["claude_code_version"] != "2.1.90" {
		t.Errorf("environment.claude_code_version = %v, want 2.1.90", env["claude_code_version"])
	}
	manifest, ok := env["install_manifest"].(map[string]interface{})
	if !ok {
		t.Fatalf("environment.install_manifest type = %T, want map[string]interface{}", env["install_manifest"])
	}
	if manifest["statusline"] != true {
		t.Errorf("install_manifest.statusline = %v, want true", manifest["statusline"])
	}
	if manifest["hooks"] != true {
		t.Errorf("install_manifest.hooks = %v, want true", manifest["hooks"])
	}

	if len(report.Results) != 7 {
		t.Fatalf("len(results) = %d, want 7", len(report.Results))
	}
	expectedChecks := []string{
		"mthc.binary",
		"mthc.install",
		"mthc.install_drift",
		"mthc.config",
		"claude.settings_present",
		"claude.disable_all_hooks",
		"claude.statusline_shadow",
	}
	for i, want := range expectedChecks {
		if report.Results[i].Check != want {
			t.Errorf("results[%d].check = %q, want %q", i, report.Results[i].Check, want)
		}
		if report.Results[i].Severity != sevPass {
			t.Errorf("results[%d].severity = %v, want pass", i, report.Results[i].Severity)
		}
	}

	summary := report.Summary
	if summary["pass"] != 7 {
		t.Errorf("summary.pass = %d, want 7", summary["pass"])
	}
	for _, key := range []string{"info", "warn", "error", "skipped"} {
		if summary[key] != 0 {
			t.Errorf("summary.%s = %d, want 0", key, summary[key])
		}
	}

	idxVersion := strings.Index(out, `"version"`)
	idxEnv := strings.Index(out, `"environment"`)
	idxResults := strings.Index(out, `"results"`)
	idxSummary := strings.Index(out, `"summary"`)
	if !(idxVersion < idxEnv && idxEnv < idxResults && idxResults < idxSummary) {
		t.Errorf("JSON field order wrong: version=%d, env=%d, results=%d, summary=%d",
			idxVersion, idxEnv, idxResults, idxSummary)
	}
}

func TestFormatTextGoldenOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	cfg := config.Defaults()
	cfg.Internal.MthcVersion = "v0.1.0-test"
	ctx := checkContext{
		home:          "/home/testuser",
		cfg:           cfg,
		claudeVersion: "2.1.90",
		selfPath:      "/usr/local/bin/mthc",
		hasStatusline: true,
		hasHooks:      true,
		stateAbsent:   false,
	}
	results := []result{
		{Severity: sevPass, Check: "mthc.binary", Message: "/usr/local/bin/mthc"},
		{Severity: sevPass, Check: "mthc.install", Message: "shim entries match running binary"},
		{Severity: sevPass, Check: "mthc.install_drift", Message: "shim paths current"},
		{Severity: sevPass, Check: "mthc.config", Message: "config and state parse OK"},
		{Severity: sevPass, Check: "claude.settings_present", Message: "settings.json parsed OK"},
		{Severity: sevPass, Check: "claude.disable_all_hooks", Message: "not set"},
		{Severity: sevPass, Check: "claude.statusline_shadow", Message: "no shadow detected"},
	}

	out := formatText(ctx, results)

	if !strings.Contains(out, "mthc doctor\n") {
		t.Error("output should contain header line 'mthc doctor'")
	}
	if !strings.Contains(out, "──────────────────────────────────────────────────────") {
		t.Error("output should contain separator line")
	}

	infoLines := []string{
		"mthc version",
		"Claude Code version",
		"config path",
		"state path",
		"install manifest",
	}
	for _, label := range infoLines {
		if !strings.Contains(out, label) {
			t.Errorf("output should contain INFO line for %q", label)
		}
	}

	passCount := strings.Count(out, "[PASS]")
	if passCount != 7 {
		t.Errorf("output contains %d [PASS] tags, want 7", passCount)
	}

	if strings.Contains(out, "\033[") {
		t.Error("output should not contain ANSI escape codes with NO_COLOR set")
	}
	for _, bad := range []string{"[ERROR]", "[WARN]", "[SKIP]"} {
		if strings.Contains(out, bad) {
			t.Errorf("output should not contain %q", bad)
		}
	}

	lines := strings.Split(out, "\n")
	headerIdx := -1
	sepIdx := -1
	firstCheckIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "mthc doctor") && headerIdx == -1 {
			headerIdx = i
		}
		if strings.Contains(line, "────────") && sepIdx == -1 {
			sepIdx = i
		}
		if strings.Contains(line, "mthc.binary") && firstCheckIdx == -1 {
			firstCheckIdx = i
		}
	}
	if headerIdx == -1 || sepIdx == -1 {
		t.Fatal("output missing header or separator")
	}
	if !(headerIdx < sepIdx) {
		t.Errorf("header at line %d should come before separator at line %d", headerIdx, sepIdx)
	}

	infoIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "mthc version") {
			infoIdx = i
			break
		}
	}
	if infoIdx == -1 {
		t.Fatal("output missing info lines")
	}
	if !(sepIdx < infoIdx) {
		t.Errorf("separator at line %d should come before info lines at line %d", sepIdx, infoIdx)
	}
	if !(infoIdx < firstCheckIdx) {
		t.Errorf("info lines at %d should come before first check at %d", infoIdx, firstCheckIdx)
	}
}

func TestSettingsErrorsCascadeToDependentChecks(t *testing.T) {
	ctx := checkContext{
		hasStatusline: true,
		mergedSettings: map[string]any{
			"disableAllHooks": true,
			"statusLine":      map[string]any{"command": "other statusline-shim"},
		},
		settingsScope: map[string]string{
			"disableAllHooks": "user",
			"statusLine":      "user",
		},
		settingsErrors: []settingsError{
			{path: "/home/x/.claude/settings.json", scope: "project", err: fmt.Errorf("invalid JSON")},
		},
		selfPath: "/usr/local/bin/mthc",
	}

	r := checkSettingsPresent(ctx)
	if r.Severity != sevError {
		t.Errorf("checkSettingsPresent: got %v, want error", r.Severity)
	}

	r = checkDisableAllHooks(ctx)
	if r.Severity != sevSkipped {
		t.Errorf("checkDisableAllHooks: got %v, want skipped", r.Severity)
	}

	r = checkStatuslineShadow(ctx)
	if r.Severity != sevSkipped {
		t.Errorf("checkStatuslineShadow: got %v, want skipped", r.Severity)
	}
}
