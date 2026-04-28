package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Thresholds ThresholdsConfig `toml:"thresholds"`
	Handoff    HandoffConfig    `toml:"handoff"`
	Display    DisplayConfig    `toml:"display"`
	Statusline StatuslineConfig `toml:"statusline"`
	HardStop   HardStopConfig   `toml:"hard_stop"`
	Recording  RecordingConfig  `toml:"recording"`
	Internal   InternalConfig   `toml:"internal"`
}

type ThresholdsConfig struct {
	SoftPct float64 `toml:"soft_pct"`
	HardPct float64 `toml:"hard_pct"`
}

type HandoffConfig struct {
	PathTemplate   string `toml:"path_template"`
	SoftPromptPath string `toml:"soft_prompt_path"`
}

type DisplayConfig struct {
	Mode string `toml:"mode"`
}

type StatuslineConfig struct {
	RefreshIntervalSeconds int `toml:"refresh_interval_seconds"`
}

type HardStopConfig struct {
	EnablePreToolDeny bool `toml:"enable_pretool_deny"`
}

type RecordingConfig struct {
	Enabled bool   `toml:"enabled"`
	Dir     string `toml:"dir"`
}

type InternalConfig struct {
	InstalledAt               string          `toml:"installed_at"`
	MthcVersion               string          `toml:"mthc_version"`
	ChainedStatusline         map[string]any  `toml:"chained_statusline,omitempty"`
	InstalledHookCommand      string          `toml:"installed_hook_command"`
	HooksPresentBeforeInstall map[string]bool `toml:"hooks_present_before_install,omitempty"`
	ClaudeSettingsPath        string          `toml:"claude_settings_path"`
}

func Defaults() *Config {
	return &Config{
		Thresholds: ThresholdsConfig{SoftPct: 85, HardPct: 95},
		Handoff:    HandoffConfig{PathTemplate: "{cwd}/.mthc/handoff-{session_id}-{window_start_ts}.md"},
		Display:    DisplayConfig{Mode: "silent"},
		Statusline: StatuslineConfig{RefreshIntervalSeconds: 10},
		HardStop:   HardStopConfig{EnablePreToolDeny: true},
	}
}

func Load(path string) (*Config, error) {
	c := Defaults()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return c, nil
	}
	if _, err := toml.DecodeFile(path, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Resolve loads config with layered resolution:
// 1. $PWD/.mthc/config.toml (per-project)
// 2. userConfigDir/mthc/config.toml (user)
// 3. Built-in defaults
func Resolve() (*Config, error) {
	c := Defaults()
	home, _ := os.UserHomeDir()
	userPath := filepath.Join(home, ".config", "mthc", "config.toml")
	if _, err := os.Stat(userPath); err == nil {
		if _, err := toml.DecodeFile(userPath, c); err != nil {
			return nil, err
		}
	}
	projPath := filepath.Join(".", ".mthc", "config.toml")
	if _, err := os.Stat(projPath); err == nil {
		if _, err := toml.DecodeFile(projPath, c); err != nil {
			return nil, err
		}
	}
	return c, nil
}
