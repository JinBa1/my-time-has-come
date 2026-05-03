package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/version"
)

const installCommandEnv = "MTHC_INSTALL_COMMAND"

var installForce bool

func runInstall() error {
	home, _ := os.UserHomeDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	cfgDir := filepath.Join(home, ".config", "mthc")
	cfgPath := filepath.Join(cfgDir, "config.toml")

	mthcCommand, err := resolveMthcCommand()
	if err != nil {
		return err
	}

	// Read existing settings.json
	settings := make(map[string]any)
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	}

	// Load existing mthc config if present (reinstall case)
	existingCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load existing config for reinstall check: %w", err)
	}

	// Divergent-reinstall guard
	if existingCfg.Internal.InstalledHookCommand != "" && !installForce {
		if divergent := checkDivergence(settings, existingCfg); len(divergent) > 0 {
			fmt.Fprintf(os.Stderr, "Divergent hooks detected since last install:\n")
			for _, d := range divergent {
				fmt.Fprintf(os.Stderr, "  - %s\n", d)
			}
			fmt.Fprintf(os.Stderr, "Use --force to overwrite.\n")
			return fmt.Errorf("aborting: hooks diverged from prior install")
		}
	}

	// Capture prior state
	priorStatusline := settings["statusLine"]
	priorHooks := capturePriorHooks(settings)
	if existingCfg.Internal.InstalledHookCommand != "" {
		priorStatusline = existingCfg.Internal.ChainedStatusline
		priorHooks = existingCfg.Internal.HooksPresentBeforeInstall
	}

	// Register statusLine
	settings["statusLine"] = map[string]any{
		"type":            "command",
		"command":         mthcCommand + " statusline-shim",
		"refreshInterval": 10,
		"padding":         0,
	}

	// Register hooks — pass old command for exact cleanup
	hookCmd := mthcCommand + " hook-shim"
	oldCmd := existingCfg.Internal.InstalledHookCommand
	registerHook(settings, "PostToolBatch", "", hookCmd, oldCmd)
	registerHook(settings, "PreToolUse", "*", hookCmd, oldCmd)

	// Write settings.json atomically
	if err := writeSettingsJSON(settingsPath, settings); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	// Write mthc config with internal metadata
	cfg := config.Defaults()
	cfg.Internal = config.InternalConfig{
		InstalledAt:               time.Now().UTC().Format(time.RFC3339),
		MthcVersion:               version.Current().Version,
		ChainedStatusline:         toMap(priorStatusline),
		InstalledHookCommand:      hookCmd,
		HooksPresentBeforeInstall: priorHooks,
		ClaudeSettingsPath:        settingsPath,
	}

	os.MkdirAll(cfgDir, 0700)
	if err := writeConfigToml(cfgPath, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Initialize empty state
	statePath := filepath.Join(cfgDir, "state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		os.WriteFile(statePath, []byte("{}"), 0600)
	}

	fmt.Println("mthc installed successfully.")
	fmt.Printf("  Command:   %s\n", mthcCommand)
	fmt.Printf("  Config:    %s\n", cfgPath)
	fmt.Printf("  Settings:  %s\n", settingsPath)
	return nil
}

func resolveMthcCommand() (string, error) {
	if command := strings.TrimSpace(os.Getenv(installCommandEnv)); command != "" {
		return command, nil
	}

	mthcBin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve mthc binary: %w", err)
	}
	mthcBin, err = filepath.EvalSymlinks(mthcBin)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	return mthcBin, nil
}

func checkDivergence(settings map[string]any, cfg *config.Config) []string {
	var divergent []string
	installedCmd := cfg.Internal.InstalledHookCommand

	// Check if our statusline is still registered
	if sl, ok := settings["statusLine"].(map[string]any); ok {
		cmd, _ := sl["command"].(string)
		expectedStatuslineCmd := strings.Replace(installedCmd, "hook-shim", "statusline-shim", 1)
		if cmd != expectedStatuslineCmd {
			divergent = append(divergent, fmt.Sprintf("statusLine command changed: expected %q, found %q", expectedStatuslineCmd, cmd))
		}
	} else if cfg.Internal.ChainedStatusline != nil {
		divergent = append(divergent, "statusLine was removed since install")
	}

	// Check hooks
	for _, hookType := range []string{"PostToolBatch", "PreToolUse"} {
		if !hasOurHook(settings, hookType, installedCmd) {
			if cfg.Internal.HooksPresentBeforeInstall[hookType] || installedCmd != "" {
				divergent = append(divergent, fmt.Sprintf("%s hook was modified or removed", hookType))
			}
		}
	}

	return divergent
}

func capturePriorHooks(settings map[string]any) map[string]bool {
	prior := make(map[string]bool)
	for _, hookType := range []string{"PostToolBatch", "PreToolUse"} {
		if hasConfiguredHookEvent(settings, hookType) {
			prior[hookType] = true
		}
	}
	return prior
}

// registerHook adds or replaces the mthc hook entry. oldCmd is the prior
// installed command (empty on first install) used for exact cleanup.
func registerHook(settings map[string]any, hookType, matcher, command, oldCmd string) {
	// Remove current command and any prior mthc command by exact match
	removeHookCommand(settings, hookType, command, oldCmd)

	hooksRoot := ensureHooksRoot(settings)
	groups, _ := hooksRoot[hookType].([]any)
	groups = append(groups, hookGroup(matcher, command))
	hooksRoot[hookType] = groups
}

func hasOurHook(settings map[string]any, hookType, ourCmd string) bool {
	if hasNestedHookCommand(settings, hookType, ourCmd) {
		return true
	}
	hooks, _ := settings[hookType].([]any)
	for _, hook := range hooks {
		if hookMap, ok := hook.(map[string]any); ok {
			cmd, _ := hookMap["command"].(string)
			if cmd == ourCmd {
				return true
			}
		}
	}
	return false
}

func hasConfiguredHookEvent(settings map[string]any, hookType string) bool {
	if hooksRoot, ok := settings["hooks"].(map[string]any); ok {
		if groups, ok := hooksRoot[hookType].([]any); ok && len(groups) > 0 {
			return true
		}
	}
	if hooks, ok := settings[hookType].([]any); ok && len(hooks) > 0 {
		return true
	}
	return false
}

func ensureHooksRoot(settings map[string]any) map[string]any {
	hooksRoot, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooksRoot = make(map[string]any)
		settings["hooks"] = hooksRoot
	}
	return hooksRoot
}

func hookGroup(matcher, command string) map[string]any {
	group := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
			},
		},
	}
	if matcher != "" {
		group["matcher"] = matcher
	}
	return group
}

func removeHookCommand(settings map[string]any, hookType string, commands ...string) {
	commandSet := make(map[string]bool)
	for _, command := range commands {
		if command != "" {
			commandSet[command] = true
		}
	}
	if len(commandSet) == 0 {
		return
	}
	removeNestedHookCommand(settings, hookType, commandSet)
	removeLegacyFlatHookCommand(settings, hookType, commandSet)
}

func removeNestedHookCommand(settings map[string]any, hookType string, commandSet map[string]bool) {
	hooksRoot, ok := settings["hooks"].(map[string]any)
	if !ok {
		return
	}
	groups, ok := hooksRoot[hookType].([]any)
	if !ok {
		return
	}

	var filteredGroups []any
	removed := false
	for _, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			filteredGroups = append(filteredGroups, group)
			continue
		}
		innerHooks, ok := groupMap["hooks"].([]any)
		if !ok {
			filteredGroups = append(filteredGroups, group)
			continue
		}

		var filteredInner []any
		groupRemoved := false
		for _, innerHook := range innerHooks {
			hookMap, ok := innerHook.(map[string]any)
			if ok {
				command, _ := hookMap["command"].(string)
				if commandSet[command] {
					removed = true
					groupRemoved = true
					continue
				}
			}
			filteredInner = append(filteredInner, innerHook)
		}
		if !groupRemoved {
			filteredGroups = append(filteredGroups, group)
			continue
		}
		if len(filteredInner) == 0 {
			continue
		}
		groupCopy := make(map[string]any, len(groupMap))
		for key, value := range groupMap {
			groupCopy[key] = value
		}
		groupCopy["hooks"] = filteredInner
		filteredGroups = append(filteredGroups, groupCopy)
	}
	if !removed {
		return
	}
	if len(filteredGroups) == 0 {
		delete(hooksRoot, hookType)
	} else {
		hooksRoot[hookType] = filteredGroups
	}
	if len(hooksRoot) == 0 {
		delete(settings, "hooks")
	}
}

func removeLegacyFlatHookCommand(settings map[string]any, hookType string, commandSet map[string]bool) {
	hooks, ok := settings[hookType].([]any)
	if !ok {
		return
	}
	var filtered []any
	removed := false
	for _, hook := range hooks {
		hookMap, ok := hook.(map[string]any)
		if ok {
			command, _ := hookMap["command"].(string)
			if commandSet[command] {
				removed = true
				continue
			}
		}
		filtered = append(filtered, hook)
	}
	if !removed {
		return
	}
	if len(filtered) == 0 {
		delete(settings, hookType)
	} else {
		settings[hookType] = filtered
	}
}

func hasNestedHookCommand(settings map[string]any, hookType, command string) bool {
	hooksRoot, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	groups, ok := hooksRoot[hookType].([]any)
	if !ok {
		return false
	}
	for _, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			continue
		}
		innerHooks, ok := groupMap["hooks"].([]any)
		if !ok {
			continue
		}
		for _, innerHook := range innerHooks {
			hookMap, ok := innerHook.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := hookMap["command"].(string); cmd == command {
				return true
			}
		}
	}
	return false
}

func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func writeSettingsJSON(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0700)
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeConfigToml(path string, cfg *config.Config) error {
	var buf strings.Builder
	buf.WriteString("# mthc configuration\n# Generated by mthc install\n\n")
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(buf.String()), 0600)
}
