package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	merged := make(map[string]any)
	scope := make(map[string]string)

	userPath := userClaudeSettingsPath(home)
	loadSettingsLayer(merged, scope, userPath, "user")

	walkSettingsPath(merged, scope, ".claude/settings.json", "project", home)
	walkSettingsPath(merged, scope, ".claude/settings.local.json", "local", home)

	managedPath := "/etc/claude-code/managed-settings.json"
	loadSettingsLayer(merged, scope, managedPath, "managed")

	if len(merged) == 0 {
		return nil, nil
	}
	return merged, scope
}

func userClaudeSettingsPath(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func loadSettingsLayer(merged map[string]any, scope map[string]string, path, layerName string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	layer := make(map[string]any)
	if err := json.Unmarshal(data, &layer); err != nil {
		return false
	}
	for k, v := range layer {
		merged[k] = v
		scope[k] = layerName
	}
	return true
}

func sameFile(a, b string) bool {
	aInfo, errA := os.Stat(a)
	bInfo, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(aInfo, bInfo)
}

func walkSettingsPath(merged map[string]any, scope map[string]string, relPath, layerName, home string) {
	dir, _ := os.Getwd()
	homeResolved, _ := filepath.EvalSymlinks(home)
	userPath := userClaudeSettingsPath(home) // M2: computed once

	for {
		candidate := filepath.Join(dir, relPath)
		if !sameFile(candidate, userPath) {
			loadSettingsLayer(merged, scope, candidate, layerName)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dirResolved, _ := filepath.EvalSymlinks(dir)
		if dirResolved == homeResolved {
			break
		}
		dir = parent
	}
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

func formatText(ctx checkContext, results []result) string {
	colorOn := useColor()
	var b strings.Builder
	b.WriteString("mthc doctor\n")
	b.WriteString("──────────────────────────────────────────────────────\n")

	// INFO lines
	mthcVersion := ""
	if ctx.cfg != nil {
		mthcVersion = ctx.cfg.Internal.MthcVersion
	}
	fmt.Fprintf(&b, "%s %-32s %s\n",
		colorize(sevInfo, fmt.Sprintf("%-9s", "[INFO]"), colorOn),
		"mthc version", mthcVersion)
	if ctx.claudeVersion != "" {
		fmt.Fprintf(&b, "%s %-32s %s\n",
			colorize(sevInfo, fmt.Sprintf("%-9s", "[INFO]"), colorOn),
			"Claude Code version", ctx.claudeVersion)
	}
	cfgDir := filepath.Join(ctx.home, ".config", "mthc")
	fmt.Fprintf(&b, "%s %-32s %s\n",
		colorize(sevInfo, fmt.Sprintf("%-9s", "[INFO]"), colorOn),
		"config path", cfgDir+"/config.toml")
	statePath := cfgDir + "/state.json"
	stateLabel := statePath
	if ctx.stateAbsent {
		stateLabel = statePath + " (not yet created)"
	}
	fmt.Fprintf(&b, "%s %-32s %s\n",
		colorize(sevInfo, fmt.Sprintf("%-9s", "[INFO]"), colorOn),
		"state path", stateLabel)

	slStr := "no"
	hkStr := "no"
	if ctx.hasStatusline {
		slStr = "yes"
	}
	if ctx.hasHooks {
		hkStr = "yes"
	}
	manifest := fmt.Sprintf("statusline=%s, hooks=%s", slStr, hkStr)
	fmt.Fprintf(&b, "%s %-32s %s\n",
		colorize(sevInfo, fmt.Sprintf("%-9s", "[INFO]"), colorOn),
		"install manifest", manifest)
	b.WriteString("\n")

	for _, r := range results {
		plainTag := severityTagPlain(r.Severity)
		paddedTag := fmt.Sprintf("%-9s", plainTag)
		coloredTag := colorize(r.Severity, paddedTag, colorOn)
		fmt.Fprintf(&b, "%s %-32s %s\n", coloredTag, r.Check, r.Message)
		if len(r.Details) > 0 {
			keys := make([]string, 0, len(r.Details))
			for k := range r.Details {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "          %s → %s\n", k, r.Details[k])
			}
		}
		if r.Remediation != "" {
			fmt.Fprintf(&b, "          → %s\n", r.Remediation)
		}
	}
	return b.String()
}

func formatJSON(ctx checkContext, results []result) string {
	mthcVersion := ""
	if ctx.cfg != nil {
		mthcVersion = ctx.cfg.Internal.MthcVersion
	}

	env := map[string]any{
		"mthc_version":     mthcVersion,
		"config_path":      filepath.Join(ctx.home, ".config", "mthc", "config.toml"),
		"state_path":       filepath.Join(ctx.home, ".config", "mthc", "state.json"),
		"install_manifest": map[string]bool{"statusline": false, "hooks": false},
	}
	if ctx.hasStatusline {
		env["install_manifest"].(map[string]bool)["statusline"] = true
	}
	if ctx.hasHooks {
		env["install_manifest"].(map[string]bool)["hooks"] = true
	}
	if ctx.claudeVersion != "" {
		env["claude_code_version"] = ctx.claudeVersion
	}

	summary := map[string]int{
		"pass": 0, "info": 0, "warn": 0, "error": 0, "skipped": 0,
	}
	for _, r := range results {
		summary[r.Severity.String()]++
	}

	report := doctorReport{
		Version:     1,
		Environment: env,
		Results:     results,
		Summary:     summary,
	}
	out, _ := json.MarshalIndent(report, "", "  ")
	return string(out)
}

func severityTagPlain(s severity) string {
	switch s {
	case sevPass:
		return "[PASS]"
	case sevInfo:
		return "[INFO]"
	case sevWarn:
		return "[WARN]"
	case sevError:
		return "[ERROR]"
	case sevSkipped:
		return "[SKIP]"
	}
	return "[????]"
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func colorize(s severity, text string, colorOn bool) string {
	if !colorOn {
		return text
	}
	switch s {
	case sevError:
		return "\033[31m" + text + "\033[0m"
	case sevWarn:
		return "\033[33m" + text + "\033[0m"
	case sevPass:
		return "\033[32m" + text + "\033[0m"
	default:
		return "\033[2m" + text + "\033[0m"
	}
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
	settings := ctx.mergedSettings
	if settings == nil {
		return result{
			Severity: sevSkipped,
			Check:    "mthc.install",
			Message:  "skipped: claude.settings_present failed",
		}
	}

	if ctx.hasStatusline {
		sl, ok := settings["statusLine"].(map[string]any)
		if !ok {
			return result{
				Severity:    sevError,
				Check:       "mthc.install",
				Message:     "statusLine entry missing from settings.json",
				Remediation: "run `mthc install` to register the statusline shim",
			}
		}
		cmd, _ := sl["command"].(string)
		shimPath := parseShimPath(cmd, "statusline-shim")
		if shimPath == "" {
			return result{
				Severity:    sevError,
				Check:       "mthc.install",
				Message:     "statusLine command is not a valid mthc shim",
				Details:     map[string]string{"command": cmd},
				Remediation: "run `mthc install` to register the statusline shim",
			}
		}
		if _, err := os.Stat(shimPath); err != nil {
			return result{
				Severity:    sevError,
				Check:       "mthc.install",
				Message:     "statusline shim path does not exist",
				Details:     map[string]string{"settings_path": shimPath},
				Remediation: "run `mthc install` to refresh shim paths",
			}
		}
	}

	if ctx.hasHooks {
		for _, hookType := range []string{"PostToolBatch", "PreToolUse"} {
			if !hasHookWithCommand(settings, hookType, "hook-shim") {
				return result{
					Severity:    sevError,
					Check:       "mthc.install",
					Message:     hookType + " hook entry missing or not a valid mthc shim",
					Remediation: "run `mthc install` to register hooks",
				}
			}
		}
	}

	return result{
		Severity: sevPass,
		Check:    "mthc.install",
		Message:  "shim entries match running binary",
	}
}

func checkInstallDrift(ctx checkContext) result {
	install := checkInstall(ctx)
	if install.Severity != sevPass {
		return result{
			Severity: sevSkipped,
			Check:    "mthc.install_drift",
			Message:  "skipped: mthc.install failed",
		}
	}

	selfResolved, _ := filepath.EvalSymlinks(ctx.selfPath)

	if ctx.hasStatusline {
		sl, _ := ctx.mergedSettings["statusLine"].(map[string]any)
		cmd, _ := sl["command"].(string)
		shimPath := parseShimPath(cmd, "statusline-shim")
		shimResolved, _ := filepath.EvalSymlinks(shimPath)
		if shimResolved != selfResolved {
			return result{
				Severity:    sevWarn,
				Check:       "mthc.install_drift",
				Message:     "statusline shim path differs from running binary",
				Details:     map[string]string{"settings_path": shimPath, "running_path": ctx.selfPath},
				Remediation: "run `mthc install` to refresh shim paths",
			}
		}
	}

	if ctx.hasHooks {
		for _, hookType := range []string{"PostToolBatch", "PreToolUse"} {
			hooks, ok := ctx.mergedSettings[hookType].([]any)
			if !ok {
				continue
			}
			for _, h := range hooks {
				m, ok := h.(map[string]any)
				if !ok {
					continue
				}
				cmd, _ := m["command"].(string)
				shimPath := parseShimPath(cmd, "hook-shim")
				if shimPath == "" {
					continue
				}
				shimResolved, _ := filepath.EvalSymlinks(shimPath)
				if shimResolved != selfResolved {
					return result{
						Severity:    sevWarn,
						Check:       "mthc.install_drift",
						Message:     hookType + " hook shim path differs from running binary",
						Details:     map[string]string{"settings_path": shimPath, "running_path": ctx.selfPath},
						Remediation: "run `mthc install` to refresh shim paths",
					}
				}
			}
		}
	}

	return result{
		Severity: sevPass,
		Check:    "mthc.install_drift",
		Message:  "shim paths current",
	}
}

func hasHookWithCommand(settings map[string]any, hookType, subcommand string) bool {
	hooks, ok := settings[hookType].([]any)
	if !ok {
		return false
	}
	for _, h := range hooks {
		m, ok := h.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := m["command"].(string)
		shimPath := parseShimPath(cmd, subcommand)
		if shimPath == "" {
			continue
		}
		if _, err := os.Stat(shimPath); err != nil {
			continue
		}
		return true
	}
	return false
}
func checkConfig(ctx checkContext) result {
	if ctx.configAbsent {
		return result{
			Severity:    sevError,
			Check:       "mthc.config",
			Message:     "config.toml not found",
			Remediation: "run `mthc install` to create config",
		}
	}
	if ctx.configErr != nil {
		return result{
			Severity:    sevError,
			Check:       "mthc.config",
			Message:     "config.toml unparseable: " + ctx.configErr.Error(),
			Remediation: "check config.toml syntax or run `mthc install` to regenerate",
		}
	}

	msg := "config and state parse OK"
	if ctx.stateAbsent {
		msg = "config OK; state.json not yet created"
	} else if ctx.stateErr != nil {
		return result{
			Severity:    sevError,
			Check:       "mthc.config",
			Message:     "state.json unparseable: " + ctx.stateErr.Error(),
			Remediation: "delete state.json to regenerate on next tick",
		}
	}

	return result{
		Severity: sevPass,
		Check:    "mthc.config",
		Message:  msg,
	}
}
func checkSettingsPresent(ctx checkContext) result {
	if ctx.mergedSettings != nil {
		return result{
			Severity: sevPass,
			Check:    "claude.settings_present",
			Message:  "settings.json parsed OK",
		}
	}
	return result{
		Severity:    sevError,
		Check:       "claude.settings_present",
		Message:     "no Claude Code settings.json found in any scope",
		Remediation: "ensure Claude Code is installed and has been run at least once",
	}
}
func checkDisableAllHooks(ctx checkContext) result {
	if ctx.mergedSettings == nil {
		return result{
			Severity: sevSkipped,
			Check:    "claude.disable_all_hooks",
			Message:  "skipped: claude.settings_present failed",
		}
	}
	val, ok := ctx.mergedSettings["disableAllHooks"]
	if !ok {
		return result{
			Severity: sevPass,
			Check:    "claude.disable_all_hooks",
			Message:  "not set",
		}
	}
	if flag, _ := val.(bool); flag {
		layer := ctx.settingsScope["disableAllHooks"]
		return result{
			Severity:    sevError,
			Check:       "claude.disable_all_hooks",
			Message:     "disableAllHooks is true — mthc hooks are blocked",
			Details:     map[string]string{"scope": layer},
			Remediation: "set disableAllHooks to false in " + layer + " settings",
		}
	}
	return result{
		Severity: sevPass,
		Check:    "claude.disable_all_hooks",
		Message:  "not set",
	}
}
func checkStatuslineShadow(ctx checkContext) result {
	if !ctx.hasStatusline {
		return result{
			Severity: sevSkipped,
			Check:    "claude.statusline_shadow",
			Message:  "skipped: statusline not installed",
		}
	}
	if ctx.mergedSettings == nil {
		return result{
			Severity: sevSkipped,
			Check:    "claude.statusline_shadow",
			Message:  "skipped: claude.settings_present failed",
		}
	}
	sl, ok := ctx.mergedSettings["statusLine"].(map[string]any)
	if !ok {
		return result{
			Severity: sevPass,
			Check:    "claude.statusline_shadow",
			Message:  "no statusLine in effective settings",
		}
	}
	cmd, _ := sl["command"].(string)
	shimPath := parseShimPath(cmd, "statusline-shim")
	if shimPath == "" {
		layer := ctx.settingsScope["statusLine"]
		return result{
			Severity:    sevError,
			Check:       "claude.statusline_shadow",
			Message:     "effective statusLine is not mthc's shim",
			Details:     map[string]string{"scope": layer, "command": cmd},
			Remediation: "statusLine set in " + layer + " scope overrides mthc — remove or update it, or run `mthc install`",
		}
	}
	selfResolved, _ := filepath.EvalSymlinks(ctx.selfPath)
	shimResolved, _ := filepath.EvalSymlinks(shimPath)
	if shimResolved == selfResolved {
		return result{
			Severity: sevPass,
			Check:    "claude.statusline_shadow",
			Message:  "no shadow detected",
		}
	}
	layer := ctx.settingsScope["statusLine"]
	return result{
		Severity:    sevError,
		Check:       "claude.statusline_shadow",
		Message:     "effective statusLine is not mthc's shim",
		Details:     map[string]string{"scope": layer, "command": cmd},
		Remediation: "statusLine set in " + layer + " scope overrides mthc — remove or update it, or run `mthc install`",
	}
}
