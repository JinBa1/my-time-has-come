package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/JinBa1/mthc/internal/config"
	"github.com/JinBa1/mthc/internal/state"
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

type result struct {
	Severity    severity          `json:"severity"`
	Check       string            `json:"check"`
	Message     string            `json:"message"`
	Details     map[string]string `json:"details,omitempty"`
	Remediation string            `json:"remediation,omitempty"`
}

type doctorReport struct {
	Version     int            `json:"version"`
	Environment map[string]any `json:"environment"`
	Results     []result       `json:"results"`
	Summary     map[string]int `json:"summary"`
}

type checkContext struct {
	home           string
	cfg            *config.Config // may be nil if configErr != nil
	state          *state.State   // may be nil if stateErr != nil or stateAbsent
	configErr      error          // nil if config.toml loaded OK
	configAbsent   bool           // true if config.toml does not exist
	stateErr       error          // nil if state.json loaded OK
	stateAbsent    bool           // true if state.json does not exist (not an error)
	mergedSettings map[string]any
	settingsScope  map[string]string
	selfPath       string
	mthcOnPath     string
	claudeVersion  string
	hasStatusline  bool
	hasHooks       bool
}

type checkFunc func(ctx checkContext) result

func buildCheckContext(home string, cfg *config.Config, s *state.State) checkContext {
	selfPath, _ := os.Executable()
	selfPath, _ = filepath.EvalSymlinks(selfPath)
	mthcOnPath, _ := exec.LookPath("mthc")
	if mthcOnPath != "" {
		mthcOnPath, _ = filepath.EvalSymlinks(mthcOnPath)
	}
	var hasStatusline, hasHooks bool
	if cfg != nil {
		hasStatusline = cfg.Internal.ChainedStatusline != nil
		hasHooks = cfg.Internal.InstalledHookCommand != ""
	}
	return checkContext{
		home:          home,
		cfg:           cfg,
		state:         s,
		selfPath:      selfPath,
		mthcOnPath:    mthcOnPath,
		hasStatusline: hasStatusline,
		hasHooks:      hasHooks,
	}
}

var doctorJSON, doctorStrict bool

func runDoctor() (rerr error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "doctor crashed:", r)
			os.Exit(2)
		}
	}()

	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.BoolVar(&doctorJSON, "json", false, "machine-readable JSON output")
	fs.BoolVar(&doctorStrict, "strict", false, "warnings cause exit 1")
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
			if s.PolicyState.HandoffPaths == nil {
				s.PolicyState.HandoffPaths = make(map[string]string)
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

	ctx.mergedSettings, ctx.settingsScope = mergeClaudeSettings(home)
	ctx.claudeVersion = detectClaudeVersion()

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

	if doctorJSON {
		fmt.Println(formatJSON(ctx, results))
	} else {
		fmt.Print(formatText(ctx, results))
	}

	rank := maxSeverityRank(results)
	if rank >= sevError.rank() || (doctorStrict && rank >= sevWarn.rank()) {
		os.Exit(1)
	}
	return nil
}

func mergeClaudeSettings(home string) (map[string]any, map[string]string) {
	return nil, nil
}

func detectClaudeVersion() string {
	return ""
}

func formatText(ctx checkContext, results []result) string {
	return ""
}

func formatJSON(ctx checkContext, results []result) string {
	return ""
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

func parseShimPath(cmd, subcommand string) string {
	cmd = strings.TrimSpace(cmd)
	parts := strings.Fields(cmd)
	if len(parts) < 2 || parts[1] != subcommand {
		return ""
	}
	return parts[0]
}

// Placeholder check functions — implemented in later tasks.
func checkBinary(ctx checkContext) result {
	if ctx.mthcOnPath == "" {
		return result{
			Severity:    sevError,
			Check:       "mthc.binary",
			Message:     "mthc not found on PATH",
			Remediation: "install mthc or add it to your PATH",
		}
	}
	return result{
		Severity: sevPass,
		Check:    "mthc.binary",
		Message:  ctx.mthcOnPath,
	}
}
func checkInstall(ctx checkContext) result {
	return result{Severity: sevPass, Check: "mthc.install"}
}
func checkInstallDrift(ctx checkContext) result {
	return result{Severity: sevPass, Check: "mthc.install_drift"}
}
func checkConfig(ctx checkContext) result {
	return result{Severity: sevPass, Check: "mthc.config"}
}
func checkSettingsPresent(ctx checkContext) result {
	return result{Severity: sevPass, Check: "claude.settings_present"}
}
func checkDisableAllHooks(ctx checkContext) result {
	return result{Severity: sevPass, Check: "claude.disable_all_hooks"}
}
func checkStatuslineShadow(ctx checkContext) result {
	return result{Severity: sevPass, Check: "claude.statusline_shadow"}
}
