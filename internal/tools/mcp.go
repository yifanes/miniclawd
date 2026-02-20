package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yifanes/miniclawd/internal/core"
)

// McpCaller is the interface for calling MCP tool servers.
type McpCaller interface {
	CallTool(ctx context.Context, serverName, toolName string, input json.RawMessage) (string, error)
}

// McpBridgeTool wraps an MCP server tool as a local tool.
type McpBridgeTool struct {
	serverName string
	toolName   string
	fullName   string
	def        core.ToolDefinition
	caller     McpCaller
}

func NewMcpBridgeTool(serverName, toolName string, def core.ToolDefinition, caller McpCaller) *McpBridgeTool {
	fullName := fmt.Sprintf("mcp_%s_%s", serverName, toolName)
	// Override the definition name to be namespaced.
	def.Name = fullName
	return &McpBridgeTool{
		serverName: serverName,
		toolName:   toolName,
		fullName:   fullName,
		def:        def,
		caller:     caller,
	}
}

func (t *McpBridgeTool) Name() string                 { return t.fullName }
func (t *McpBridgeTool) Definition() core.ToolDefinition { return t.def }

func (t *McpBridgeTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	if t.caller == nil {
		return Error("MCP caller not configured")
	}

	result, err := t.caller.CallTool(ctx, t.serverName, t.toolName, input)
	if err != nil {
		return Error(fmt.Sprintf("MCP error: %v", err))
	}
	return Success(result)
}
