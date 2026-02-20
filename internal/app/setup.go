package app

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// RunSetup runs the interactive setup wizard to create miniclawd.config.yaml.
func RunSetup() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("MiniClawd Setup Wizard")
	fmt.Println("======================")
	fmt.Println()

	// LLM Provider.
	fmt.Print("LLM provider (anthropic/openai/ollama) [anthropic]: ")
	provider, _ := reader.ReadString('\n')
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "anthropic"
	}

	// API Key.
	fmt.Print("API key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	// Model (auto-detect).
	model := ""
	switch provider {
	case "anthropic":
		model = "claude-sonnet-4-5-20250929"
	case "ollama":
		model = "llama3.2"
	default:
		model = "gpt-4o"
	}
	fmt.Printf("Model [%s]: ", model)
	customModel, _ := reader.ReadString('\n')
	customModel = strings.TrimSpace(customModel)
	if customModel != "" {
		model = customModel
	}

	// Telegram.
	fmt.Print("Telegram bot token (leave empty to skip): ")
	tgToken, _ := reader.ReadString('\n')
	tgToken = strings.TrimSpace(tgToken)

	tgUsername := ""
	if tgToken != "" {
		fmt.Print("Telegram bot username (without @): ")
		tgUsername, _ = reader.ReadString('\n')
		tgUsername = strings.TrimSpace(tgUsername)
	}

	// Write config.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("llm_provider: %q\n", provider))
	sb.WriteString(fmt.Sprintf("api_key: %q\n", apiKey))
	sb.WriteString(fmt.Sprintf("model: %q\n", model))
	sb.WriteString("max_tokens: 8192\n")
	sb.WriteString("data_dir: \"./miniclawd.data\"\n")
	sb.WriteString("working_dir: \"./tmp\"\n")
	sb.WriteString("timezone: \"UTC\"\n")

	if tgToken != "" {
		sb.WriteString(fmt.Sprintf("telegram_bot_token: %q\n", tgToken))
		sb.WriteString(fmt.Sprintf("bot_username: %q\n", tgUsername))
	}

	configPath := "miniclawd.config.yaml"
	if err := os.WriteFile(configPath, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Printf("\nConfig written to %s\n", configPath)
	fmt.Println("Run 'miniclawd start' to begin.")
	return nil
}
