package tools

import (
	"context"
	"encoding/json"

	"github.com/yifanes/miniclawd/internal/core"
)

// Tool is the interface all tools must implement.
type Tool interface {
	Name() string
	Definition() core.ToolDefinition
	Execute(ctx context.Context, input json.RawMessage) ToolResult
}

// ToolResult holds the output of a tool execution.
type ToolResult struct {
	Content   string
	IsError   bool
	StatusCode *int
	Bytes     int
	DurationMs *int64
	ErrorType  *string
}

// Success creates a successful ToolResult.
func Success(content string) ToolResult {
	code := 0
	return ToolResult{Content: content, StatusCode: &code, Bytes: len(content)}
}

// Error creates an error ToolResult.
func Error(content string) ToolResult {
	code := 1
	return ToolResult{Content: content, IsError: true, StatusCode: &code, Bytes: len(content)}
}

// ErrorWithType creates an error ToolResult with an error type tag.
func ErrorWithType(content, errorType string) ToolResult {
	code := 1
	return ToolResult{Content: content, IsError: true, StatusCode: &code, Bytes: len(content), ErrorType: &errorType}
}

// ToolRisk classifies tool risk levels.
type ToolRisk int

const (
	RiskLow    ToolRisk = iota
	RiskMedium
	RiskHigh
)

// ToolAuthContext carries caller identity for authorization checks.
type ToolAuthContext struct {
	CallerChannel  string  `json:"caller_channel"`
	CallerChatID   int64   `json:"caller_chat_id"`
	ControlChatIDs []int64 `json:"control_chat_ids"`
}

// IsControlChat returns true if the caller is a control chat.
func (a *ToolAuthContext) IsControlChat() bool {
	for _, id := range a.ControlChatIDs {
		if id == a.CallerChatID {
			return true
		}
	}
	return false
}

// CanAccessChat returns true if the caller can access the target chat.
func (a *ToolAuthContext) CanAccessChat(targetChatID int64) bool {
	return a.IsControlChat() || a.CallerChatID == targetChatID
}

// ExtractAuthContext extracts the auth context from tool input JSON.
func ExtractAuthContext(input json.RawMessage) *ToolAuthContext {
	var wrapper struct {
		Auth *ToolAuthContext `json:"__miniclawd_auth"`
	}
	if err := json.Unmarshal(input, &wrapper); err != nil {
		return nil
	}
	return wrapper.Auth
}

// InjectAuthContext adds the auth context to tool input JSON.
func InjectAuthContext(input json.RawMessage, auth *ToolAuthContext) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		m = make(map[string]json.RawMessage)
	}
	authBytes, _ := json.Marshal(auth)
	m["__miniclawd_auth"] = authBytes
	out, _ := json.Marshal(m)
	return out
}

// MakeDef is a helper to build a ToolDefinition with a JSON schema.
func MakeDef(name, description string, properties map[string]any, required []string) core.ToolDefinition {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, _ := json.Marshal(schema)
	return core.ToolDefinition{
		Name:        name,
		Description: description,
		InputSchema: raw,
	}
}

// StringProp creates a string property for JSON schema.
func StringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// IntProp creates an integer property for JSON schema.
func IntProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

// BoolProp creates a boolean property for JSON schema.
func BoolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// EnumProp creates a string enum property for JSON schema.
func EnumProp(desc string, values []string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}
