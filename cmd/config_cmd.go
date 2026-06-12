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
		fmt.Printf("  unit = %q\n", th.UnitOrDefault())
		fmt.Printf("  soft = %.0f\n", th.Soft)
		fmt.Printf("  hard = %.0f\n", th.Hard)
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

// settableKeys is the user-facing config surface. [internal] is owned by
// install/uninstall and deliberately excluded. This list is also the
// future env-var override whitelist (ROADMAP backlog item 6).
var settableKeys = []string{
	"policy.enabled",
	"thresholds.five_hour.enabled",
	"thresholds.five_hour.unit",
	"thresholds.five_hour.soft",
	"thresholds.five_hour.hard",
	"thresholds.seven_day.enabled",
	"thresholds.seven_day.unit",
	"thresholds.seven_day.soft",
	"thresholds.seven_day.hard",
	"handoff.path_template",
	"handoff.soft_prompt_path",
	"display.mode",
	"statusline.refresh_interval_seconds",
	"hard_stop.enable_pretool_deny",
	"recording.enabled",
	"recording.dir",
	"recording.active_window",
}

func validateSettableKey(key string) error {
	for _, k := range settableKeys {
		if k == key {
			return nil
		}
	}
	best, bestDist := "", len(key)+1
	for _, k := range settableKeys {
		if d := editDistance(key, k); d < bestDist {
			best, bestDist = k, d
		}
	}
	if bestDist <= len(key)/2 {
		return fmt.Errorf("unknown config key %q (did you mean %q?)", key, best)
	}
	return fmt.Errorf("unknown config key %q", key)
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func runConfigSet() error {
	if len(os.Args) < 5 {
		return fmt.Errorf("usage: mthc config set <key> <value>")
	}
	key := os.Args[3]
	value := os.Args[4]
	if err := validateSettableKey(key); err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mthc")
	cfgPath := filepath.Join(cfgDir, "config.toml")
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

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
	// Deliberately validates only the user-level config file in isolation.
	// Project-level overrides (./.mthc/config.toml) are validated by the
	// shims' fail-open decode path; changing this to config.Resolve would
	// make validate's result depend on the invocation directory.
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
		switch th.UnitOrDefault() {
		case "percent":
			if th.Soft < 0 || th.Soft > 100 || th.Hard < 0 || th.Hard > 100 {
				return fmt.Errorf("%s percent thresholds must be within 0..100", window.ID)
			}
		case "tokens", "usd":
			if th.Soft <= 0 || th.Hard <= 0 {
				return fmt.Errorf("%s %s thresholds must be positive", window.ID, th.UnitOrDefault())
			}
		default:
			return fmt.Errorf("%s has unknown threshold unit %q", window.ID, th.Unit)
		}
		if th.Soft >= th.Hard {
			return fmt.Errorf("%s soft must be less than hard", window.ID)
		}
	}
	if cfg.Policy.Enabled && enabled == 0 {
		return fmt.Errorf("policy.enabled=true requires at least one enabled policy window")
	}
	return nil
}

func clearPolicyStateForConfigToggle(home string, key string, value any) error {
	clearWindow := ""
	clearAll := false
	switch key {
	case "policy.enabled":
		if enabled, ok := value.(bool); !ok || enabled {
			return nil
		}
		clearAll = true
	case "thresholds.five_hour.enabled":
		if enabled, ok := value.(bool); !ok || enabled {
			return nil
		}
		clearWindow = policy.WindowFiveHour
	case "thresholds.seven_day.enabled":
		if enabled, ok := value.(bool); !ok || enabled {
			return nil
		}
		clearWindow = policy.WindowSevenDay
	case "thresholds.five_hour.unit":
		clearWindow = policy.WindowFiveHour
	case "thresholds.seven_day.unit":
		clearWindow = policy.WindowSevenDay
	default:
		return nil
	}

	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	if err := state.Update(statePath, func(s *state.State) error {
		// Config changes and state cleanup are separate writes. Policy decisions
		// already short-circuit disabled windows, so this cleanup is best-effort
		// stale-state removal rather than the primary safety mechanism.
		if clearAll {
			s.PolicyState.HardTriggeredByWindow = map[string]int64{}
			s.PolicyState.HandoffWrittenAtByWindow = map[string]time.Time{}
			s.PolicyState.HandoffPathsByWindow = map[string]map[string]string{}
			for _, sess := range s.Sessions {
				sess.SoftInjectedByWindow = map[string]int64{}
			}
			return nil
		}
		clearWindowPolicyState(s, clearWindow)
		return nil
	}); err != nil {
		return fmt.Errorf("clear policy state for config change: %w", err)
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
