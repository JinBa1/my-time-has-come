package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/playback"
	"github.com/JinBa1/mthc/internal/policy"
)

func runPlayback() error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: mthc playback replay <file|dir>...")
	}
	subcmd := os.Args[2]
	switch subcmd {
	case "replay":
		return runPlaybackReplay()
	default:
		return fmt.Errorf("unknown playback subcommand: %s", subcmd)
	}
}

func runPlaybackReplay() error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: mthc playback replay [--config-from-recording] <file|dir>...")
	}

	useEmbeddedCfg := false
	var files []string
	for _, arg := range os.Args[3:] {
		if arg == "--config-from-recording" {
			useEmbeddedCfg = true
			continue
		}
		files = append(files, arg)
	}
	if len(files) == 0 {
		return fmt.Errorf("usage: mthc playback replay [--config-from-recording] <file|dir>...")
	}

	var cfg *config.Config
	if useEmbeddedCfg {
		metaCfg, err := loadConfigFromRecording(files)
		if err != nil {
			return fmt.Errorf("load embedded config: %w", err)
		}
		cfg = metaCfg
	} else {
		var err error
		cfg, err = config.Resolve()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}

	steps, err := playback.Replay(files, cfg)
	if err != nil {
		return err
	}

	for i, s := range steps {
		fmt.Printf("--- Step %d ---\n", i+1)
		fmt.Printf("  ts:        %s\n", s.TS.Format("2006-01-02T15:04:05Z"))
		fmt.Printf("  type:      %s", s.EventType)
		if s.EventName != "" {
			fmt.Printf(" (%s)", s.EventName)
		}
		fmt.Println()
		fmt.Printf("  session:   %s\n", s.SessionID)
		fmt.Printf("  decision:  %s\n", decisionName(s.Decision))
		if len(s.SideEffects) > 0 {
			fmt.Printf("  effects:   ")
			for _, se := range s.SideEffects {
				fmt.Printf("%s:%s  ", se.Type, se.SessionID)
			}
			fmt.Println()
		}
		if s.Response.PermissionDecision != "" {
			fmt.Printf("  response:  %s\n", s.Response.PermissionDecision)
		}
		if s.Response.HookSpecificOutput != nil {
			out, _ := json.Marshal(s.Response.HookSpecificOutput)
			fmt.Printf("  output:    %s\n", string(out))
		}
		sessions := 0
		for range s.State.Sessions {
			sessions++
		}
		fmt.Printf("  sessions:  %d\n", sessions)
		fmt.Printf("  usage:     %.1f%%\n", s.State.AccountWindow.FiveHour.UsedPercentage)
	}

	fmt.Printf("\n%d steps replayed\n", len(steps))
	return nil
}

func decisionName(d policy.Decision) string {
	switch d {
	case policy.NoAction:
		return "NoAction"
	case policy.SoftInject:
		return "SoftInject"
	case policy.HardStop:
		return "HardStop"
	default:
		return fmt.Sprintf("Unknown(%d)", d)
	}
}

// loadConfigFromRecording loads config from meta.toml in a recording directory.
// C2 fix: meta.toml is TOML-encoded (written by runRecordStart's
// toml.NewEncoder), so it must be decoded as TOML. Config structs only
// have toml tags — JSON decode would leave all fields zero-valued.
func loadConfigFromRecording(files []string) (*config.Config, error) {
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.IsDir() {
			metaPath := filepath.Join(f, "meta.toml")
			data, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}
			cfg := config.Defaults()
			if _, err := toml.Decode(string(data), cfg); err != nil {
				return nil, fmt.Errorf("decode meta config: %w", err)
			}
			return cfg, nil
		}
	}
	return nil, fmt.Errorf("no directory with meta.toml found in arguments")
}
