package policy

import (
	"time"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

type Decision int

const (
	NoAction Decision = iota
	SoftInject
	HardStop
)

const (
	WindowFiveHour = "five_hour"
	WindowSevenDay = "seven_day"
)

type Trigger struct {
	WindowID       string
	WindowLabel    string
	UsedPercentage float64
	ResetsAt       int64
	Severity       Decision
}

type Result struct {
	Decision Decision
	Trigger  Trigger
}

type candidate struct {
	id        string
	label     string
	window    state.WindowObservation
	threshold config.WindowThresholdConfig
}

// Decide is a pure function of state, config, and current time.
func Decide(s *state.State, c *config.Config, now time.Time) (map[string]*state.Session, Result) {
	if !c.Policy.Enabled {
		return nil, Result{Decision: NoAction}
	}

	active := activeSessions(s, c, now)
	if len(active) == 0 {
		return nil, Result{Decision: NoAction}
	}

	candidates := enabledObservedWindows(s, c)

	hard := selectCrossed(candidates, HardStop)
	if hard != nil {
		if s.PolicyState.HardTriggeredByWindow[hard.id] != hard.window.ResetsAt {
			return active, Result{Decision: HardStop, Trigger: triggerFromCandidate(*hard, HardStop)}
		}
	}

	soft := selectCrossed(candidates, SoftInject)
	if soft != nil {
		pending := make(map[string]*state.Session)
		for id, sess := range active {
			if sess.SoftInjectedByWindow == nil {
				sess.SoftInjectedByWindow = make(map[string]int64)
			}
			if sess.SoftInjectedByWindow[soft.id] != soft.window.ResetsAt {
				pending[id] = sess
			}
		}
		if len(pending) > 0 {
			return pending, Result{Decision: SoftInject, Trigger: triggerFromCandidate(*soft, SoftInject)}
		}
	}

	return nil, Result{Decision: NoAction}
}

func enabledObservedWindows(s *state.State, c *config.Config) []candidate {
	candidates := []candidate{
		{
			id:        WindowFiveHour,
			label:     "5-hour",
			window:    s.AccountWindow.FiveHour,
			threshold: c.Thresholds.FiveHour,
		},
		{
			id:        WindowSevenDay,
			label:     "7-day",
			window:    s.AccountWindow.SevenDay,
			threshold: c.Thresholds.SevenDay,
		},
	}
	observed := make([]candidate, 0, len(candidates))
	for _, cand := range candidates {
		if !cand.threshold.Enabled || cand.window.Absent || cand.window.ResetsAt == 0 {
			continue
		}
		observed = append(observed, cand)
	}
	return observed
}

func selectCrossed(candidates []candidate, severity Decision) *candidate {
	var selected *candidate
	var largestOvershoot float64
	for i := range candidates {
		threshold := thresholdForSeverity(candidates[i].threshold, severity)
		if candidates[i].window.UsedPercentage < threshold {
			continue
		}
		overshoot := candidates[i].window.UsedPercentage - threshold
		if selected == nil || overshoot > largestOvershoot {
			selected = &candidates[i]
			largestOvershoot = overshoot
		}
	}
	return selected
}

func thresholdForSeverity(threshold config.WindowThresholdConfig, severity Decision) float64 {
	if severity == HardStop {
		return threshold.HardPct
	}
	return threshold.SoftPct
}

func triggerFromCandidate(c candidate, severity Decision) Trigger {
	return Trigger{
		WindowID:       c.id,
		WindowLabel:    c.label,
		UsedPercentage: c.window.UsedPercentage,
		ResetsAt:       c.window.ResetsAt,
		Severity:       severity,
	}
}

func activeSessions(s *state.State, c *config.Config, now time.Time) map[string]*state.Session {
	result := make(map[string]*state.Session)
	for id, sess := range s.Sessions {
		if sess.IsActive(now, c.Statusline.RefreshIntervalSeconds) {
			result[id] = sess
		}
	}
	return result
}
