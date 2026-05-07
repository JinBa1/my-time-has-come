package adapter

import (
	"strings"
	"testing"
)

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
	if !p.RateLimitsAbsent {
		t.Error("rate_limits should be absent for empty payload")
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
	if p.RateLimitsAbsent {
		t.Error("rate_limits root should be present")
	}
}

func BenchmarkParseStatusline(b *testing.B) {
	fixture := `{"session_id":"c787e54c","transcript_path":"/home/jin/.claude/X.jsonl","cwd":"/home/jin/repos/foo","model":{"id":"claude-opus-4-7"},"rate_limits":{"five_hour":{"used_percentage":47.2,"resets_at":1745000000},"seven_day":{"used_percentage":18.4,"resets_at":1745432000}}}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseStatusline(strings.NewReader(fixture))
	}
}
