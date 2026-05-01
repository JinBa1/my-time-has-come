package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		if !isExecutableFile(shimPath) {
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
		if m["type"] != "command" {
			continue
		}
		if hookType == "PreToolUse" && m["matcher"] != "*" {
			continue
		}
		cmd, _ := m["command"].(string)
		shimPath := parseShimPath(cmd, subcommand)
		if shimPath == "" {
			continue
		}
		if !isExecutableFile(shimPath) {
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
