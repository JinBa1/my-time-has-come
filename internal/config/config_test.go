package config

import "testing"

func TestDefaultsMatchSpec(t *testing.T) {
	c := Defaults()
	if c.Thresholds.SoftPct != 85 {
		t.Errorf("soft_pct: got %v, want 85", c.Thresholds.SoftPct)
	}
	if c.Thresholds.HardPct != 95 {
		t.Errorf("hard_pct: got %v, want 95", c.Thresholds.HardPct)
	}
	if c.Statusline.RefreshIntervalSeconds != 10 {
		t.Errorf("refresh_interval: got %v, want 10", c.Statusline.RefreshIntervalSeconds)
	}
	if !c.HardStop.EnablePreToolDeny {
		t.Error("enable_pretool_deny should default true")
	}
	if c.Handoff.PathTemplate == "" {
		t.Error("handoff path_template should have a default")
	}
	if c.Display.Mode != "silent" {
		t.Errorf("display mode: got %q, want silent", c.Display.Mode)
	}
}
