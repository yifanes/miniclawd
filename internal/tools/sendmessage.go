package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
)

// ChannelSender is the interface needed by send_message to deliver messages.
type ChannelSender interface {
	SendText(ctx context.Context, chatID int64, text string) error
	SendAttachment(ctx context.Context, chatID int64, filePath string, caption *string) (string, error)
	IsLocalOnly(chatID int64) bool
}

type SendMessageTool struct {
	db      *storage.Database
	sender  ChannelSender
}

func NewSendMessageTool(db *storage.Database, sender ChannelSender) *SendMessageTool {
	return &SendMessageTool{db: db, sender: sender}
}

func (t *SendMessageTool) Name() string { return "send_message" }

func (t *SendMessageTool) Definition() core.ToolDefinition {
	return MakeDef("send_message",
		"Send a message to a chat. Requires text or attachment_path (or both).",
		map[string]any{
			"chat_id":         IntProp("Target chat ID"),
			"text":            StringProp("Message text to send"),
			"attachment_path": StringProp("Local file path to send as attachment"),
			"caption":         StringProp("Caption for the attachment"),
		},
		[]string{"chat_id"},
	)
}

func (t *SendMessageTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
	var params struct {
		ChatID         int64   `json:"chat_id"`
		Text           *string `json:"text"`
		AttachmentPath *string `json:"attachment_path"`
		Caption        *string `json:"caption"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return Error("invalid input: " + err.Error())
	}

	if params.Text == nil && params.AttachmentPath == nil {
		return Error("text or attachment_path is required")
	}

	auth := ExtractAuthContext(input)
	if auth != nil && !auth.CanAccessChat(params.ChatID) {
		return Error("you don't have access to this chat")
	}

	if t.sender != nil && t.sender.IsLocalOnly(params.ChatID) {
		if auth != nil && params.ChatID != auth.CallerChatID {
			return Error("web chats can only send messages to themselves")
		}
	}

	if params.Text != nil && *params.Text != "" {
		if t.sender != nil {
			if err := t.sender.SendText(ctx, params.ChatID, *params.Text); err != nil {
				return Error(fmt.Sprintf("send error: %v", err))
			}
		}
	}

	if params.AttachmentPath != nil && *params.AttachmentPath != "" {
		if t.sender != nil {
			if t.sender.IsLocalOnly(params.ChatID) {
				return Error("attachment_path not supported for web")
			}
			if _, err := t.sender.SendAttachment(ctx, params.ChatID, *params.AttachmentPath, params.Caption); err != nil {
				return Error(fmt.Sprintf("attachment send error: %v", err))
			}
		}
	}

	return Success(fmt.Sprintf("Message sent to chat %d", params.ChatID))
}
