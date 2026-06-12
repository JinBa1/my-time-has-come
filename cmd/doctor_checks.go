package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/JinBa1/my-time-has-come/internal/state"
)

func parseShimPath(cmd, subcommand string) string {
	cmd = strings.TrimSpace(cmd)
	parts := strings.Fields(cmd)
	if len(parts) < 2 || parts[1] != subcommand {
		return ""
	}
	return parts[0]
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0111 != 0
}

func isPathLikeCommand(command string) bool {
	return filepath.IsAbs(command) || strings.Contains(command, string(os.PathSeparator))
}

func isExecutableCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if isPathLikeCommand(command) {
		return isExecutableFile(command)
	}
	_, err := exec.LookPath(command)
	return err == nil
}

func stableInstallCommand() string {
	return strings.TrimSpace(os.Getenv(installCommandEnv))
}

func resolveExecutableCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if isPathLikeCommand(command) {
		return filepath.EvalSymlinks(command)
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func sameExecutableOrStableCommand(commandPath, selfPath string) bool {
	commandPath = strings.TrimSpace(commandPath)
	if stable := stableInstallCommand(); stable != "" && stable == commandPath {
		return true
	}
	commandResolved, err := resolveExecutableCommand(commandPath)
	if err != nil {
		return false
	}
	selfResolved, err := resolveExecutableCommand(selfPath)
	if err != nil {
		return false
	}
	return commandResolved == selfResolved
}

func checkBinary(ctx checkContext) result {
	if ctx.mthcOnPath == "" {
		if ctx.selfPath != "" && isExecutableCommand(ctx.selfPath) {
			return result{
				Severity: sevWarn,
				Check:    "mthc.binary",
				Message:  "mthc not found on PATH; running " + ctx.selfPath,
			}
		}
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
	if len(ctx.settingsErrors) > 0 {
		return result{
			Severity: sevSkipped,
			Check:    "mthc.install",
			Message:  "skipped: claude.settings_present encountered errors",
		}
	}

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
				Message:     "statusLine entry missing from effective Claude settings",
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
		if !isExecutableCommand(shimPath) {
			return result{
				Severity:    sevError,
				Check:       "mthc.install",
				Message:     "statusline shim path does not exist or is not executable",
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
		Message:  "shim entries are present and valid",
	}
}

func checkInstallDrift(ctx checkContext) result {
	install := checkInstall(ctx)
	if install.Severity != sevPass {
		msg := "skipped: mthc.install failed"
		if install.Severity == sevSkipped {
			msg = install.Message
		}
		return result{
			Severity: sevSkipped,
			Check:    "mthc.install_drift",
			Message:  msg,
		}
	}

	if ctx.hasStatusline {
		sl, _ := ctx.mergedSettings["statusLine"].(map[string]any)
		cmd, _ := sl["command"].(string)
		shimPath := parseShimPath(cmd, "statusline-shim")
		if !sameExecutableOrStableCommand(shimPath, ctx.selfPath) {
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
			for _, command := range nestedHookShimCommands(ctx.mergedSettings, hookType, "hook-shim") {
				shimPath := parseShimPath(command, "hook-shim")
				if !sameExecutableOrStableCommand(shimPath, ctx.selfPath) {
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

func nestedHookShimCommands(settings map[string]any, hookType, subcommand string) []string {
	hooksRoot, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	groups, ok := hooksRoot[hookType].([]any)
	if !ok {
		return nil
	}
	var commands []string
	for _, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			continue
		}
		if hookType == "PreToolUse" && groupMap["matcher"] != "*" {
			continue
		}
		innerHooks, ok := groupMap["hooks"].([]any)
		if !ok {
			continue
		}
		for _, innerHook := range innerHooks {
			hookMap, ok := innerHook.(map[string]any)
			if !ok {
				continue
			}
			if hookMap["type"] != "command" {
				continue
			}
			command, _ := hookMap["command"].(string)
			if parseShimPath(command, subcommand) == "" {
				continue
			}
			commands = append(commands, command)
		}
	}
	return commands
}

func hasHookWithCommand(settings map[string]any, hookType, subcommand string) bool {
	for _, command := range nestedHookShimCommands(settings, hookType, subcommand) {
		shimPath := parseShimPath(command, subcommand)
		if !isExecutableCommand(shimPath) {
			continue
		}
		return true
	}
	return false
}

func checkConfig(ctx checkContext) result {
	if hasOldFlatThresholdConfig(ctx.configData) {
		return result{
			Severity:    sevError,
			Check:       "mthc.config",
			Message:     "config schema changed: soft_pct/hard_pct are no longer supported; thresholds are now unit-tagged (enabled/unit/soft/hard)",
			Remediation: "replace soft_pct/hard_pct keys with [thresholds.five_hour] and [thresholds.seven_day] tables using enabled/unit/soft/hard fields",
		}
	}
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

// hasOldFlatThresholdConfig detects the pre-v0.2 flat shape:
//
//	[thresholds]
//	soft_pct = 85
//	hard_pct = 95
func hasOldFlatThresholdConfig(data []byte) bool {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return false
	}
	thresholds, ok := raw["thresholds"].(map[string]any)
	if !ok {
		return false
	}
	_, hasSoft := thresholds["soft_pct"]
	_, hasHard := thresholds["hard_pct"]
	if hasSoft || hasHard {
		return true
	}
	// Also detect the legacy nested shape, e.g.:
	//   [thresholds.five_hour]
	//   soft_pct = 85
	for _, v := range thresholds {
		window, ok := v.(map[string]any)
		if !ok {
			continue
		}
		_, wSoft := window["soft_pct"]
		_, wHard := window["hard_pct"]
		if wSoft || wHard {
			return true
		}
	}
	return false
}

func checkSettingsPresent(ctx checkContext) result {
	if len(ctx.settingsErrors) > 0 {
		e := ctx.settingsErrors[0]
		msg := fmt.Sprintf("settings file %s (scope: %s): %v", e.path, e.scope, e.err)
		return result{
			Severity:    sevError,
			Check:       "claude.settings_present",
			Message:     msg,
			Remediation: "fix or remove the malformed/unreadable settings file",
		}
	}
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
	if len(ctx.settingsErrors) > 0 {
		return result{
			Severity: sevSkipped,
			Check:    "claude.disable_all_hooks",
			Message:  "skipped: claude.settings_present encountered errors",
		}
	}
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
	flag, ok := val.(bool)
	layer := ctx.settingsScope["disableAllHooks"]
	if !ok {
		details := map[string]string{"type": fmt.Sprintf("%T", val)}
		if layer != "" {
			details["scope"] = layer
		}
		remediation := "set disableAllHooks to false or remove it from Claude settings"
		if layer != "" {
			remediation = "set disableAllHooks to false or remove it from " + layer + " settings"
		}
		return result{
			Severity:    sevError,
			Check:       "claude.disable_all_hooks",
			Message:     "disableAllHooks must be a boolean",
			Details:     details,
			Remediation: remediation,
		}
	}
	if flag {
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
		Message:  "set to false",
	}
}

// windowThresholdEntries returns the set of (windowID, WindowThresholdConfig)
// pairs that are explicitly configured. This keeps the check independent of
// how many windows exist.
func windowThresholdEntries(ctx checkContext) []struct {
	id      string
	cfg     interface{ UnitOrDefault() string }
	enabled bool
} {
	if ctx.cfg == nil {
		return nil
	}
	type entry = struct {
		id      string
		cfg     interface{ UnitOrDefault() string }
		enabled bool
	}
	return []entry{
		{"five_hour", ctx.cfg.Thresholds.FiveHour, ctx.cfg.Thresholds.FiveHour.Enabled},
		{"seven_day", ctx.cfg.Thresholds.SevenDay, ctx.cfg.Thresholds.SevenDay.Enabled},
	}
}

// checkThresholdUnitMismatch warns when an enabled threshold window has a
// non-Absent statusline observation whose unit differs from the configured
// unit. Such a threshold can never trigger because the comparison is
// unit-gated.
func checkThresholdUnitMismatch(ctx checkContext) result {
	if ctx.cfg == nil || ctx.state == nil {
		return result{Severity: sevPass, Check: "mthc.threshold_unit", Message: "no config or state"}
	}
	type mismatch struct {
		window         string
		configuredUnit string
		observedUnit   string
	}
	var mismatches []mismatch
	for _, e := range windowThresholdEntries(ctx) {
		if !e.enabled {
			continue
		}
		obs := ctx.state.Observation(e.id, state.SourceStatusline)
		if obs == nil || obs.Absent {
			continue
		}
		want := e.cfg.UnitOrDefault()
		if obs.Unit != want {
			mismatches = append(mismatches, mismatch{
				window:         e.id,
				configuredUnit: want,
				observedUnit:   obs.Unit,
			})
		}
	}
	if len(mismatches) == 0 {
		return result{Severity: sevPass, Check: "mthc.threshold_unit", Message: "threshold units match observations"}
	}
	// Sort for deterministic output
	sort.Slice(mismatches, func(i, j int) bool {
		return mismatches[i].window < mismatches[j].window
	})
	m := mismatches[0]
	msg := fmt.Sprintf("window %s: configured unit %q but observed unit %q — this threshold can never trigger",
		m.window, m.configuredUnit, m.observedUnit)
	details := map[string]string{
		"window":          m.window,
		"configured_unit": m.configuredUnit,
		"observed_unit":   m.observedUnit,
	}
	if len(mismatches) > 1 {
		var extra []string
		for _, mm := range mismatches[1:] {
			extra = append(extra, mm.window)
		}
		details["also"] = strings.Join(extra, ", ")
	}
	return result{
		Severity:    sevWarn,
		Check:       "mthc.threshold_unit",
		Message:     msg,
		Details:     details,
		Remediation: "align the [thresholds.<window>] unit field with the unit your statusline reports",
	}
}

// checkUnknownHarness notes sessions whose Harness field is empty or "unknown".
// Gate scoping (roadmap step 3) will treat them fail-open.
func checkUnknownHarness(ctx checkContext) result {
	if ctx.state == nil {
		return result{Severity: sevPass, Check: "mthc.harness", Message: "no state"}
	}
	var unknown []string
	for id, sess := range ctx.state.Sessions {
		if sess == nil {
			continue
		}
		if sess.Harness == "" || sess.Harness == state.HarnessUnknown {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return result{Severity: sevPass, Check: "mthc.harness", Message: "all sessions have known harness"}
	}
	sort.Strings(unknown)
	msg := fmt.Sprintf("session %s has unknown harness — gate scoping will treat it fail-open",
		strings.Join(unknown, ", "))
	return result{
		Severity: sevInfo,
		Check:    "mthc.harness",
		Message:  msg,
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
	if len(ctx.settingsErrors) > 0 {
		return result{
			Severity: sevSkipped,
			Check:    "claude.statusline_shadow",
			Message:  "skipped: claude.settings_present encountered errors",
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
	if sameExecutableOrStableCommand(shimPath, ctx.selfPath) {
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
