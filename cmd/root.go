package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/JinBa1/my-time-has-come/internal/version"
)

func Execute() error {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "--version" || args[0] == "version") {
		fmt.Print(version.Current().Format())
		return nil
	}
	if len(args) == 0 || isHelpArg(args[0]) {
		printRootHelp()
		return nil
	}
	if args[0] == "help" {
		if len(args) == 1 || isHelpArg(args[1]) {
			printRootHelp()
			return nil
		}
		return printCommandHelp(args[1])
	}
	if hasHelpArg(args[1:]) {
		return printCommandHelp(args[0])
	}

	switch args[0] {
	case "install":
		installCmd := flag.NewFlagSet("install", flag.ContinueOnError)
		installCmd.BoolVar(&installForce, "force", false, "overwrite divergent hooks")
		if err := installCmd.Parse(args[1:]); err != nil {
			return err
		}
		return runInstall()
	case "uninstall":
		return runUninstall()
	case "status":
		return runStatus()
	case "doctor":
		return runDoctor()
	case "config":
		return runConfig()
	case "dismiss":
		return runDismiss()
	case "statusline-shim":
		return runStatuslineShim()
	case "hook-shim":
		return runHookShim()
	case "playback":
		return runPlayback()
	case "record":
		return runRecord()
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if isHelpArg(arg) {
			return true
		}
	}
	return false
}

func printRootHelp() {
	fmt.Println("mthc - local execution-governor for coding-agent CLIs")
	fmt.Println("Usage: mthc <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  install    Install mthc into Claude Code settings")
	fmt.Println("  uninstall  Remove mthc hooks and restore prior settings")
	fmt.Println("  status     Show current policy and window state")
	fmt.Println("  doctor     Diagnose installation and runtime state")
	fmt.Println("  config     Show, set, or validate config")
	fmt.Println("  dismiss    Clear current soft or hard stop state")
	fmt.Println("  record     Manage JSONL event capture")
	fmt.Println("  playback   Replay recorded events")
	fmt.Println("  version    Show mthc version information")
	fmt.Println()
	fmt.Println("Internal commands:")
	fmt.Println("  statusline-shim  Internal Claude Code statusline adapter")
	fmt.Println("  hook-shim        Internal Claude Code hook adapter")
	fmt.Println()
	fmt.Println("Run 'mthc help <command>' for command details.")
}

func printCommandHelp(command string) error {
	help, ok := commandHelp[command]
	if !ok {
		return fmt.Errorf("unknown command: %s", command)
	}
	fmt.Print(help)
	return nil
}

var commandHelp = map[string]string{
	"install": `Usage: mthc install [--force]

Install mthc into Claude Code settings and create config/state files.

Options:
  --force  overwrite divergent mthc-managed hooks
`,
	"uninstall": `Usage: mthc uninstall

Remove mthc hooks/statusline entries and restore prior settings.
`,
	"status": `Usage: mthc status

Show current policy state, active windows, sessions, and config path.
`,
	"doctor": `Usage: mthc doctor [--json] [--strict]

Diagnose common mthc installation and runtime problems.

Options:
  --json    print machine-readable diagnostics
  --strict  return an error when warnings are present
`,
	"config": `Usage:
  mthc config
  mthc config show
  mthc config set <key> <value>
  mthc config validate

Show, update, or validate mthc config.
`,
	"dismiss": `Usage: mthc dismiss [--soft] [--hard] [--dry-run]

Clear current stop state for the active window.

Options:
  --soft     clear soft-injection flags
  --hard     disarm the hard gate
  --dry-run  print the planned change without writing state
`,
	"record": `Usage:
  mthc record start
  mthc record stop
  mthc record status

Manage JSONL capture of statusline and hook events.
`,
	"playback": `Usage: mthc playback replay [--config-from-recording] <file|dir>...

Replay recorded JSONL events through the pure core pipeline.

Options:
  --config-from-recording  load meta.toml from a recording directory
`,
	"version": `Usage: mthc version

Show mthc version, commit, and build date.
`,
	"statusline-shim": `Usage: mthc statusline-shim

Internal command invoked by Claude Code statusline. Reads JSON from stdin; not for direct use.
`,
	"hook-shim": `Usage: mthc hook-shim

Internal command invoked by Claude Code hooks. Reads JSON from stdin; not for direct use.
`,
}
