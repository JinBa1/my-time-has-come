package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JinBa1/mthc/internal/state"
)

func runDismiss() error {
	hardOnly := hasArg("--hard")
	softOnly := hasArg("--soft")
	dryRun := hasArg("--dry-run")

	if hardOnly && softOnly {
		return fmt.Errorf("cannot use --hard and --soft together")
	}

	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".config", "mthc", "state.json")

	if dryRun {
		s, _ := state.Load(statePath)
		fmt.Println("Would disarm:")
		if !softOnly {
			if s.PolicyState.HardTriggeredForResetsAt != nil {
				fmt.Printf("  hard gate (resets_at=%d)\n", *s.PolicyState.HardTriggeredForResetsAt)
			} else {
				fmt.Println("  hard gate (already disarmed)")
			}
		}
		if !hardOnly {
			count := 0
			for _, sess := range s.Sessions {
				if sess.SoftInjectedForResetsAt != nil {
					count++
				}
			}
			fmt.Printf("  soft injection (%d sessions)\n", count)
		}
		return nil
	}

	return state.Update(statePath, func(s *state.State) error {
		now := time.Now().UTC()
		if !softOnly { // --hard or default
			s.PolicyState.HardTriggeredForResetsAt = nil
			fmt.Println("Hard gate disarmed.")
		}
		if !hardOnly { // --soft or default
			for _, sess := range s.Sessions {
				sess.SoftInjectedForResetsAt = nil
			}
			fmt.Println("Soft injection cleared for all sessions.")
		}
		s.PolicyState.DismissedAt = &now
		return nil
	})
}

func hasArg(name string) bool {
	for _, arg := range os.Args {
		if strings.EqualFold(arg, name) {
			return true
		}
	}
	return false
}
