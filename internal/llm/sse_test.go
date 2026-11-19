package llm

import (
	"strings"
	"testing"

	"github.com/yifanes/miniclawd/internal/core"
)

// buildSSE renders SSE frames from raw JSON payloads.
func buildSSE(payloads ...string) string {
	var sb strings.Builder
	for _, p := range payloads {
		sb.WriteString("data: ")
		sb.WriteString(p)
		sb.WriteString("\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")
	return sb.String()
}

func TestParseSSE_TextAfterThinkingBlock(t *testing.T) {
	// A thinking block at index 0 must not shift the text block that follows.
	body := buildSSE(
		`{"type":"message_start","message":{"usage":{"input_tokens":42,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":" world"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
	)

	p := &AnthropicProvider{}
	var streamed strings.Builder
	resp, err := p.parseSSE(strings.NewReader(body), func(d string) { streamed.WriteString(d) })
	if err != nil {
		t.Fatalf("parseSSE error: %v", err)
	}

	if got, want := streamed.String(), "hello world"; got != want {
		t.Errorf("streamed text = %q, want %q", got, want)
	}
	if got, want := resp.StopReason, "end_turn"; got != want {
		t.Errorf("stop reason = %q, want %q", got, want)
	}

	var text string
	for _, b := range resp.Content {
		if b.Type == "text" {
			text += b.Text
		}
		if b.Type == "thinking" {
			t.Errorf("empty/thinking placeholder leaked into response blocks: %q", b.Text)
		}
	}
	if text != "hello world" {
		t.Errorf("response text = %q, want %q", text, "hello world")
	}

	if resp.Usage == nil {
		t.Fatal("usage is nil")
	}
	// input_tokens must survive the message_delta event.
	if resp.Usage.InputTokens != 42 {
		t.Errorf("input tokens = %d, want 42", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 7 {
		t.Errorf("output tokens = %d, want 7", resp.Usage.OutputTokens)
	}
}

func TestParseSSE_ToolUseInput(t *testing.T) {
	body := buildSSE(
		`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"running"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"bash"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
	)

	p := &AnthropicProvider{}
	resp, err := p.parseSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatalf("parseSSE error: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop reason = %q, want tool_use", resp.StopReason)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("got %d blocks, want 2", len(resp.Content))
	}
	tool := resp.Content[1]
	if tool.Type != "tool_use" || tool.ID != "toolu_1" || tool.Name != "bash" {
		t.Fatalf("unexpected tool block: %+v", tool)
	}
	if tool.Input == nil || string(tool.Input) != `{"command":"ls"}` {
		t.Errorf("tool input = %v, want {\"command\":\"ls\"}", tool.Input)
	}
}

func TestMergeUsage(t *testing.T) {
	cur := &core.Usage{InputTokens: 10, OutputTokens: 2}
	next := &core.Usage{InputTokens: 0, OutputTokens: 9}
	merged := mergeUsage(cur, next)
	if merged.InputTokens != 10 || merged.OutputTokens != 9 {
		t.Errorf("merged usage = %+v, want input=10 output=9", merged)
	}
}
