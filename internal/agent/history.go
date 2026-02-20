package agent

import (
	"fmt"
	"strings"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"
)

// HistoryToMessages converts StoredMessages to LLM Message format.
func HistoryToMessages(history []storage.StoredMessage, botUsername string) []core.Message {
	var messages []core.Message

	for _, msg := range history {
		role := "user"
		if msg.IsFromBot {
			role = "assistant"
		}

		content := msg.Content
		if !msg.IsFromBot && msg.SenderName != "" {
			content = fmt.Sprintf("[%s]: %s", msg.SenderName, content)
		}

		messages = append(messages, core.Message{
			Role:    role,
			Content: core.TextContent(content),
		})
	}

	return mergeConsecutiveMessages(messages)
}

// LoadMessagesFromDB loads recent messages and converts them to LLM format.
func LoadMessagesFromDB(db *storage.Database, chatID int64, chatType string, maxHistory int, botUsername string) ([]core.Message, error) {
	var history []storage.StoredMessage
	var err error

	if chatType == "group" || strings.HasSuffix(chatType, "_group") {
		// Group catch-up: load messages since last bot response.
		history, err = db.GetMessagesSinceLastBotResponse(chatID, maxHistory, maxHistory)
	} else {
		history, err = db.GetRecentMessages(chatID, maxHistory)
	}

	if err != nil {
		return nil, fmt.Errorf("loading messages: %w", err)
	}

	return HistoryToMessages(history, botUsername), nil
}

// mergeConsecutiveMessages merges adjacent same-role messages.
func mergeConsecutiveMessages(messages []core.Message) []core.Message {
	if len(messages) <= 1 {
		return messages
	}

	var merged []core.Message
	for _, msg := range messages {
		if len(merged) > 0 && merged[len(merged)-1].Role == msg.Role {
			last := &merged[len(merged)-1]
			prevText := last.Content.Text
			newText := msg.Content.Text
			if prevText != "" && newText != "" {
				last.Content = core.TextContent(prevText + "\n" + newText)
			} else if newText != "" {
				last.Content = core.TextContent(newText)
			}
		} else {
			merged = append(merged, msg)
		}
	}
	return merged
}

// MessageToText extracts text from a message, handling both string and block content.
func MessageToText(msg *core.Message) string {
	if !msg.Content.IsBlocks() {
		return msg.Content.Text
	}
	var parts []string
	for _, b := range msg.Content.Blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// StripThinking removes <think>...</think> blocks from text.
func StripThinking(text string) string {
	for {
		start := strings.Index(text, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "</think>")
		if end == -1 {
			// Unclosed tag: remove to end.
			text = text[:start]
			break
		}
		text = text[:start] + text[start+end+len("</think>"):]
	}
	return strings.TrimSpace(text)
}

// StripImagesForSession removes image blocks from messages to avoid storing large base64 data.
func StripImagesForSession(messages []core.Message) []core.Message {
	result := make([]core.Message, len(messages))
	for i, msg := range messages {
		if !msg.Content.IsBlocks() {
			result[i] = msg
			continue
		}
		var filtered []core.ContentBlock
		for _, b := range msg.Content.Blocks {
			if b.Type != "image" {
				filtered = append(filtered, b)
			}
		}
		if len(filtered) == 0 {
			result[i] = core.Message{Role: msg.Role, Content: core.TextContent("[image]")}
		} else {
			result[i] = core.Message{Role: msg.Role, Content: core.BlocksContent(filtered)}
		}
	}
	return result
}
