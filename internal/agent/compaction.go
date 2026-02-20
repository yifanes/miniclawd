package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/llm"
)

// CompactMessages summarizes old messages and keeps recent ones verbatim.
// Returns the compacted message list.
func CompactMessages(ctx context.Context, provider llm.LLMProvider, messages []core.Message, keepRecent int, timeoutSecs int) ([]core.Message, error) {
	if len(messages) <= keepRecent {
		return messages, nil
	}

	// Split into old (to summarize) and recent (to keep).
	splitPoint := len(messages) - keepRecent
	oldMessages := messages[:splitPoint]
	recentMessages := messages[splitPoint:]

	// Build summary of old messages.
	var summaryInput strings.Builder
	summaryInput.WriteString("Summarize the following conversation concisely, preserving key facts, decisions, and context:\n\n")
	for _, msg := range oldMessages {
		text := MessageToText(&msg)
		if text != "" {
			summaryInput.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, text))
		}
	}

	summaryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	resp, err := provider.SendMessage(summaryCtx, "You are a concise summarizer. Summarize the conversation preserving key context.", []core.Message{
		{Role: "user", Content: core.TextContent(summaryInput.String())},
	}, nil)
	if err != nil {
		return messages, fmt.Errorf("compaction LLM call: %w", err)
	}

	// Extract summary text.
	var summaryText string
	for _, block := range resp.Content {
		if block.Type == "text" {
			summaryText += block.Text
		}
	}

	if summaryText == "" {
		return messages, nil
	}

	// Build compacted message list.
	compacted := []core.Message{
		{
			Role: "user",
			Content: core.TextContent(fmt.Sprintf(
				"[Previous conversation summary]\n%s\n[End of summary - conversation continues below]",
				summaryText,
			)),
		},
		{
			Role:    "assistant",
			Content: core.TextContent("Understood, I have the context from our previous conversation. Let's continue."),
		},
	}
	compacted = append(compacted, recentMessages...)

	return compacted, nil
}

// ArchiveConversation saves messages to a markdown file before compaction.
func ArchiveConversation(dataDir, channel string, chatID int64, messages []core.Message) {
	dir := filepath.Join(dataDir, "runtime", "groups", fmt.Sprintf("%d", chatID), "archives")
	os.MkdirAll(dir, 0o755)

	ts := time.Now().UTC().Format("20060102_150405")
	path := filepath.Join(dir, fmt.Sprintf("archive_%s.md", ts))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Conversation Archive\n\nChannel: %s\nChat ID: %d\nArchived: %s\n\n---\n\n",
		channel, chatID, ts))

	for _, msg := range messages {
		text := MessageToText(&msg)
		sb.WriteString(fmt.Sprintf("**%s**\n\n%s\n\n---\n\n", msg.Role, text))
	}

	os.WriteFile(path, []byte(sb.String()), 0o644)
}

// SaveSession serializes messages and persists to the database.
func SaveSession(db interface{ SaveSession(int64, string) error }, chatID int64, messages []core.Message) error {
	// Strip images before saving.
	stripped := StripImagesForSession(messages)
	data, err := json.Marshal(stripped)
	if err != nil {
		return fmt.Errorf("marshalling session: %w", err)
	}
	return db.SaveSession(chatID, string(data))
}

// LoadSession deserializes messages from the database.
// Returns (messages, updatedAt, found, error).
func LoadSession(db interface {
	LoadSession(int64) (string, string, bool, error)
}, chatID int64) ([]core.Message, string, bool, error) {
	data, updatedAt, found, err := db.LoadSession(chatID)
	if err != nil || !found {
		return nil, "", found, err
	}
	var messages []core.Message
	if err := json.Unmarshal([]byte(data), &messages); err != nil {
		return nil, updatedAt, true, fmt.Errorf("unmarshalling session: %w", err)
	}
	return messages, updatedAt, true, nil
}
