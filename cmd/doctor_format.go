package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
