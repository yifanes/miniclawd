package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const (
	darwinLabel    = "ai.miniclawd.gateway"
	darwinPlistDir = "Library/LaunchAgents"
)

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, darwinPlistDir, darwinLabel+".plist")
}

func domainTarget() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func serviceTarget() string {
	return fmt.Sprintf("%s/%s", domainTarget(), darwinLabel)
}

var plistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{ .Label }}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{ .ExePath }}</string>
        <string>start</string>
    </array>
    <key>WorkingDirectory</key>
    <string>{{ .WorkingDir }}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>MINICLAWD_GATEWAY</key>
        <string>1</string>
{{- if .ConfigPath }}
        <key>MINICLAWD_CONFIG</key>
        <string>{{ .ConfigPath }}</string>
{{- end }}
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{ .StdoutPath }}</string>
    <key>StandardErrorPath</key>
    <string>{{ .StderrPath }}</string>
</dict>
</plist>
`))

type plistData struct {
	Label      string
	ExePath    string
	WorkingDir string
	ConfigPath string
	StdoutPath string
	StderrPath string
}

func installDarwin(ctx *ServiceContext) error {
	// Ensure logs directory exists.
	if err := os.MkdirAll(ctx.LogsDir, 0o755); err != nil {
		return fmt.Errorf("creating logs directory: %w", err)
	}

	// Ensure plist directory exists.
	plist := plistPath()
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return fmt.Errorf("creating LaunchAgents directory: %w", err)
	}

	data := plistData{
		Label:      darwinLabel,
		ExePath:    ctx.ExePath,
		WorkingDir: ctx.WorkingDir,
		ConfigPath: ctx.ConfigPath,
		StdoutPath: filepath.Join(ctx.LogsDir, "launchd-stdout.log"),
		StderrPath: filepath.Join(ctx.LogsDir, "launchd-stderr.log"),
	}

	f, err := os.Create(plist)
	if err != nil {
		return fmt.Errorf("creating plist: %w", err)
	}
	defer f.Close()

	if err := plistTemplate.Execute(f, data); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	fmt.Printf("Service plist written to %s\n", plist)

	// Bootstrap the service.
	cmd := exec.Command("launchctl", "bootstrap", domainTarget(), plist)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Error 37 means already loaded — not fatal.
		if !strings.Contains(string(out), "37:") {
			return fmt.Errorf("launchctl bootstrap: %s (%w)", string(out), err)
		}
		fmt.Println("Service already loaded, restarting...")
		restart := exec.Command("launchctl", "kickstart", "-k", serviceTarget())
		if out, err := restart.CombinedOutput(); err != nil {
			return fmt.Errorf("launchctl kickstart: %s (%w)", string(out), err)
		}
	}

	fmt.Println("Gateway service installed and started.")
	return nil
}

func uninstallDarwin() error {
	plist := plistPath()

	// Bootout the service.
	cmd := exec.Command("launchctl", "bootout", serviceTarget())
	if out, err := cmd.CombinedOutput(); err != nil {
		// Ignore if not loaded.
		if !strings.Contains(string(out), "No such process") &&
			!strings.Contains(string(out), "Could not find specified service") {
			return fmt.Errorf("launchctl bootout: %s (%w)", string(out), err)
		}
	}

	// Remove plist.
	if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}

	fmt.Println("Gateway service uninstalled.")
	return nil
}

func startDarwin() error {
	cmd := exec.Command("launchctl", "kickstart", serviceTarget())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart: %s (%w)", string(out), err)
	}
	fmt.Println("Gateway service started.")
	return nil
}

func stopDarwin() error {
	cmd := exec.Command("launchctl", "kill", "SIGTERM", serviceTarget())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kill: %s (%w)", string(out), err)
	}
	fmt.Println("Gateway service stopped.")
	return nil
}

func statusDarwin() error {
	cmd := exec.Command("launchctl", "print", serviceTarget())
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Could not find service") {
			fmt.Println("Gateway service is not installed.")
			return nil
		}
		return fmt.Errorf("launchctl print: %s (%w)", string(out), err)
	}

	// Parse output for key status lines.
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "state =") ||
			strings.HasPrefix(trimmed, "pid =") ||
			strings.HasPrefix(trimmed, "last exit code =") {
			fmt.Println(trimmed)
		}
	}
	return nil
}
