package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JinBa1/mthc/internal/config"
)

func runUninstall() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mthc")
	cfgPath := filepath.Join(cfgDir, "config.toml")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	settingsPath := cfg.Internal.ClaudeSettingsPath
	if settingsPath == "" {
		settingsPath = filepath.Join(home, ".claude", "settings.json")
	}

	settings := make(map[string]any)
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	}

	// Restore chained statusline
	if cfg.Internal.ChainedStatusline != nil {
		settings["statusLine"] = cfg.Internal.ChainedStatusline
	} else {
		delete(settings, "statusLine")
	}

	// Remove mthc hooks (exact match on installed command)
	installedCmd := cfg.Internal.InstalledHookCommand
	for _, hookType := range []string{"PostToolBatch", "PreToolUse"} {
		removeHookCommand(settings, hookType, installedCmd)
	}

	// Write settings.json atomically, but only if it already existed
	if _, statErr := os.Stat(settingsPath); statErr == nil {
		if err := writeSettingsJSON(settingsPath, settings); err != nil {
			return fmt.Errorf("write settings: %w", err)
		}
	}

	// Remove mthc config, state, and lockfile. Best-effort dir cleanup.
	os.Remove(cfgPath)
	statePath := filepath.Join(cfgDir, "state.json")
	os.Remove(statePath)
	os.Remove(statePath + ".lock")
	os.Remove(cfgDir)
	fmt.Println("mthc uninstalled successfully.")
	fmt.Printf("  Settings restored: %s\n", settingsPath)
	return nil
}
