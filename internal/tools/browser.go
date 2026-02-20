package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
)

type BrowserTool struct {
	dataDir string
}

func NewBrowserTool(dataDir string) *BrowserTool {
	return &BrowserTool{dataDir: dataDir}
}

func (t *BrowserTool) Name() string { return "browser" }

func (t *BrowserTool) Definition() core.ToolDefinition {
	return MakeDef("browser",
		"Control a headless browser via agent-browser. Supports open, click, fill, type, screenshot, eval, and more. Uses per-chat persistent sessions.",
		map[string]any{
			"command":      StringProp("agent-browser CLI command (e.g., 'open https://example.com')"),
			"timeout_secs": IntProp("Timeout in seconds (default 30)"),
		},
		[]string{"command"},
	)
}

func (t *BrowserTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Command     string `json:"command"`
		TimeoutSecs *int   `json:"timeout_secs"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Command == "" {
		return Error("command is required")
	}

	auth := ExtractAuthContext(input)
	chatID := int64(0)
	if auth != nil {
		chatID = auth.CallerChatID
	}

	timeout := 30
	if params.TimeoutSecs != nil && *params.TimeoutSecs > 0 {
		timeout = *params.TimeoutSecs
	}

	profileDir := filepath.Join(t.dataDir, "runtime", "groups", fmt.Sprintf("%d", chatID), "browser-profile")

	// Find agent-browser binary.
	binary := "agent-browser"
	if p, err := exec.LookPath("agent-browser"); err == nil {
		binary = p
	}

	args := parseShellArgs(params.Command)
	args = append(args, "--profile", profileDir)

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	output := stdout.String()
	errOutput := stderr.String()

	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes] + "\n... (truncated)"
	}

	combined := output
	if errOutput != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += "STDERR:\n" + errOutput
	}

	result := ToolResult{Content: combined, Bytes: len(combined), DurationMs: &duration}

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.IsError = true
			result.Content = fmt.Sprintf("browser command timed out after %ds\n%s", timeout, combined)
			et := "timeout"
			result.ErrorType = &et
			return result
		}
		result.IsError = true
		et := "process_exit"
		result.ErrorType = &et
		return result
	}

	code := 0
	result.StatusCode = &code
	return result
}

// parseShellArgs splits a command string respecting quotes.
func parseShellArgs(s string) []string {
	var args []string
	var current []byte
	inSingle, inDouble, escaped := false, false, false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			current = append(current, c)
			escaped = false
			continue
		}
		if c == '\\' && !inSingle {
			escaped = true
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if c == ' ' && !inSingle && !inDouble {
			if len(current) > 0 {
				args = append(args, string(current))
				current = current[:0]
			}
			continue
		}
		current = append(current, c)
	}
	if len(current) > 0 {
		args = append(args, string(current))
	}
	return args
}
