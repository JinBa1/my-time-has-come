package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/state"
)

func runStatus() error {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "mthc", "config.toml")
	statePath := filepath.Join(home, ".config", "mthc", "state.json")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mthc: %v\n", err)
		os.Exit(1)
	}
	s, err := state.Load(statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mthc: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("mthc status")
	fmt.Println()

	// Install status
	if cfg.Internal.InstalledAt != "" {
		fmt.Printf("  Installed:     %s\n", cfg.Internal.InstalledAt)
		fmt.Printf("  Version:       %s\n", cfg.Internal.MthcVersion)
	} else {
		fmt.Println("  Installed:     no")
	}

	fmt.Println()
	printWindowStatus("5-hour", s.AccountWindow.FiveHour)
	fmt.Println()
	printWindowStatus("7-day", s.AccountWindow.SevenDay)

	fmt.Println()
	fmt.Printf("Policy:         enabled=%v\n", cfg.Policy.Enabled)

	fmt.Println()
	fmt.Println("Thresholds:")
	fmt.Printf("  five_hour: enabled=%v soft=%.0f%% hard=%.0f%%\n", cfg.Thresholds.FiveHour.Enabled, cfg.Thresholds.FiveHour.SoftPct, cfg.Thresholds.FiveHour.HardPct)
	fmt.Printf("  seven_day: enabled=%v soft=%.0f%% hard=%.0f%%\n", cfg.Thresholds.SevenDay.Enabled, cfg.Thresholds.SevenDay.SoftPct, cfg.Thresholds.SevenDay.HardPct)

	// Policy state
	fmt.Println()
	fmt.Println("Policy state:")
	fmt.Println("  Hard gates:")
	printHardGateStatus("five_hour", s.AccountWindow.FiveHour, s.PolicyState.HardTriggeredByWindow)
	printHardGateStatus("seven_day", s.AccountWindow.SevenDay, s.PolicyState.HardTriggeredByWindow)
	if s.PolicyState.DismissedAt != nil {
		fmt.Printf("  Last dismiss:  %s\n", s.PolicyState.DismissedAt.Format(time.RFC3339))
	}
	if len(s.PolicyState.HandoffPathsByWindow) > 0 {
		fmt.Println("  Handoffs:")
		printHandoffPaths(s.PolicyState.HandoffPathsByWindow)
	}

	// Active sessions
	fmt.Println()
	fmt.Printf("Sessions:       %d registered\n", len(s.Sessions))
	now := time.Now().UTC()
	sessionIDs := make([]string, 0, len(s.Sessions))
	for id := range s.Sessions {
		sessionIDs = append(sessionIDs, id)
	}
	sort.Strings(sessionIDs)
	for _, id := range sessionIDs {
		sess := s.Sessions[id]
		if sess == nil {
			continue
		}
		active := sess.IsActive(now, cfg.Statusline.RefreshIntervalSeconds)
		status := "active"
		if !active {
			status = "stale"
		}
		fmt.Printf("  %s: %s  model=%s  soft-injected=%s  last_seen=%s\n",
			id, status, sess.ModelID, softInjectedStatus(sess), sess.LastSeenAt.Format(time.RFC3339))
	}

	return nil
}

func printWindowStatus(label string, w state.WindowObservation) {
	fmt.Printf("%s window:\n", label)
	if w.Absent || w.ResetsAt == 0 {
		fmt.Println("  No data yet")
		return
	}
	fmt.Printf("  Usage:         %.1f%%\n", w.UsedPercentage)
	fmt.Printf("  Resets at:     %s\n", time.Unix(w.ResetsAt, 0).UTC().Format(time.RFC3339))
	fmt.Printf("  Last observed: %s\n", w.LastObservedAt.Format(time.RFC3339))
	fmt.Printf("  Source:        %s\n", w.Source)
}

func printHardGateStatus(windowID string, w state.WindowObservation, triggeredByWindow map[string]int64) {
	triggered, ok := triggeredByWindow[windowID]
	switch {
	case ok && !w.Absent && w.ResetsAt != 0 && triggered == w.ResetsAt:
		fmt.Printf("  %s: ARMED (resets_at=%d)\n", windowID, triggered)
	case ok:
		fmt.Printf("  %s: disarmed (stale trigger resets_at=%d)\n", windowID, triggered)
	default:
		fmt.Printf("  %s: disarmed\n", windowID)
	}
}

func softInjectedStatus(sess *state.Session) string {
	if sess == nil || len(sess.SoftInjectedByWindow) == 0 {
		return "no"
	}
	parts := make([]string, 0, 2)
	for _, id := range []string{"five_hour", "seven_day"} {
		if resetsAt, ok := sess.SoftInjectedByWindow[id]; ok {
			parts = append(parts, fmt.Sprintf("%s:%d", id, resetsAt))
		}
	}
	if len(parts) == 0 {
		return "no"
	}
	return strings.Join(parts, ",")
}

func printHandoffPaths(pathsByWindow map[string]map[string]string) {
	windowIDs := orderedWindowIDs(pathsByWindow)
	for _, windowID := range windowIDs {
		sessionPaths := pathsByWindow[windowID]
		sessionIDs := make([]string, 0, len(sessionPaths))
		for id := range sessionPaths {
			sessionIDs = append(sessionIDs, id)
		}
		sort.Strings(sessionIDs)
		for _, id := range sessionIDs {
			fmt.Printf("    %s/%s: %s\n", windowID, id, sessionPaths[id])
		}
	}
}

func orderedWindowIDs[T any](m map[string]T) []string {
	seen := make(map[string]bool, len(m))
	ids := make([]string, 0, len(m))
	for _, id := range []string{"five_hour", "seven_day"} {
		if _, ok := m[id]; ok {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	var rest []string
	for id := range m {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	return append(ids, rest...)
}
