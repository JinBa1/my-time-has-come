package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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

	// 5-hour window
	fw := s.AccountWindow.FiveHour
	fmt.Println()
	fmt.Println("5-hour window:")
	if fw.ResetsAt == 0 {
		fmt.Println("  No data yet")
	} else {
		fmt.Printf("  Usage:         %.1f%%\n", fw.UsedPercentage)
		fmt.Printf("  Resets at:     %s\n", time.Unix(fw.ResetsAt, 0).UTC().Format(time.RFC3339))
		fmt.Printf("  Last observed: %s\n", fw.LastObservedAt.Format(time.RFC3339))
		fmt.Printf("  Source:        %s\n", fw.Source)
	}

	// Thresholds
	fmt.Println()
	fmt.Printf("Thresholds:     soft=%.0f%%  hard=%.0f%%\n", cfg.Thresholds.FiveHour.SoftPct, cfg.Thresholds.FiveHour.HardPct)

	// Policy state
	fmt.Println()
	fmt.Println("Policy state:")
	if s.PolicyState.HardTriggeredForResetsAt != nil && *s.PolicyState.HardTriggeredForResetsAt == fw.ResetsAt {
		fmt.Printf("  Hard gate:     ARMED (resets_at=%d)\n", *s.PolicyState.HardTriggeredForResetsAt)
	} else if s.PolicyState.HardTriggeredForResetsAt != nil {
		fmt.Printf("  Hard gate:     disarmed (stale trigger resets_at=%d)\n", *s.PolicyState.HardTriggeredForResetsAt)
	} else {
		fmt.Println("  Hard gate:     disarmed")
	}
	if s.PolicyState.DismissedAt != nil {
		fmt.Printf("  Last dismiss:  %s\n", s.PolicyState.DismissedAt.Format(time.RFC3339))
	}
	if len(s.PolicyState.HandoffPaths) > 0 {
		fmt.Println("  Handoffs:")
		for id, p := range s.PolicyState.HandoffPaths {
			fmt.Printf("    %s: %s\n", id, p)
		}
	}

	// Active sessions
	fmt.Println()
	fmt.Printf("Sessions:       %d registered\n", len(s.Sessions))
	now := time.Now().UTC()
	for id, sess := range s.Sessions {
		active := sess.IsActive(now, cfg.Statusline.RefreshIntervalSeconds)
		status := "active"
		if !active {
			status = "stale"
		}
		softStatus := "no"
		if sess.SoftInjectedForResetsAt != nil {
			softStatus = fmt.Sprintf("yes (resets_at=%d)", *sess.SoftInjectedForResetsAt)
		}
		fmt.Printf("  %s: %s  model=%s  soft-injected=%s  last_seen=%s\n",
			id, status, sess.ModelID, softStatus, sess.LastSeenAt.Format(time.RFC3339))
	}

	return nil
}
