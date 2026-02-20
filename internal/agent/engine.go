package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/yifanes/miniclawd/internal/config"
	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/llm"
	"github.com/yifanes/miniclawd/internal/storage"
	"github.com/yifanes/miniclawd/internal/tools"
)

// AgentRequestContext provides caller identity for a request.
type AgentRequestContext struct {
	CallerChannel string
	ChatID        int64
	ChatType      string
}

// AgentDeps holds all dependencies needed by the agent engine.
type AgentDeps struct {
	Config    *config.Config
	DB        *storage.Database
	LLM       llm.LLMProvider
	Tools     *tools.ToolRegistry
	Skills    string // skills catalog for system prompt
}

// ProcessWithAgent runs the agentic loop for a user message.
func ProcessWithAgent(ctx context.Context, deps *AgentDeps, reqCtx AgentRequestContext,
	overridePrompt *string, imageData *ImageData) (string, error) {
	return ProcessWithEvents(ctx, deps, reqCtx, overridePrompt, imageData, nil)
}

// ProcessWithEvents runs the agentic loop with event streaming.
func ProcessWithEvents(ctx context.Context, deps *AgentDeps, reqCtx AgentRequestContext,
	overridePrompt *string, imageData *ImageData, eventCh chan<- AgentEvent) (string, error) {

	cfg := deps.Config

	// Check for explicit memory command (/remember: fast path).
	if overridePrompt == nil {
		msgs, err := deps.DB.GetRecentMessages(reqCtx.ChatID, 1)
		if err == nil && len(msgs) > 0 {
			lastMsg := msgs[len(msgs)-1]
			if !lastMsg.IsFromBot && strings.HasPrefix(strings.ToLower(lastMsg.Content), "/remember:") {
				content := strings.TrimSpace(lastMsg.Content[len("/remember:"):])
				if content != "" {
					chatID := reqCtx.ChatID
					deps.DB.InsertMemoryWithMetadata(&chatID, content, "KNOWLEDGE", "user_explicit", 0.95)
					return "Remembered.", nil
				}
			}
		}
	}

	// Load or build message history.
	var messages []core.Message
	sessionMessages, sessionUpdatedAt, found, err := LoadSession(deps.DB, reqCtx.ChatID)
	if err != nil {
		log.Printf("[agent] session load error for chat %d: %v", reqCtx.ChatID, err)
	}

	if found && len(sessionMessages) > 0 {
		messages = sessionMessages
		log.Printf("[agent] chat %d: loaded session (%d msgs, updated_at=%s)", reqCtx.ChatID, len(messages), sessionUpdatedAt)

		if overridePrompt != nil {
			// Scheduler override: append the override prompt directly.
			messages = append(messages, core.Message{
				Role:    "user",
				Content: core.TextContent(*overridePrompt),
			})
			log.Printf("[agent] chat %d: appended override prompt", reqCtx.ChatID)
		} else {
			// Normal flow: append new user messages from DB since session was saved.
			newMsgs, dbErr := deps.DB.GetNewUserMessagesSince(reqCtx.ChatID, sessionUpdatedAt, cfg.MaxHistoryMessages)
			if dbErr != nil {
				log.Printf("[agent] loading new messages since session: %v", dbErr)
			}
			if len(newMsgs) > 0 {
				newConverted := HistoryToMessages(newMsgs, cfg.BotUsername)
				messages = append(messages, newConverted...)
				log.Printf("[agent] chat %d: appended %d new user messages from DB", reqCtx.ChatID, len(newMsgs))
			}
		}
	} else {
		// No session: build from message history.
		messages, err = LoadMessagesFromDB(deps.DB, reqCtx.ChatID, reqCtx.ChatType, cfg.MaxHistoryMessages, cfg.BotUsername)
		if err != nil {
			return "", fmt.Errorf("loading history: %w", err)
		}
		log.Printf("[agent] chat %d: no session, built from DB history (%d msgs)", reqCtx.ChatID, len(messages))
		if overridePrompt != nil {
			messages = append(messages, core.Message{
				Role:    "user",
				Content: core.TextContent(*overridePrompt),
			})
		}
	}

	// Add image if provided.
	if imageData != nil && len(messages) > 0 {
		lastIdx := len(messages) - 1
		if messages[lastIdx].Role == "user" {
			messages[lastIdx].Content = core.BlocksContent([]core.ContentBlock{
				core.ImageBlock(imageData.MediaType, imageData.Base64),
				core.TextBlock(messages[lastIdx].Content.Text),
			})
		}
	}

	// Compact if needed.
	if len(messages) > cfg.MaxSessionMessages {
		ArchiveConversation(cfg.DataDir, reqCtx.CallerChannel, reqCtx.ChatID, messages)
		compacted, err := CompactMessages(ctx, deps.LLM, messages, cfg.CompactKeepRecent, int(cfg.CompactionTimeout))
		if err != nil {
			log.Printf("[agent] compaction error: %v", err)
		} else {
			messages = compacted
		}
	}

	// Build system prompt.
	soulContent := LoadSoulContent(cfg, reqCtx.ChatID)

	// Get query for memory context (last user message text).
	query := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			query = MessageToText(&messages[i])
			break
		}
	}

	memoryContext := BuildDBMemoryContext(deps.DB, reqCtx.ChatID, query, cfg.MemoryTokenBudget)
	systemPrompt := BuildSystemPrompt(cfg.BotUsername, reqCtx.CallerChannel, memoryContext, reqCtx.ChatID, deps.Skills, soulContent)

	// Build auth context for tools.
	auth := &tools.ToolAuthContext{
		CallerChannel:  reqCtx.CallerChannel,
		CallerChatID:   reqCtx.ChatID,
		ControlChatIDs: cfg.ControlChatIDs,
	}

	// Log the user query being sent.
	lastUserText := query
	if len(lastUserText) > 200 {
		lastUserText = lastUserText[:200] + "..."
	}
	log.Printf("[agent] chat %d: processing, %d messages, user query: %q", reqCtx.ChatID, len(messages), lastUserText)

	// Agentic loop.
	toolDefs := deps.Tools.Definitions()
	emptyVisibleRetried := false

	for iteration := 0; iteration < cfg.MaxToolIterations; iteration++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		if eventCh != nil {
			eventCh <- IterationEvent(iteration)
		}

		log.Printf("[agent] chat %d: iteration %d, sending %d messages to LLM (%s/%s)",
			reqCtx.ChatID, iteration, len(messages), deps.LLM.ProviderName(), deps.LLM.ModelName())

		// Call LLM.
		var resp *core.MessagesResponse
		if eventCh != nil {
			resp, err = deps.LLM.SendMessageStream(ctx, systemPrompt, messages, toolDefs, func(delta string) {
				eventCh <- TextDeltaEvent(delta)
			})
		} else {
			resp, err = deps.LLM.SendMessage(ctx, systemPrompt, messages, toolDefs)
		}
		if err != nil {
			log.Printf("[agent] chat %d: LLM error at iteration %d: %v", reqCtx.ChatID, iteration, err)
			return "", fmt.Errorf("LLM call (iteration %d): %w", iteration, err)
		}

		// Log usage.
		if resp.Usage != nil {
			log.Printf("[agent] chat %d: usage in=%d out=%d, stop_reason=%s",
				reqCtx.ChatID, resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.StopReason)
			deps.DB.LogLLMUsage(reqCtx.ChatID, reqCtx.CallerChannel, deps.LLM.ProviderName(),
				deps.LLM.ModelName(), int(resp.Usage.InputTokens), int(resp.Usage.OutputTokens), "agent_loop")
		}

		switch resp.StopReason {
		case "end_turn", "stop":
			text := extractText(resp)
			text = StripThinking(text)

			preview := text
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			log.Printf("[agent] chat %d: end_turn, response (%d chars): %q", reqCtx.ChatID, len(text), preview)

			// Handle empty visible reply.
			if strings.TrimSpace(text) == "" && !emptyVisibleRetried {
				emptyVisibleRetried = true
				messages = append(messages, core.Message{
					Role:    "assistant",
					Content: responseToContent(resp),
				})
				messages = append(messages, core.Message{
					Role:    "user",
					Content: core.TextContent("Please provide a visible text answer to the user's request."),
				})
				continue
			}

			// Save session.
			messages = append(messages, core.Message{
				Role:    "assistant",
				Content: responseToContent(resp),
			})
			SaveSession(deps.DB, reqCtx.ChatID, messages)

			if eventCh != nil {
				eventCh <- FinalResponseEvent(text)
			}
			return text, nil

		case "tool_use":
			// Build assistant message with tool_use blocks.
			var assistantBlocks []core.ContentBlock
			var toolUses []core.ResponseContentBlock

			for _, block := range resp.Content {
				switch block.Type {
				case "text":
					if block.Text != "" {
						assistantBlocks = append(assistantBlocks, core.TextBlock(block.Text))
					}
				case "tool_use":
					raw := json.RawMessage(block.Input)
					assistantBlocks = append(assistantBlocks, core.ToolUseBlock(block.ID, block.Name, raw))
					toolUses = append(toolUses, block)
				}
			}

			var toolNames []string
			for _, tu := range toolUses {
				toolNames = append(toolNames, tu.Name)
			}
			log.Printf("[agent] chat %d: tool_use, calling %d tools: %v", reqCtx.ChatID, len(toolUses), toolNames)

			messages = append(messages, core.Message{
				Role:    "assistant",
				Content: core.BlocksContent(assistantBlocks),
			})

			// Execute each tool.
			var resultBlocks []core.ContentBlock
			for _, tu := range toolUses {
				if eventCh != nil {
					eventCh <- ToolStartEvent(tu.Name)
				}

				inputPreview := string(tu.Input)
				if len(inputPreview) > 300 {
					inputPreview = inputPreview[:300] + "..."
				}
				log.Printf("[agent] chat %d: tool %s input: %s", reqCtx.ChatID, tu.Name, inputPreview)

				result := deps.Tools.ExecuteWithAuth(ctx, tu.Name, tu.Input, auth)

				resultPreview := result.Content
				if len(resultPreview) > 300 {
					resultPreview = resultPreview[:300] + "..."
				}
				log.Printf("[agent] chat %d: tool %s result (err=%v, %dms): %s",
					reqCtx.ChatID, tu.Name, result.IsError, derefDuration(result.DurationMs), resultPreview)

				if eventCh != nil {
					eventCh <- ToolResultEvent(tu.Name, result.IsError, resultPreview,
						derefDuration(result.DurationMs), result.StatusCode, result.Bytes, result.ErrorType)
				}

				resultBlocks = append(resultBlocks, core.ToolResultBlock(tu.ID, result.Content, result.IsError))
			}

			messages = append(messages, core.Message{
				Role:    "user",
				Content: core.BlocksContent(resultBlocks),
			})

		case "max_tokens":
			text := extractText(resp)
			text = StripThinking(text)
			if text == "" {
				text = "(Response truncated due to max_tokens limit)"
			}
			messages = append(messages, core.Message{
				Role:    "assistant",
				Content: responseToContent(resp),
			})
			SaveSession(deps.DB, reqCtx.ChatID, messages)

			if eventCh != nil {
				eventCh <- FinalResponseEvent(text)
			}
			return text, nil

		default:
			// Unknown stop reason: treat as end_turn.
			text := extractText(resp)
			text = StripThinking(text)
			messages = append(messages, core.Message{
				Role:    "assistant",
				Content: responseToContent(resp),
			})
			SaveSession(deps.DB, reqCtx.ChatID, messages)

			if eventCh != nil {
				eventCh <- FinalResponseEvent(text)
			}
			return text, nil
		}
	}

	// Max iterations reached.
	SaveSession(deps.DB, reqCtx.ChatID, messages)
	return fmt.Sprintf("Reached maximum tool iterations (%d). The task may be partially complete.", cfg.MaxToolIterations), nil
}

// ImageData holds image information for the agent.
type ImageData struct {
	MediaType string
	Base64    string
}

func extractText(resp *core.MessagesResponse) string {
	var parts []string
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func responseToContent(resp *core.MessagesResponse) core.MessageContent {
	if len(resp.Content) == 1 && resp.Content[0].Type == "text" {
		return core.TextContent(resp.Content[0].Text)
	}
	var blocks []core.ContentBlock
	for _, b := range resp.Content {
		switch b.Type {
		case "text":
			blocks = append(blocks, core.TextBlock(b.Text))
		case "tool_use":
			raw := json.RawMessage(b.Input)
			blocks = append(blocks, core.ToolUseBlock(b.ID, b.Name, raw))
		}
	}
	return core.BlocksContent(blocks)
}

func derefDuration(d *int64) int64 {
	if d != nil {
		return *d
	}
	return 0
}
