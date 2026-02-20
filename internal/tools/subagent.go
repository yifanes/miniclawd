package tools

import (
	"context"
	"encoding/json"

	"github.com/yifanes/miniclawd/internal/core"
)

// SubAgentRunner is the interface the agent engine exposes for sub-agent invocations.
type SubAgentRunner interface {
	RunSubAgent(ctx context.Context, task, extraContext string, auth *ToolAuthContext) (string, error)
}

type SubAgentTool struct {
	runner SubAgentRunner
}

func NewSubAgentTool(runner SubAgentRunner) *SubAgentTool {
	return &SubAgentTool{runner: runner}
}

func (t *SubAgentTool) Name() string { return "sub_agent" }

func (t *SubAgentTool) Definition() core.ToolDefinition {
	return MakeDef("sub_agent",
		"Spawn a sub-agent to complete a task. The sub-agent has restricted tools (no send_message, write_memory, schedule, or recursive sub_agent). Max 10 iterations.",
		map[string]any{
			"task":    StringProp("Task description for the sub-agent"),
			"context": StringProp("Additional context to provide (optional)"),
		},
		[]string{"task"},
	)
}

func (t *SubAgentTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Task    string  `json:"task"`
		Context *string `json:"context"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Task == "" {
		return Error("task is required")
	}

	extraCtx := ""
	if params.Context != nil {
		extraCtx = *params.Context
	}

	auth := ExtractAuthContext(input)

	if t.runner == nil {
		return Error("sub_agent runner not configured")
	}

	result, err := t.runner.RunSubAgent(ctx, params.Task, extraCtx, auth)
	if err != nil {
		return Error("sub_agent error: " + err.Error())
	}
	return Success(result)
}
