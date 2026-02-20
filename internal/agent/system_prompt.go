package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/config"
)

// BuildSystemPrompt constructs the system prompt for the LLM.
func BuildSystemPrompt(botUsername, callerChannel, memoryContext string, chatID int64, skillsCatalog string, soulContent *string) string {
	var sb strings.Builder

	// Identity / Soul
	if soulContent != nil && *soulContent != "" {
		sb.WriteString("<soul>\n")
		sb.WriteString(*soulContent)
		sb.WriteString("\n</soul>\n\n")
	} else {
		sb.WriteString("You are a helpful AI assistant")
		if botUsername != "" {
			sb.WriteString(fmt.Sprintf(" named %s", botUsername))
		}
		sb.WriteString(".\n\n")
	}

	// Channel context
	sb.WriteString(fmt.Sprintf("You are communicating via the %s channel.\n", callerChannel))
	sb.WriteString(fmt.Sprintf("Current chat ID: %d\n", chatID))
	sb.WriteString(fmt.Sprintf("Current time: %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	// Capabilities
	sb.WriteString(`You have access to various tools for file operations, web browsing, memory management, scheduling, and more. Use them as needed to complete tasks.

Key behaviors:
- Execute tool calls when needed to fulfill requests
- You can make multiple tool calls in sequence (agentic loop)
- Store important information in memory for future reference
- Be concise and direct in responses
- For code tasks, read relevant files before making changes
`)

	// Skills catalog
	if skillsCatalog != "" {
		sb.WriteString("\n<available_skills>\n")
		sb.WriteString(skillsCatalog)
		sb.WriteString("\n</available_skills>\n")
	}

	// Memory context
	if memoryContext != "" {
		sb.WriteString("\n")
		sb.WriteString(memoryContext)
	}

	return sb.String()
}

// LoadSoulContent loads the SOUL.md content with priority:
// 1. config soul_path
// 2. <data_dir>/SOUL.md
// 3. ./SOUL.md
// 4. Per-chat override: <runtime_dir>/groups/{chatID}/SOUL.md
func LoadSoulContent(cfg *config.Config, chatID int64) *string {
	// Per-chat override takes highest priority.
	perChatPath := filepath.Join(cfg.RuntimeDir(), "groups", fmt.Sprintf("%d", chatID), "SOUL.md")
	if content := readFileIfExists(perChatPath); content != nil {
		return content
	}

	// Config-specified path.
	if cfg.SoulPath != nil && *cfg.SoulPath != "" {
		if content := readFileIfExists(*cfg.SoulPath); content != nil {
			return content
		}
	}

	// Data dir.
	dataPath := filepath.Join(cfg.DataDir, "SOUL.md")
	if content := readFileIfExists(dataPath); content != nil {
		return content
	}

	// Project root.
	if content := readFileIfExists("SOUL.md"); content != nil {
		return content
	}

	return nil
}

func readFileIfExists(path string) *string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return nil
	}
	return &s
}
