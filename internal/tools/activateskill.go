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

type ActivateSkillTool struct {
	skillsDir string
}

func NewActivateSkillTool(skillsDir string) *ActivateSkillTool {
	return &ActivateSkillTool{skillsDir: skillsDir}
}

func (t *ActivateSkillTool) Name() string { return "activate_skill" }

func (t *ActivateSkillTool) Definition() core.ToolDefinition {
	return MakeDef("activate_skill",
		"Activate a skill by name. Loads and returns the skill's instructions from SKILL.md.",
		map[string]any{
			"skill_name": StringProp("Name of the skill to activate"),
		},
		[]string{"skill_name"},
	)
}

func (t *ActivateSkillTool) Execute(_ context.Context, input json.RawMessage) ToolResult {
	var params struct {
		SkillName string `json:"skill_name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}
	if params.SkillName == "" {
		return Error("skill_name is required")
	}

	path := filepath.Join(t.skillsDir, params.SkillName, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Error(fmt.Sprintf("skill %q not found", params.SkillName))
		}
		return Error(fmt.Sprintf("read error: %v", err))
	}

	content := string(data)

	// Parse YAML frontmatter if present.
	var meta map[string]any
	body := content
	if strings.HasPrefix(content, "---\n") {
		parts := strings.SplitN(content[4:], "\n---\n", 2)
		if len(parts) == 2 {
			yaml.Unmarshal([]byte(parts[0]), &meta)
			body = parts[1]
		}
	}

	var sb strings.Builder
	if meta != nil {
		if name, ok := meta["name"]; ok {
			sb.WriteString(fmt.Sprintf("Skill: %v\n", name))
		}
		if desc, ok := meta["description"]; ok {
			sb.WriteString(fmt.Sprintf("Description: %v\n", desc))
		}
		if version, ok := meta["version"]; ok {
			sb.WriteString(fmt.Sprintf("Version: %v\n", version))
		}
		sb.WriteString("\n---\n\n")
	}
	sb.WriteString(strings.TrimSpace(body))

	return Success(sb.String())
}
