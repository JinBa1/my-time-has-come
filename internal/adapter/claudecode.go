package adapter

import (
	"encoding/json"
	"io"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/policy"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

type StatuslinePayload struct {
	SessionID        string
	TranscriptPath   string
	ModelID          string
	CWD              string
	FiveHourPresent  bool
	FiveHourUsedPct  float64
	FiveHourResetsAt int64
	SevenDayPresent  bool
	SevenDayUsedPct  float64
	SevenDayResetsAt int64
}

type statuslineRaw struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Model          struct {
		ID string `json:"id"`
	} `json:"model"`
	RateLimits *struct {
		FiveHour *windowRaw `json:"five_hour"`
		SevenDay *windowRaw `json:"seven_day"`
	} `json:"rate_limits"`
}

type windowRaw struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

// Observations converts the parsed payload into keyed observations —
// one per window present in the payload. Absent windows are handled by
// core's known-window iteration, not by the adapter.
func (p StatuslinePayload) Observations(harnessID string, now time.Time) []state.Observation {
	var obs []state.Observation
	add := func(windowID string, pct float64, resetsAt int64) {
		obs = append(obs, state.Observation{
			Source:     state.SourceStatusline,
			Harness:    harnessID,
			Unit:       state.UnitPercent,
			Value:      pct,
			Window:     state.WindowRef{ID: windowID, ResetsAt: resetsAt},
			Scope:      state.ScopeAccount,
			ObservedAt: now,
		})
	}
	if p.FiveHourPresent {
		add(policy.WindowFiveHour, p.FiveHourUsedPct, p.FiveHourResetsAt)
	}
	if p.SevenDayPresent {
		add(policy.WindowSevenDay, p.SevenDayUsedPct, p.SevenDayResetsAt)
	}
	return obs
}

// HasRateLimits reports whether the payload carried a rate_limits object
// (the weak Claude-shape hint for harness detection).
func (p StatuslinePayload) HasRateLimits() bool {
	return p.FiveHourPresent || p.SevenDayPresent
}

func ParseStatusline(r io.Reader) (StatuslinePayload, error) {
	var raw statuslineRaw
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return StatuslinePayload{}, err
	}

	p := StatuslinePayload{
		SessionID:      raw.SessionID,
		TranscriptPath: raw.TranscriptPath,
		CWD:            raw.CWD,
		ModelID:        raw.Model.ID,
	}

	if raw.RateLimits == nil {
		return p, nil
	}

	if raw.RateLimits.FiveHour != nil && raw.RateLimits.FiveHour.ResetsAt != 0 {
		p.FiveHourPresent = true
		p.FiveHourUsedPct = raw.RateLimits.FiveHour.UsedPercentage
		p.FiveHourResetsAt = raw.RateLimits.FiveHour.ResetsAt
	}
	if raw.RateLimits.SevenDay != nil && raw.RateLimits.SevenDay.ResetsAt != 0 {
		p.SevenDayPresent = true
		p.SevenDayUsedPct = raw.RateLimits.SevenDay.UsedPercentage
		p.SevenDayResetsAt = raw.RateLimits.SevenDay.ResetsAt
	}

	return p, nil
}
