package playback

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/JinBa1/my-time-has-come/internal/core"
	"github.com/JinBa1/my-time-has-come/internal/policy"
)

// TraceStep is the state-free, schema-stable projection of a replay Step.
// It is the equivalence contract for the unit-aware observation refactor:
// traces over identical inputs must be byte-identical before and after.
type TraceStep struct {
	EventType   string            `json:"event_type"`
	EventName   string            `json:"event_name,omitempty"`
	SessionID   string            `json:"session_id"`
	Decision    string            `json:"decision"`
	WindowID    string            `json:"window_id,omitempty"`
	Response    core.HookResponse `json:"response"`
	SideEffects []TraceEffect     `json:"side_effects,omitempty"`
}

type TraceEffect struct {
	Type          string `json:"type"`
	SessionID     string `json:"session_id"`
	WindowID      string `json:"window_id"`
	ResetsAt      int64  `json:"resets_at"`
	Path          string `json:"path,omitempty"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
}

func decisionString(d policy.Decision) string {
	switch d {
	case policy.SoftInject:
		return "soft_inject"
	case policy.HardStop:
		return "hard_stop"
	default:
		return "no_action"
	}
}

// Trace projects replay steps into the state-free equivalence format.
func Trace(steps []Step) []TraceStep {
	out := make([]TraceStep, 0, len(steps))
	for _, st := range steps {
		ts := TraceStep{
			EventType: st.EventType,
			EventName: st.EventName,
			SessionID: st.SessionID,
			Decision:  decisionString(st.Decision),
			WindowID:  st.Trigger.WindowID,
			Response:  st.Response,
		}
		for _, se := range st.SideEffects {
			eff := TraceEffect{
				Type:      se.Type,
				SessionID: se.SessionID,
				WindowID:  se.WindowID,
				ResetsAt:  se.ResetsAt,
				Path:      se.Path,
			}
			if se.Content != "" {
				sum := sha256.Sum256([]byte(se.Content))
				eff.ContentSHA256 = hex.EncodeToString(sum[:])
			}
			ts.SideEffects = append(ts.SideEffects, eff)
		}
		out = append(out, ts)
	}
	return out
}
