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
		json.Unmarshal(data, &settings)
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
		hooks, _ := settings[hookType].([]any)
		var filtered []any
		for _, h := range hooks {
			if m, ok := h.(map[string]any); ok {
				cmd, _ := m["command"].(string)
				if cmd != installedCmd {
					filtered = append(filtered, h)
				}
			} else {
				filtered = append(filtered, h)
			}
		}
		if len(filtered) == 0 {
			delete(settings, hookType)
		} else {
			settings[hookType] = filtered
		}
	}

	// Write settings.json atomically
	if err := writeSettingsJSON(settingsPath, settings); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	// Remove mthc config and state
	os.Remove(cfgPath)
	fmt.Println("mthc uninstalled successfully.")
	fmt.Printf("  Settings restored: %s\n", settingsPath)
	return nil
}
