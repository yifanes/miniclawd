package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillMeta holds parsed SKILL.md frontmatter.
type SkillMeta struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	UpdatedAt   string   `yaml:"updated_at"`
	Platforms   []string `yaml:"platforms"`
	Deps        []string `yaml:"deps"`
}

// Skill represents a discovered skill.
type Skill struct {
	Name        string
	DirName     string
	Meta        SkillMeta
	Body        string // instruction body (after frontmatter)
	Path        string // full path to SKILL.md
}

// SkillManager discovers and manages skills.
type SkillManager struct {
	skillsDir string
	skills    []Skill
}

// NewSkillManager creates a SkillManager scanning the skills directory.
func NewSkillManager(skillsDir string) *SkillManager {
	sm := &SkillManager{skillsDir: skillsDir}
	sm.loadSkills()
	return sm
}

func (m *SkillManager) loadSkills() {
	entries, err := os.ReadDir(m.skillsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillFile := filepath.Join(m.skillsDir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		skill := parseSkillFile(string(data), entry.Name(), skillFile)
		m.skills = append(m.skills, skill)
	}
}

func parseSkillFile(content, dirName, path string) Skill {
	skill := Skill{
		DirName: dirName,
		Path:    path,
		Body:    content,
	}

	if strings.HasPrefix(content, "---\n") {
		parts := strings.SplitN(content[4:], "\n---\n", 2)
		if len(parts) == 2 {
			var meta SkillMeta
			yaml.Unmarshal([]byte(parts[0]), &meta)
			skill.Meta = meta
			skill.Body = strings.TrimSpace(parts[1])
		}
	}

	skill.Name = skill.Meta.Name
	if skill.Name == "" {
		skill.Name = dirName
	}

	return skill
}

// List returns all discovered skills.
func (m *SkillManager) List() []Skill {
	return m.skills
}

// Get returns a skill by name.
func (m *SkillManager) Get(name string) *Skill {
	for i := range m.skills {
		if m.skills[i].Name == name || m.skills[i].DirName == name {
			return &m.skills[i]
		}
	}
	return nil
}

// BuildCatalog returns a formatted skills catalog for the system prompt.
func (m *SkillManager) BuildCatalog() string {
	if len(m.skills) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, s := range m.skills {
		desc := s.Meta.Description
		if desc == "" {
			desc = "(no description)"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, desc))
	}
	return sb.String()
}

// Reload rescans the skills directory.
func (m *SkillManager) Reload() {
	m.skills = nil
	m.loadSkills()
}
