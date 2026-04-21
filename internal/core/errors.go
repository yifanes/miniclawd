package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for matching with errors.Is.
var (
	ErrRateLimited   = errors.New("rate limited, retry after backoff")
	ErrMaxIterations = errors.New("max tool iterations reached")
)

// MiniClawdError wraps domain-specific errors with a kind tag.
type MiniClawdError struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

type ErrorKind int

const (
	ErrKindLLMAPI ErrorKind = iota
	ErrKindDatabase
	ErrKindHTTP
	ErrKindJSON
	ErrKindIO
	ErrKindToolExecution
	ErrKindConfig
)

func (e *MiniClawdError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *MiniClawdError) Unwrap() error { return e.Cause }

func NewLLMError(msg string) error {
	return &MiniClawdError{Kind: ErrKindLLMAPI, Message: msg}
}

func NewLLMErrorf(format string, args ...any) error {
	return &MiniClawdError{Kind: ErrKindLLMAPI, Message: fmt.Sprintf(format, args...)}
}

func NewConfigError(msg string) error {
	return &MiniClawdError{Kind: ErrKindConfig, Message: msg}
}

func NewToolError(msg string) error {
	return &MiniClawdError{Kind: ErrKindToolExecution, Message: msg}
}

func WrapDBError(err error) error {
	return &MiniClawdError{Kind: ErrKindDatabase, Message: "database error", Cause: err}
}

func WrapIOError(err error) error {
	return &MiniClawdError{Kind: ErrKindIO, Message: "io error", Cause: err}
}

// UserFacingError returns a human-readable error message suitable for sending
// to end users via Telegram/Discord. It classifies the error and includes
// enough detail for the user to understand what went wrong.
func UserFacingError(err error) string {
	if err == nil {
		return ""
	}

	// Context cancellation / timeout.
	if errors.Is(err, context.Canceled) {
		return "请求已取消。"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "请求超时，请稍后再试。"
	}

	// Rate limited.
	if errors.Is(err, ErrRateLimited) {
		return "服务繁忙 (rate limited)，请稍后再试。"
	}

	// Max iterations.
	if errors.Is(err, ErrMaxIterations) {
		return "处理步骤过多，已达到最大迭代次数。"
	}

	// Typed MiniClawdError — extract detail.
	var mcErr *MiniClawdError
	if errors.As(err, &mcErr) {
		msg := mcErr.Message
		switch mcErr.Kind {
		case ErrKindLLMAPI:
			// Extract short reason from API error messages.
			reason := extractAPIReason(msg)
			if strings.Contains(msg, "status 429") {
				return "AI 服务繁忙 (429)，请稍后再试。"
			}
			if strings.Contains(msg, "status 5") {
				return fmt.Sprintf("AI 服务暂时不可用: %s", reason)
			}
			if strings.Contains(msg, "status 400") {
				return fmt.Sprintf("请求格式错误: %s", reason)
			}
			return fmt.Sprintf("AI 服务错误: %s", reason)
		case ErrKindDatabase:
			return "内部存储错误，请重试。"
		default:
			return fmt.Sprintf("处理消息时出错: %s", truncateStr(msg, 200))
		}
	}

	return fmt.Sprintf("处理消息时出错: %s", truncateStr(err.Error(), 200))
}

// extractAPIReason tries to pull a readable reason from LLM API error strings.
func extractAPIReason(msg string) string {
	// Try to find "message":"..." in JSON-like error bodies.
	if idx := strings.Index(msg, "\"message\":\""); idx != -1 {
		start := idx + len("\"message\":\"")
		end := strings.Index(msg[start:], "\"")
		if end > 0 {
			return msg[start : start+end]
		}
	}
	// Fallback: return the message after the status prefix.
	if idx := strings.Index(msg, "): "); idx != -1 {
		return truncateStr(msg[idx+3:], 150)
	}
	return truncateStr(msg, 150)
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
