package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yifanes/miniclawd/internal/core"
)

const (
	grepResultCap = 500
	grepFileCap   = 10000
)

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "target": true,
	".next": true, "__pycache__": true, ".tox": true,
	"vendor": true, "dist": true, "build": true,
}

type GrepTool struct {
	workingDir string
}

func NewGrepTool(workingDir string) *GrepTool {
	return &GrepTool{workingDir: workingDir}
}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Definition() core.ToolDefinition {
	return MakeDef("grep",
		"Search file contents using regex. Returns matching lines with file paths and line numbers (max 500 results).",
		map[string]any{
			"pattern": StringProp("Regex pattern to search for"),
			"path":    StringProp("File or directory to search in (default: working directory)"),
			"glob":    StringProp("Glob filter for file names (e.g., '*.go', '*.ts')"),
		},
		[]string{"pattern"},
	)
}

func (t *GrepTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Pattern == "" {
		return Error("pattern is required")
	}

	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return Error(fmt.Sprintf("invalid regex: %v", err))
	}

	base := t.workingDir
	if params.Path != "" {
		base = resolvePath(t.workingDir, params.Path)
	}

	info, err := os.Stat(base)
	if err != nil {
		return Error(fmt.Sprintf("cannot access path: %v", err))
	}

	var results []string
	fileCount := 0

	if !info.IsDir() {
		// Search single file.
		results = searchFile(base, re, base)
	} else {
		// Walk directory.
		filepath.Walk(base, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if fi.IsDir() {
				if skipDirs[fi.Name()] || (strings.HasPrefix(fi.Name(), ".") && fi.Name() != ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if IsBlocked(path) {
				return nil
			}
			if params.Glob != "" {
				matched, _ := filepath.Match(params.Glob, fi.Name())
				if !matched {
					return nil
				}
			}
			fileCount++
			if fileCount > grepFileCap {
				return fmt.Errorf("file limit reached")
			}
			matches := searchFile(path, re, base)
			results = append(results, matches...)
			if len(results) >= grepResultCap {
				return fmt.Errorf("result limit reached")
			}
			return nil
		})
	}

	if len(results) == 0 {
		return Success("no matches found")
	}

	total := len(results)
	if total > grepResultCap {
		results = results[:grepResultCap]
	}

	output := strings.Join(results, "\n")
	if total > grepResultCap {
		output += fmt.Sprintf("\n... +%d more matches", total-grepResultCap)
	}

	return Success(output)
}

func searchFile(path string, re *regexp.Regexp, base string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	relPath, err := filepath.Rel(base, path)
	if err != nil {
		relPath = path
	}

	var results []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			if len(line) > 500 {
				line = line[:500] + "..."
			}
			results = append(results, fmt.Sprintf("%s:%d: %s", relPath, lineNum, line))
		}
	}
	return results
}
