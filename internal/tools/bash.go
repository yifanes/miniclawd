package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
)

const maxOutputBytes = 30000

type BashTool struct {
	workingDir string
}

func NewBashTool(workingDir string) *BashTool {
	return &BashTool{workingDir: workingDir}
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Definition() core.ToolDefinition {
	return MakeDef("bash",
		"Execute a bash command. Returns stdout, stderr, and exit code. Use for running scripts, git commands, build tools, etc.",
		map[string]any{
			"command":      StringProp("The bash command to execute"),
			"timeout_secs": IntProp("Timeout in seconds (default 120, max 600)"),
		},
		[]string{"command"},
	)
}

func (t *BashTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
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

	timeout := 120
	if params.TimeoutSecs != nil && *params.TimeoutSecs > 0 {
		timeout = *params.TimeoutSecs
		if timeout > 600 {
			timeout = 600
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", params.Command)
	cmd.Dir = t.workingDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	output := stdout.String()
	errOutput := stderr.String()

	// Truncate if too large.
	if len(output) > maxOutputBytes {
		output = output[:core.FloorCharBoundary(output, maxOutputBytes)] + "\n... (output truncated)"
	}
	if len(errOutput) > maxOutputBytes {
		errOutput = errOutput[:core.FloorCharBoundary(errOutput, maxOutputBytes)] + "\n... (stderr truncated)"
	}

	combined := output
	if errOutput != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += "STDERR:\n" + errOutput
	}

	result := ToolResult{
		Content:    combined,
		Bytes:      len(combined),
		DurationMs: &duration,
	}

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.IsError = true
			result.Content = fmt.Sprintf("command timed out after %ds\n%s", timeout, combined)
			et := "timeout"
			result.ErrorType = &et
			return result
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			result.StatusCode = &code
			result.IsError = true
			et := "process_exit"
			result.ErrorType = &et
			return result
		}
		result.IsError = true
		result.Content = "spawn error: " + err.Error()
		et := "spawn_error"
		result.ErrorType = &et
		return result
	}

	code := 0
	result.StatusCode = &code
	return result
}
