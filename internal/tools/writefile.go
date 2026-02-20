package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yifanes/miniclawd/internal/core"
)

type WriteFileTool struct {
	workingDir string
}

func NewWriteFileTool(workingDir string) *WriteFileTool {
	return &WriteFileTool{workingDir: workingDir}
}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Definition() core.ToolDefinition {
	return MakeDef("write_file",
		"Write content to a file. Creates parent directories if needed. Overwrites existing content.",
		map[string]any{
			"path":    StringProp("File path to write (relative to working directory or absolute)"),
			"content": StringProp("Content to write to the file"),
		},
		[]string{"path", "content"},
	)
}

func (t *WriteFileTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Path == "" {
		return Error("path is required")
	}

	path := resolvePath(t.workingDir, params.Path)
	if err := CheckPath(path); err != nil {
		return Error(err.Error())
	}

	// Create parent directories.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Error(fmt.Sprintf("cannot create directory: %v", err))
	}

	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return Error(fmt.Sprintf("cannot write file: %v", err))
	}

	return Success(fmt.Sprintf("wrote %d bytes to %s", len(params.Content), path))
}
