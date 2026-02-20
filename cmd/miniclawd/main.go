package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yifanes/miniclawd/internal/app"
	"github.com/yifanes/miniclawd/internal/config"
	"github.com/yifanes/miniclawd/internal/gateway"
	"github.com/yifanes/miniclawd/internal/hooks"
	"github.com/yifanes/miniclawd/internal/logging"
)

var version = "dev"

func Run() int {
	if len(os.Args) < 2 {
		printUsage()
		return 1
	}

	switch os.Args[1] {
	case "start":
		return runStart()
	case "setup":
		return runSetup()
	case "doctor":
		return runDoctor()
	case "gateway":
		return runGateway()
	case "hooks":
		return runHooks()
	case "version":
		fmt.Printf("miniclawd %s\n", version)
		return 0
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Println(`miniclawd - multi-platform AI chat bot

Usage:
  miniclawd <command>

Commands:
  start     Start the bot
  setup     Interactive setup wizard
  doctor    Run preflight diagnostics
  gateway   Manage background gateway service
  hooks     Manage hooks (list, enable, disable)
  version   Print version
  help      Show this help`)
}

func runStart() int {
	cfg, db, err := bootstrap()
	if err != nil {
		fmt.Fprintf(os.Stderr, "startup error: %v\n", err)
		return 1
	}
	defer db.Close()

	// In gateway mode, redirect logs to hourly-rotated files.
	if os.Getenv("MINICLAWD_GATEWAY") != "" {
		logsDir := filepath.Join(cfg.RuntimeDir(), "logs")
		if err := logging.InitFileLogging(logsDir); err != nil {
			fmt.Fprintf(os.Stderr, "logging init error: %v\n", err)
			return 1
		}
	}

	if err := app.Run(cfg, db); err != nil {
		fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
		return 1
	}
	return 0
}

func runGateway() int {
	args := os.Args[2:]
	if err := gateway.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "gateway error: %v\n", err)
		return 1
	}
	return 0
}

func runSetup() int {
	if err := app.RunSetup(); err != nil {
		fmt.Fprintf(os.Stderr, "setup error: %v\n", err)
		return 1
	}
	return 0
}

func runDoctor() int {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		fmt.Println("Run 'miniclawd setup' to create a config file.")
		return 1
	}

	checks := app.RunDoctor(cfg)
	fmt.Print(app.FormatChecks(checks))

	for _, c := range checks {
		if c.Status == "fail" {
			return 1
		}
	}
	return 0
}

func runHooks() int {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}

	mgr := hooks.NewHookManager(cfg.DataDir)
	hks := mgr.ListHooks()

	if len(os.Args) < 3 || os.Args[2] == "list" {
		if len(hks) == 0 {
			fmt.Println("No hooks found.")
			return 0
		}
		for _, h := range hks {
			status := "enabled"
			if !h.Enabled {
				status = "disabled"
			}
			fmt.Printf("  %s [%s] event=%s cmd=%s\n", h.Name, status, h.Event, h.Command)
		}
		return 0
	}

	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "usage: miniclawd hooks <enable|disable> <hook-name>\n")
		return 1
	}

	hookName := os.Args[3]
	switch os.Args[2] {
	case "enable":
		if !mgr.EnableHook(hookName) {
			fmt.Fprintf(os.Stderr, "hook %q not found\n", hookName)
			return 1
		}
		fmt.Printf("Hook %q enabled.\n", hookName)
	case "disable":
		if !mgr.DisableHook(hookName) {
			fmt.Fprintf(os.Stderr, "hook %q not found\n", hookName)
			return 1
		}
		fmt.Printf("Hook %q disabled.\n", hookName)
	default:
		fmt.Fprintf(os.Stderr, "unknown hooks subcommand: %s\n", os.Args[2])
		return 1
	}
	return 0
}
