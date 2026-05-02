package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/JinBa1/my-time-has-come/internal/config"
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
	fmt.Printf("[thresholds]\n")
	fmt.Printf("  soft_pct = %.0f\n", cfg.Thresholds.SoftPct)
	fmt.Printf("  hard_pct = %.0f\n", cfg.Thresholds.HardPct)
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
		toml.Decode(string(data), &userCfg)
	}

	// Parse dotted key like "thresholds.soft_pct"
	parts := strings.Split(key, ".")
	if len(parts) == 2 {
		section, ok := userCfg[parts[0]].(map[string]any)
		if !ok {
			section = make(map[string]any)
		}
		section[parts[1]] = parseConfigValue(value)
		userCfg[parts[0]] = section
	} else {
		userCfg[key] = parseConfigValue(value)
	}

	// Write back via toml encoder
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(userCfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return os.WriteFile(cfgPath, []byte(buf.String()), 0600)
}

func runConfigValidate() error {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "mthc", "config.toml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Printf("INVALID: %v\n", err)
		return err
	}
	if cfg.Thresholds.SoftPct >= cfg.Thresholds.HardPct {
		fmt.Println("INVALID: soft_pct must be less than hard_pct")
		return fmt.Errorf("validation failed")
	}
	if cfg.Statusline.RefreshIntervalSeconds < 1 {
		fmt.Println("INVALID: refresh_interval_seconds must be >= 1")
		return fmt.Errorf("validation failed")
	}
	fmt.Println("Config is valid.")
	return nil
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
