package llm

import "github.com/yifanes/miniclawd/internal/core"

// SanitizeMessages cleans messages before sending to the LLM:
// 1. Removes orphan tool_result blocks (no matching tool_use)
// 2. Merges consecutive same-role messages
// 3. Ensures the conversation ends with a user message
func SanitizeMessages(messages []core.Message) []core.Message {
	if len(messages) == 0 {
		return messages
	}

	// Collect all tool_use IDs from assistant messages.
	toolUseIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		if msg.Content.IsBlocks() {
			for _, b := range msg.Content.Blocks {
				if b.Type == "tool_use" {
					toolUseIDs[b.ID] = true
				}
			}
		}
	}

	// Remove orphan tool_result blocks.
	var cleaned []core.Message
	for _, msg := range messages {
		if msg.Role == "user" && msg.Content.IsBlocks() {
			var kept []core.ContentBlock
			for _, b := range msg.Content.Blocks {
				if b.Type == "tool_result" && !toolUseIDs[b.ToolUseID] {
					continue // orphan
				}
				kept = append(kept, b)
			}
			if len(kept) == 0 {
				continue // skip entirely empty message
			}
			cleaned = append(cleaned, core.Message{
				Role:    msg.Role,
				Content: core.BlocksContent(kept),
			})
		} else {
			cleaned = append(cleaned, msg)
		}
	}

	// Merge consecutive same-role messages.
	var merged []core.Message
	for _, msg := range cleaned {
		if len(merged) > 0 && merged[len(merged)-1].Role == msg.Role {
			last := &merged[len(merged)-1]
			// Merge text into the last message.
			lastText := messageToText(last)
			newText := messageToText(&msg)
			if lastText != "" || newText != "" {
				combined := lastText
				if combined != "" && newText != "" {
					combined += "\n"
				}
				combined += newText
				last.Content = core.TextContent(combined)
			}
		} else {
			merged = append(merged, msg)
		}
	}

	return merged
}

// messageToText extracts text content from a message.
func messageToText(msg *core.Message) string {
	if !msg.Content.IsBlocks() {
		return msg.Content.Text
	}
	var parts []string
	for _, b := range msg.Content.Blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "\n" + p
	}
	return result
}
