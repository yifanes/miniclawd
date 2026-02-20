package tools

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
)

// ToolRegistry manages tool registration, definition caching, and execution.
type ToolRegistry struct {
	tools    map[string]Tool
	defs     []core.ToolDefinition
	defsOnce sync.Once
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Definitions returns cached tool definitions.
func (r *ToolRegistry) Definitions() []core.ToolDefinition {
	r.defsOnce.Do(func() {
		r.defs = make([]core.ToolDefinition, 0, len(r.tools))
		for _, t := range r.tools {
			r.defs = append(r.defs, t.Definition())
		}
	})
	return r.defs
}

// Execute runs a tool by name and returns its result.
func (r *ToolRegistry) Execute(ctx context.Context, name string, input json.RawMessage) ToolResult {
	t, ok := r.tools[name]
	if !ok {
		return Error("unknown tool: " + name)
	}

	start := time.Now()
	result := t.Execute(ctx, input)
	dur := time.Since(start).Milliseconds()
	result.DurationMs = &dur

	return result
}

// ExecuteWithAuth injects auth context into the input, then executes.
func (r *ToolRegistry) ExecuteWithAuth(ctx context.Context, name string, input json.RawMessage, auth *ToolAuthContext) ToolResult {
	if auth != nil {
		input = InjectAuthContext(input, auth)
	}
	return r.Execute(ctx, name, input)
}

// Has returns true if the registry contains a tool with the given name.
func (r *ToolRegistry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// ToolNames returns a sorted list of registered tool names.
func (r *ToolRegistry) ToolNames() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// RegistryConfig holds dependencies needed to build the full tool registry.
type RegistryConfig struct {
	WorkingDir string
	DataDir    string
	SkillsDir  string
	Timezone   string
	DB         *storage.Database
	Sender     ChannelSender
	SubAgent   SubAgentRunner
	McpCaller  McpCaller

	// ClawHub
	ClawHubEnabled    bool
	ClawHubRegistry   string
	ClawHubToken      *string
}

// BuildStandardRegistry creates the full tool registry with all tools.
func BuildStandardRegistry(cfg RegistryConfig) *ToolRegistry {
	r := NewToolRegistry()

	// File tools
	r.Register(NewBashTool(cfg.WorkingDir))
	r.Register(NewReadFileTool(cfg.WorkingDir))
	r.Register(NewWriteFileTool(cfg.WorkingDir))
	r.Register(NewEditFileTool(cfg.WorkingDir))
	r.Register(NewGlobTool(cfg.WorkingDir))
	r.Register(NewGrepTool(cfg.WorkingDir))

	// Web tools
	r.Register(NewWebFetchTool())
	r.Register(NewWebSearchTool())
	r.Register(NewBrowserTool(cfg.DataDir))

	// Memory tools
	r.Register(NewReadMemoryTool(cfg.DataDir, cfg.DB))
	r.Register(NewWriteMemoryTool(cfg.DataDir, cfg.DB))
	r.Register(NewStructuredMemorySearchTool(cfg.DB))
	r.Register(NewStructuredMemoryDeleteTool(cfg.DB))
	r.Register(NewStructuredMemoryUpdateTool(cfg.DB))

	// Communication
	r.Register(NewSendMessageTool(cfg.DB, cfg.Sender))

	// Scheduling
	r.Register(NewScheduleTaskTool(cfg.DB, cfg.Timezone))
	r.Register(NewListScheduledTasksTool(cfg.DB))
	r.Register(NewPauseScheduledTaskTool(cfg.DB))
	r.Register(NewResumeScheduledTaskTool(cfg.DB, cfg.Timezone))
	r.Register(NewCancelScheduledTaskTool(cfg.DB))
	r.Register(NewGetScheduledTaskHistoryTool(cfg.DB))

	// Export
	r.Register(NewExportChatTool(cfg.DB, cfg.DataDir))

	// Sub-agent
	r.Register(NewSubAgentTool(cfg.SubAgent))

	// Todos
	r.Register(NewTodoReadTool(cfg.DataDir))
	r.Register(NewTodoWriteTool(cfg.DataDir))

	// Skills
	r.Register(NewActivateSkillTool(cfg.SkillsDir))
	r.Register(NewSyncSkillsTool(cfg.SkillsDir))

	// ClawHub
	if cfg.ClawHubEnabled {
		r.Register(NewClawHubSearchTool(cfg.ClawHubRegistry, cfg.ClawHubToken))
		r.Register(NewClawHubInstallTool(cfg.ClawHubRegistry, cfg.ClawHubToken, cfg.SkillsDir))
	}

	return r
}

// BuildSubAgentRegistry creates a restricted tool registry for sub-agents.
func BuildSubAgentRegistry(cfg RegistryConfig) *ToolRegistry {
	r := NewToolRegistry()

	r.Register(NewBashTool(cfg.WorkingDir))
	r.Register(NewReadFileTool(cfg.WorkingDir))
	r.Register(NewWriteFileTool(cfg.WorkingDir))
	r.Register(NewEditFileTool(cfg.WorkingDir))
	r.Register(NewGlobTool(cfg.WorkingDir))
	r.Register(NewGrepTool(cfg.WorkingDir))
	r.Register(NewWebFetchTool())
	r.Register(NewWebSearchTool())
	r.Register(NewBrowserTool(cfg.DataDir))
	r.Register(NewReadMemoryTool(cfg.DataDir, cfg.DB))
	r.Register(NewActivateSkillTool(cfg.SkillsDir))

	return r
}
