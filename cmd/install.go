package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JinBa1/mthc/internal/config"
)

var installForce bool

func runInstall() error {
	home, _ := os.UserHomeDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	cfgDir := filepath.Join(home, ".config", "mthc")
	cfgPath := filepath.Join(cfgDir, "config.toml")

	// Resolve the mthc binary path
	mthcBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve mthc binary: %w", err)
	}
	mthcBin, err = filepath.EvalSymlinks(mthcBin)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	// Read existing settings.json
	settings := make(map[string]any)
	if data, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(data, &settings)
	}

	// Load existing mthc config if present (reinstall case)
	existingCfg, _ := config.Load(cfgPath)

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

	// Register statusLine
	settings["statusLine"] = map[string]any{
		"type":            "command",
		"command":         mthcBin + " statusline-shim",
		"refreshInterval": 10,
		"padding":         0,
	}

	// Register PostToolBatch hook
	hookCmd := mthcBin + " hook-shim"
	registerHook(settings, "PostToolBatch", "", hookCmd)

	// Register PreToolUse hook
	registerHook(settings, "PreToolUse", "*", hookCmd)

	// Write settings.json atomically
	if err := writeSettingsJSON(settingsPath, settings); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	// Write mthc config with internal metadata
	cfg := config.Defaults()
	cfg.Internal = config.InternalConfig{
		InstalledAt:               time.Now().UTC().Format(time.RFC3339),
		MthcVersion:               "v0-dev",
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
	fmt.Printf("  Binary:    %s\n", mthcBin)
	fmt.Printf("  Config:    %s\n", cfgPath)
	fmt.Printf("  Settings:  %s\n", settingsPath)
	return nil
}

func checkDivergence(settings map[string]any, cfg *config.Config) []string {
	var divergent []string
	installedCmd := cfg.Internal.InstalledHookCommand

	// Check if our statusline is still registered
	if sl, ok := settings["statusLine"].(map[string]any); ok {
		cmd, _ := sl["command"].(string)
		// Statusline command is <bin> statusline-shim; hook command is <bin> hook-shim
		expectedStatuslineCmd := strings.Replace(installedCmd, "hook-shim", "statusline-shim", 1)
		if cmd != expectedStatuslineCmd && !strings.Contains(cmd, "mthc") {
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
		if hooks, ok := settings[hookType].([]any); ok && len(hooks) > 0 {
			prior[hookType] = true
		}
	}
	return prior
}

func registerHook(settings map[string]any, hookType, matcher, command string) {
	hooks, _ := settings[hookType].([]any)

	entry := map[string]any{
		"type":    "command",
		"command": command,
	}
	if matcher != "" {
		entry["matcher"] = matcher
	}

	// Remove any existing mthc entry to avoid duplicates
	var filtered []any
	for _, h := range hooks {
		if m, ok := h.(map[string]any); ok {
			if cmd, _ := m["command"].(string); !isMthcCommand(cmd) {
				filtered = append(filtered, h)
			}
		} else {
			filtered = append(filtered, h)
		}
	}
	filtered = append(filtered, entry)
	settings[hookType] = filtered
}

func hasOurHook(settings map[string]any, hookType, ourCmd string) bool {
	hooks, _ := settings[hookType].([]any)
	for _, h := range hooks {
		if m, ok := h.(map[string]any); ok {
			if cmd, _ := m["command"].(string); cmd == ourCmd {
				return true
			}
		}
	}
	return false
}

func isMthcCommand(cmd string) bool {
	return strings.Contains(cmd, "mthc hook-shim") || strings.Contains(cmd, "mthc statusline-shim")
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
	tmp := path + ".tmp." + fmt.Sprintf("%d", os.Getpid())
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeConfigToml(path string, cfg *config.Config) error {
	s := "# mthc configuration\n"
	s += "# Generated by mthc install — edit with care\n\n"
	s += fmt.Sprintf("[thresholds]\nsoft_pct = %g\nhard_pct = %g\n\n", cfg.Thresholds.SoftPct, cfg.Thresholds.HardPct)
	s += fmt.Sprintf("[handoff]\npath_template = %q\n\n", cfg.Handoff.PathTemplate)
	s += fmt.Sprintf("[display]\nmode = %q\n\n", cfg.Display.Mode)
	s += fmt.Sprintf("[statusline]\nrefresh_interval_seconds = %d\n\n", cfg.Statusline.RefreshIntervalSeconds)
	s += fmt.Sprintf("[hard_stop]\nenable_pretool_deny = %v\n\n", cfg.HardStop.EnablePreToolDeny)
	s += "[internal]\n"
	s += fmt.Sprintf("installed_at = %q\n", cfg.Internal.InstalledAt)
	s += fmt.Sprintf("mthc_version = %q\n", cfg.Internal.MthcVersion)
	s += fmt.Sprintf("installed_hook_command = %q\n", cfg.Internal.InstalledHookCommand)
	s += fmt.Sprintf("claude_settings_path = %q\n", cfg.Internal.ClaudeSettingsPath)
	return os.WriteFile(path, []byte(s), 0600)
}
