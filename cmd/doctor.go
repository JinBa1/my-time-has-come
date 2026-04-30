package cmd

import (
	"encoding/json"
	"fmt"
)

type severity int

const (
	sevPass severity = iota
	sevInfo
	sevWarn
	sevError
	sevSkipped
)

func (s severity) rank() int {
	switch s {
	case sevWarn:
		return 1
	case sevError:
		return 2
	default:
		return 0
	}
}

func (s severity) String() string {
	switch s {
	case sevPass:
		return "pass"
	case sevInfo:
		return "info"
	case sevWarn:
		return "warn"
	case sevError:
		return "error"
	case sevSkipped:
		return "skipped"
	}
	return "unknown"
}

func (s severity) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

type result struct {
	Severity    severity          `json:"severity"`
	Check       string            `json:"check"`
	Message     string            `json:"message"`
	Details     map[string]string `json:"details,omitempty"`
	Remediation string            `json:"remediation,omitempty"`
}

type doctorReport struct {
	Version     int            `json:"version"`
	Environment map[string]any `json:"environment"`
	Results     []result       `json:"results"`
	Summary     map[string]int `json:"summary"`
}

func runDoctor() error {
	return fmt.Errorf("not implemented")
}
