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

// Decide is a pure function of state, config, and current time.
func Decide(s *state.State, c *config.Config, now time.Time) (map[string]*state.Session, Decision) {
	fw := s.AccountWindow.FiveHour
	if fw.Absent || fw.ResetsAt == 0 {
		return nil, NoAction
	}

	active := activeSessions(s, c, now)
	if len(active) == 0 {
		return nil, NoAction
	}

	// Hard path (checked first — hard takes precedence)
	if fw.UsedPercentage >= c.Thresholds.HardPct {
		if s.PolicyState.HardTriggeredForResetsAt == nil || *s.PolicyState.HardTriggeredForResetsAt != fw.ResetsAt {
			return active, HardStop
		}
		// Already triggered for this window — fall through to soft path
	}

	// Soft path
	if fw.UsedPercentage >= c.Thresholds.SoftPct {
		pending := make(map[string]*state.Session)
		for id, sess := range active {
			if sess.SoftInjectedForResetsAt == nil || *sess.SoftInjectedForResetsAt != fw.ResetsAt {
				pending[id] = sess
			}
		}
		if len(pending) > 0 {
			return pending, SoftInject
		}
	}

	return nil, NoAction
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
