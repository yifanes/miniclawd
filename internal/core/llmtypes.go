package core

import "encoding/json"

// Message represents an LLM conversation message.
type Message struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

// MessageContent is either a plain string or a slice of ContentBlocks.
// When marshalling: string → "content": "text", blocks → "content": [...]
type MessageContent struct {
	Text   string         // used when content is a plain string
	Blocks []ContentBlock // used when content is structured blocks
}

func (mc MessageContent) MarshalJSON() ([]byte, error) {
	if mc.Blocks != nil {
		return json.Marshal(mc.Blocks)
	}
	return json.Marshal(mc.Text)
}

func (mc *MessageContent) UnmarshalJSON(data []byte) error {
	// Try string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		mc.Text = s
		mc.Blocks = nil
		return nil
	}
	// Try blocks.
	var blocks []ContentBlock
	if err := json.Unmarshal(data, &blocks); err == nil {
		mc.Blocks = blocks
		mc.Text = ""
		return nil
	}
	// Fallback: treat as string.
	mc.Text = string(data)
	return nil
}

// IsBlocks returns true if the content uses block format.
func (mc MessageContent) IsBlocks() bool {
	return mc.Blocks != nil
}

// TextContent creates a simple text MessageContent.
func TextContent(text string) MessageContent {
	return MessageContent{Text: text}
}

// BlocksContent creates a blocks-based MessageContent.
func BlocksContent(blocks []ContentBlock) MessageContent {
	return MessageContent{Blocks: blocks}
}

// ContentBlock is a tagged union for message content parts.
type ContentBlock struct {
	Type string `json:"type"`

	// Text block fields
	Text string `json:"text,omitempty"`

	// Image block fields
	Source *ImageSource `json:"source,omitempty"`

	// ToolUse block fields
	ID    string           `json:"id,omitempty"`
	Name  string           `json:"name,omitempty"`
	Input *json.RawMessage `json:"input,omitempty"`

	// ToolResult block fields
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   *bool  `json:"is_error,omitempty"`
}

// ImageSource describes an inline image.
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg", etc.
	Data      string `json:"data"`       // base64-encoded
}

// Convenience constructors for ContentBlock.

func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

func ImageBlock(mediaType, base64Data string) ContentBlock {
	return ContentBlock{
		Type: "image",
		Source: &ImageSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      base64Data,
		},
	}
}

func ToolUseBlock(id, name string, input json.RawMessage) ContentBlock {
	raw := input
	return ContentBlock{
		Type:  "tool_use",
		ID:    id,
		Name:  name,
		Input: &raw,
	}
}

func ToolResultBlock(toolUseID, content string, isError bool) ContentBlock {
	var errPtr *bool
	if isError {
		errPtr = &isError
	}
	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
		IsError:   errPtr,
	}
}

// ToolDefinition describes a tool for the LLM.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// MessagesRequest is the request body for the Anthropic Messages API.
type MessagesRequest struct {
	Model     string           `json:"model"`
	MaxTokens uint32           `json:"max_tokens"`
	System    string           `json:"system"`
	Messages  []Message        `json:"messages"`
	Tools     []ToolDefinition `json:"tools,omitempty"`
	Stream    *bool            `json:"stream,omitempty"`
}

// MessagesResponse is the response from an LLM provider.
type MessagesResponse struct {
	Content    []ResponseContentBlock `json:"content"`
	StopReason string                 `json:"stop_reason"`
	Usage      *Usage                 `json:"usage,omitempty"`
}

// ResponseContentBlock represents a block in the LLM response.
type ResponseContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// Usage tracks token counts.
type Usage struct {
	InputTokens  uint32 `json:"input_tokens"`
	OutputTokens uint32 `json:"output_tokens"`
}
