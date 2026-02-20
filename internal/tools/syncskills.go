package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
)

type SyncSkillsTool struct {
	skillsDir string
}

func NewSyncSkillsTool(skillsDir string) *SyncSkillsTool {
	return &SyncSkillsTool{skillsDir: skillsDir}
}

func (t *SyncSkillsTool) Name() string { return "sync_skills" }

func (t *SyncSkillsTool) Definition() core.ToolDefinition {
	return MakeDef("sync_skills",
		"Download a skill from a GitHub repository.",
		map[string]any{
			"skill_name":  StringProp("Skill name in the source repo"),
			"source_repo": StringProp("GitHub repo (e.g., 'gavrielc/nanoclaw')"),
			"git_ref":     StringProp("Git branch or tag (default: 'main')"),
			"target_name": StringProp("Local skill name (default: skill_name)"),
		},
		[]string{"skill_name", "source_repo"},
	)
}

func (t *SyncSkillsTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		SkillName  string  `json:"skill_name"`
		SourceRepo string  `json:"source_repo"`
		GitRef     *string `json:"git_ref"`
		TargetName *string `json:"target_name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	gitRef := "main"
	if params.GitRef != nil && *params.GitRef != "" {
		gitRef = *params.GitRef
	}

	targetName := params.SkillName
	if params.TargetName != nil && *params.TargetName != "" {
		targetName = *params.TargetName
	}

	// Try multiple URL patterns for GitHub raw content.
	urls := []string{
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/skills/%s/SKILL.md", params.SourceRepo, gitRef, params.SkillName),
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/src/skills/%s/SKILL.md", params.SourceRepo, gitRef, params.SkillName),
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/SKILL.md", params.SourceRepo, gitRef, params.SkillName),
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var content string
	var found bool

	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			content = string(body)
			found = true
			break
		}
	}

	if !found {
		return Error(fmt.Sprintf("could not find SKILL.md for %q in %s (ref %s)", params.SkillName, params.SourceRepo, gitRef))
	}

	// Write to local skills directory.
	dir := filepath.Join(t.skillsDir, targetName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Error(fmt.Sprintf("directory error: %v", err))
	}

	path := filepath.Join(dir, "SKILL.md")
	// Normalize frontmatter: ensure it has miniclawd-compatible format.
	normalized := normalizeSkillFrontmatter(content, params.SkillName, params.SourceRepo)
	if err := os.WriteFile(path, []byte(normalized), 0o644); err != nil {
		return Error(fmt.Sprintf("write error: %v", err))
	}

	return Success(fmt.Sprintf("Skill %q synced to %s", targetName, path))
}

func normalizeSkillFrontmatter(content, name, source string) string {
	// If no frontmatter, add one.
	if !strings.HasPrefix(content, "---\n") {
		return fmt.Sprintf("---\nname: %s\nsource: %s\n---\n%s", name, source, content)
	}
	return content
}
