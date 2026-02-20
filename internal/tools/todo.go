package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yifanes/miniclawd/internal/core"
	"gopkg.in/yaml.v3"
)

type todoItem struct {
	Task   string `yaml:"task" json:"task"`
	Status string `yaml:"status" json:"status"` // "pending", "in_progress", "completed"
}

// TodoReadTool reads per-chat todo list.
type TodoReadTool struct {
	dataDir string
}

func NewTodoReadTool(dataDir string) *TodoReadTool {
	return &TodoReadTool{dataDir: dataDir}
}

func (t *TodoReadTool) Name() string { return "todo_read" }

func (t *TodoReadTool) Definition() core.ToolDefinition {
	return MakeDef("todo_read",
		"Read the todo list for a chat.",
		map[string]any{},
		nil,
	)
}

func (t *TodoReadTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	auth := ExtractAuthContext(input)
	if auth == nil {
		return Error("auth context required")
	}

	path := filepath.Join(t.dataDir, "runtime", "groups", fmt.Sprintf("%d", auth.CallerChatID), "todos.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Success("No todos found.")
		}
		return Error(fmt.Sprintf("read error: %v", err))
	}

	var todos []todoItem
	if err := yaml.Unmarshal(data, &todos); err != nil {
		return Error(fmt.Sprintf("parse error: %v", err))
	}

	if len(todos) == 0 {
		return Success("No todos found.")
	}

	var sb strings.Builder
	for i, td := range todos {
		marker := "[ ]"
		if td.Status == "completed" {
			marker = "[x]"
		} else if td.Status == "in_progress" {
			marker = "[~]"
		}
		sb.WriteString(fmt.Sprintf("%d. %s %s\n", i+1, marker, td.Task))
	}
	return Success(sb.String())
}

// TodoWriteTool writes per-chat todo list.
type TodoWriteTool struct {
	dataDir string
}

func NewTodoWriteTool(dataDir string) *TodoWriteTool {
	return &TodoWriteTool{dataDir: dataDir}
}

func (t *TodoWriteTool) Name() string { return "todo_write" }

func (t *TodoWriteTool) Definition() core.ToolDefinition {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "Array of todo items",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task":   map[string]any{"type": "string", "description": "Task description"},
						"status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
					},
					"required": []string{"task", "status"},
				},
			},
		},
		"required": []string{"todos"},
	}
	raw, _ := json.Marshal(schema)
	return core.ToolDefinition{
		Name:        "todo_write",
		Description: "Replace the entire todo list for the current chat.",
		InputSchema: raw,
	}
}

func (t *TodoWriteTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Todos []todoItem `json:"todos"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	auth := ExtractAuthContext(input)
	if auth == nil {
		return Error("auth context required")
	}

	dir := filepath.Join(t.dataDir, "runtime", "groups", fmt.Sprintf("%d", auth.CallerChatID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Error(fmt.Sprintf("directory error: %v", err))
	}

	data, err := yaml.Marshal(params.Todos)
	if err != nil {
		return Error(fmt.Sprintf("marshal error: %v", err))
	}

	path := filepath.Join(dir, "todos.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Error(fmt.Sprintf("write error: %v", err))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Saved %d todos:\n", len(params.Todos)))
	for i, td := range params.Todos {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, td.Status, td.Task))
	}
	return Success(sb.String())
}
