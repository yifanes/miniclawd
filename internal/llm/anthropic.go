package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

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
		client:    newHTTPClient(),
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

	const maxRetries = 3
	backoffs := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[anthropic] retry %d/%d after error: %v", attempt, maxRetries-1, lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoffs[attempt-1]):
			}
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
			lastErr = fmt.Errorf("http request: %w", err)
			continue // network error, retry
		}

		if resp.StatusCode == 429 {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = core.ErrRateLimited
			continue // rate limited, retry
		}
		if resp.StatusCode >= 500 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = core.NewLLMErrorf("anthropic API error (status %d): %s", resp.StatusCode, string(errBody))
			continue // server error, retry
		}
		if resp.StatusCode != 200 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, core.NewLLMErrorf("anthropic API error (status %d): %s", resp.StatusCode, string(errBody))
		}

		// Success path.
		if stream {
			result, parseErr := p.parseSSE(resp.Body, onDelta)
			resp.Body.Close()
			return result, parseErr
		}
		var result core.MessagesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		resp.Body.Close()
		return &result, nil
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
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
		toolBlocks = make(map[int]*streamToolBlock)
		// blockPos maps the SSE block index to its position in blocks.
		blockPos = make(map[int]int)
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
			// Always append a placeholder so that block indices stay aligned with
			// their position in blocks, even for blocks we do not render
			// (e.g. thinking, redacted_thinking).
			blockPos[event.Index] = len(blocks)
			if event.ContentBlock != nil {
				switch event.ContentBlock.Type {
				case "tool_use":
					blocks = append(blocks, core.ResponseContentBlock{
						Type: "tool_use",
						ID:   event.ContentBlock.ID,
						Name: event.ContentBlock.Name,
					})
					toolBlocks[event.Index] = &streamToolBlock{
						id:   event.ContentBlock.ID,
						name: event.ContentBlock.Name,
					}
				default:
					blocks = append(blocks, core.ResponseContentBlock{Type: event.ContentBlock.Type})
				}
			} else {
				blocks = append(blocks, core.ResponseContentBlock{Type: "text"})
			}

		case "content_block_delta":
			pos, ok := blockPos[event.Index]
			if !ok || pos >= len(blocks) {
				continue
			}
			if event.Delta != nil {
				switch event.Delta.Type {
				case "text_delta":
					blocks[pos].Text += event.Delta.Text
					if onDelta != nil {
						onDelta(event.Delta.Text)
					}
				case "input_json_delta":
					if tb, ok := toolBlocks[event.Index]; ok {
						tb.inputJSON.WriteString(event.Delta.PartialJSON)
					}
				}
			}

		case "content_block_stop":
			if tb, ok := toolBlocks[event.Index]; ok {
				pos := blockPos[event.Index]
				raw := json.RawMessage(tb.inputJSON.String())
				if len(raw) == 0 {
					raw = json.RawMessage("{}")
				}
				if pos < len(blocks) {
					blocks[pos].Input = raw
				}
				delete(toolBlocks, event.Index)
			}

		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != "" {
				stopReason = event.Delta.StopReason
			}
			// message_delta carries only output_tokens; merge instead of replacing
			// so input_tokens from message_start is preserved.
			if event.Usage != nil {
				usage = mergeUsage(usage, event.Usage)
			}

		case "message_start":
			if event.Message != nil && event.Message.Usage != nil {
				usage = mergeUsage(usage, event.Message.Usage)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	// Drop placeholder blocks that never received any content.
	var final []core.ResponseContentBlock
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				final = append(final, b)
			}
		case "tool_use":
			final = append(final, b)
		}
	}

	return &core.MessagesResponse{
		Content:    final,
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}

// mergeUsage combines token usage from multiple SSE events, preferring the
// largest values seen for each field.
func mergeUsage(cur, next *core.Usage) *core.Usage {
	if cur == nil {
		copyUsage := *next
		return &copyUsage
	}
	if next.InputTokens > cur.InputTokens {
		cur.InputTokens = next.InputTokens
	}
	if next.OutputTokens > cur.OutputTokens {
		cur.OutputTokens = next.OutputTokens
	}
	return cur
}

type streamToolBlock struct {
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
