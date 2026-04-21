package config

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
	RefreshIntervalSeconds int
}

type HardStopConfig struct {
	EnableSIGINTFallback bool `toml:"enable_sigint_fallback"`
	SIGINTGraceSeconds   int  `toml:"sigint_grace_seconds"`
}

type RecordingConfig struct {
	Enabled bool   `toml:"enabled"`
	Dir     string `toml:"dir"`
}

type InternalConfig struct {
	InstalledAt              string                 `toml:"installed_at"`
	MthcVersion              string                 `toml:"mthc_version"`
	ChainedStatusline        map[string]interface{} `toml:"chained_statusline"`
	InstalledStopHookCommand []string               `toml:"installed_stop_hook_command"`
	StopHookPresentBefore    bool                   `toml:"stop_hook_present_before"`
	ClaudeSettingsPath       string                 `toml:"claude_settings_path"`
}

func Defaults() *Config {
	return &Config{
		Thresholds: ThresholdsConfig{SoftPct: 85, HardPct: 95},
		Handoff:    HandoffConfig{PathTemplate: "{cmd}/.mthc/handoff-{session_id}-{window_start_ts}.md"},
		Display:    DisplayConfig{Mode: "silent"},
		Statusline: StatuslineConfig{RefreshIntervalSeconds: 10},
		HardStop:   HardStopConfig{EnableSIGINTFallback: true, SIGINTGraceSeconds: 30},
	}
}

func Load(path string) (*Config, error) {
	return Defaults(), nil
}
