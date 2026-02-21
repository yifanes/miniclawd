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
	linuxServiceFile = "miniclawd-gateway.service"
	linuxServiceDir  = ".config/systemd/user"
)

func serviceFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, linuxServiceDir, linuxServiceFile)
}

var unitTemplate = template.Must(template.New("unit").Parse(`[Unit]
Description=MiniClawd Gateway Service
After=network.target

[Service]
Type=simple
ExecStart={{ .ExePath }} start
WorkingDirectory={{ .WorkingDir }}
Environment=MINICLAWD_GATEWAY=1
{{- if .ConfigPath }}
Environment=MINICLAWD_CONFIG={{ .ConfigPath }}
{{- end }}
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`))

type unitData struct {
	ExePath    string
	WorkingDir string
	ConfigPath string
}

func installLinux(ctx *ServiceContext) error {
	// Ensure logs directory exists.
	if err := os.MkdirAll(ctx.LogsDir, 0o755); err != nil {
		return fmt.Errorf("creating logs directory: %w", err)
	}

	// Ensure systemd user directory exists.
	svcPath := serviceFilePath()
	if err := os.MkdirAll(filepath.Dir(svcPath), 0o755); err != nil {
		return fmt.Errorf("creating systemd user directory: %w", err)
	}

	data := unitData{
		ExePath:    ctx.ExePath,
		WorkingDir: ctx.WorkingDir,
		ConfigPath: ctx.ConfigPath,
	}

	f, err := os.Create(svcPath)
	if err != nil {
		return fmt.Errorf("creating service file: %w", err)
	}
	defer f.Close()

	if err := unitTemplate.Execute(f, data); err != nil {
		return fmt.Errorf("writing service file: %w", err)
	}

	fmt.Printf("Service file written to %s\n", svcPath)

	// Enable linger so user services survive SSH logout.
	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = "root"
	}
	linger := exec.Command("loginctl", "enable-linger", currentUser)
	if out, err := linger.CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to enable linger: %s (%v)\n", strings.TrimSpace(string(out)), err)
		fmt.Println("Without linger, the service will stop when you log out.")
		fmt.Println("Run manually: loginctl enable-linger", currentUser)
	} else {
		fmt.Println("Linger enabled: service will persist after logout.")
	}

	// Reload systemd and enable/start the service.
	commands := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", serviceName},
		{"systemctl", "--user", "start", serviceName},
	}

	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %s (%w)", strings.Join(args, " "), string(out), err)
		}
	}

	fmt.Println("Gateway service installed and started.")
	return nil
}

func uninstallLinux() error {
	// Stop and disable.
	commands := [][]string{
		{"systemctl", "--user", "stop", serviceName},
		{"systemctl", "--user", "disable", serviceName},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.CombinedOutput() // Ignore errors (may not be running).
	}

	// Remove service file.
	svcPath := serviceFilePath()
	if err := os.Remove(svcPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing service file: %w", err)
	}

	// Reload daemon.
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	cmd.CombinedOutput()

	fmt.Println("Gateway service uninstalled.")
	return nil
}

func startLinux() error {
	cmd := exec.Command("systemctl", "--user", "start", serviceName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl start: %s (%w)", string(out), err)
	}
	fmt.Println("Gateway service started.")
	return nil
}

func restartLinux() error {
	cmd := exec.Command("systemctl", "--user", "restart", serviceName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart: %s (%w)", string(out), err)
	}
	fmt.Println("Gateway service restarted.")
	return nil
}

func stopLinux() error {
	cmd := exec.Command("systemctl", "--user", "stop", serviceName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl stop: %s (%w)", string(out), err)
	}
	fmt.Println("Gateway service stopped.")
	return nil
}

func statusLinux() error {
	cmd := exec.Command("systemctl", "--user", "status", serviceName)
	out, _ := cmd.CombinedOutput() // status returns non-zero if not running.
	fmt.Print(string(out))
	return nil
}
