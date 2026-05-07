package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/policy"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

func runConfig() error {
	if len(os.Args) < 3 {
		return runConfigShow()
	}
	switch os.Args[2] {
	case "show":
		return runConfigShow()
	case "set":
		return runConfigSet()
	case "validate":
		return runConfigValidate()
	default:
		return fmt.Errorf("unknown config subcommand: %s", os.Args[2])
	}
}

func runConfigShow() error {
	cfg, err := config.Resolve()
	if err != nil {
		return err
	}
	fmt.Printf("[policy]\n")
	fmt.Printf("  enabled = %v\n", cfg.Policy.Enabled)
	for _, window := range policy.Windows() {
		th := policy.WindowThreshold(cfg, window.ID)
		fmt.Printf("[thresholds.%s]\n", window.ID)
		fmt.Printf("  enabled = %v\n", th.Enabled)
		fmt.Printf("  soft_pct = %.0f\n", th.SoftPct)
		fmt.Printf("  hard_pct = %.0f\n", th.HardPct)
	}
	fmt.Printf("[handoff]\n")
	fmt.Printf("  path_template = %q\n", cfg.Handoff.PathTemplate)
	fmt.Printf("[display]\n")
	fmt.Printf("  mode = %q\n", cfg.Display.Mode)
	fmt.Printf("[statusline]\n")
	fmt.Printf("  refresh_interval_seconds = %d\n", cfg.Statusline.RefreshIntervalSeconds)
	fmt.Printf("[hard_stop]\n")
	fmt.Printf("  enable_pretool_deny = %v\n", cfg.HardStop.EnablePreToolDeny)
	return nil
}

func runConfigSet() error {
	if len(os.Args) < 5 {
		return fmt.Errorf("usage: mthc config set <key> <value>")
	}
	key := os.Args[3]
	value := os.Args[4]

	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mthc")
	cfgPath := filepath.Join(cfgDir, "config.toml")
	os.MkdirAll(cfgDir, 0700)

	// Load existing user config as raw map
	userCfg := make(map[string]any)
	if data, err := os.ReadFile(cfgPath); err == nil {
		if _, err := toml.Decode(string(data), &userCfg); err != nil {
			return fmt.Errorf("decode config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}

	parsed := parseConfigValue(value)
	setDottedValue(userCfg, key, parsed)

	// Write back via toml encoder
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(userCfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(cfgPath, []byte(buf.String()), 0600); err != nil {
		return err
	}
	return clearPolicyStateForConfigToggle(home, key, parsed)
}

func runConfigValidate() error {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "mthc", "config.toml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Printf("INVALID: %v\n", err)
		return err
	}
	if err := validateConfig(cfg); err != nil {
		fmt.Printf("INVALID: %v\n", err)
		return fmt.Errorf("validation failed")
	}
	fmt.Println("Config is valid.")
	return nil
}

func setDottedValue(root map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func validateConfig(cfg *config.Config) error {
	if cfg.Statusline.RefreshIntervalSeconds < 1 {
		return fmt.Errorf("refresh_interval_seconds must be >= 1")
	}

	enabled := 0
	for _, window := range policy.Windows() {
		th := policy.WindowThreshold(cfg, window.ID)
		if !th.Enabled {
			continue
		}
		enabled++
		if th.SoftPct < 0 || th.SoftPct > 100 ||
			th.HardPct < 0 || th.HardPct > 100 {
			return fmt.Errorf("%s percentages must be within 0..100", window.ID)
		}
		if th.SoftPct >= th.HardPct {
			return fmt.Errorf("%s soft_pct must be less than hard_pct", window.ID)
		}
	}
	if cfg.Policy.Enabled && enabled == 0 {
		return fmt.Errorf("policy.enabled=true requires at least one enabled policy window")
	}
	return nil
}

func clearPolicyStateForConfigToggle(home string, key string, value any) error {
	enabled, ok := value.(bool)
	if !ok || enabled {
		return nil
	}
	switch key {
	case "policy.enabled", "thresholds.five_hour.enabled", "thresholds.seven_day.enabled":
	default:
		return nil
	}

	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	if err := state.Update(statePath, func(s *state.State) error {
		// Config changes and state cleanup are separate writes. Policy decisions
		// already short-circuit disabled windows, so this cleanup is best-effort
		// stale-state removal rather than the primary safety mechanism.
		switch key {
		case "policy.enabled":
			s.PolicyState.HardTriggeredByWindow = map[string]int64{}
			s.PolicyState.HandoffWrittenAtByWindow = map[string]time.Time{}
			s.PolicyState.HandoffPathsByWindow = map[string]map[string]string{}
			for _, sess := range s.Sessions {
				sess.SoftInjectedByWindow = map[string]int64{}
			}
		case "thresholds.five_hour.enabled":
			clearWindowPolicyState(s, policy.WindowFiveHour)
		case "thresholds.seven_day.enabled":
			clearWindowPolicyState(s, policy.WindowSevenDay)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("clear disabled policy state: %w", err)
	}
	return nil
}

func clearWindowPolicyState(s *state.State, windowID string) {
	delete(s.PolicyState.HardTriggeredByWindow, windowID)
	delete(s.PolicyState.HandoffWrittenAtByWindow, windowID)
	delete(s.PolicyState.HandoffPathsByWindow, windowID)
	for _, sess := range s.Sessions {
		delete(sess.SoftInjectedByWindow, windowID)
	}
}

func parseConfigValue(s string) any {
	// Try int first — if no decimal point and parses as integer, use int64.
	if !strings.Contains(s, ".") {
		if i, err := parseInt(s); err == nil {
			return i
		}
	}
	if f, err := parseFloat(s); err == nil {
		return f
	}
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	return s
}

func parseInt(s string) (int64, error) {
	var i int64
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
