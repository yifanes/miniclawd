package tools

import (
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
	maxSkillContentChars = 100_000
	maxSkillNameChars    = 64
)

var skillNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// SkillManageTool provides CRUD operations on skill files.
type SkillManageTool struct {
	skillsDir      string
	controlChatIDs []int64
}

// NewSkillManageTool creates a new SkillManageTool.
func NewSkillManageTool(skillsDir string, controlChatIDs []int64) *SkillManageTool {
	return &SkillManageTool{
		skillsDir:      skillsDir,
		controlChatIDs: controlChatIDs,
	}
}

func (t *SkillManageTool) Name() string { return "skill_manage" }

func (t *SkillManageTool) Definition() core.ToolDefinition {
	return core.ToolDefinition{
		Name:        "skill_manage",
		Description: "Create, read, update, or delete skill files. Requires control chat authorization.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["create", "read", "update", "delete", "list"],
					"description": "The operation to perform"
				},
				"name": {
					"type": "string",
					"description": "Skill name (alphanumeric, hyphens, underscores, max 64 chars)"
				},
				"content": {
					"type": "string",
					"description": "Skill content in SKILL.md format (for create/update)"
				}
			},
			"required": ["action"]
		}`),
	}
}

func (t *SkillManageTool) isAuthorized(input json.RawMessage) bool {
	if len(t.controlChatIDs) == 0 {
		return false
	}
	auth := extractAuthContext(input)
	if auth == nil {
		return false
	}
	for _, id := range t.controlChatIDs {
		if id == auth.CallerChatID {
			return true
		}
	}
	return false
}

func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name cannot be empty")
	}
	if len(name) > maxSkillNameChars {
		return fmt.Errorf("skill name too long (max %d chars)", maxSkillNameChars)
	}
	if !skillNameRegex.MatchString(name) {
		return fmt.Errorf("skill name must contain only alphanumeric characters, hyphens, and underscores")
	}
	// Prevent path traversal.
	if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("invalid skill name")
	}
	return nil
}

func (t *SkillManageTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	if !t.isAuthorized(input) {
		return Error("skill_manage requires control chat authorization")
	}

	var params struct {
		Action  string `json:"action"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	switch params.Action {
	case "list":
		return t.listSkills()
	case "read":
		if err := validateSkillName(params.Name); err != nil {
			return Error(err.Error())
		}
		return t.readSkill(params.Name)
	case "create":
		if err := validateSkillName(params.Name); err != nil {
			return Error(err.Error())
		}
		if len(params.Content) > maxSkillContentChars {
			return Error(fmt.Sprintf("content too large (max %d chars)", maxSkillContentChars))
		}
		return t.createSkill(params.Name, params.Content)
	case "update":
		if err := validateSkillName(params.Name); err != nil {
			return Error(err.Error())
		}
		if len(params.Content) > maxSkillContentChars {
			return Error(fmt.Sprintf("content too large (max %d chars)", maxSkillContentChars))
		}
		return t.updateSkill(params.Name, params.Content)
	case "delete":
		if err := validateSkillName(params.Name); err != nil {
			return Error(err.Error())
		}
		return t.deleteSkill(params.Name)
	default:
		return Error("unknown action: " + params.Action)
	}
}

func (t *SkillManageTool) skillPath(name string) string {
	return filepath.Join(t.skillsDir, name+".SKILL.md")
}

func (t *SkillManageTool) listSkills() ToolResult {
	entries, err := os.ReadDir(t.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Success("No skills directory found.")
		}
		return Error("failed to list skills: " + err.Error())
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".SKILL.md") {
			name := strings.TrimSuffix(e.Name(), ".SKILL.md")
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return Success("No skills found.")
	}
	return Success(fmt.Sprintf("Skills (%d):\n- %s", len(names), strings.Join(names, "\n- ")))
}

func (t *SkillManageTool) readSkill(name string) ToolResult {
	data, err := os.ReadFile(t.skillPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return Error("skill not found: " + name)
		}
		return Error("failed to read skill: " + err.Error())
	}
	return Success(string(data))
}

func (t *SkillManageTool) createSkill(name, content string) ToolResult {
	path := t.skillPath(name)
	if _, err := os.Stat(path); err == nil {
		return Error("skill already exists: " + name + " (use update to modify)")
	}

	if err := os.MkdirAll(t.skillsDir, 0o755); err != nil {
		return Error("failed to create skills directory: " + err.Error())
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Error("failed to create skill: " + err.Error())
	}
	return Success("Skill created: " + name)
}

func (t *SkillManageTool) updateSkill(name, content string) ToolResult {
	path := t.skillPath(name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Error("skill not found: " + name)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Error("failed to update skill: " + err.Error())
	}
	return Success("Skill updated: " + name)
}

func (t *SkillManageTool) deleteSkill(name string) ToolResult {
	path := t.skillPath(name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Error("skill not found: " + name)
	}

	if err := os.Remove(path); err != nil {
		return Error("failed to delete skill: " + err.Error())
	}
	return Success("Skill deleted: " + name)
}

// extractAuthContext extracts the auth context from tool input JSON.
func extractAuthContext(input json.RawMessage) *ToolAuthContext {
	var wrapper struct {
		Auth *ToolAuthContext `json:"__miniclawd_auth"`
	}
	if err := json.Unmarshal(input, &wrapper); err != nil {
		return nil
	}
	return wrapper.Auth
}
