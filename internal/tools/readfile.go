package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yifanes/miniclawd/internal/core"
)

type ReadFileTool struct {
	workingDir string
}

func NewReadFileTool(workingDir string) *ReadFileTool {
	return &ReadFileTool{workingDir: workingDir}
}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Definition() core.ToolDefinition {
	return MakeDef("read_file",
		"Read the contents of a file. Returns lines with line numbers. Use offset/limit for large files.",
		map[string]any{
			"path":   StringProp("File path to read (relative to working directory or absolute)"),
			"offset": IntProp("1-based line number to start reading from (default: 1)"),
			"limit":  IntProp("Maximum number of lines to read (default: 2000)"),
		},
		[]string{"path"},
	)
}

func (t *ReadFileTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Path   string `json:"path"`
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
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

	offset := 1
	if params.Offset != nil && *params.Offset > 0 {
		offset = *params.Offset
	}
	limit := 2000
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
	}

	file, err := os.Open(path)
	if err != nil {
		return Error(fmt.Sprintf("cannot read file: %v", err))
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	// Allow long lines.
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum < offset {
			continue
		}
		if lineNum >= offset+limit {
			break
		}
		line := scanner.Text()
		// Truncate very long lines.
		if len(line) > 2000 {
			line = line[:2000] + "..."
		}
		lines = append(lines, fmt.Sprintf("%d\t%s", lineNum, line))
	}
	if err := scanner.Err(); err != nil {
		return Error(fmt.Sprintf("error reading file: %v", err))
	}

	if len(lines) == 0 {
		return Success("(empty file)")
	}

	return Success(strings.Join(lines, "\n"))
}

func resolvePath(workingDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(workingDir, path))
}
