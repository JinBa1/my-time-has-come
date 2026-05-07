package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/JinBa1/my-time-has-come/internal/config"
	"github.com/JinBa1/my-time-has-come/internal/state"
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

func (s *severity) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case "pass":
		*s = sevPass
	case "info":
		*s = sevInfo
	case "warn":
		*s = sevWarn
	case "error":
		*s = sevError
	case "skipped":
		*s = sevSkipped
	default:
		return fmt.Errorf("unknown severity: %q", str)
	}
	return nil
}

type result struct {
	Severity    severity          `json:"severity"`
	Check       string            `json:"check"`
	Message     string            `json:"message"`
	Details     map[string]string `json:"details,omitempty"`
	Remediation string            `json:"remediation,omitempty"`
}

type installManifest struct {
	Statusline bool `json:"statusline"`
	Hooks      bool `json:"hooks"`
}

type doctorEnvironment struct {
	MthcVersion       string          `json:"mthc_version"`
	ConfigPath        string          `json:"config_path"`
	StatePath         string          `json:"state_path"`
	InstallManifest   installManifest `json:"install_manifest"`
	ClaudeCodeVersion string          `json:"claude_code_version,omitempty"`
}

type doctorSummary struct {
	Pass    int `json:"pass"`
	Info    int `json:"info"`
	Warn    int `json:"warn"`
	Error   int `json:"error"`
	Skipped int `json:"skipped"`
}

type doctorReport struct {
	Version     int               `json:"version"`
	Environment doctorEnvironment `json:"environment"`
	Results     []result          `json:"results"`
	Summary     doctorSummary     `json:"summary"`
}

type settingsError struct {
	path  string
	scope string
	err   error
}

type checkContext struct {
	home           string
	cfg            *config.Config // may be nil if configErr != nil
	state          *state.State   // may be nil if stateErr != nil or stateAbsent
	configData     []byte         // raw config.toml bytes, when read succeeds
	configErr      error          // nil if config.toml loaded OK
	configAbsent   bool           // true if config.toml does not exist
	stateErr       error          // nil if state.json loaded OK
	stateAbsent    bool           // true if state.json does not exist (not an error)
	mergedSettings map[string]any
	settingsScope  map[string]string
	settingsErrors []settingsError // non-nil entries for malformed/unreadable settings
	selfPath       string
	mthcOnPath     string
	claudeVersion  string
	hasStatusline  bool
	hasHooks       bool
}

type checkFunc func(ctx checkContext) result

type doctorOptions struct {
	jsonOutput bool
	strict     bool
}

var managedSettingsPath = "/etc/claude-code/managed-settings.json"

func executeDoctorChecks(ctx checkContext, strict bool) ([]result, int) {
	checks := []checkFunc{
		checkBinary,
		checkInstall,
		checkInstallDrift,
		checkConfig,
		checkSettingsPresent,
		checkDisableAllHooks,
		checkStatuslineShadow,
	}
	var results []result
	for _, cf := range checks {
		results = append(results, cf(ctx))
	}
	rank := maxSeverityRank(results)
	switch {
	case rank >= sevError.rank():
		return results, 1
	case strict && rank >= sevWarn.rank():
		return results, 1
	default:
		return results, 0
	}
}

func runDoctor() (rerr error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "doctor crashed:", r)
			os.Exit(2)
		}
	}()

	var opts doctorOptions
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.BoolVar(&opts.jsonOutput, "json", false, "machine-readable JSON output")
	fs.BoolVar(&opts.strict, "strict", false, "warnings cause exit 1")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "mthc")
	cfgPath := filepath.Join(cfgDir, "config.toml")
	statePath := filepath.Join(cfgDir, "state.json")

	var ctx checkContext
	ctx.home = home

	// Tolerant config load — side-effect-free, never crashes on missing file (C6)
	if data, err := os.ReadFile(cfgPath); err != nil {
		if os.IsNotExist(err) {
			ctx.configAbsent = true
		} else {
			ctx.configErr = err
		}
	} else {
		ctx.configData = data
		c := config.Defaults()
		if _, err := toml.Decode(string(data), c); err != nil {
			ctx.configErr = err
		} else {
			ctx.cfg = c
		}
	}

	// Tolerant state load — side-effect-free (C1). Does NOT call state.Load.
	if data, err := os.ReadFile(statePath); err != nil {
		if os.IsNotExist(err) {
			ctx.stateAbsent = true
		} else {
			ctx.stateErr = err
		}
	} else {
		var s state.State
		if err := json.Unmarshal(data, &s); err != nil {
			ctx.stateErr = err
		} else {
			if s.Sessions == nil {
				s.Sessions = make(map[string]*state.Session)
			}
			for id, sess := range s.Sessions {
				if sess == nil {
					s.Sessions[id] = &state.Session{SoftInjectedByWindow: make(map[string]int64)}
					continue
				}
				if sess.SoftInjectedByWindow == nil {
					sess.SoftInjectedByWindow = make(map[string]int64)
				}
			}
			if s.PolicyState.HardTriggeredByWindow == nil {
				s.PolicyState.HardTriggeredByWindow = make(map[string]int64)
			}
			if s.PolicyState.HandoffWrittenAtByWindow == nil {
				s.PolicyState.HandoffWrittenAtByWindow = make(map[string]time.Time)
			}
			if s.PolicyState.HandoffPathsByWindow == nil {
				s.PolicyState.HandoffPathsByWindow = make(map[string]map[string]string)
			}
			if s.TranscriptCursors == nil {
				s.TranscriptCursors = make(map[string]*state.CursorEntry)
			}
			ctx.state = &s
		}
	}

	// Build remaining context fields
	if ctx.cfg != nil {
		ctx.hasStatusline = ctx.cfg.Internal.ChainedStatusline != nil
		ctx.hasHooks = ctx.cfg.Internal.InstalledHookCommand != ""
	}
	selfPath, _ := os.Executable()
	ctx.selfPath, _ = filepath.EvalSymlinks(selfPath)
	ctx.mthcOnPath, _ = exec.LookPath("mthc")
	if ctx.mthcOnPath != "" {
		ctx.mthcOnPath, _ = filepath.EvalSymlinks(ctx.mthcOnPath)
	}

	ctx.mergedSettings, ctx.settingsScope, ctx.settingsErrors = mergeClaudeSettings(home)
	ctx.claudeVersion = detectClaudeVersion()

	results, exitCode := executeDoctorChecks(ctx, opts.strict)

	if opts.jsonOutput {
		fmt.Println(formatJSON(ctx, results))
	} else {
		fmt.Print(formatText(ctx, results))
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

func detectClaudeVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func maxSeverityRank(results []result) int {
	max := 0
	for _, r := range results {
		if r.Severity.rank() > max {
			max = r.Severity.rank()
		}
	}
	return max
}
