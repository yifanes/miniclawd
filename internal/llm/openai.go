package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yifanes/miniclawd/internal/core"
)

// OpenAIProvider implements the OpenAI-compatible chat completions API.
type OpenAIProvider struct {
	client    *http.Client
	apiKey    string
	model     string
	maxTokens uint32
	chatURL   string
}

func NewOpenAIProvider(apiKey, model string, maxTokens uint32, baseURL string) *OpenAIProvider {
	url := resolveOpenAIURL(baseURL)
	return &OpenAIProvider{
		client:    &http.Client{},
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		chatURL:   url,
	}
}

func (p *OpenAIProvider) ProviderName() string { return "openai" }
func (p *OpenAIProvider) ModelName() string     { return p.model }

func (p *OpenAIProvider) SendMessage(ctx context.Context, system string, messages []core.Message,
	tools []core.ToolDefinition) (*core.MessagesResponse, error) {
	return p.doRequest(ctx, system, messages, tools, false, nil)
}

func (p *OpenAIProvider) SendMessageStream(ctx context.Context, system string, messages []core.Message,
	tools []core.ToolDefinition, onDelta func(string)) (*core.MessagesResponse, error) {
	return p.doRequest(ctx, system, messages, tools, true, onDelta)
}

func (p *OpenAIProvider) doRequest(ctx context.Context, system string, messages []core.Message,
	tools []core.ToolDefinition, stream bool, onDelta func(string)) (*core.MessagesResponse, error) {

	// Convert to OpenAI format.
	oaiMessages := translateToOpenAI(system, messages)
	var oaiTools []oaiToolDef
	for _, t := range tools {
		oaiTools = append(oaiTools, oaiToolDef{
			Type: "function",
			Function: oaiFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	body := map[string]any{
		"model":      p.model,
		"max_tokens": p.maxTokens,
		"messages":   oaiMessages,
	}
	if len(oaiTools) > 0 {
		body["tools"] = oaiTools
	}
	if stream {
		body["stream"] = true
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.chatURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		io.Copy(io.Discard, resp.Body)
		return nil, core.ErrRateLimited
	}
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, core.NewLLMErrorf("openai API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	if stream {
		return p.parseStream(resp.Body, onDelta)
	}

	return p.parseNonStream(resp.Body)
}

func (p *OpenAIProvider) parseNonStream(body io.Reader) (*core.MessagesResponse, error) {
	var oaiResp oaiResponse
	if err := json.NewDecoder(body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return translateFromOpenAI(oaiResp), nil
}

func (p *OpenAIProvider) parseStream(body io.Reader, onDelta func(string)) (*core.MessagesResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var textContent strings.Builder
	var toolCalls []oaiStreamToolCall
	var finishReason string
	var usage *core.Usage

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk oaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				usage = &core.Usage{
					InputTokens:  uint32(chunk.Usage.PromptTokens),
					OutputTokens: uint32(chunk.Usage.CompletionTokens),
				}
			}
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			textContent.WriteString(delta.Content)
			if onDelta != nil {
				onDelta(delta.Content)
			}
		}

		if chunk.Choices[0].FinishReason != "" {
			finishReason = chunk.Choices[0].FinishReason
		}

		for _, tc := range delta.ToolCalls {
			// Extend or create tool call entry.
			for len(toolCalls) <= tc.Index {
				toolCalls = append(toolCalls, oaiStreamToolCall{})
			}
			if tc.ID != "" {
				toolCalls[tc.Index].ID = tc.ID
			}
			if tc.Function.Name != "" {
				toolCalls[tc.Index].Name = tc.Function.Name
			}
			toolCalls[tc.Index].Arguments += tc.Function.Arguments
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading stream: %w", err)
	}

	// Build response.
	var blocks []core.ResponseContentBlock
	if textContent.Len() > 0 {
		blocks = append(blocks, core.ResponseContentBlock{Type: "text", Text: textContent.String()})
	}
	for _, tc := range toolCalls {
		blocks = append(blocks, core.ResponseContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: json.RawMessage(tc.Arguments),
		})
	}

	stopReason := translateStopReason(finishReason)
	return &core.MessagesResponse{
		Content:    blocks,
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}

// --- OpenAI format translation ---

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    any             `json:"content,omitempty"`
	ToolCalls  []oaiToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type oaiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiToolDef struct {
	Type     string         `json:"type"`
	Function oaiFunctionDef `json:"function"`
}

type oaiFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Usage   *oaiUsage   `json:"usage,omitempty"`
}

type oaiChoice struct {
	Message      oaiRespMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type oaiRespMessage struct {
	Content   *string       `json:"content"`
	ToolCalls []oaiToolCall `json:"tool_calls,omitempty"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type oaiStreamChunk struct {
	Choices []oaiStreamChoice `json:"choices"`
	Usage   *oaiUsage         `json:"usage,omitempty"`
}

type oaiStreamChoice struct {
	Delta        oaiStreamDelta `json:"delta"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

type oaiStreamDelta struct {
	Content   string                 `json:"content,omitempty"`
	ToolCalls []oaiStreamToolCallDelta `json:"tool_calls,omitempty"`
}

type oaiStreamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type oaiStreamToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// translateToOpenAI converts Anthropic-style messages to OpenAI format.
func translateToOpenAI(system string, messages []core.Message) []oaiMessage {
	var out []oaiMessage

	// System message.
	if system != "" {
		out = append(out, oaiMessage{Role: "system", Content: system})
	}

	for _, msg := range messages {
		if !msg.Content.IsBlocks() {
			out = append(out, oaiMessage{Role: msg.Role, Content: msg.Content.Text})
			continue
		}

		// Process blocks: split into text, tool_use, tool_result groups.
		var textParts []string
		var toolCalls []oaiToolCall
		var toolResults []oaiMessage

		for _, block := range msg.Content.Blocks {
			switch block.Type {
			case "text":
				textParts = append(textParts, block.Text)
			case "tool_use":
				inputStr := "{}"
				if block.Input != nil {
					inputStr = string(*block.Input)
				}
				toolCalls = append(toolCalls, oaiToolCall{
					ID:   block.ID,
					Type: "function",
					Function: oaiFunction{
						Name:      block.Name,
						Arguments: inputStr,
					},
				})
			case "tool_result":
				role := "tool"
				toolResults = append(toolResults, oaiMessage{
					Role:       role,
					Content:    block.Content,
					ToolCallID: block.ToolUseID,
				})
			case "image":
				textParts = append(textParts, "[image]")
			}
		}

		if msg.Role == "assistant" {
			m := oaiMessage{Role: "assistant"}
			if len(textParts) > 0 {
				m.Content = strings.Join(textParts, "\n")
			}
			if len(toolCalls) > 0 {
				m.ToolCalls = toolCalls
			}
			out = append(out, m)
		} else if msg.Role == "user" {
			if len(textParts) > 0 {
				out = append(out, oaiMessage{Role: "user", Content: strings.Join(textParts, "\n")})
			}
		}

		// Tool results are separate messages.
		out = append(out, toolResults...)
	}

	return out
}

// translateFromOpenAI converts an OpenAI response to our standard format.
func translateFromOpenAI(resp oaiResponse) *core.MessagesResponse {
	if len(resp.Choices) == 0 {
		return &core.MessagesResponse{StopReason: "end_turn"}
	}

	choice := resp.Choices[0]
	var blocks []core.ResponseContentBlock

	if choice.Message.Content != nil && *choice.Message.Content != "" {
		blocks = append(blocks, core.ResponseContentBlock{Type: "text", Text: *choice.Message.Content})
	}

	for _, tc := range choice.Message.ToolCalls {
		blocks = append(blocks, core.ResponseContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	var usage *core.Usage
	if resp.Usage != nil {
		usage = &core.Usage{
			InputTokens:  uint32(resp.Usage.PromptTokens),
			OutputTokens: uint32(resp.Usage.CompletionTokens),
		}
	}

	return &core.MessagesResponse{
		Content:    blocks,
		StopReason: translateStopReason(choice.FinishReason),
		Usage:      usage,
	}
}

func translateStopReason(oaiReason string) string {
	switch oaiReason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

func resolveOpenAIURL(baseURL string) string {
	if baseURL == "" {
		return "https://api.openai.com/v1/chat/completions"
	}
	u := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(u, "/chat/completions") {
		if strings.HasSuffix(u, "/v1") {
			return u + "/chat/completions"
		}
		return u + "/v1/chat/completions"
	}
	return u
}
