package adapter

import (
	"encoding/json"
	"io"
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
	RateLimitsAbsent bool
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
		p.RateLimitsAbsent = true
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
