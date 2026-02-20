package agent

// AgentEvent represents events emitted during agent processing (for SSE streaming).
type AgentEvent struct {
	Type       string // "iteration", "tool_start", "tool_result", "text_delta", "final_response"
	Iteration  int
	Name       string
	IsError    bool
	Preview    string
	DurationMs int64
	StatusCode *int
	Bytes      int
	ErrorType  *string
	Delta      string
	Text       string
}

func IterationEvent(iteration int) AgentEvent {
	return AgentEvent{Type: "iteration", Iteration: iteration}
}

func ToolStartEvent(name string) AgentEvent {
	return AgentEvent{Type: "tool_start", Name: name}
}

func ToolResultEvent(name string, isError bool, preview string, durationMs int64, statusCode *int, bytes int, errorType *string) AgentEvent {
	return AgentEvent{
		Type: "tool_result", Name: name, IsError: isError, Preview: preview,
		DurationMs: durationMs, StatusCode: statusCode, Bytes: bytes, ErrorType: errorType,
	}
}

func TextDeltaEvent(delta string) AgentEvent {
	return AgentEvent{Type: "text_delta", Delta: delta}
}

func FinalResponseEvent(text string) AgentEvent {
	return AgentEvent{Type: "final_response", Text: text}
}
