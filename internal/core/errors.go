package core

import (
	"errors"
	"fmt"
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
