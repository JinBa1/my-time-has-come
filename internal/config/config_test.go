package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestThresholdUnitDefaultsToPercent(t *testing.T) {
	c := Defaults()
	if c.Thresholds.FiveHour.UnitOrDefault() != "percent" {
		t.Fatalf("five_hour default unit: %q", c.Thresholds.FiveHour.UnitOrDefault())
	}
	var empty WindowThresholdConfig
	if empty.UnitOrDefault() != "percent" {
		t.Fatalf("zero-value unit: %q", empty.UnitOrDefault())
	}
}

func TestLoadParsesUnitTaggedThresholds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[thresholds.five_hour]\nenabled = true\nunit = \"tokens\"\nsoft = 1000000\nhard = 2000000\n"), 0o600)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	th := c.Thresholds.FiveHour
	if th.Unit != "tokens" || th.Soft != 1000000 || th.Hard != 2000000 {
		t.Fatalf("parsed: %+v", th)
	}
}

func TestLoadRejectsLegacyThresholdKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[thresholds.five_hour]\nsoft_pct = 85\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("legacy soft_pct accepted")
	}
}

func TestDefaultsMatchSpec(t *testing.T) {
	c := Defaults()
	if !c.Policy.Enabled {
		t.Error("policy.enabled should default true")
	}
	if !c.Thresholds.FiveHour.Enabled {
		t.Error("thresholds.five_hour.enabled should default true")
	}
	if c.Thresholds.FiveHour.Soft != 85 {
		t.Errorf("five_hour soft: got %v, want 85", c.Thresholds.FiveHour.Soft)
	}
	if c.Thresholds.FiveHour.Hard != 95 {
		t.Errorf("five_hour hard: got %v, want 95", c.Thresholds.FiveHour.Hard)
	}
	if c.Thresholds.FiveHour.Unit != "percent" {
		t.Errorf("five_hour unit: got %q, want percent", c.Thresholds.FiveHour.Unit)
	}
	if !c.Thresholds.SevenDay.Enabled {
		t.Error("thresholds.seven_day.enabled should default true")
	}
	if c.Thresholds.SevenDay.Soft != 90 {
		t.Errorf("seven_day soft: got %v, want 90", c.Thresholds.SevenDay.Soft)
	}
	if c.Thresholds.SevenDay.Hard != 98 {
		t.Errorf("seven_day hard: got %v, want 98", c.Thresholds.SevenDay.Hard)
	}
	if c.Thresholds.SevenDay.Unit != "percent" {
		t.Errorf("seven_day unit: got %q, want percent", c.Thresholds.SevenDay.Unit)
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
soft = 81
hard = 91

[thresholds.seven_day]
enabled = false
soft = 70
hard = 88
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
	if cfg.Thresholds.FiveHour.Soft != 81 || cfg.Thresholds.FiveHour.Hard != 91 {
		t.Errorf("five_hour thresholds = %+v", cfg.Thresholds.FiveHour)
	}
	if cfg.Thresholds.SevenDay.Enabled {
		t.Error("seven_day.enabled should decode false")
	}
	if cfg.Thresholds.SevenDay.Soft != 70 || cfg.Thresholds.SevenDay.Hard != 88 {
		t.Errorf("seven_day thresholds = %+v", cfg.Thresholds.SevenDay)
	}
}

func TestLoadRejectsUnknownThresholdWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[thresholds.sevenday]\nenabled = true\nsoft = 80\nhard = 90\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("unknown thresholds window accepted")
	}
}

func TestLoadToleratesUnknownNonThresholdKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[future_section]\nx = 1\n"), 0o600)
	if _, err := Load(path); err != nil {
		t.Fatalf("unrelated unknown section must stay tolerated: %v", err)
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
