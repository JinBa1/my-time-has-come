package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsMatchSpec(t *testing.T) {
	c := Defaults()
	if !c.Policy.Enabled {
		t.Error("policy.enabled should default true")
	}
	if !c.Thresholds.FiveHour.Enabled {
		t.Error("thresholds.five_hour.enabled should default true")
	}
	if c.Thresholds.FiveHour.SoftPct != 85 {
		t.Errorf("five_hour soft_pct: got %v, want 85", c.Thresholds.FiveHour.SoftPct)
	}
	if c.Thresholds.FiveHour.HardPct != 95 {
		t.Errorf("five_hour hard_pct: got %v, want 95", c.Thresholds.FiveHour.HardPct)
	}
	if !c.Thresholds.SevenDay.Enabled {
		t.Error("thresholds.seven_day.enabled should default true")
	}
	if c.Thresholds.SevenDay.SoftPct != 80 {
		t.Errorf("seven_day soft_pct: got %v, want 80", c.Thresholds.SevenDay.SoftPct)
	}
	if c.Thresholds.SevenDay.HardPct != 90 {
		t.Errorf("seven_day hard_pct: got %v, want 90", c.Thresholds.SevenDay.HardPct)
	}
	if c.Handoff.PathTemplate != "{cwd}/.mthc/handoff-{session_id}-{window_id}-{window_start_ts}.md" {
		t.Errorf("handoff path_template: got %q", c.Handoff.PathTemplate)
	}
	if c.Statusline.RefreshIntervalSeconds != 10 {
		t.Errorf("refresh_interval: got %v, want 10", c.Statusline.RefreshIntervalSeconds)
	}
	if !c.HardStop.EnablePreToolDeny {
		t.Error("enable_pretool_deny should default true")
	}
	if c.Display.Mode != "silent" {
		t.Errorf("display mode: got %q, want silent", c.Display.Mode)
	}
}

func TestLoadNestedThresholds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[policy]
enabled = false

[thresholds.five_hour]
enabled = true
soft_pct = 81
hard_pct = 91

[thresholds.seven_day]
enabled = false
soft_pct = 70
hard_pct = 88
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Policy.Enabled {
		t.Error("policy.enabled should decode false")
	}
	if cfg.Thresholds.FiveHour.SoftPct != 81 || cfg.Thresholds.FiveHour.HardPct != 91 {
		t.Errorf("five_hour thresholds = %+v", cfg.Thresholds.FiveHour)
	}
	if cfg.Thresholds.SevenDay.Enabled {
		t.Error("seven_day.enabled should decode false")
	}
	if cfg.Thresholds.SevenDay.SoftPct != 70 || cfg.Thresholds.SevenDay.HardPct != 88 {
		t.Errorf("seven_day thresholds = %+v", cfg.Thresholds.SevenDay)
	}
}

func TestDefaultsSetsRecordingDir(t *testing.T) {
	cfg := Defaults()
	if cfg.Recording.Dir != "" {
		t.Errorf("expected empty default recording dir, got %q", cfg.Recording.Dir)
	}
	if cfg.Recording.Enabled {
		t.Error("expected recording disabled by default")
	}
	if cfg.Recording.ActiveWindow != "" {
		t.Error("expected empty active_window by default")
	}
}
