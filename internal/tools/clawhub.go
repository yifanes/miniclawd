package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
)

// ClawHubSearchTool searches the ClawHub registry.
type ClawHubSearchTool struct {
	registryURL string
	token       *string
}

func NewClawHubSearchTool(registryURL string, token *string) *ClawHubSearchTool {
	return &ClawHubSearchTool{registryURL: registryURL, token: token}
}

func (t *ClawHubSearchTool) Name() string { return "clawhub_search" }

func (t *ClawHubSearchTool) Definition() core.ToolDefinition {
	return MakeDef("clawhub_search",
		"Search the ClawHub registry for skills.",
		map[string]any{
			"query": StringProp("Search query"),
		},
		[]string{"query"},
	)
}

func (t *ClawHubSearchTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.Query == "" {
		return Error("query is required")
	}

	u := fmt.Sprintf("%s/api/v1/skills/search?q=%s", strings.TrimRight(t.registryURL, "/"), url.QueryEscape(params.Query))
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return Error(fmt.Sprintf("request error: %v", err))
	}
	if t.token != nil && *t.token != "" {
		req.Header.Set("Authorization", "Bearer "+*t.token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Error(fmt.Sprintf("fetch error: %v", err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return Error(fmt.Sprintf("registry error (status %d): %s", resp.StatusCode, string(body)))
	}

	return Success(string(body))
}

// ClawHubInstallTool installs a skill from ClawHub.
type ClawHubInstallTool struct {
	registryURL string
	token       *string
	skillsDir   string
}

func NewClawHubInstallTool(registryURL string, token *string, skillsDir string) *ClawHubInstallTool {
	return &ClawHubInstallTool{registryURL: registryURL, token: token, skillsDir: skillsDir}
}

func (t *ClawHubInstallTool) Name() string { return "clawhub_install" }

func (t *ClawHubInstallTool) Definition() core.ToolDefinition {
	return MakeDef("clawhub_install",
		"Install a skill from the ClawHub registry.",
		map[string]any{
			"skill_name": StringProp("Name of the skill to install"),
			"version":    StringProp("Version to install (default: latest)"),
		},
		[]string{"skill_name"},
	)
}

func (t *ClawHubInstallTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		SkillName string  `json:"skill_name"`
		Version   *string `json:"version"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	versionParam := ""
	if params.Version != nil && *params.Version != "" {
		versionParam = "?version=" + url.QueryEscape(*params.Version)
	}

	u := fmt.Sprintf("%s/api/v1/skills/%s/download%s",
		strings.TrimRight(t.registryURL, "/"), url.PathEscape(params.SkillName), versionParam)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return Error(fmt.Sprintf("request error: %v", err))
	}
	if t.token != nil && *t.token != "" {
		req.Header.Set("Authorization", "Bearer "+*t.token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Error(fmt.Sprintf("fetch error: %v", err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return Error(fmt.Sprintf("registry error (status %d): %s", resp.StatusCode, string(body)))
	}

	dir := filepath.Join(t.skillsDir, params.SkillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Error(fmt.Sprintf("directory error: %v", err))
	}

	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return Error(fmt.Sprintf("write error: %v", err))
	}

	return Success(fmt.Sprintf("Skill %q installed to %s", params.SkillName, path))
}
