package adapter

import (
	"strings"
	"testing"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/state"
)

func TestObservationsFromPayload(t *testing.T) {
	p := StatuslinePayload{
		SessionID:       "s1",
		FiveHourPresent: true, FiveHourUsedPct: 96, FiveHourResetsAt: 100,
		SevenDayPresent: false,
	}
	now := time.Unix(1000, 0).UTC()
	obs := p.Observations("claude-code", now)
	if len(obs) != 1 {
		t.Fatalf("want 1 observation, got %d", len(obs))
	}
	o := obs[0]
	if o.Window.ID != "five_hour" || o.Value != 96 || o.Unit != state.UnitPercent ||
		o.Source != state.SourceStatusline || o.Harness != "claude-code" ||
		o.Scope != state.ScopeAccount || !o.ObservedAt.Equal(now) {
		t.Fatalf("bad observation: %+v", o)
	}
}

func TestParseStatuslinePayload(t *testing.T) {
	raw := `{
		"session_id": "c787e54c-b38d-421d-ae2e-c82f63b26301",
		"transcript_path": "/home/jin/.claude/projects/.../X.jsonl",
		"cwd": "/home/jin/repos/foo",
		"model": {"id": "claude-opus-4-7"},
		"rate_limits": {
			"five_hour": {"used_percentage": 47.2, "resets_at": 1745000000},
			"seven_day": {"used_percentage": 18.4, "resets_at": 1745432000}
		}
	}`
	p, err := ParseStatusline(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if p.SessionID != "c787e54c-b38d-421d-ae2e-c82f63b26301" {
		t.Errorf("session_id: got %q", p.SessionID)
	}
	if p.FiveHourUsedPct != 47.2 {
		t.Errorf("five_hour used_pct: got %v", p.FiveHourUsedPct)
	}
	if p.FiveHourResetsAt != 1745000000 {
		t.Errorf("five_hour resets_at: got %v", p.FiveHourResetsAt)
	}
	if !p.FiveHourPresent {
		t.Error("five_hour should be present")
	}
	if !p.SevenDayPresent {
		t.Error("seven_day should be present")
	}
	if p.SevenDayUsedPct != 18.4 {
		t.Errorf("seven_day used_pct: got %v", p.SevenDayUsedPct)
	}
	if p.SevenDayResetsAt != 1745432000 {
		t.Errorf("seven_day resets_at: got %v", p.SevenDayResetsAt)
	}
	if p.ModelID != "claude-opus-4-7" {
		t.Errorf("model_id: got %q", p.ModelID)
	}
	if p.CWD != "/home/jin/repos/foo" {
		t.Errorf("cwd: got %q", p.CWD)
	}
	if p.TranscriptPath == "" {
		t.Error("transcript_path should be set")
	}
}

func TestParseStatuslinePayloadMissingFields(t *testing.T) {
	p, err := ParseStatusline(strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.SessionID != "" {
		t.Error("session_id should be empty for missing data")
	}
	if p.FiveHourUsedPct != 0 {
		t.Error("used_pct should be 0 for missing data")
	}
	if p.FiveHourPresent || p.SevenDayPresent {
		t.Error("windows should be absent for empty payload")
	}
}

func TestParseStatuslinePayloadPartialRateLimits(t *testing.T) {
	p, err := ParseStatusline(strings.NewReader(`{
		"rate_limits": {
			"five_hour": {"used_percentage": 47.2, "resets_at": 1745000000}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !p.FiveHourPresent {
		t.Error("five_hour should be present")
	}
	if p.SevenDayPresent {
		t.Error("seven_day should be absent")
	}
}

func BenchmarkParseStatusline(b *testing.B) {
	fixture := `{"session_id":"c787e54c","transcript_path":"/home/jin/.claude/X.jsonl","cwd":"/home/jin/repos/foo","model":{"id":"claude-opus-4-7"},"rate_limits":{"five_hour":{"used_percentage":47.2,"resets_at":1745000000},"seven_day":{"used_percentage":18.4,"resets_at":1745432000}}}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseStatusline(strings.NewReader(fixture))
	}
}
