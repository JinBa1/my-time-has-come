package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

// Consolidation candidate: these severity tests encode one small contract and
// can become a single table-driven test if this file needs further trimming.
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

// Consolidation candidate: the binary check has only pass/error branches, so
// these can be folded into one table once the diagnostic messages settle.
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

// Consolidation candidate: these install and drift checks are valuable branch
// coverage, but most share the same executable/settings setup and can be
// expressed as table cases around small fixture builders.
func TestCheckInstallShimMatch(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:       bin,
		hasStatusline:  true,
		hasHooks:       true,
		mergedSettings: nestedHookSettingsWithStatusline(bin),
	}
	r := checkInstall(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass: %s", r.Severity, r.Message)
	}
	if r.Message != "shim entries are present and valid" {
		t.Errorf("message = %q, want %q", r.Message, "shim entries are present and valid")
	}
}

func TestCheckInstallAcceptsBareCommandOnPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mthc")
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx := checkContext{
		selfPath:      bin,
		hasStatusline: true,
		hasHooks:      true,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{
				"command": "mthc statusline-shim",
			},
			"hooks": map[string]any{
				"PostToolBatch": []any{
					map[string]any{"hooks": []any{
						map[string]any{"type": "command", "command": "mthc hook-shim"},
					}},
				},
				"PreToolUse": []any{
					map[string]any{"matcher": "*", "hooks": []any{
						map[string]any{"type": "command", "command": "mthc hook-shim"},
					}},
				},
			},
		},
	}
	r := checkInstall(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass for bare command on PATH: %s", r.Severity, r.Message)
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
	if !strings.Contains(r.Message, "effective Claude settings") {
		t.Errorf("message = %q, want mention effective Claude settings", r.Message)
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

func TestCheckInstallDriftAcceptsStableInstallCommand(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mthc")
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(installCommandEnv, "mthc")

	selfPath := filepath.Join(t.TempDir(), "native-mthc")
	writeFile(t, selfPath, "#!/bin/sh\n")
	os.Chmod(selfPath, 0755)

	ctx := checkContext{
		selfPath:      selfPath,
		hasStatusline: true,
		hasHooks:      true,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{
				"command": "mthc statusline-shim",
			},
			"hooks": map[string]any{
				"PostToolBatch": []any{
					map[string]any{"hooks": []any{
						map[string]any{"type": "command", "command": "mthc hook-shim"},
					}},
				},
				"PreToolUse": []any{
					map[string]any{"matcher": "*", "hooks": []any{
						map[string]any{"type": "command", "command": "mthc hook-shim"},
					}},
				},
			},
		},
	}
	r := checkInstallDrift(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass for stable install command: %s", r.Severity, r.Message)
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
		selfPath:       bin,
		hasStatusline:  false,
		hasHooks:       true,
		mergedSettings: nestedHookSettings(bin),
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
			"hooks": map[string]any{
				"PostToolBatch": []any{
					map[string]any{"hooks": []any{
						map[string]any{"command": bin + " hook-shim"}, // missing "type"
					}},
				},
				"PreToolUse": []any{
					map[string]any{"matcher": "*", "hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook-shim"},
					}},
				},
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
			"hooks": map[string]any{
				"PostToolBatch": []any{
					map[string]any{"hooks": []any{
						map[string]any{"type": "prompt", "command": bin + " hook-shim"},
					}},
				},
				"PreToolUse": []any{
					map[string]any{"matcher": "*", "hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook-shim"},
					}},
				},
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
			"hooks": map[string]any{
				"PostToolBatch": []any{
					map[string]any{"hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook-shim"},
					}},
				},
				"PreToolUse": []any{
					map[string]any{"hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook-shim"},
					}},
				},
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
			"hooks": map[string]any{
				"PostToolBatch": []any{
					map[string]any{"hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook-shim"},
					}},
				},
				"PreToolUse": []any{
					map[string]any{"matcher": "Read", "hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook-shim"},
					}},
				},
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

// Consolidation candidate: config check tests are independent branches of one
// state machine and can become table cases with expected severity/message
// fragments.
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

// Consolidation candidate: settings merge tests should eventually share one
// fixture helper that creates user, project, and managed settings scopes and
// returns the merged value plus scope map.
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

func TestMergeClaudeSettingsHooksMergeAcrossScopes(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	isolateManagedSettings(t)
	home := t.TempDir()
	osMkdirAll(t, home+"/.claude")
	writeFile(t, home+"/.claude/settings.json", `{
		"hooks": {
			"PostToolBatch": [
				{"hooks": [{"type": "command", "command": "mthc hook-shim"}]}
			]
		}
	}`)

	proj := home + "/work"
	osMkdirAll(t, proj+"/.claude")
	writeFile(t, proj+"/.claude/settings.json", `{
		"hooks": {
			"PostToolBatch": [
				{"hooks": [{"type": "command", "command": "project-post"}]}
			],
			"PreToolUse": [
				{"matcher": "Bash", "hooks": [{"type": "command", "command": "project-pre"}]}
			]
		}
	}`)

	origWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origWd) })
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}

	merged, _, errs := mergeClaudeSettings(home)
	if len(errs) != 0 {
		t.Fatalf("unexpected settings errors: %v", errs)
	}
	hooks, ok := merged["hooks"].(map[string]any)
	if !ok {
		t.Fatal("merged hooks missing")
	}
	post, ok := hooks["PostToolBatch"].([]any)
	if !ok {
		t.Fatal("merged PostToolBatch hooks missing")
	}
	if len(post) != 2 {
		t.Fatalf("len(PostToolBatch) = %d, want merged user+project hooks", len(post))
	}
	pre, ok := hooks["PreToolUse"].([]any)
	if !ok {
		t.Fatal("merged PreToolUse hooks missing")
	}
	if len(pre) != 1 {
		t.Fatalf("len(PreToolUse) = %d, want project hook", len(pre))
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

// Consolidation candidate: these settings-derived checks are small severity
// branch tests and can be grouped by check function.
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
		settingsScope:  map[string]string{"disableAllHooks": "user"},
	}
	r := checkDisableAllHooks(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass", r.Severity)
	}
	if r.Message != "set to false" {
		t.Errorf("message = %q, want %q", r.Message, "set to false")
	}
}

func TestCheckDisableAllHooksNonBool(t *testing.T) {
	ctx := checkContext{
		mergedSettings: map[string]any{"disableAllHooks": "false"},
		settingsScope:  map[string]string{"disableAllHooks": "project"},
	}
	r := checkDisableAllHooks(ctx)
	if r.Severity != sevError {
		t.Errorf("got %v, want error", r.Severity)
	}
	if r.Details["scope"] != "project" {
		t.Errorf("scope detail = %q, want %q", r.Details["scope"], "project")
	}
	if !strings.Contains(r.Remediation, "disableAllHooks") {
		t.Error("remediation should mention disableAllHooks")
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

func TestCheckStatuslineShadowAcceptsStableInstallCommand(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mthc")
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(installCommandEnv, "mthc")

	selfPath := filepath.Join(t.TempDir(), "native-mthc")
	writeFile(t, selfPath, "#!/bin/sh\n")
	os.Chmod(selfPath, 0755)

	ctx := checkContext{
		selfPath:      selfPath,
		hasStatusline: true,
		mergedSettings: map[string]any{
			"statusLine": map[string]any{"command": "mthc statusline-shim"},
		},
		settingsScope: map[string]string{"statusLine": "user"},
	}
	r := checkStatuslineShadow(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass for stable install command: %s", r.Severity, r.Message)
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

// Kept outside the golden fixture because healthy output has no detail map;
// this guards deterministic ordering for diagnostic detail lines.
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

// Consolidation candidate: color and NO_COLOR behavior can be folded into a
// formatter-focused table if colored text output gets more cases.
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

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data))
}

func nestedHookSettings(bin string) map[string]any {
	return map[string]any{
		"hooks": map[string]any{
			"PostToolBatch": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook-shim"},
					},
				},
			},
			"PreToolUse": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook-shim"},
					},
				},
			},
		},
	}
}

func nestedHookSettingsWithStatusline(bin string) map[string]any {
	settings := nestedHookSettings(bin)
	settings["statusLine"] = map[string]any{
		"command": bin + " statusline-shim",
	}
	return settings
}

func TestCheckInstallNestedHooksPass(t *testing.T) {
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	ctx := checkContext{
		selfPath:       bin,
		hasStatusline:  false,
		hasHooks:       true,
		mergedSettings: nestedHookSettings(bin),
	}
	r := checkInstall(ctx)
	if r.Severity != sevPass {
		t.Errorf("got %v, want pass for nested hooks: %s", r.Severity, r.Message)
	}
}

func TestCheckInstallFlatOnlyHooksFail(t *testing.T) {
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
	if r.Severity != sevError {
		t.Errorf("got %v, want error for legacy flat-only hooks", r.Severity)
	}
	if !strings.Contains(r.Remediation, "mthc install") {
		t.Errorf("remediation = %q, want reinstall guidance", r.Remediation)
	}
}

// The integration test keeps one broad happy-path check so fragmented unit
// tests do not become the only proof that the doctor checks work together.
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
	settings := nestedHookSettingsWithStatusline(bin)
	settings["statusLine"].(map[string]any)["type"] = "command"
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

// Golden tests are the primary contract for formatter shape. Prefer updating
// these fixtures over adding narrow formatter smoke tests.
func TestFormatJSONGoldenOutput(t *testing.T) {
	ctx, results := healthyDoctorContext()
	got := formatJSON(ctx, results) + "\n"

	want, err := os.ReadFile("testdata/doctor_healthy.golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	if got != string(want) {
		t.Errorf("JSON output does not match golden fixture.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestFormatTextGoldenOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	ctx, results := healthyDoctorContext()
	got := formatText(ctx, results)

	want, err := os.ReadFile("testdata/doctor_healthy.golden.txt")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	if got != string(want) {
		t.Errorf("text output does not match golden fixture.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// healthyDoctorContext returns a fixed checkContext and results for a fully
// healthy install, used by both JSON and text golden tests.
func healthyDoctorContext() (checkContext, []result) {
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
		{Severity: sevPass, Check: "mthc.install", Message: "shim entries are present and valid"},
		{Severity: sevPass, Check: "mthc.install_drift", Message: "shim paths current"},
		{Severity: sevPass, Check: "mthc.config", Message: "config and state parse OK"},
		{Severity: sevPass, Check: "claude.settings_present", Message: "settings.json parsed OK"},
		{Severity: sevPass, Check: "claude.disable_all_hooks", Message: "not set"},
		{Severity: sevPass, Check: "claude.statusline_shadow", Message: "no shadow detected"},
	}
	return ctx, results
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

	r = checkInstall(ctx)
	if r.Severity != sevSkipped {
		t.Errorf("checkInstall: got %v, want skipped", r.Severity)
	}
	if r.Message != "skipped: claude.settings_present encountered errors" {
		t.Errorf("checkInstall message = %q, want settings error skip", r.Message)
	}

	r = checkInstallDrift(ctx)
	if r.Severity != sevSkipped {
		t.Errorf("checkInstallDrift: got %v, want skipped", r.Severity)
	}
	if r.Message != "skipped: claude.settings_present encountered errors" {
		t.Errorf("checkInstallDrift message = %q, want settings error skip", r.Message)
	}
}

func allPassCheckContext(t *testing.T) checkContext {
	t.Helper()
	bin := t.TempDir() + "/mthc"
	writeFile(t, bin, "#!/bin/sh\n")
	os.Chmod(bin, 0755)

	return checkContext{
		home:           "/tmp",
		cfg:            defaultsConfig(),
		state:          newState(),
		selfPath:       bin,
		mthcOnPath:     bin,
		hasStatusline:  true,
		hasHooks:       true,
		mergedSettings: nestedHookSettingsWithStatusline(bin),
		settingsScope: map[string]string{
			"statusLine": "user",
		},
	}
}

// Consolidation candidate: these exit-code tests can be a table once the setup
// helpers return named scenarios instead of embedding result assertions inline.
func TestExecuteDoctorChecksExit0OnPass(t *testing.T) {
	ctx := allPassCheckContext(t)
	results, exitCode := executeDoctorChecks(ctx, false)

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
	if len(results) != 7 {
		t.Errorf("len(results) = %d, want 7", len(results))
	}
	for _, r := range results {
		if r.Severity != sevPass {
			t.Errorf("check %s: got %v, want pass: %s", r.Check, r.Severity, r.Message)
		}
	}
}

func TestExecuteDoctorChecksExit1OnError(t *testing.T) {
	ctx := checkContext{mthcOnPath: ""}
	results, exitCode := executeDoctorChecks(ctx, false)

	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
	hasError := false
	for _, r := range results {
		if r.Severity == sevError {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected at least one error result")
	}
	_ = results
}

func driftCheckContext(t *testing.T) checkContext {
	t.Helper()
	bin1 := t.TempDir() + "/mthc-old"
	writeFile(t, bin1, "#!/bin/sh\n")
	os.Chmod(bin1, 0755)

	bin2 := t.TempDir() + "/mthc-new"
	writeFile(t, bin2, "#!/bin/sh\n")
	os.Chmod(bin2, 0755)

	return checkContext{
		home:           "/tmp",
		cfg:            defaultsConfig(),
		state:          newState(),
		selfPath:       bin2,
		mthcOnPath:     bin2,
		hasStatusline:  false,
		hasHooks:       true,
		mergedSettings: nestedHookSettings(bin1),
	}
}

func TestExecuteDoctorChecksExit1OnWarnWithStrict(t *testing.T) {
	ctx := driftCheckContext(t)
	results, exitCode := executeDoctorChecks(ctx, true)

	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 (strict mode with warning)", exitCode)
	}

	hasWarn := false
	for _, r := range results {
		if r.Severity == sevWarn {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Error("expected at least one warn result from drift")
	}
	_ = results
}

func TestExecuteDoctorChecksExit0OnWarnWithoutStrict(t *testing.T) {
	ctx := driftCheckContext(t)
	results, exitCode := executeDoctorChecks(ctx, false)

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0 (non-strict mode with warning)", exitCode)
	}
	_ = results
}
