package cmd

import (
	"flag"
	"fmt"
	"os"
)

func Execute() error {
	if len(os.Args) < 2 {
		fmt.Println("mthc - local execution-governor for coding-agent CLIs")
		fmt.Println("Usage: mthc <command> [options]")
		fmt.Println("Commands: install, uninstall, status, doctor, config, dismiss, statusline-shim, hook-shim, playback")
		return nil
	}
	switch os.Args[1] {
	case "install":
		installCmd := flag.NewFlagSet("install", flag.ContinueOnError)
		installCmd.BoolVar(&installForce, "force", false, "overwrite divergent hooks")
		installCmd.Parse(os.Args[2:])
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
	default:
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}
