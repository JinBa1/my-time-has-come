package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/core"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

func TestConfigSetNestedKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"mthc", "config", "set", "thresholds.seven_day.enabled", "false"}
	if err := runConfigSet(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "mthc", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[thresholds.seven_day]") {
		t.Fatalf("expected nested seven_day table, got:\n%s", data)
	}
	if !strings.Contains(string(data), "enabled = false") {
		t.Fatalf("expected enabled false, got:\n%s", data)
	}
}

func TestConfigSetDisabledWindowClearsState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	s := &state.State{
		SchemaVersion: 2,
		Sessions: map[string]*state.Session{
			"sess-1": {SoftInjectedByWindow: map[string]int64{"seven_day": 1745432000}},
		},
		PolicyState: state.PolicyState{
			HardTriggeredByWindow:    map[string]int64{"seven_day": 1745432000},
			HandoffWrittenAtByWindow: map[string]time.Time{"seven_day": time.Now().UTC()},
			HandoffPathsByWindow: map[string]map[string]string{
				"seven_day": {"sess-1": "/tmp/handoff.md"},
			},
		},
	}
	if err := s.Write(statePath); err != nil {
		t.Fatal(err)
	}
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"mthc", "config", "set", "thresholds.seven_day.enabled", "false"}
	if err := runConfigSet(); err != nil {
		t.Fatal(err)
	}
	loaded, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.PolicyState.HardTriggeredByWindow["seven_day"]; ok {
		t.Fatalf("seven_day hard state should be cleared: %+v", loaded.PolicyState.HardTriggeredByWindow)
	}
	if _, ok := loaded.PolicyState.HandoffWrittenAtByWindow["seven_day"]; ok {
		t.Fatalf("seven_day handoff written state should be cleared: %+v", loaded.PolicyState.HandoffWrittenAtByWindow)
	}
	if _, ok := loaded.PolicyState.HandoffPathsByWindow["seven_day"]; ok {
		t.Fatalf("seven_day handoff paths should be cleared: %+v", loaded.PolicyState.HandoffPathsByWindow)
	}
	if _, ok := loaded.Sessions["sess-1"].SoftInjectedByWindow["seven_day"]; ok {
		t.Fatalf("seven_day soft state should be cleared: %+v", loaded.Sessions["sess-1"].SoftInjectedByWindow)
	}
}

func TestConfigSetRejectsMalformedExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, ".config", "mthc", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("[thresholds.five_hour\nsoft_pct = 80\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"mthc", "config", "set", "thresholds.five_hour.soft_pct", "80"}
	if err := runConfigSet(); err == nil {
		t.Fatal("expected malformed existing config to fail")
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[thresholds.five_hour") {
		t.Fatalf("malformed config should not be overwritten, got:\n%s", data)
	}
}

func TestDismissClearsV2PolicyState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	statePath := filepath.Join(home, ".config", "mthc", "state.json")
	s := &state.State{
		SchemaVersion: 2,
		Sessions: map[string]*state.Session{
			"sess-1": {SoftInjectedByWindow: map[string]int64{"five_hour": 1745000000, "seven_day": 1745432000}},
		},
		PolicyState: state.PolicyState{
			HardTriggeredByWindow:    map[string]int64{"five_hour": 1745000000, "seven_day": 1745432000},
			HandoffWrittenAtByWindow: map[string]time.Time{"five_hour": time.Now().UTC()},
			HandoffPathsByWindow: map[string]map[string]string{
				"five_hour": {"sess-1": "/tmp/five.md"},
				"seven_day": {"sess-1": "/tmp/seven.md"},
			},
		},
	}
	if err := s.Write(statePath); err != nil {
		t.Fatal(err)
	}
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"mthc", "dismiss"}
	output := captureStdout(t, runDismiss)
	if !strings.Contains(output, "Hard gates disarmed.") {
		t.Fatalf("dismiss output missing hard clear:\n%s", output)
	}
	if !strings.Contains(output, "Soft injections cleared for all sessions.") {
		t.Fatalf("dismiss output missing soft clear:\n%s", output)
	}
	loaded, err := state.Load(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.PolicyState.HardTriggeredByWindow) != 0 {
		t.Fatalf("hard gates should be cleared: %+v", loaded.PolicyState.HardTriggeredByWindow)
	}
	if len(loaded.PolicyState.HandoffWrittenAtByWindow) != 0 {
		t.Fatalf("handoff written state should be cleared: %+v", loaded.PolicyState.HandoffWrittenAtByWindow)
	}
	if len(loaded.PolicyState.HandoffPathsByWindow) != 0 {
		t.Fatalf("handoff paths should be cleared: %+v", loaded.PolicyState.HandoffPathsByWindow)
	}
	if len(loaded.Sessions["sess-1"].SoftInjectedByWindow) != 0 {
		t.Fatalf("soft injections should be cleared: %+v", loaded.Sessions["sess-1"].SoftInjectedByWindow)
	}
	if loaded.PolicyState.DismissedAt == nil {
		t.Fatal("dismissed_at should be set")
	}
}

func TestWriteHandoffFromSideEffectUsesWindowFallbackAndV2State(t *testing.T) {
	home := t.TempDir()
	primary := filepath.Join(home, "handoff.md")
	if err := os.WriteFile(primary, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &state.State{
		PolicyState: state.PolicyState{
			HandoffWrittenAtByWindow: map[string]time.Time{},
			HandoffPathsByWindow:     map[string]map[string]string{},
		},
	}
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	se := core.SideEffect{
		Type:      core.SideEffectHandoffWrite,
		SessionID: "sess-1",
		Path:      primary,
		Content:   "handoff body",
		WindowID:  "seven_day",
		ResetsAt:  1745432000,
	}
	writeHandoffFromSideEffect(s, se, now, home)
	wantPath := filepath.Join(home, ".config", "mthc", "handoffs", "handoff-sess-1-seven_day-1745432000.md")
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "handoff body" {
		t.Fatalf("handoff content = %q", data)
	}
	if got := s.PolicyState.HandoffPathsByWindow["seven_day"]["sess-1"]; got != wantPath {
		t.Fatalf("handoff path state = %q, want %q", got, wantPath)
	}
	if got := s.PolicyState.HandoffWrittenAtByWindow["seven_day"]; !got.Equal(now) {
		t.Fatalf("handoff written time = %s, want %s", got, now)
	}
}

func TestValidateConfigRejectsEnabledWindowSoftAtHard(t *testing.T) {
	cfg := config.Defaults()
	cfg.Thresholds.SevenDay.SoftPct = 90
	cfg.Thresholds.SevenDay.HardPct = 90
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected validation error for seven_day soft_pct >= hard_pct")
	}
}

func TestValidateConfigRejectsOutOfRangePercentage(t *testing.T) {
	cfg := config.Defaults()
	cfg.Thresholds.FiveHour.SoftPct = -1
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected validation error for percentage below 0")
	}
	cfg = config.Defaults()
	cfg.Thresholds.FiveHour.HardPct = 101
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected validation error for percentage above 100")
	}
}

func TestValidateConfigRejectsPolicyEnabledWithNoWindows(t *testing.T) {
	cfg := config.Defaults()
	cfg.Thresholds.FiveHour.Enabled = false
	cfg.Thresholds.SevenDay.Enabled = false
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected validation error when policy enabled with no windows")
	}
}

func TestValidateConfigAllowsNoWindowsWhenPolicyDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Policy.Enabled = false
	cfg.Thresholds.FiveHour.Enabled = false
	cfg.Thresholds.SevenDay.Enabled = false
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected disabled policy to validate, got %v", err)
	}
}

func TestValidateConfigRejectsInvalidEnabledWindowWhenPolicyDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Policy.Enabled = false
	cfg.Thresholds.FiveHour.SoftPct = 95
	cfg.Thresholds.FiveHour.HardPct = 90
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected enabled window threshold validation even when policy disabled")
	}
}

func TestConfigShowPrintsNestedThresholds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	output := captureStdout(t, func() error {
		return runConfigShow()
	})
	for _, want := range []string{
		"[policy]",
		"enabled = true",
		"[thresholds.five_hour]",
		"soft_pct = 85",
		"[thresholds.seven_day]",
		"soft_pct = 80",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("config show output missing %q:\n%s", want, output)
		}
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	if err := fn(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}
