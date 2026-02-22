package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/yifanes/miniclawd/internal/agent"
	"github.com/yifanes/miniclawd/internal/core"
	"github.com/yifanes/miniclawd/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramAdapter implements ChannelAdapter for Telegram.
type TelegramAdapter struct {
	bot           *tgbotapi.BotAPI
	botUsername   string
	allowedGroups []int64
}

func NewTelegramAdapter(token, botUsername string, allowedGroups []int64) (*TelegramAdapter, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	username := botUsername
	if username == "" {
		username = bot.Self.UserName
	}

	return &TelegramAdapter{
		bot:           bot,
		botUsername:   username,
		allowedGroups: allowedGroups,
	}, nil
}

func (a *TelegramAdapter) Name() string { return "telegram" }

func (a *TelegramAdapter) ChatTypeRoutes() map[string]ConversationKind {
	return map[string]ConversationKind{
		"telegram_private":    Private,
		"telegram_group":      Group,
		"telegram_supergroup": Group,
		"telegram_channel":    Group,
	}
}

func (a *TelegramAdapter) IsLocalOnly() bool     { return false }
func (a *TelegramAdapter) AllowsCrossChat() bool  { return true }

func (a *TelegramAdapter) SendText(ctx context.Context, externalChatID, text string) error {
	var chatID int64
	fmt.Sscanf(externalChatID, "%d", &chatID)

	chunks := core.SplitText(text, 4096)
	for _, chunk := range chunks {
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = "MarkdownV2"
		if _, err := a.bot.Send(msg); err != nil {
			// Fallback to plain text.
			msg.ParseMode = ""
			msg.Text = chunk
			if _, err := a.bot.Send(msg); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *TelegramAdapter) SendAttachment(ctx context.Context, externalChatID, filePath string, caption *string) (string, error) {
	var chatID int64
	fmt.Sscanf(externalChatID, "%d", &chatID)

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	if caption != nil {
		doc.Caption = *caption
	}
	_, err := a.bot.Send(doc)
	if err != nil {
		return "", err
	}
	return filePath, nil
}

// StartTelegramBot runs the Telegram long-poll loop.
func StartTelegramBot(ctx context.Context, adapter *TelegramAdapter, db *storage.Database, deps *agent.AgentDeps) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := adapter.bot.GetUpdatesChan(u)

	log.Printf("[telegram] bot @%s started", adapter.botUsername)

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			go handleTelegramMessage(ctx, adapter, db, deps, update.Message)
		}
	}
}

func handleTelegramMessage(ctx context.Context, adapter *TelegramAdapter, db *storage.Database, deps *agent.AgentDeps, msg *tgbotapi.Message) {
	// Determine chat type.
	chatType := "telegram_private"
	switch msg.Chat.Type {
	case "group":
		chatType = "telegram_group"
	case "supergroup":
		chatType = "telegram_supergroup"
	case "channel":
		chatType = "telegram_channel"
	}

	externalID := fmt.Sprintf("%d", msg.Chat.ID)
	title := msg.Chat.Title
	if title == "" && msg.Chat.FirstName != "" {
		title = msg.Chat.FirstName
		if msg.Chat.LastName != "" {
			title += " " + msg.Chat.LastName
		}
	}

	// Resolve chat ID.
	chatID, err := db.ResolveOrCreateChatID("telegram", externalID, &title, chatType)
	if err != nil {
		log.Printf("[telegram] resolve chat error: %v", err)
		return
	}

	// Handle slash commands.
	if msg.IsCommand() {
		switch msg.Command() {
		case "reset":
			db.ClearChatContext(chatID)
			reply := tgbotapi.NewMessage(msg.Chat.ID, "Context cleared.")
			adapter.bot.Send(reply)
			return
		case "usage":
			summary, _ := db.GetLLMUsageSummary(chatID)
			text := fmt.Sprintf("Requests: %d\nInput: %d tokens\nOutput: %d tokens\nTotal: %d tokens",
				summary.Requests, summary.InputTokens, summary.OutputTokens, summary.TotalTokens)
			reply := tgbotapi.NewMessage(msg.Chat.ID, text)
			adapter.bot.Send(reply)
			return
		case "skills":
			reply := tgbotapi.NewMessage(msg.Chat.ID, "Skills: "+deps.Skills)
			adapter.bot.Send(reply)
			return
		case "archive":
			messages, _, _, _ := agent.LoadSession(db, chatID)
			if messages != nil && len(messages) > 0 {
				agent.ArchiveConversation(deps.Config.DataDir, "telegram", chatID, messages)
				db.ClearChatContext(chatID)
				reply := tgbotapi.NewMessage(msg.Chat.ID, "Conversation archived and context cleared.")
				adapter.bot.Send(reply)
			} else {
				reply := tgbotapi.NewMessage(msg.Chat.ID, "No active session to archive.")
				adapter.bot.Send(reply)
			}
			return
		}
	}

	// Extract content.
	content := msg.Text
	if content == "" && msg.Caption != "" {
		content = msg.Caption
	}
	hasPhoto := msg.Photo != nil && len(msg.Photo) > 0
	if content == "" && !hasPhoto {
		return
	}
	if content == "" && hasPhoto {
		content = "请描述这张图片"
	}

	senderName := msg.From.FirstName
	if msg.From.LastName != "" {
		senderName += " " + msg.From.LastName
	}

	// Store message.
	log.Printf("[telegram] chat %d (%s) from %s: %s", chatID, chatType, senderName, truncate(content, 200))
	db.StoreMessage(storage.StoredMessage{
		ID:         fmt.Sprintf("tg_%d", msg.MessageID),
		ChatID:     chatID,
		SenderName: senderName,
		Content:    content,
		IsFromBot:  false,
		Timestamp:  time.Unix(int64(msg.Date), 0).UTC().Format(time.RFC3339),
	})

	// Check allowed groups.
	if len(adapter.allowedGroups) > 0 && chatType != "telegram_private" {
		allowed := false
		for _, gid := range adapter.allowedGroups {
			if gid == msg.Chat.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}

	// Determine if we should respond.
	shouldRespond := chatType == "telegram_private"
	if !shouldRespond {
		// Check for @mention in groups.
		mention := "@" + adapter.botUsername
		if strings.Contains(content, mention) {
			shouldRespond = true
			content = strings.ReplaceAll(content, mention, "")
			content = strings.TrimSpace(content)
		}
	}

	if !shouldRespond {
		return
	}

	// Start typing indicator.
	typingCtx, cancelTyping := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				action := tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping)
				adapter.bot.Send(action)
			}
		}
	}()
	// Send initial typing.
	action := tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping)
	adapter.bot.Send(action)

	// Check for photo.
	var imageData *agent.ImageData
	if msg.Photo != nil && len(msg.Photo) > 0 {
		// Get the largest photo.
		photo := msg.Photo[len(msg.Photo)-1]
		imageData = downloadTelegramPhoto(adapter.bot, photo.FileID)
	}

	// Process with agent.
	reqCtx := agent.AgentRequestContext{
		CallerChannel: "telegram",
		ChatID:        chatID,
		ChatType:      chatType,
	}

	response, err := agent.ProcessWithAgent(ctx, deps, reqCtx, nil, imageData)
	cancelTyping()

	if err != nil {
		log.Printf("[telegram] agent error for chat %d: %v", chatID, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Sorry, I encountered an error processing your message.")
		adapter.bot.Send(reply)
		return
	}

	log.Printf("[telegram] chat %d: response (%d chars): %s", chatID, len(response), truncate(response, 200))

	// Send response.
	chunks := core.SplitText(response, 4096)
	for _, chunk := range chunks {
		reply := tgbotapi.NewMessage(msg.Chat.ID, escapeMarkdownV2(chunk))
		reply.ParseMode = "MarkdownV2"
		if _, err := adapter.bot.Send(reply); err != nil {
			// Fallback to plain text.
			reply.ParseMode = ""
			reply.Text = chunk
			adapter.bot.Send(reply)
		}
	}

	// Store bot response.
	db.StoreMessage(storage.StoredMessage{
		ID:         fmt.Sprintf("tg_bot_%d", time.Now().UnixNano()),
		ChatID:     chatID,
		SenderName: adapter.botUsername,
		Content:    response,
		IsFromBot:  true,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	})
}

func downloadTelegramPhoto(bot *tgbotapi.BotAPI, fileID string) *agent.ImageData {
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil
	}
	url := file.Link(bot.Token)
	resp, err := http.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	mediaType := "image/jpeg"
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		mediaType = ct
	}

	return &agent.ImageData{
		MediaType: mediaType,
		Base64:    base64.StdEncoding.EncodeToString(data),
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// escapeMarkdownV2 converts text to Telegram MarkdownV2 format.
// Fenced code blocks (```) and inline code spans (`) are preserved with their
// content correctly escaped per Telegram spec (only \ and ` inside code).
// All other MarkdownV2 special characters are escaped in regular text regions.
func escapeMarkdownV2(text string) string {
	var buf strings.Builder
	i := 0
	n := len(text)
	for i < n {
		// Fenced code block: ```[lang]\n...\n```
		if i+2 < n && text[i] == '`' && text[i+1] == '`' && text[i+2] == '`' {
			end := strings.Index(text[i+3:], "```")
			if end >= 0 {
				// Per Telegram spec, inside pre/code only \ and ` must be escaped.
				content := text[i+3 : i+3+end]
				content = strings.ReplaceAll(content, `\`, `\\`)
				content = strings.ReplaceAll(content, "`", "\\`")
				buf.WriteString("```")
				buf.WriteString(content)
				buf.WriteString("```")
				i += 3 + end + 3
				continue
			}
			// Unclosed block: escape each backtick individually and move on.
			buf.WriteString("\\`\\`\\`")
			i += 3
			continue
		}
		// Inline code span: `...`
		if text[i] == '`' {
			end := strings.IndexByte(text[i+1:], '`')
			if end >= 0 {
				content := text[i+1 : i+1+end]
				content = strings.ReplaceAll(content, `\`, `\\`)
				content = strings.ReplaceAll(content, "`", "\\`")
				buf.WriteByte('`')
				buf.WriteString(content)
				buf.WriteByte('`')
				i += 2 + end
				continue
			}
			buf.WriteString("\\`")
			i++
			continue
		}
		// Regular text: escape all MarkdownV2 special characters.
		// \ must be escaped first to avoid double-escaping subsequent chars.
		c := text[i]
		switch c {
		case '\\', '_', '*', '[', ']', '(', ')', '~', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!':
			buf.WriteByte('\\')
		}
		buf.WriteByte(c)
		i++
	}
	return buf.String()
}
