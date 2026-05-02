package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/config"
)

func readSettingsFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := make(map[string]any)
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func nestedHookCommands(settings map[string]any, hookType string) []string {
	hooksRoot, _ := settings["hooks"].(map[string]any)
	groups, _ := hooksRoot[hookType].([]any)
	var commands []string
	for _, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := groupMap["hooks"].([]any)
		for _, hook := range inner {
			hookMap, ok := hook.(map[string]any)
			if !ok {
				continue
			}
			command, _ := hookMap["command"].(string)
			commands = append(commands, command)
		}
	}
	return commands
}

func legacyFlatHookCommands(settings map[string]any, hookType string) []string {
	hooks, _ := settings[hookType].([]any)
	var commands []string
	for _, hook := range hooks {
		hookMap, ok := hook.(map[string]any)
		if !ok {
			continue
		}
		command, _ := hookMap["command"].(string)
		commands = append(commands, command)
	}
	return commands
}

func hasCommand(commands []string, want string) bool {
	for _, command := range commands {
		if command == want {
			return true
		}
	}
	return false
}

func countCommand(commands []string, want string) int {
	var count int
	for _, command := range commands {
		if command == want {
			count++
		}
	}
	return count
}

func currentTestHookCommand(t *testing.T) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err = filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatal(err)
	}
	return bin + " hook-shim"
}

func TestInstallWritesNestedHooksAndPreservesUnrelatedHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldForce := installForce
	installForce = false
	t.Cleanup(func() { installForce = oldForce })

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	hookCmd := currentTestHookCommand(t)
	unrelatedPost := "echo unrelated-post"
	unrelatedPre := "echo unrelated-pre"
	unrelatedStop := "echo unrelated-stop"
	writeJSON(t, settingsPath, map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "echo prior"},
		"hooks": map[string]any{
			"PostToolBatch": []any{
				map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": unrelatedPost},
				}},
			},
			"PreToolUse": []any{
				map[string]any{"matcher": "Bash", "hooks": []any{
					map[string]any{"type": "command", "command": unrelatedPre},
				}},
			},
			"Stop": []any{
				map[string]any{"matcher": "*", "hooks": []any{
					map[string]any{"type": "command", "command": unrelatedStop},
				}},
			},
		},
		"PostToolBatch": []any{
			map[string]any{"type": "command", "command": hookCmd},
		},
		"PreToolUse": []any{
			map[string]any{"type": "command", "command": hookCmd, "matcher": "*"},
		},
	})

	if err := runInstall(); err != nil {
		t.Fatal(err)
	}

	settings := readSettingsFile(t, settingsPath)
	if _, ok := settings["hooks"].(map[string]any); !ok {
		t.Fatal("settings missing top-level hooks object")
	}
	for _, hookType := range []string{"PostToolBatch", "PreToolUse"} {
		commands := nestedHookCommands(settings, hookType)
		if countCommand(commands, hookCmd) != 1 {
			t.Fatalf("%s nested commands = %v, want exactly one mthc hook", hookType, commands)
		}
		if hasCommand(legacyFlatHookCommands(settings, hookType), hookCmd) {
			t.Fatalf("%s legacy flat hooks still contain mthc command", hookType)
		}
	}
	if !hasCommand(nestedHookCommands(settings, "PostToolBatch"), unrelatedPost) {
		t.Error("unrelated PostToolBatch group was not preserved")
	}
	if !hasCommand(nestedHookCommands(settings, "PreToolUse"), unrelatedPre) {
		t.Error("unrelated PreToolUse group was not preserved")
	}
	if !hasCommand(nestedHookCommands(settings, "Stop"), unrelatedStop) {
		t.Error("unrelated Stop hook group was not preserved")
	}
}

func TestInstallMigratesPriorMthcHookCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldForce := installForce
	installForce = false
	t.Cleanup(func() { installForce = oldForce })

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	cfgDir := filepath.Join(home, ".config", "mthc")
	osMkdirAll(t, cfgDir)

	oldBin := filepath.Join(home, "old", "mthc")
	oldHookCmd := oldBin + " hook-shim"
	newHookCmd := currentTestHookCommand(t)
	unrelatedPost := "echo unrelated-post"
	writeJSON(t, settingsPath, map[string]any{
		"statusLine": map[string]any{"type": "command", "command": oldBin + " statusline-shim"},
		"hooks": map[string]any{
			"PostToolBatch": []any{
				map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": unrelatedPost},
					map[string]any{"type": "command", "command": oldHookCmd},
				}},
			},
			"PreToolUse": []any{
				map[string]any{"matcher": "*", "hooks": []any{
					map[string]any{"type": "command", "command": oldHookCmd},
				}},
			},
		},
		"PostToolBatch": []any{
			map[string]any{"type": "command", "command": oldHookCmd},
		},
		"PreToolUse": []any{
			map[string]any{"type": "command", "command": oldHookCmd, "matcher": "*"},
		},
	})
	cfg := config.Defaults()
	cfg.Internal = config.InternalConfig{
		InstalledAt:          time.Now().UTC().Format(time.RFC3339),
		ChainedStatusline:    map[string]any{"type": "command", "command": "echo prior"},
		InstalledHookCommand: oldHookCmd,
		ClaudeSettingsPath:   settingsPath,
	}
	if err := writeConfigToml(filepath.Join(cfgDir, "config.toml"), cfg); err != nil {
		t.Fatal(err)
	}

	if err := runInstall(); err != nil {
		t.Fatal(err)
	}

	settings := readSettingsFile(t, settingsPath)
	for _, hookType := range []string{"PostToolBatch", "PreToolUse"} {
		if hasCommand(nestedHookCommands(settings, hookType), oldHookCmd) {
			t.Fatalf("%s nested hooks still contain old mthc command", hookType)
		}
		if hasCommand(legacyFlatHookCommands(settings, hookType), oldHookCmd) {
			t.Fatalf("%s legacy flat hooks still contain old mthc command", hookType)
		}
		if countCommand(nestedHookCommands(settings, hookType), newHookCmd) != 1 {
			t.Fatalf("%s nested hooks do not contain exactly one new mthc command", hookType)
		}
	}
	if !hasCommand(nestedHookCommands(settings, "PostToolBatch"), unrelatedPost) {
		t.Error("unrelated nested PostToolBatch hook was not preserved")
	}

	cfgPath := filepath.Join(cfgDir, "config.toml")
	updatedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	chained, _ := updatedCfg.Internal.ChainedStatusline["command"].(string)
	if chained != "echo prior" {
		t.Fatalf("chained statusline = %q, want original prior statusline", chained)
	}
}

func TestUninstallRemovesNestedAndLegacyFlatMthcHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".config", "mthc")
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	osMkdirAll(t, cfgDir)

	installedCmd := filepath.Join(home, "bin", "mthc") + " hook-shim"
	unrelatedPost := "echo unrelated-post"
	unrelatedPre := "echo unrelated-pre"
	unrelatedFlatPost := "echo flat-post"
	unrelatedFlatPre := "echo flat-pre"
	writeJSON(t, settingsPath, map[string]any{
		"statusLine": map[string]any{"type": "command", "command": filepath.Join(home, "bin", "mthc") + " statusline-shim"},
		"hooks": map[string]any{
			"PostToolBatch": []any{
				map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": unrelatedPost},
				}},
				map[string]any{"hooks": []any{
					map[string]any{"type": "command", "command": installedCmd},
				}},
			},
			"PreToolUse": []any{
				map[string]any{"matcher": "Bash", "hooks": []any{
					map[string]any{"type": "command", "command": unrelatedPre},
				}},
				map[string]any{"matcher": "*", "hooks": []any{
					map[string]any{"type": "command", "command": installedCmd},
				}},
			},
		},
		"PostToolBatch": []any{
			map[string]any{"type": "command", "command": installedCmd},
			map[string]any{"type": "command", "command": unrelatedFlatPost},
		},
		"PreToolUse": []any{
			map[string]any{"type": "command", "command": installedCmd, "matcher": "*"},
			map[string]any{"type": "command", "command": unrelatedFlatPre, "matcher": "Bash"},
		},
	})
	cfg := config.Defaults()
	cfg.Internal = config.InternalConfig{
		InstalledAt:          time.Now().UTC().Format(time.RFC3339),
		ChainedStatusline:    map[string]any{"type": "command", "command": "echo prior"},
		InstalledHookCommand: installedCmd,
		ClaudeSettingsPath:   settingsPath,
	}
	if err := writeConfigToml(filepath.Join(cfgDir, "config.toml"), cfg); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cfgDir, "state.json"), "{}")

	if err := runUninstall(); err != nil {
		t.Fatal(err)
	}

	settings := readSettingsFile(t, settingsPath)
	sl, ok := settings["statusLine"].(map[string]any)
	if !ok {
		t.Fatal("statusLine was not restored")
	}
	if sl["command"] != "echo prior" {
		t.Fatalf("statusLine command = %q, want prior", sl["command"])
	}
	for _, hookType := range []string{"PostToolBatch", "PreToolUse"} {
		if hasCommand(nestedHookCommands(settings, hookType), installedCmd) {
			t.Fatalf("%s nested hooks still contain mthc command", hookType)
		}
		if hasCommand(legacyFlatHookCommands(settings, hookType), installedCmd) {
			t.Fatalf("%s legacy flat hooks still contain mthc command", hookType)
		}
	}
	if !hasCommand(nestedHookCommands(settings, "PostToolBatch"), unrelatedPost) {
		t.Error("unrelated nested PostToolBatch hook was not preserved")
	}
	if !hasCommand(nestedHookCommands(settings, "PreToolUse"), unrelatedPre) {
		t.Error("unrelated nested PreToolUse hook was not preserved")
	}
	if !hasCommand(legacyFlatHookCommands(settings, "PostToolBatch"), unrelatedFlatPost) {
		t.Error("unrelated legacy flat PostToolBatch hook was not preserved")
	}
	if !hasCommand(legacyFlatHookCommands(settings, "PreToolUse"), unrelatedFlatPre) {
		t.Error("unrelated legacy flat PreToolUse hook was not preserved")
	}
}
