package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/yifanes/miniclawd/internal/core"
)

const globCap = 500

type GlobTool struct {
	workingDir string
}

func NewGlobTool(workingDir string) *GlobTool {
	return &GlobTool{workingDir: workingDir}
}

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Definition() core.ToolDefinition {
	return MakeDef("glob",
		"Find files matching a glob pattern. Returns matching paths sorted alphabetically (max 500).",
		map[string]any{
			"pattern": StringProp("Glob pattern (e.g., '**/*.go', 'src/**/*.ts')"),
			"path":    StringProp("Base directory to search from (default: working directory)"),
		},
		[]string{"pattern"},
	)
}

func (t *GlobTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Pattern == "" {
		return Error("pattern is required")
	}

	base := t.workingDir
	if params.Path != "" {
		base = resolvePath(t.workingDir, params.Path)
	}

	// Use doublestar to match files.
	fsys := os.DirFS(base)
	matches, err := doublestar.Glob(fsys, params.Pattern)
	if err != nil {
		return Error(fmt.Sprintf("glob error: %v", err))
	}

	// Convert to absolute paths and filter.
	var results []string
	for _, m := range matches {
		abs := filepath.Join(base, m)
		// Skip hidden directories (except the pattern explicitly requests them).
		if containsHiddenDir(m) && !strings.HasPrefix(params.Pattern, ".") {
			continue
		}
		if IsBlocked(abs) {
			continue
		}
		results = append(results, abs)
	}

	sort.Strings(results)

	if len(results) == 0 {
		return Success("no files found matching pattern")
	}

	total := len(results)
	if total > globCap {
		results = results[:globCap]
	}

	output := strings.Join(results, "\n")
	if total > globCap {
		output += fmt.Sprintf("\n... +%d more files", total-globCap)
	}

	return Success(output)
}

func containsHiddenDir(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts[:len(parts)-1] { // skip filename itself
		if strings.HasPrefix(p, ".") && p != "." && p != ".." {
			return true
		}
	}
	return false
}
