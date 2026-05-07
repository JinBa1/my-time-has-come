package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JinBa1/my-time-has-come/internal/state"
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
			if len(s.PolicyState.HardTriggeredByWindow) > 0 {
				fmt.Printf("  hard gates (%d windows)\n", len(s.PolicyState.HardTriggeredByWindow))
			} else {
				fmt.Println("  hard gates (already disarmed)")
			}
		}
		if !hardOnly {
			count := 0
			for _, sess := range s.Sessions {
				if len(sess.SoftInjectedByWindow) > 0 {
					count++
				}
			}
			fmt.Printf("  soft injections (%d sessions)\n", count)
		}
		return nil
	}

	return state.Update(statePath, func(s *state.State) error {
		now := time.Now().UTC()
		if !softOnly { // --hard or default
			s.PolicyState.HardTriggeredByWindow = map[string]int64{}
			s.PolicyState.HandoffWrittenAtByWindow = map[string]time.Time{}
			s.PolicyState.HandoffPathsByWindow = map[string]map[string]string{}
			fmt.Println("Hard gates disarmed.")
		}
		if !hardOnly { // --soft or default
			for _, sess := range s.Sessions {
				sess.SoftInjectedByWindow = map[string]int64{}
			}
			fmt.Println("Soft injections cleared for all sessions.")
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
