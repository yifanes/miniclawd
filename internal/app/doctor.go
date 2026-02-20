package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/yifanes/miniclawd/internal/config"
)

// Check represents a single diagnostic check result.
type Check struct {
	Name   string
	Status string // "ok", "warn", "fail"
	Detail string
}

// RunDoctor performs preflight diagnostics.
func RunDoctor(cfg *config.Config) []Check {
	var checks []Check

	// Check config file.
	checks = append(checks, checkConfig(cfg))

	// Check data directory.
	checks = append(checks, checkDataDir(cfg.DataDir))

	// Check working directory.
	checks = append(checks, checkWorkingDir(cfg.WorkingDir))

	// Check LLM API key.
	checks = append(checks, checkAPIKey(cfg))

	// Check Telegram bot token.
	if cfg.TelegramBotToken != "" {
		checks = append(checks, Check{Name: "telegram_token", Status: "ok", Detail: "Token configured"})
	} else {
		checks = append(checks, Check{Name: "telegram_token", Status: "warn", Detail: "No Telegram token set"})
	}

	// Check optional tools.
	checks = append(checks, checkBinary("bash"))
	checks = append(checks, checkBinary("git"))
	checks = append(checks, checkBinary("agent-browser"))

	return checks
}

func checkConfig(cfg *config.Config) Check {
	if err := cfg.Validate(); err != nil {
		return Check{Name: "config", Status: "fail", Detail: err.Error()}
	}
	return Check{Name: "config", Status: "ok", Detail: fmt.Sprintf("Provider: %s, Model: %s", cfg.LLMProvider, cfg.Model)}
}

func checkDataDir(dataDir string) Check {
	if info, err := os.Stat(dataDir); err != nil {
		if os.IsNotExist(err) {
			return Check{Name: "data_dir", Status: "warn", Detail: fmt.Sprintf("%s does not exist (will be created)", dataDir)}
		}
		return Check{Name: "data_dir", Status: "fail", Detail: err.Error()}
	} else if !info.IsDir() {
		return Check{Name: "data_dir", Status: "fail", Detail: fmt.Sprintf("%s is not a directory", dataDir)}
	}
	return Check{Name: "data_dir", Status: "ok", Detail: dataDir}
}

func checkWorkingDir(workingDir string) Check {
	if info, err := os.Stat(workingDir); err != nil {
		if os.IsNotExist(err) {
			return Check{Name: "working_dir", Status: "warn", Detail: fmt.Sprintf("%s does not exist (will be created)", workingDir)}
		}
		return Check{Name: "working_dir", Status: "fail", Detail: err.Error()}
	} else if !info.IsDir() {
		return Check{Name: "working_dir", Status: "fail", Detail: fmt.Sprintf("%s is not a directory", workingDir)}
	}
	return Check{Name: "working_dir", Status: "ok", Detail: workingDir}
}

func checkAPIKey(cfg *config.Config) Check {
	if cfg.APIKey == "" {
		if cfg.LLMProvider == "ollama" {
			return Check{Name: "api_key", Status: "ok", Detail: "Not required for Ollama"}
		}
		return Check{Name: "api_key", Status: "fail", Detail: "API key not set"}
	}
	masked := cfg.APIKey[:4] + strings.Repeat("*", len(cfg.APIKey)-8) + cfg.APIKey[len(cfg.APIKey)-4:]
	return Check{Name: "api_key", Status: "ok", Detail: fmt.Sprintf("Set (%s)", masked)}
}

func checkBinary(name string) Check {
	path, err := exec.LookPath(name)
	if err != nil {
		return Check{Name: name, Status: "warn", Detail: "Not found in PATH"}
	}
	return Check{Name: name, Status: "ok", Detail: path}
}

// FormatChecks formats diagnostic results for display.
func FormatChecks(checks []Check) string {
	var sb strings.Builder
	for _, c := range checks {
		icon := "✓"
		switch c.Status {
		case "warn":
			icon = "!"
		case "fail":
			icon = "✗"
		}
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", icon, c.Name, c.Detail))
	}
	return sb.String()
}
