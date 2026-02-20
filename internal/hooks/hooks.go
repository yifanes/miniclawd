package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// HookAction is the response action from a hook.
type HookAction string

const (
	ActionAllow  HookAction = "allow"
	ActionBlock  HookAction = "block"
	ActionModify HookAction = "modify"
)

// HookDefinition describes a hook parsed from HOOK.md.
type HookDefinition struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Event       string `yaml:"event"` // "before_llm", "after_tool", etc.
	Command     string `yaml:"command"`
	Timeout     int    `yaml:"timeout"` // seconds, default 10
	Enabled     bool   `yaml:"enabled"`
}

// HookResponse is the JSON response from a hook subprocess.
type HookResponse struct {
	Action  HookAction `json:"action"`
	Message string     `json:"message,omitempty"`
	Data    any        `json:"data,omitempty"`
}

// HookManager manages hook discovery and execution.
type HookManager struct {
	hooks   []HookDefinition
	hooksDir string
}

// NewHookManager creates a HookManager by scanning the hooks directory.
func NewHookManager(dataDir string) *HookManager {
	hooksDir := filepath.Join(dataDir, "hooks")
	hm := &HookManager{hooksDir: hooksDir}
	hm.loadHooks()
	return hm
}

func (m *HookManager) loadHooks() {
	entries, err := os.ReadDir(m.hooksDir)
	if err != nil {
		return // No hooks directory.
	}

	for _, entry := range entries {
		if entry.IsDir() {
			hookFile := filepath.Join(m.hooksDir, entry.Name(), "HOOK.md")
			if def := parseHookFile(hookFile); def != nil {
				m.hooks = append(m.hooks, *def)
			}
		}
	}
}

func parseHookFile(path string) *HookDefinition {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return nil
	}

	parts := strings.SplitN(content[4:], "\n---\n", 2)
	if len(parts) < 1 {
		return nil
	}

	var def HookDefinition
	if err := yaml.Unmarshal([]byte(parts[0]), &def); err != nil {
		return nil
	}

	if def.Timeout <= 0 {
		def.Timeout = 10
	}
	if def.Name == "" {
		def.Name = filepath.Base(filepath.Dir(path))
	}

	return &def
}

// RunHooks executes all enabled hooks matching the given event.
func (m *HookManager) RunHooks(ctx context.Context, event string, input any) (*HookResponse, error) {
	for _, hook := range m.hooks {
		if !hook.Enabled || hook.Event != event {
			continue
		}

		resp, err := m.executeHook(ctx, hook, input)
		if err != nil {
			log.Printf("[hooks] error running hook %q: %v", hook.Name, err)
			continue
		}

		if resp.Action == ActionBlock {
			return resp, nil
		}
		if resp.Action == ActionModify {
			return resp, nil
		}
	}
	return &HookResponse{Action: ActionAllow}, nil
}

func (m *HookManager) executeHook(ctx context.Context, hook HookDefinition, input any) (*HookResponse, error) {
	timeout := time.Duration(hook.Timeout) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", hook.Command)
	cmd.Dir = filepath.Join(m.hooksDir, hook.Name)

	// Pass input as JSON on stdin.
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshalling input: %w", err)
	}
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("hook timed out after %ds", hook.Timeout)
		}
		return nil, fmt.Errorf("hook error: %v (stderr: %s)", err, stderr.String())
	}

	var resp HookResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		// If no valid JSON, treat as allow.
		return &HookResponse{Action: ActionAllow}, nil
	}

	return &resp, nil
}

// ListHooks returns all discovered hooks.
func (m *HookManager) ListHooks() []HookDefinition {
	return m.hooks
}

// EnableHook enables a hook by name.
func (m *HookManager) EnableHook(name string) bool {
	for i := range m.hooks {
		if m.hooks[i].Name == name {
			m.hooks[i].Enabled = true
			return true
		}
	}
	return false
}

// DisableHook disables a hook by name.
func (m *HookManager) DisableHook(name string) bool {
	for i := range m.hooks {
		if m.hooks[i].Name == name {
			m.hooks[i].Enabled = false
			return true
		}
	}
	return false
}
