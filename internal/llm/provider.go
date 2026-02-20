package llm

import (
	"context"

	"github.com/yifanes/miniclawd/internal/core"
)

// LLMProvider defines the interface for LLM backends.
type LLMProvider interface {
	// SendMessage sends a non-streaming request.
	SendMessage(ctx context.Context, system string, messages []core.Message,
		tools []core.ToolDefinition) (*core.MessagesResponse, error)

	// SendMessageStream sends a streaming request, calling onDelta for each text chunk.
	SendMessageStream(ctx context.Context, system string, messages []core.Message,
		tools []core.ToolDefinition, onDelta func(string)) (*core.MessagesResponse, error)

	// ProviderName returns the provider identifier (e.g., "anthropic", "openai").
	ProviderName() string

	// ModelName returns the model being used.
	ModelName() string
}
