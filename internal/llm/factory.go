package llm

import "github.com/yifanes/miniclawd/internal/config"

// CreateProvider builds the appropriate LLM provider from config.
func CreateProvider(cfg *config.Config) LLMProvider {
	baseURL := ""
	if cfg.LLMBaseURL != nil {
		baseURL = *cfg.LLMBaseURL
	}

	switch cfg.LLMProvider {
	case "anthropic":
		return NewAnthropicProvider(cfg.APIKey, cfg.Model, cfg.MaxTokens, baseURL)
	default:
		// openai, ollama, and other OpenAI-compatible providers
		return NewOpenAIProvider(cfg.APIKey, cfg.Model, cfg.MaxTokens, baseURL)
	}
}
