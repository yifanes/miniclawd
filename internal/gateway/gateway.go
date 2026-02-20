package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/yifanes/miniclawd/internal/logging"
)

const (
	serviceName = "miniclawd-gateway"
	envGateway  = "MINICLAWD_GATEWAY"
	envConfig   = "MINICLAWD_CONFIG"
)

// ServiceContext holds paths needed to install and manage the gateway service.
type ServiceContext struct {
	ExePath    string // path to the miniclawd binary
	WorkingDir string // working directory for the service
	ConfigPath string // config file path (may be empty)
	LogsDir    string // directory for log files
}

// Run dispatches gateway sub-commands.
func Run(args []string) error {
	if len(args) == 0 {
		printGatewayUsage()
		return nil
	}

	switch args[0] {
	case "install":
		ctx, err := buildContext()
		if err != nil {
			return err
		}
		return install(ctx)
	case "uninstall":
		return uninstall()
	case "start":
		return start()
	case "stop":
		return stop()
	case "status":
		return status()
	case "logs":
		lines := 50
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil && n > 0 {
				lines = n
			}
		}
		return logs(lines)
	case "help", "-h", "--help":
		printGatewayUsage()
		return nil
	default:
		return fmt.Errorf("unknown gateway subcommand: %s", args[0])
	}
}

func printGatewayUsage() {
	fmt.Println(`miniclawd gateway - manage the background gateway service

Usage:
  miniclawd gateway <subcommand>

Subcommands:
  install     Install and start the gateway service
  uninstall   Stop and remove the gateway service
  start       Start the gateway service
  stop        Stop the gateway service
  status      Show service status
  logs [N]    Show last N lines of logs (default 50)
  help        Show this help`)
}

func buildContext() (*ServiceContext, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolving executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, fmt.Errorf("resolving symlinks: %w", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	// Determine config path from env or default candidates.
	configPath := os.Getenv(envConfig)
	if configPath == "" {
		candidates := []string{"miniclawd.config.yaml", "miniclawd.config.yml"}
		for _, c := range candidates {
			abs := filepath.Join(wd, c)
			if _, err := os.Stat(abs); err == nil {
				configPath = abs
				break
			}
		}
	}

	// Determine logs directory.
	dataDir := "./miniclawd.data"
	// Try to read data_dir from a simple scan if config exists.
	if configPath != "" {
		if d := readDataDirFromConfig(configPath); d != "" {
			dataDir = d
		}
	}
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(wd, dataDir)
	}
	logsDir := filepath.Join(dataDir, "runtime", "logs")

	return &ServiceContext{
		ExePath:    exe,
		WorkingDir: wd,
		ConfigPath: configPath,
		LogsDir:    logsDir,
	}, nil
}

// readDataDirFromConfig does a simple scan of the config file for data_dir.
func readDataDirFromConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Simple line-by-line scan, no full YAML parse needed.
	for _, line := range splitLines(string(data)) {
		if len(line) > 0 && line[0] != '#' {
			if key, val, ok := parseSimpleYAML(line); ok && key == "data_dir" {
				return val
			}
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func parseSimpleYAML(line string) (key, val string, ok bool) {
	idx := 0
	for idx < len(line) && (line[idx] == ' ' || line[idx] == '\t') {
		idx++
	}
	colonIdx := -1
	for i := idx; i < len(line); i++ {
		if line[i] == ':' {
			colonIdx = i
			break
		}
	}
	if colonIdx < 0 {
		return "", "", false
	}
	key = line[idx:colonIdx]
	val = line[colonIdx+1:]
	// Trim spaces and quotes.
	val = trimSpacesAndQuotes(val)
	return key, val, true
}

func trimSpacesAndQuotes(s string) string {
	// Trim whitespace.
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	s = s[start:end]
	// Strip surrounding quotes.
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		s = s[1 : len(s)-1]
	}
	return s
}

// Platform dispatch functions.

func install(ctx *ServiceContext) error {
	switch runtime.GOOS {
	case "darwin":
		return installDarwin(ctx)
	case "linux":
		return installLinux(ctx)
	default:
		return fmt.Errorf("gateway service not supported on %s", runtime.GOOS)
	}
}

func uninstall() error {
	switch runtime.GOOS {
	case "darwin":
		return uninstallDarwin()
	case "linux":
		return uninstallLinux()
	default:
		return fmt.Errorf("gateway service not supported on %s", runtime.GOOS)
	}
}

func start() error {
	switch runtime.GOOS {
	case "darwin":
		return startDarwin()
	case "linux":
		return startLinux()
	default:
		return fmt.Errorf("gateway service not supported on %s", runtime.GOOS)
	}
}

func stop() error {
	switch runtime.GOOS {
	case "darwin":
		return stopDarwin()
	case "linux":
		return stopLinux()
	default:
		return fmt.Errorf("gateway service not supported on %s", runtime.GOOS)
	}
}

func status() error {
	switch runtime.GOOS {
	case "darwin":
		return statusDarwin()
	case "linux":
		return statusLinux()
	default:
		return fmt.Errorf("gateway service not supported on %s", runtime.GOOS)
	}
}

func logs(lines int) error {
	ctx, err := buildContext()
	if err != nil {
		return err
	}

	output, err := logging.ReadRecentLogs(ctx.LogsDir, lines)
	if err != nil {
		return err
	}
	fmt.Print(output)
	return nil
}
