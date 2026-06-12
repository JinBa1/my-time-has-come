package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Policy     PolicyConfig     `toml:"policy"`
	Thresholds ThresholdsConfig `toml:"thresholds"`
	Handoff    HandoffConfig    `toml:"handoff"`
	Display    DisplayConfig    `toml:"display"`
	Statusline StatuslineConfig `toml:"statusline"`
	HardStop   HardStopConfig   `toml:"hard_stop"`
	Recording  RecordingConfig  `toml:"recording"`
	Internal   InternalConfig   `toml:"internal"`
}

type PolicyConfig struct {
	Enabled bool `toml:"enabled"`
}

type ThresholdsConfig struct {
	FiveHour WindowThresholdConfig `toml:"five_hour"`
	SevenDay WindowThresholdConfig `toml:"seven_day"`
}

type WindowThresholdConfig struct {
	Enabled bool    `toml:"enabled"`
	Unit    string  `toml:"unit"`
	Soft    float64 `toml:"soft"`
	Hard    float64 `toml:"hard"`
}

// UnitOrDefault applies the reader default: an omitted unit means percent.
func (c WindowThresholdConfig) UnitOrDefault() string {
	if c.Unit == "" {
		return "percent"
	}
	return c.Unit
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
	Enabled      bool   `toml:"enabled"`
	Dir          string `toml:"dir"`
	ActiveWindow string `toml:"active_window"`
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
		Policy: PolicyConfig{Enabled: true},
		Thresholds: ThresholdsConfig{
			FiveHour: WindowThresholdConfig{Enabled: true, Unit: "percent", Soft: 85, Hard: 95},
			SevenDay: WindowThresholdConfig{Enabled: true, Unit: "percent", Soft: 90, Hard: 98},
		},
		Handoff:    HandoffConfig{PathTemplate: "{cwd}/.mthc/handoff-{session_id}-{window_id}-{window_start_ts}.md"},
		Display:    DisplayConfig{Mode: "silent"},
		Statusline: StatuslineConfig{RefreshIntervalSeconds: 10},
		HardStop:   HardStopConfig{EnablePreToolDeny: true},
	}
}

func decodeRejectingLegacy(path string, c *Config) error {
	md, err := toml.DecodeFile(path, c)
	if err != nil {
		return err
	}
	for _, key := range md.Undecoded() {
		ks := key.String()
		if strings.HasSuffix(ks, ".soft_pct") || strings.HasSuffix(ks, ".hard_pct") {
			return fmt.Errorf("config %q uses removed key %q: thresholds are now unit-tagged (soft/hard/unit); see README", path, ks)
		}
	}
	return nil
}

func Load(path string) (*Config, error) {
	c := Defaults()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return c, nil
	}
	if err := decodeRejectingLegacy(path, c); err != nil {
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
		if err := decodeRejectingLegacy(userPath, c); err != nil {
			return nil, err
		}
	}
	projPath := filepath.Join(".", ".mthc", "config.toml")
	if _, err := os.Stat(projPath); err == nil {
		if err := decodeRejectingLegacy(projPath, c); err != nil {
			return nil, err
		}
	}
	return c, nil
}
