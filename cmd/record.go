package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

func runRecord() error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: mthc record <start|stop|status>")
	}
	switch os.Args[2] {
	case "start":
		return runRecordStart()
	case "stop":
		return runRecordStop()
	case "status":
		return runRecordStatus()
	default:
		return fmt.Errorf("unknown record subcommand: %s", os.Args[2])
	}
}

func runRecordStart() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mthc")
	cfgPath := filepath.Join(cfgDir, "config.toml")

	userCfg := loadConfigMap(cfgPath)

	rec, ok := userCfg["recording"].(map[string]any)
	if !ok {
		rec = make(map[string]any)
	}
	if enabled, _ := rec["enabled"].(bool); enabled {
		fmt.Println("already recording")
		return nil
	}

	windowName := time.Now().UTC().Format("2006-01-02T150405Z")
	rec["enabled"] = true
	rec["active_window"] = windowName
	userCfg["recording"] = rec

	if dir, _ := rec["dir"].(string); dir == "" {
		rec["dir"] = filepath.Join(home, ".config", "mthc", "recordings")
		userCfg["recording"] = rec
	}

	windowDir := filepath.Join(rec["dir"].(string), windowName)
	if err := os.MkdirAll(windowDir, 0755); err != nil {
		return fmt.Errorf("create window dir: %w", err)
	}

	// Write meta.toml with config snapshot (TOML-encoded for round-trip)
	cfgSnapshot := make(map[string]any)
	for k, v := range userCfg {
		if k != "internal" {
			cfgSnapshot[k] = v
		}
	}
	var metaBuf strings.Builder
	if err := toml.NewEncoder(&metaBuf).Encode(cfgSnapshot); err != nil {
		return fmt.Errorf("encode meta: %w", err)
	}
	if err := os.WriteFile(filepath.Join(windowDir, "meta.toml"), []byte(metaBuf.String()), 0644); err != nil {
		return fmt.Errorf("write meta.toml: %w", err)
	}

	if err := writeConfigAtomic(cfgPath, userCfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("recording started → %s\n", windowDir)
	fmt.Println("note: record commands rewrite config.toml; comments not preserved")
	return nil
}

func runRecordStop() error {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "mthc", "config.toml")

	userCfg := loadConfigMap(cfgPath)
	rec, ok := userCfg["recording"].(map[string]any)
	if !ok {
		rec = make(map[string]any)
	}
	if enabled, _ := rec["enabled"].(bool); !enabled {
		fmt.Println("not recording")
		return nil
	}

	rec["enabled"] = false
	rec["active_window"] = ""
	userCfg["recording"] = rec

	if err := writeConfigAtomic(cfgPath, userCfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Println("recording disabled")
	return nil
}

func runRecordStatus() error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mthc")
	cfgPath := filepath.Join(cfgDir, "config.toml")

	userCfg := loadConfigMap(cfgPath)
	rec, _ := userCfg["recording"].(map[string]any)
	enabled, _ := rec["enabled"].(bool)
	activeWindow, _ := rec["active_window"].(string)
	dir, _ := rec["dir"].(string)
	if dir == "" {
		dir = filepath.Join(home, ".config", "mthc", "recordings")
	}

	fmt.Printf("enabled: %v\n", enabled)
	if enabled && activeWindow != "" {
		fmt.Printf("active window: %s\n", activeWindow)
	}
	fmt.Printf("dir: %s\n", dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("windows: (error reading dir)\n")
		}
		return nil
	}
	windowCount := 0
	for _, e := range entries {
		if e.IsDir() {
			windowCount++
		}
	}
	fmt.Printf("windows: %d\n", windowCount)
	return nil
}

func loadConfigMap(path string) map[string]any {
	userCfg := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		toml.Decode(string(data), &userCfg)
	}
	return userCfg
}

func writeConfigAtomic(path string, userCfg map[string]any) error {
	os.MkdirAll(filepath.Dir(path), 0700)
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(userCfg); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(buf.String()), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
