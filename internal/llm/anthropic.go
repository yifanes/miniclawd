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

// AnthropicProvider implements the Anthropic Messages API.
type AnthropicProvider struct {
	client    *http.Client
	apiKey    string
	model     string
	maxTokens uint32
	baseURL   string
}

func NewAnthropicProvider(apiKey, model string, maxTokens uint32, baseURL string) *AnthropicProvider {
	url := resolveAnthropicURL(baseURL)
	return &AnthropicProvider{
		client:    &http.Client{},
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		baseURL:   url,
	}
}

func (p *AnthropicProvider) ProviderName() string { return "anthropic" }
func (p *AnthropicProvider) ModelName() string     { return p.model }

func (p *AnthropicProvider) SendMessage(ctx context.Context, system string, messages []core.Message,
	tools []core.ToolDefinition) (*core.MessagesResponse, error) {
	return p.doRequest(ctx, system, messages, tools, false, nil)
}

func (p *AnthropicProvider) SendMessageStream(ctx context.Context, system string, messages []core.Message,
	tools []core.ToolDefinition, onDelta func(string)) (*core.MessagesResponse, error) {
	return p.doRequest(ctx, system, messages, tools, true, onDelta)
}

func (p *AnthropicProvider) doRequest(ctx context.Context, system string, messages []core.Message,
	tools []core.ToolDefinition, stream bool, onDelta func(string)) (*core.MessagesResponse, error) {

	body := map[string]any{
		"model":      p.model,
		"max_tokens": p.maxTokens,
		"system":     system,
		"messages":   messages,
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

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
		return nil, core.NewLLMErrorf("anthropic API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	if stream {
		return p.parseSSE(resp.Body, onDelta)
	}

	var result core.MessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// parseSSE processes Anthropic SSE stream events.
func (p *AnthropicProvider) parseSSE(body io.Reader, onDelta func(string)) (*core.MessagesResponse, error) {
	scanner := bufio.NewScanner(body)
	// Allow large lines for tool input JSON.
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var (
		blocks     []core.ResponseContentBlock
		stopReason string
		usage      *core.Usage
		// Track tool_use blocks being streamed incrementally.
		toolBlocks   []streamToolBlock
		currentIndex int = -1
	)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event sseEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			currentIndex = event.Index
			if event.ContentBlock != nil {
				switch event.ContentBlock.Type {
				case "text":
					blocks = append(blocks, core.ResponseContentBlock{Type: "text"})
				case "tool_use":
					blocks = append(blocks, core.ResponseContentBlock{
						Type: "tool_use",
						ID:   event.ContentBlock.ID,
						Name: event.ContentBlock.Name,
					})
					toolBlocks = append(toolBlocks, streamToolBlock{
						index:     currentIndex,
						id:        event.ContentBlock.ID,
						name:      event.ContentBlock.Name,
						inputJSON: strings.Builder{},
					})
				}
			}

		case "content_block_delta":
			if event.Delta != nil {
				switch event.Delta.Type {
				case "text_delta":
					if currentIndex >= 0 && currentIndex < len(blocks) {
						blocks[currentIndex].Text += event.Delta.Text
					}
					if onDelta != nil {
						onDelta(event.Delta.Text)
					}
				case "input_json_delta":
					// Append to the corresponding tool block's JSON accumulator.
					for i := range toolBlocks {
						if toolBlocks[i].index == currentIndex {
							toolBlocks[i].inputJSON.WriteString(event.Delta.PartialJSON)
							break
						}
					}
				}
			}

		case "content_block_stop":
			// Finalize any tool_use block's input JSON.
			for _, tb := range toolBlocks {
				if tb.index == currentIndex && currentIndex < len(blocks) {
					raw := json.RawMessage(tb.inputJSON.String())
					if len(raw) == 0 {
						raw = json.RawMessage("{}")
					}
					blocks[currentIndex].Input = raw
					break
				}
			}

		case "message_delta":
			if event.Delta != nil {
				if event.Delta.StopReason != "" {
					stopReason = event.Delta.StopReason
				}
			}
			if event.Usage != nil {
				usage = event.Usage
			}

		case "message_start":
			if event.Message != nil && event.Message.Usage != nil {
				usage = event.Message.Usage
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	return &core.MessagesResponse{
		Content:    blocks,
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}

type streamToolBlock struct {
	index     int
	id        string
	name      string
	inputJSON strings.Builder
}

// SSE event structures for Anthropic streaming.
type sseEvent struct {
	Type         string          `json:"type"`
	Index        int             `json:"index"`
	ContentBlock *sseBlock       `json:"content_block,omitempty"`
	Delta        *sseDelta       `json:"delta,omitempty"`
	Message      *sseMessage     `json:"message,omitempty"`
	Usage        *core.Usage     `json:"usage,omitempty"`
}

type sseBlock struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type sseDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type sseMessage struct {
	Usage *core.Usage `json:"usage,omitempty"`
}

func resolveAnthropicURL(baseURL string) string {
	if baseURL == "" {
		return "https://api.anthropic.com/v1/messages"
	}
	u := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(u, "/messages") && !strings.HasSuffix(u, "/v1/messages") {
		if strings.HasSuffix(u, "/v1") {
			return u + "/messages"
		}
		return u + "/v1/messages"
	}
	return u
}
