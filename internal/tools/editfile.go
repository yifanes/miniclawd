package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/yifanes/miniclawd/internal/core"
)

type EditFileTool struct {
	workingDir string
}

func NewEditFileTool(workingDir string) *EditFileTool {
	return &EditFileTool{workingDir: workingDir}
}

func (t *EditFileTool) Name() string { return "edit_file" }

func (t *EditFileTool) Definition() core.ToolDefinition {
	return MakeDef("edit_file",
		"Edit a file by replacing an exact string match. The old_string must appear exactly once in the file.",
		map[string]any{
			"path":       StringProp("File path to edit"),
			"old_string": StringProp("The exact string to find (must be unique in the file)"),
			"new_string": StringProp("The replacement string"),
		},
		[]string{"path", "old_string", "new_string"},
	)
}

func (t *EditFileTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Path == "" || params.OldString == "" {
		return Error("path and old_string are required")
	}

	path := resolvePath(t.workingDir, params.Path)
	if err := CheckPath(path); err != nil {
		return Error(err.Error())
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return Error(fmt.Sprintf("cannot read file: %v", err))
	}

	text := string(content)
	count := strings.Count(text, params.OldString)
	if count == 0 {
		return Error("old_string not found in file")
	}
	if count > 1 {
		return Error(fmt.Sprintf("old_string found %d times (must be unique). Provide more context to make it unique.", count))
	}

	newText := strings.Replace(text, params.OldString, params.NewString, 1)
	if err := os.WriteFile(path, []byte(newText), 0o644); err != nil {
		return Error(fmt.Sprintf("cannot write file: %v", err))
	}

	return Success(fmt.Sprintf("edited %s (replaced 1 occurrence)", path))
}
