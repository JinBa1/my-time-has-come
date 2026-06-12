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

type WindowDef struct {
	ID       string
	Label    string
	Duration time.Duration
}

var windows = []WindowDef{
	{ID: WindowFiveHour, Label: "5-hour", Duration: 5 * time.Hour},
	{ID: WindowSevenDay, Label: "7-day", Duration: 7 * 24 * time.Hour},
}

func Windows() []WindowDef {
	return append([]WindowDef(nil), windows...)
}

func WindowByID(id string) (WindowDef, bool) {
	for _, w := range windows {
		if w.ID == id {
			return w, true
		}
	}
	return WindowDef{}, false
}

func WindowDurationSeconds(id string) int64 {
	w, ok := WindowByID(id)
	if !ok {
		return int64((5 * time.Hour).Seconds())
	}
	return int64(w.Duration.Seconds())
}

func WindowThreshold(c *config.Config, id string) config.WindowThresholdConfig {
	switch id {
	case WindowSevenDay:
		return c.Thresholds.SevenDay
	default:
		return c.Thresholds.FiveHour
	}
}

func WindowObservation(s *state.State, id string) state.WindowObservation {
	switch id {
	case WindowSevenDay:
		return s.AccountWindow.SevenDay
	default:
		return s.AccountWindow.FiveHour
	}
}

type Trigger struct {
	WindowID       string
	WindowLabel    string
	UsedPercentage float64
	ResetsAt       int64
	Severity       Decision
}

type Result struct {
	Decision Decision
	Trigger  Trigger // Zero value when Decision is NoAction.
}

type candidate struct {
	def       WindowDef
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
		if s.PolicyState.HardTriggeredByWindow[hard.def.ID] != hard.window.ResetsAt {
			return active, Result{Decision: HardStop, Trigger: triggerFromCandidate(*hard, HardStop)}
		}
	}

	soft := selectCrossed(candidates, SoftInject)
	if soft != nil {
		pending := make(map[string]*state.Session)
		for id, sess := range active {
			if sess.SoftInjectedByWindow[soft.def.ID] != soft.window.ResetsAt {
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
	observed := make([]candidate, 0, len(windows))
	for _, def := range windows {
		cand := candidate{
			def:       def,
			window:    WindowObservation(s, def.ID),
			threshold: WindowThreshold(c, def.ID),
		}
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
		return threshold.Hard
	}
	return threshold.Soft
}

func triggerFromCandidate(c candidate, severity Decision) Trigger {
	return Trigger{
		WindowID:       c.def.ID,
		WindowLabel:    c.def.Label,
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
